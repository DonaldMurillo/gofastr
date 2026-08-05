package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTimeoutStreamingOutlivesDeadline pins the contract documented in
// framework/docs/content/reactivity.md ("streams outlive the request
// timeout", issue #159): once a handler flushes — flipping the timeout
// writer into streaming mode — the deadline must no longer terminate the
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

// TestTimeoutHungHandlerStill504s is the control for the streaming
// exemption: a handler that never flushes and simply hangs must still get
// its 504 at the deadline — the middleware's slowloris/hung-handler
// protection — and the client must receive it promptly, not after the
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
