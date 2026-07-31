package middleware

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
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
	finished    bool   // handler completed and its response was committed
	onStream    func() // fires once, on the flip into streaming mode
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
		if tw.onStream != nil {
			tw.onStream()
		}
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
	if !tw.streaming {
		tw.streaming = true
		if tw.onStream != nil {
			tw.onStream()
		}
	}
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
// the handler completed normally and the timeout has not fired. It marks
// the response finished so a deadline timer that fires afterwards cannot
// append a 504 to what was already sent — the timer is only stopped once
// the parent's select returns, so that window is real.
func (tw *timeoutWriter) finish() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.streaming || tw.finished {
		return
	}
	tw.commitBufferedLocked()
	tw.finished = true
}

// expire flags the timeout. Returns false when the response is already
// streaming or already committed by a completed handler — in either case
// the caller MUST NOT attempt to write a 504, because bytes have reached
// the underlying writer and appending to them corrupts the response
// (a committed 200 body followed by "Gateway Timeout").
func (tw *timeoutWriter) expire() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.streaming || tw.timedOut || tw.finished {
		return false
	}
	tw.timedOut = true
	return true
}

// Timeout returns middleware that enforces a deadline on request processing.
// If the downstream handler does not complete within the given duration,
// a 504 Gateway Timeout response is returned.
//
// The deadline applies to buffered responses only. The moment a handler
// flips into streaming mode (first Flush or Hijack) it has declared
// itself long-lived: the pending 504 is abandoned, the request context
// is left alive for the life of the stream, and the connection's server
// read/write deadlines are cleared so http.Server's fixed timeouts
// cannot sever an active stream. SSE subscribers and hijacked upgrades
// therefore outlive both the middleware deadline and the server's
// write timeout; everything else keeps the hard cutoff.
//
// The handler runs in a goroutine; a buffered response writer prevents
// concurrent writes to the underlying http.Header map between the handler
// goroutine and the timeout path.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Cancellation is tied to the deadline actually expiring on
			// a buffered response — not to a fixed context deadline —
			// so a stream that starts before the timer fires keeps its
			// context for as long as the connection lives. A handler cut
			// off by the deadline sees ctx.Err() == Canceled with
			// context.Cause(ctx) == DeadlineExceeded, which keeps the
			// timeout distinguishable from a client disconnect.
			ctx, cancel := context.WithCancelCause(r.Context())
			defer cancel(context.Canceled)

			tw := newTimeoutWriter(w)
			tw.onStream = func() {
				// Streaming responses own their connection lifetime.
				// Clear the server-level deadlines (ResponseController
				// walks Unwrap through outer wrappers); ignore errors —
				// test recorders don't support deadlines.
				rc := http.NewResponseController(w)
				_ = rc.SetReadDeadline(time.Time{})
				_ = rc.SetWriteDeadline(time.Time{})
			}

			// The timer only SIGNALS. Every write to w happens on this
			// goroutine, which is what keeps the 504 from racing the
			// server's finishRequest: if the timer goroutine wrote the
			// 504 itself, a handler completing at the same moment would
			// let ServeHTTP return while that write was still in flight.
			expired := make(chan struct{})
			timer := time.AfterFunc(d, func() { close(expired) })
			defer timer.Stop()

			done := make(chan struct{})
			// Recover panics in the child goroutine and re-raise them
			// in the parent so outer Recovery middleware sees them.
			// Without this, a handler panic crashes the whole process
			// because the parent's defer recover lives in a different
			// goroutine.
			var childPanic any
			go func() {
				defer func() {
					if v := recover(); v != nil {
						childPanic = v
					}
					close(done)
				}()
				next.ServeHTTP(tw, r.WithContext(ctx))
			}()

			select {
			case <-done:
				if childPanic != nil {
					panic(childPanic)
				}
				tw.finish()
			case <-expired:
				if !tw.expire() {
					// The response is streaming (it owns its own
					// lifetime — see the doc comment) or the handler
					// already committed it in the same instant. Either
					// way the deadline does not apply: wait for the
					// handler exactly as the <-done branch does, and
					// never cancel a live stream's context.
					<-done
					if childPanic != nil {
						panic(childPanic)
					}
					tw.finish()
					return
				}
				cancel(context.DeadlineExceeded)
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				// Parent took the timeout branch; the handler goroutine
				// is still running and may yet panic. Watch for that
				// late panic and surface it through slog.Default so it
				// doesn't vanish — otherwise debugging "why does this
				// endpoint sometimes 504 with no further trace" is
				// impossible.
				go func() {
					<-done
					if childPanic != nil {
						slog.Error("panic in timed-out handler",
							"error", truncate(fmt.Sprint(childPanic), maxRecoveryPanicLen),
							"path", truncate(r.URL.Path, maxRecoveryPathLen),
							"method", r.Method,
						)
					}
				}()
			}
		})
	}
}
