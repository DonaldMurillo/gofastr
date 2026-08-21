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
// starts, the timeout path can no longer write the 504: the underlying
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
// + body and flips into streaming mode, required for SSE handlers whose
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

// timeoutCtx is the request context handed to the wrapped handler. It
// replaces context.WithTimeout because a stdlib deadline is irrevocable:
// once it fires, Err() is frozen at DeadlineExceeded forever, so a client
// disconnect AFTER the deadline becomes invisible to a still-streaming
// handler that discriminates DeadlineExceeded (ignore, issue #159) from
// Canceled (unwind). This context keeps the same observable behavior for
// non-streaming requests: Done fires at the deadline with
// Err() == DeadlineExceeded, and earlier parent cancellation propagates.
// But the deadline is DISARMED when the response has started streaming,
// leaving the context live so a later disconnect still delivers Canceled.
//
// Deadline() deliberately reports no deadline: for a streaming response
// the deadline may never fire, and advertising one that then doesn't fire
// makes stdlib helpers misbehave (context.WithTimeout derived from a
// parent whose advertised deadline is earlier trusts the parent to fire
// and drops its own timer, a lost timeout). Callers needing a budget
// still observe Done()/Err() exactly as with context.WithTimeout.
type timeoutCtx struct {
	parent context.Context // original *http.Request context (values, cause)

	mu   sync.Mutex
	done chan struct{}
	err  error
}

func newTimeoutCtx(parent context.Context) *timeoutCtx {
	return &timeoutCtx{parent: parent, done: make(chan struct{})}
}

func (c *timeoutCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *timeoutCtx) Done() <-chan struct{}       { return c.done }
func (c *timeoutCtx) Value(key any) any           { return c.parent.Value(key) }

func (c *timeoutCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// cancel completes the context with err. First caller wins; later calls
// are no-ops, so the deadline, parent-cancellation propagation, and
// end-of-request cleanup can race safely.
func (c *timeoutCtx) cancel(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
}

// Timeout returns middleware that enforces a deadline on request processing.
// If the downstream handler does not complete within the given duration,
// a 504 Gateway Timeout response is returned.
//
// The handler runs in a goroutine; a buffered response writer prevents
// concurrent writes to the underlying http.Header map between the handler
// goroutine and the timeout path.
//
// Streaming contract: once the handler flushes or hijacks, flipping the
// wrapped writer into streaming mode, the deadline stops applying to the
// response. The middleware waits for the handler to return instead of
// handing the finalized response back to net/http underneath it (which
// would make the handler's next write panic, the bug that killed every
// SSE stream older than the timeout), and the request context stays LIVE
// past the deadline so a later client disconnect is still delivered as
// context.Canceled. A streaming handler therefore owns its lifetime: it
// unwinds on disconnect, on its own bound (like the SSE bus, issue #159),
// or on a failed write, never because the request deadline lapsed.
// Handlers that never start streaming keep the hard guarantee: a hung
// handler gets its 504 at the deadline, with the context completed as
// DeadlineExceeded.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := newTimeoutCtx(r.Context())
			// End-of-request cleanup: release the propagation callback
			// and anyone still selecting on ctx.Done(). A context that
			// already completed (deadline or disconnect) ignores this.
			defer ctx.cancel(context.Canceled)
			// Client disconnect (or any parent cancellation) reaches the
			// handler even after the deadline has been disarmed by a
			// streaming response. AfterFunc costs no goroutine until the
			// parent actually completes; stop() releases it when the
			// request ends first.
			stop := context.AfterFunc(r.Context(), func() {
				ctx.cancel(r.Context().Err())
			})
			defer stop()
			timer := time.NewTimer(d)
			defer timer.Stop()

			tw := newTimeoutWriter(w)
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

			// abandon handles "the request is over but the handler is
			// not": the deadline fired, or the client disconnected while
			// the handler was still running. cause completes the
			// handler's context when it hasn't completed already.
			abandon := func(cause error) {
				if !tw.expire() {
					// The response is already streaming (or hijacked): the
					// handler owns the connection lifetime and the deadline
					// no longer applies. Returning here would hand the
					// response back to net/http, which finalizes it and
					// nils its internal buffered writer; the still-
					// streaming handler's next write (e.g. the SSE
					// heartbeat) would then panic inside net/http. Block
					// until the handler returns instead. The handler's ctx
					// is NOT cancelled here; it stays live so a later
					// client disconnect still reaches the handler as
					// Canceled (via the AfterFunc above); the stream's
					// lifetime belongs to the handler and its own bound
					// (#159).
					<-done
					if childPanic != nil {
						panic(childPanic)
					}
					return
				}
				ctx.cancel(cause)
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				// Parent abandoned the handler; the handler goroutine
				// is still running and may yet panic. Watch for that
				// late panic and surface it through slog.Default so it
				// doesn't vanish. Otherwise debugging "why does this
				// endpoint sometimes 504 with no further trace" is
				// impossible.
				go func() {
					<-done
					if childPanic != nil {
						slog.Error("panic in timed-out handler",
							"error", truncate(scrubControlBytes(fmt.Sprint(childPanic)), maxRecoveryPanicLen),
							"path", truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
							"method", truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
						)
					}
				}()
			}

			select {
			case <-done:
				if childPanic != nil {
					panic(childPanic)
				}
				tw.finish()
			case <-r.Context().Done():
				// Client gone before the handler finished (the AfterFunc
				// has already delivered the parent's error to ctx). Same
				// exit as the deadline: don't hold the connection open
				// for a peer that left. The 504 goes to a dead socket:
				// written for parity, observed by no one.
				abandon(r.Context().Err())
			case <-timer.C:
				abandon(context.DeadlineExceeded)
			}
		})
	}
}
