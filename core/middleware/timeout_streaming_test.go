package middleware

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTimeoutStreamingOutlivesDeadline pins the contract documented in
// framework/docs/content/reactivity.md ("streams outlive the request
// timeout", issue #159): once a handler flushes, flipping the timeout
// writer into streaming mode, the deadline must no longer terminate the
// response. Before the fix, the middleware's parent goroutine returned to
// net/http at the deadline, which finalized the response underneath the
// still-streaming handler; the handler's next write (the SSE heartbeat)
// then panicked with a nil-pointer deref inside net/http ("panic in
// timed-out handler ... path=/__gofastr/sse" at exact heartbeat cadence).
//
// Needs a real server: httptest.ResponseRecorder never finalizes, so the
// bug is invisible against a recorder.
func TestTimeoutStreamingOutlivesDeadline(t *testing.T) {
	type outcome struct {
		panicked any
		writeErr error
	}
	outcomeCh := make(chan outcome, 1)

	h := Timeout(40 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out outcome
		defer func() {
			out.panicked = recover()
			outcomeCh <- out
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		w.(http.Flusher).Flush() // streaming starts, well before the deadline

		time.Sleep(150 * time.Millisecond) // deadline fires in here

		// The post-deadline heartbeat write. Before the fix this panicked
		// inside net/http (response already finalized by the parent).
		_, out.writeErr = w.Write([]byte("data: second\n\n"))
		w.(http.Flusher).Flush()
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)

	out := <-outcomeCh
	if out.panicked != nil {
		t.Fatalf("post-deadline write panicked: %v", out.panicked)
	}
	if out.writeErr != nil {
		t.Fatalf("post-deadline write failed: %v", out.writeErr)
	}
	if readErr != nil {
		t.Fatalf("reading stream: %v", readErr)
	}
	if !strings.Contains(string(body), "data: second") {
		t.Fatalf("client never received the post-deadline frame; body=%q", body)
	}
}

// TestTimeoutStreamDisconnectAfterDeadline pins the third leg of the
// streaming contract: a client disconnect must promptly unwind a streaming
// handler even when it happens AFTER the request deadline has passed. The
// handler mimics the SSE stream loop (core-ui/island/stream.go): a one-shot
// watcher on r.Context() that ignores DeadlineExceeded (issue #159, the
// timeout firing on a live client) and unwinds only on context.Canceled (a
// real disconnect). With context.WithTimeout, the deadline permanently
// freezes ctx.Err() at DeadlineExceeded, so a later disconnect is invisible
// and the stream lives to its own bound instead of unwinding.
func TestTimeoutStreamDisconnectAfterDeadline(t *testing.T) {
	unwound := make(chan string, 1)
	h := Timeout(40 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		w.(http.Flusher).Flush()

		// stream.go's disconnect discrimination, verbatim in shape.
		reqCtx := r.Context()
		streamCtx, streamCancel := context.WithCancel(context.Background())
		defer streamCancel()
		go func() {
			<-reqCtx.Done()
			if reqCtx.Err() == context.Canceled {
				streamCancel()
			}
		}()

		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-streamCtx.Done():
				unwound <- "canceled"
				return
			case <-tick.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					// A cancel racing the same tick still counts as clean.
					select {
					case <-streamCtx.Done():
						unwound <- "canceled"
					default:
						unwound <- "write-error"
					}
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: t\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// Read the response head + first frame so the stream is known-started.
	buf := make([]byte, 512)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read response head: %v", err)
	}

	time.Sleep(120 * time.Millisecond) // deadline (40ms) fires in here; stream survives
	_ = conn.Close()                   // the disconnect happens AFTER the deadline

	select {
	case reason := <-unwound:
		if reason != "canceled" {
			t.Fatalf("stream unwound via %q, want context.Canceled from the disconnect", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler never unwound after post-deadline client disconnect")
	}
}

// TestTimeoutHungHandlerStill504s is the control for the streaming
// exemption: a handler that never flushes and simply hangs must still get
// its 504 at the deadline, the middleware's slowloris/hung-handler
// protection, and the client must receive it promptly, not after the
// handler finally returns.
func TestTimeoutHungHandlerStill504s(t *testing.T) {
	release := make(chan struct{})
	h := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hang far past the deadline, no Flush
		_, _ = w.Write([]byte("too late"))
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer close(release)

	start := time.Now()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d (body=%q)", resp.StatusCode, body)
	}
	// The 504 must arrive at the deadline, not when the handler unblocks.
	if elapsed > 2*time.Second {
		t.Fatalf("504 took %v; timeout path is blocking on the handler", elapsed)
	}
	if strings.Contains(string(body), "too late") {
		t.Fatalf("handler output leaked past the 504: %q", body)
	}
}

// TestTimeoutFailedHijackKeepsBuffering: Hijack set streaming before the
// attempt and never undid it when the hijack FAILED (HTTP/2, a writer
// that cannot hijack). The handler then answered through the writer,
// but in streaming mode its headers never reached the client, and the
// deadline was shed, so a hung handler never got its 504. A failed
// hijack now leaves the writer buffered with the deadline armed.
func TestTimeoutFailedHijackKeepsBuffering(t *testing.T) {
	t.Run("headers", func(t *testing.T) {
		rec := noHijackRecorder{httptest.NewRecorder()}
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, _, err := http.NewResponseController(w).Hijack(); err == nil {
				t.Fatal("premise: the recorder cannot hijack")
			}
			w.Header().Set("X-Fallback", "1")
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "upgrade unavailable")
		})
		Timeout(time.Minute)(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
		if got := rec.Header().Get("X-Fallback"); got != "1" || rec.Body.String() != "upgrade unavailable" {
			t.Fatalf("after a failed hijack: X-Fallback=%q body=%q, want the handler's answer", got, rec.Body.String())
		}
	})
	t.Run("deadline", func(t *testing.T) {
		rec := noHijackRecorder{httptest.NewRecorder()}
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _, _ = http.NewResponseController(w).Hijack() // fails
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		})
		start := time.Now()
		Timeout(50*time.Millisecond)(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
		if rec.Code != http.StatusGatewayTimeout || time.Since(start) > time.Second {
			t.Fatalf("status %d after %v, want 504 at the 50ms deadline", rec.Code, time.Since(start))
		}
	})
}

// TestTimeoutExpireWaitsOutHijack: the hijack attempt runs outside the
// writer's lock, so the deadline can land mid-attempt. expire must not
// decide until the answer is in: writing a 504 under a hijack that
// succeeds races the handler for the connection, and shedding the
// deadline for one that fails is the bug above.
func TestTimeoutExpireWaitsOutHijack(t *testing.T) {
	for _, hijacked := range []bool{true, false} {
		tw := newTimeoutWriter(httptest.NewRecorder())
		tw.mu.Lock()
		tw.hijacking = true
		tw.mu.Unlock()
		got := make(chan bool, 1)
		go func() { got <- tw.expire() }()
		select {
		case v := <-got:
			t.Fatalf("expire decided (%v) while the hijack was still in progress", v)
		case <-time.After(50 * time.Millisecond):
		}
		tw.mu.Lock()
		tw.hijacking = false
		tw.streaming = hijacked
		tw.hijackDone.Broadcast()
		tw.mu.Unlock()
		if v := <-got; v != !hijacked {
			t.Fatalf("hijack succeeded=%v: expire = %v, want %v", hijacked, v, !hijacked)
		}
	}
}
