package middleware

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// timeoutWriter wraps an http.ResponseWriter so that the timeout path and
// the handler goroutine cannot race on the underlying response.
//
// Buffered mode (default): handler writes land in a private http.Header
// + body buffer; nothing reaches the underlying ResponseWriter until the
// handler returns successfully. On timeout, the buffer is abandoned and
// a 504 is written directly to the underlying writer. The handler never
// shares its header map with the timeout goroutine, eliminating the
// `concurrent map writes` panic that triggered on the pagination island
// handler under the chaos rapid-click test.
//
// Streaming mode: first call to Flush() or Hijack() commits the buffered
// headers + body to the underlying writer and flips into passthrough so
// SSE handlers and WebSocket upgrades keep working. After streaming
// starts, the timeout path can no longer write the 504 — the underlying
// connection is owned by the handler.
type timeoutWriter struct {
	w http.ResponseWriter

	mu          sync.Mutex
	h           http.Header   // buffered handler headers (separate map)
	body        *bytes.Buffer // buffered handler body
	wroteHeader bool
	code        int
	streaming   bool // committed to passthrough (Flush/Hijack used)
	timedOut    bool
}

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{
		w:    w,
		h:    make(http.Header),
		body: &bytes.Buffer{},
		code: http.StatusOK,
	}
}

func (tw *timeoutWriter) Header() http.Header { return tw.h }

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.wroteHeader {
		return
	}
	tw.wroteHeader = true
	tw.code = code
	if tw.streaming {
		tw.copyHeadersToUnderlyingLocked()
		tw.w.WriteHeader(code)
	}
}

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !tw.wroteHeader {
		tw.wroteHeader = true
	}
	if tw.streaming {
		return tw.w.Write(p)
	}
	return tw.body.Write(p)
}

// Flush passes through when supported. First call commits buffered headers
// + body and flips into streaming mode — required for SSE handlers whose
// post-headers `Flush()` call is the signal that subsequent Writes must
// reach the client immediately.
func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	if !tw.streaming {
		tw.commitBufferedLocked()
		tw.streaming = true
	}
	if f, ok := tw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack flips into streaming mode (no buffer to commit because hijack
// implies the handler owns the raw connection from this point on) and
// hands off the underlying writer.
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	tw.mu.Lock()
	if tw.timedOut {
		tw.mu.Unlock()
		return nil, nil, fmt.Errorf("timeout middleware: response already timed out")
	}
	tw.streaming = true
	tw.mu.Unlock()
	if h, ok := tw.w.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("timeout middleware: underlying ResponseWriter does not support hijacking")
}

// copyHeadersToUnderlyingLocked merges the handler's buffered http.Header
// into the underlying ResponseWriter's. Caller holds tw.mu.
func (tw *timeoutWriter) copyHeadersToUnderlyingLocked() {
	dst := tw.w.Header()
	for k, v := range tw.h {
		dst[k] = v
	}
}

// commitBufferedLocked writes the buffered headers, status, and body to
// the underlying ResponseWriter. Caller holds tw.mu.
func (tw *timeoutWriter) commitBufferedLocked() {
	tw.copyHeadersToUnderlyingLocked()
	if tw.wroteHeader {
		tw.w.WriteHeader(tw.code)
	}
	if tw.body.Len() > 0 {
		_, _ = tw.body.WriteTo(tw.w)
	}
}

// finish commits buffered handler output to the underlying writer when
// the handler completed normally and the timeout has not fired.
func (tw *timeoutWriter) finish() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.streaming {
		return
	}
	tw.commitBufferedLocked()
}

// expire flags the timeout. Returns false when the response is already
// streaming and the caller MUST NOT attempt to write a 504 (would race
// with handler writes that have already reached the underlying writer).
func (tw *timeoutWriter) expire() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.streaming || tw.timedOut {
		return false
	}
	tw.timedOut = true
	return true
}

// Timeout returns middleware that enforces a deadline on request processing.
// If the downstream handler does not complete within the given duration,
// a 504 Gateway Timeout response is returned.
//
// The handler runs in a goroutine; a buffered response writer prevents
// concurrent writes to the underlying http.Header map between the handler
// goroutine and the timeout path.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			tw := newTimeoutWriter(w)
			done := make(chan struct{})
			go func() {
				next.ServeHTTP(tw, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				tw.finish()
			case <-ctx.Done():
				if tw.expire() {
					http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				}
			}
		})
	}
}
