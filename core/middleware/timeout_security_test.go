package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Property: when the deadline fires on a buffered (not yet flushed)
// response, the abandoned partial response must not reach the client.
// The handler may have set headers and written body bytes before the
// deadline; the 504 that replaces it must carry ONLY the middleware's
// own error text. A leak here would (a) disclose whatever the handler
// had already computed and (b) let a half-written response corrupt the
// 504 framing. Post-timeout handler writes must fail with
// http.ErrHandlerTimeout rather than racing the 504 onto the wire.
func TestTimeoutAbandonsPartialResponse(t *testing.T) {
	writeErr := make(chan error, 1)
	handlerDone := make(chan struct{})
	h := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.Header().Set("X-Partial-Secret", "handler-state-leak")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PARTIAL-BODY-LEAK"))
		<-r.Context().Done() // hold until the deadline fires
		// Post-timeout write: must be rejected, never buffered or sent.
		_, err := w.Write([]byte("POST-TIMEOUT-BYTE"))
		writeErr <- err
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))
	// ServeHTTP returns as soon as the 504 is written; the abandoned
	// handler is still running. Reading rec now would let a broken
	// late-write guard corrupt it after the assertions had passed.
	awaitHandler(t, handlerDone)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
	body := rec.Body.String()
	if body != "Gateway Timeout\n" {
		t.Fatalf("504 body must be exactly the middleware's error text, got %q", body)
	}
	if got := rec.Header().Get("X-Partial-Secret"); got != "" {
		t.Fatalf("SECURITY: abandoned handler header leaked into the 504: %q", got)
	}
	if strings.Contains(body, "PARTIAL-BODY-LEAK") || strings.Contains(body, "POST-TIMEOUT-BYTE") {
		t.Fatalf("SECURITY: abandoned handler body leaked into the 504: %q", body)
	}
	select {
	case err := <-writeErr:
		if !errors.Is(err, http.ErrHandlerTimeout) {
			t.Fatalf("post-timeout Write err = %v, want http.ErrHandlerTimeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never observed the deadline")
	}
}

// Property: the Flush commit and the timer's expire() are mutually
// exclusive. Whichever wins, the client must never see stream bytes
// FOLLOWED by the 504 text (a split/corrupted response), and a response
// that committed to streaming must never receive the 504 at all.
func TestTimeoutFlushTimerNeverInterleaves(t *testing.T) {
	const frame = "data: frame\n\n"

	// B1: flush commits before the deadline; the deadline is then shed.
	// The stream must run past the budget with no 504 ever written.
	t.Run("flush-wins", func(t *testing.T) {
		h := Timeout(40 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush() // commit BEFORE the deadline
			for range 8 {
				_, _ = w.Write([]byte(frame))
				w.(http.Flusher).Flush()
				time.Sleep(15 * time.Millisecond) // deliberately cross the budget
			}
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sse", nil))
		body := rec.Body.String()
		if strings.Contains(body, "Gateway Timeout") {
			t.Fatalf("SECURITY: 504 written onto a committed stream:\n%s", body)
		}
		if got := strings.Count(body, frame); got != 8 {
			t.Fatalf("stream frames lost after deadline: got %d, want 8", got)
		}
	})

	// B2: timer fires while the handler is still buffered; the handler's
	// late Flush must be a no-op and the response must be exactly the 504.
	t.Run("timer-wins", func(t *testing.T) {
		h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(60 * time.Millisecond) // budget lapses first
			_, _ = w.Write([]byte(frame))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(frame))
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/late", nil))
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("status = %d, want 504", rec.Code)
		}
		if strings.Contains(rec.Body.String(), frame) {
			t.Fatalf("SECURITY: handler bytes reached the wire after the 504:\n%s", rec.Body.String())
		}
	})

	// B3: race the boundary. The delays must STRADDLE the deadline, or
	// this only repeats the flush-wins path under another name: an
	// earlier version jittered 0-5ms against a 15ms budget, so every
	// iteration flushed a clear 10ms before expire() and nothing ever
	// contended. The sweep below brackets 15ms from both sides, and the
	// tally afterwards fails if the run landed entirely on one side --
	// which is what a future timing change would do silently.
	t.Run("boundary-race", func(t *testing.T) {
		// 0 and 30 are the anchors: whatever the machine's load, the
		// first flushes long before the deadline and the last long
		// after, so both outcomes are reachable. The values around 15
		// are where the two actually contend.
		delays := []time.Duration{0, 5, 12, 14, 15, 16, 18, 30}
		var streamedN, gwN int
		for i := range 32 {
			delay := delays[i%len(delays)] * time.Millisecond
			iterDone := make(chan struct{})
			h := Timeout(15 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(iterDone)
				time.Sleep(delay)
				w.(http.Flusher).Flush()
				for range 4 {
					_, _ = w.Write([]byte(frame))
					time.Sleep(5 * time.Millisecond)
				}
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/race", nil))
			awaitHandler(t, iterDone)
			body := rec.Body.String()
			streamed := strings.Contains(body, frame)
			gw := strings.Contains(body, "Gateway Timeout")
			if streamed && gw {
				t.Fatalf("iter %d (delay=%s): 504 interleaved with stream bytes:\n%s", i, delay, body)
			}
			if !streamed && rec.Code != http.StatusGatewayTimeout {
				t.Fatalf("iter %d (delay=%s): neither stream nor 504 (code=%d):\n%s", i, delay, rec.Code, body)
			}
			if streamed {
				streamedN++
			}
			if gw {
				gwN++
			}
		}
		// Anti-vacuity: a sweep that never crossed the deadline would
		// satisfy every assertion above while testing one path.
		if streamedN == 0 || gwN == 0 {
			t.Fatalf("sweep never straddled the deadline: %d streamed, %d timed out over %d iterations",
				streamedN, gwN, 32)
		}
		t.Logf("boundary straddled: %d streamed, %d timed out", streamedN, gwN)
	})
}

// awaitHandler blocks until the abandoned handler goroutine has
// finished, so assertions read a recorder nothing can still write to.
// Bounded, because a handler that never returns is itself the bug.
func awaitHandler(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never returned after the deadline")
	}
}
