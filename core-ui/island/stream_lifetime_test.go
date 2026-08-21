package island

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// readSSE reads from an SSE response body until stop matches the buffered
// output OR the read returns an error (EOF / connection close). It reports
// the buffered text and the terminal error. Used by the stream-lifetime
// tests to observe what the server actually wrote and when the stream ended.
func readSSE(t *testing.T, body io.Reader, stop func(buf string) bool) (string, error) {
	t.Helper()
	var buf strings.Builder
	tmp := make([]byte, 4096)
	for {
		n, err := body.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if stop != nil && stop(buf.String()) {
				return buf.String(), nil
			}
		}
		if err != nil {
			return buf.String(), err
		}
	}
}

// TestSSEStreamOutlivesRequestDeadline is the core regression for issue #159:
// middleware.Timeout's request deadline (a context.WithTimeout on r.Context())
// must NOT cut a live SSE stream mid-flight. A stream that survives its own
// request deadline and still delivers a post-deadline update proves the fix.
//
// The handler simulates exactly what middleware.Timeout does, derive a short
// deadline from r.Context() and run the handler under it, without importing
// the middleware (which would risk a cycle and couple the unit test to the
// middleware's goroutine plumbing).
func TestSSEStreamOutlivesRequestDeadline(t *testing.T) {
	mgr := NewManager(WithSSEStreamBound(1 * time.Second)) // bound past the 300ms push, short enough for prompt cleanup
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
		defer cancel()
		mgr.ServeSSE(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=deadline", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// Wait until WELL past the 100ms request deadline.
	time.Sleep(300 * time.Millisecond)

	// A stream cut at the deadline is gone here; a surviving stream delivers.
	mgr.PushUpdate(IslandUpdate{IslandID: "after-deadline", HTML: "<p>survived</p>"}, "deadline")

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		b, err := readSSE(t, resp.Body, func(s string) bool {
			return strings.Contains(s, "after-deadline")
		})
		got <- result{b, err}
	}()

	select {
	case r := <-got:
		if !strings.Contains(r.body, "after-deadline") {
			t.Fatalf("stream did not deliver the post-deadline update (request deadline cut the stream); body=%q err=%v", r.body, r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream was cut by the request deadline — the post-deadline update never arrived")
	}
}

// TestSSEHeartbeatKeepsIdleStreamAlive proves the stream writes keepalive
// comment frames on an idle connection, so proxies and load balancers don't
// idle-kill a live stream and so a half-closed peer surfaces as a write error.
func TestSSEHeartbeatKeepsIdleStreamAlive(t *testing.T) {
	mgr := NewManager(
		WithSSEHeartbeat(50*time.Millisecond),
		WithSSEStreamBound(30*time.Second),
	)
	server := httptest.NewServer(http.HandlerFunc(mgr.ServeSSE))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=heartbeat", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// ": connected" is the initial comment; a heartbeat writes ": ping". Only
	// the heartbeat produces "ping", so its presence proves periodic writes.
	got := make(chan string, 1)
	go func() {
		b, _ := readSSE(t, resp.Body, func(s string) bool {
			return strings.Contains(s, "ping")
		})
		got <- b
	}()
	select {
	case body := <-got:
		if !strings.Contains(body, "ping") {
			t.Fatalf("no heartbeat comment frame on idle stream; body=%q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a heartbeat comment frame on an idle stream")
	}
}

// TestSSEStreamBoundReclaimsStream proves a stream is closed after the bound
// even when nothing else ends it, the safety net that reclaims a stream
// stranded by a peer the server cannot observe as gone (whose heartbeat writes
// keep succeeding into the kernel buffer). With the bound, EventSource simply
// reconnects on a live stream; a stranded stream is dropped.
func TestSSEStreamBoundReclaimsStream(t *testing.T) {
	mgr := NewManager(
		WithSSEHeartbeat(50*time.Millisecond),
		WithSSEStreamBound(150*time.Millisecond),
	)
	server := httptest.NewServer(http.HandlerFunc(mgr.ServeSSE))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=bound", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// Read until the server closes the body (EOF). The bound closes the
	// stream; without it the loop blocks forever and this times out.
	got := make(chan error, 1)
	go func() {
		_, err := readSSE(t, resp.Body, nil)
		got <- err
	}()
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("stream body ended without an error; expected EOF from the bound closing the stream")
		}
		// EOF (or a transport reset from the closed connection) is the proof
		// the handler returned at the bound.
	case <-time.After(2 * time.Second):
		t.Fatal("stream was not reclaimed within the bound — the loop is blocking forever (stranded stream)")
	}
}

// TestSSEStreamClosesOnClientDisconnect is the regression guard: ignoring the
// request deadline (DeadlineExceeded) must NOT also ignore a real client
// disconnect (context.Canceled). The stream must unwind promptly when the peer
// goes away.
func TestSSEStreamClosesOnClientDisconnect(t *testing.T) {
	mgr := NewManager(
		WithSSEHeartbeat(50*time.Millisecond),
		WithSSEStreamBound(30*time.Second),
	)
	server := httptest.NewServer(http.HandlerFunc(mgr.ServeSSE))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=disconnect", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// Give the stream a moment to be serving, then drop the client.
	time.Sleep(100 * time.Millisecond)
	cancel()

	got := make(chan error, 1)
	go func() {
		_, err := readSSE(t, resp.Body, nil)
		got <- err
	}()
	select {
	case <-got:
		// The body closed (EOF/reset): the handler observed the disconnect.
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after client disconnect — context.Canceled is not being observed")
	}
}

// TestSSEStreamBoundReclaimsNoGoroutineLeak defends the invariant the new
// watcher goroutine introduces: a stream reclaimed by the BOUND (not by a
// client disconnect) must still release its watcher goroutine. The watcher
// blocks on the request context's Done channel, which net/http cancels when
// ServeHTTP returns, so once the bound closes the stream the watcher must
// unwind, not linger for the lifetime of the keep-alive connection.
func TestSSEStreamBoundReclaimsNoGoroutineLeak(t *testing.T) {
	mgr := NewManager(
		WithSSEHeartbeat(20*time.Millisecond),
		WithSSEStreamBound(80*time.Millisecond),
	)
	server := httptest.NewServer(http.HandlerFunc(mgr.ServeSSE))
	defer server.Close()

	// DisableKeepAlives so the client's transport goroutines release as soon as
	// the response body closes, otherwise they linger in the connection pool
	// and mask whether the SERVER-side watcher goroutine unwound.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	// Settle the baseline AFTER the test server and client are up so their
	// goroutines are already counted; only the per-stream watcher should move
	// the number.
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=noleak", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// The bound closes the stream; read to EOF so ServeHTTP returns and the
	// request context is cancelled (which unblocks the watcher).
	if _, err := readSSE(t, resp.Body, nil); err == nil {
		resp.Body.Close()
		t.Fatal("expected the bound to close the stream (read error/EOF)")
	}
	resp.Body.Close()

	// The watcher unblocks shortly after ServeHTTP returns. Poll rather than
	// sleep: a real leak never settles, a clean unwind does so within ms.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("goroutine leak after bound reclaim: baseline=%d now=%d (watcher did not unwind)", baseline, runtime.NumGoroutine())
}
