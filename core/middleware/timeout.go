package middleware

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// timeoutWriter wraps an http.ResponseWriter so that the timeout path and
// the handler goroutine cannot race on the underlying response.
//
// Buffered mode (default): handler writes land in a private http.Header
// + body buffer; nothing reaches the underlying ResponseWriter until the
// handler returns successfully. The private header map starts as a copy
// of the headers earlier middleware set, and replaces them on commit, so
// the handler reads, adds to and deletes them exactly as it would with no
// timeout in the chain. On timeout, the buffer is abandoned and
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
	// hijacking marks a Hijack in progress: the handler is asking for the
	// connection and the answer is not in yet. expire waits it out (on
	// hijackDone) instead of writing a 504 underneath a hijack that is
	// about to succeed, or shedding the deadline for one that fails.
	hijacking  bool
	hijackDone *sync.Cond
	timedOut   bool
	returned   bool // the handler returned; every later call is refused
}

// timeoutBodies recycles the body buffers. A page-sized response grew a
// fresh buffer by doubling on every request: about 260 KB allocated to
// send a 96 KB page.
var timeoutBodies = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledBody is the largest buffer handed back to timeoutBodies. A
// rare multi-megabyte response would otherwise stay pinned in the pool
// long after the request that needed it.
const maxPooledBody = 1 << 20

// errWriteAfterReturn answers a write through a writer whose handler has
// already returned. Its buffer may belong to another request by then.
var errWriteAfterReturn = errors.New("timeout middleware: write after the handler returned")

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	tw := &timeoutWriter{
		w:    w,
		h:    w.Header().Clone(),
		body: timeoutBodies.Get().(*bytes.Buffer),
		code: http.StatusOK,
	}
	tw.hijackDone = sync.NewCond(&tw.mu)
	return tw
}

// release returns the body buffer to the pool once the handler has
// returned and its output is committed or abandoned. Clearing tw.body
// under the lock is what keeps a write from a goroutine the handler left
// behind out of the next request's response: Write sees nil and refuses.
func (tw *timeoutWriter) release() {
	tw.mu.Lock()
	b := tw.body
	tw.body = nil
	tw.returned = true
	tw.mu.Unlock()
	if b != nil && poolableBody(b) {
		b.Reset()
		timeoutBodies.Put(b)
	}
}

func poolableBody(b *bytes.Buffer) bool { return b.Cap() <= maxPooledBody }

func (tw *timeoutWriter) Header() http.Header { return tw.h }

// Unwrap lets http.ResponseController reach the connection's deadlines.
// Flush and Hijack stop at timeoutWriter's own methods, which commit the
// buffer first, so the controller never writes around it.
func (tw *timeoutWriter) Unwrap() http.ResponseWriter { return tw.w }

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.returned || tw.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		// Informational (103 Early Hints): the response is buffered until
		// the handler returns, so the hint could not arrive early; drop it
		// rather than record it as the status. Streaming starts only once
		// the response is committed, after which a 1xx is too late.
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
	if tw.returned {
		return 0, errWriteAfterReturn
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
	if tw.timedOut || tw.returned {
		return
	}
	if !tw.streaming {
		tw.commitBufferedLocked()
		tw.streaming = true
	}
	_ = http.NewResponseController(tw.w).Flush()
}

// Hijack hands off the underlying connection and, once that succeeded,
// flips into streaming mode (no buffer to commit: the handler owns the
// raw connection from here on). A hijack that FAILED (HTTP/2, a writer
// that cannot hijack) leaves the writer buffered and the deadline armed:
// the handler answers through the writer after all, so its headers must
// commit and a hung handler still owes the client its 504. The attempt
// runs outside the lock with hijacking set, so expire cannot decide
// either way until the answer is in.
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	tw.mu.Lock()
	if tw.timedOut {
		tw.mu.Unlock()
		return nil, nil, fmt.Errorf("timeout middleware: response already timed out")
	}
	if tw.returned {
		tw.mu.Unlock()
		return nil, nil, errWriteAfterReturn
	}
	tw.hijacking = true
	tw.mu.Unlock()
	var (
		conn net.Conn
		rw   *bufio.ReadWriter
		err  error
		ok   bool
	)
	// Deferred so a Hijack that panics (a wrapper asserting
	// http.Hijacker unchecked) still clears hijacking: expire waits on
	// it, and a handler that recovers and keeps running would otherwise
	// leave the request goroutine waiting forever.
	defer func() {
		tw.mu.Lock()
		tw.hijacking = false
		if ok && err == nil {
			tw.streaming = true
		}
		tw.hijackDone.Broadcast()
		tw.mu.Unlock()
	}()
	conn, rw, err = http.NewResponseController(tw.w).Hijack()
	ok = true
	if errors.Is(err, http.ErrNotSupported) {
		return nil, nil, fmt.Errorf("timeout middleware: underlying ResponseWriter does not support hijacking")
	}
	return conn, rw, err
}

// copyHeadersToUnderlyingLocked makes the underlying ResponseWriter's
// headers the handler's buffered set. The buffer began as a copy of them,
// so a header the handler deleted is deleted here too. Caller holds tw.mu.
func (tw *timeoutWriter) copyHeadersToUnderlyingLocked() {
	dst := tw.w.Header()
	for k := range dst {
		if _, ok := tw.h[k]; !ok {
			delete(dst, k)
		}
	}
	maps.Copy(dst, tw.h)
}

// commitBufferedLocked writes the buffered headers, status, and body to
// the underlying ResponseWriter. Caller holds tw.mu.
func (tw *timeoutWriter) commitBufferedLocked() {
	tw.copyHeadersToUnderlyingLocked()
	if tw.wroteHeader {
		tw.w.WriteHeader(tw.code)
	}
	if tw.body != nil && tw.body.Len() > 0 {
		_, _ = tw.body.WriteTo(tw.w)
	}
}

// finish commits buffered handler output to the underlying writer when
// the handler completed normally and the timeout has not fired, then
// releases the buffer. Call it only after the handler has returned.
func (tw *timeoutWriter) finish() {
	tw.mu.Lock()
	if !tw.timedOut && !tw.streaming {
		tw.commitBufferedLocked()
	}
	tw.mu.Unlock()
	tw.release()
}

// expire flags the timeout. Returns false when the response is already
// streaming and the caller MUST NOT attempt to write a 504 (would race
// with handler writes that have already reached the underlying writer).
func (tw *timeoutWriter) expire() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	for tw.hijacking {
		tw.hijackDone.Wait()
	}
	if tw.streaming || tw.timedOut {
		return false
	}
	tw.timedOut = true
	return true
}

// handlerPanic carries a panic out of Timeout's handler goroutine with
// the stack captured there. Recovery unwraps it and logs that stack: the
// re-raise runs on the request goroutine, whose own stack shows only
// Timeout. http.ErrAbortHandler is never wrapped, so net/http and
// Recovery still see the sentinel itself.
type handlerPanic struct {
	v     any
	stack []byte
}

func (p handlerPanic) Error() string { return fmt.Sprint(p.v) }

// Unwrap hands an outer recover the handler's own error, so errors.Is
// and errors.As still see it through Timeout's goroutine hop.
func (p handlerPanic) Unwrap() error { e, _ := p.v.(error); return e }

// reraise is the value the request goroutine re-panics with for the
// handler goroutine's panic v. When the response was already streaming
// (flushed or hijacked), v rides in an abortedStream as well, so
// Recovery cuts the connection instead of appending a 500 to a response
// that is on the wire. Call it after the handler returned.
func (tw *timeoutWriter) reraise(v any, stack []byte) any {
	if v == http.ErrAbortHandler {
		return v
	}
	if inner, ok := v.(handlerPanic); ok { // a nested Timeout: keep the innermost stack
		v, stack = inner.v, inner.stack
	}
	tw.mu.Lock()
	streamed := tw.streaming
	tw.mu.Unlock()
	if _, ok := v.(abortedStream); streamed && !ok {
		v = abortedStream{v}
	}
	return handlerPanic{v: v, stack: stack}
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

// RouteTimeout is the per-route request-timeout resolution stamped onto
// the request context by the router before the middleware chain runs.
// Budget > 0 replaces the app-wide duration for this request; Budget <= 0
// (router.NoTimeout, or an explicit zero, matching net/http where a zero
// duration means "no timeout") exempts the request from the deadline
// entirely. Method and Pattern identify the matched route (the
// registered pattern, not the request path) so the timeout log line
// points at the route.
type RouteTimeout struct {
	Method  string
	Pattern string
	Budget  time.Duration
}

type routeTimeoutKey struct{}

// WithRouteTimeout returns a context carrying the route's timeout
// resolution. Called by core/router when a route or group override is
// configured; the Timeout middleware consumes it.
func WithRouteTimeout(ctx context.Context, rt RouteTimeout) context.Context {
	return context.WithValue(ctx, routeTimeoutKey{}, rt)
}

// RouteTimeoutFromContext reports the route's timeout resolution, if the
// router stamped one.
func RouteTimeoutFromContext(ctx context.Context) (RouteTimeout, bool) {
	rt, ok := ctx.Value(routeTimeoutKey{}).(RouteTimeout)
	return rt, ok
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
//
// Per-route budgets: when the router stamped a RouteTimeout on the
// request context (a route or group override is configured), its Budget
// replaces d for this request; a negative Budget skips the deadline
// entirely. When the deadline fires on a buffered response, a structured
// warning names the method, path, matched pattern, and budget.
// handlerAlreadyFinished reports whether the handler closed done before the
// deadline branch ran.
//
// It exists as a named function rather than an inline select so the
// tie-break has a test seam. The race it settles cannot be reproduced from
// outside — a select picks randomly only when both channels are ready at the
// instant it is evaluated, and producing that overlap needs the middleware
// goroutine descheduled across the deadline, which nothing outside the
// runtime can arrange. So the RACE has no deterministic test, but the
// DECISION does, and a decision nobody can make fail is the same untested
// guard either way.
func handlerAlreadyFinished(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// handlerBeatTheDeadline reports whether the handler finished, and finished
// INSIDE its budget.
//
// The second half is the whole point. A closed done proves the handler is no
// longer running; it says nothing about when it stopped. Delivering on a
// closed channel alone hands a 200 to a handler that overran by any amount,
// as long as this goroutine happened to be descheduled past its finish — the
// deadline would then bound nothing whenever the scheduler was slow, which
// is exactly when a deadline matters.
//
// finishedAt is written before close(done) and read only after receiving on
// it, so the channel orders the two without a lock.
func handlerBeatTheDeadline(done <-chan struct{}, finishedAt *time.Time, deadline time.Time) bool {
	select {
	case <-done:
		// Strictly before, as the completion branch decides: a context
		// is past its deadline at the deadline itself.
		return finishedAt.Before(deadline)
	default:
		return false
	}
}

func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			effective := d
			rt, hasRoute := RouteTimeoutFromContext(r.Context())
			if hasRoute {
				if rt.Budget <= 0 {
					next.ServeHTTP(w, r)
					return
				}
				effective = rt.Budget
			}
			// Same rule for the constructor's own duration as for a route
			// budget, and for the same reason: zero means no deadline, as
			// it does in net/http. Without this, time.NewTimer(0) fires
			// before the handler can finish and every request 504s — so
			// `Timeout(cfg.RequestTimeout)` with the field unset turned
			// the whole surface off rather than leaving it untimed. The
			// framework's own wiring guards it (app.go only installs the
			// middleware when RequestTimeout > 0), which is why nothing
			// in-repo tripped it; a direct caller of the core API had no
			// such cover.
			if effective <= 0 {
				next.ServeHTTP(w, r)
				return
			}
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
			deadline := time.Now().Add(effective)
			timer := time.NewTimer(effective)
			defer timer.Stop()

			tw := newTimeoutWriter(w)
			done := make(chan struct{})
			// Recover panics in the child goroutine and re-raise them
			// in the parent so outer Recovery middleware sees them.
			// Without this, a handler panic crashes the whole process
			// because the parent's defer recover lives in a different
			// goroutine.
			var childPanic any
			// childStack is the handler goroutine's stack at the panic,
			// captured there: the re-raise below runs on this goroutine,
			// whose stack names only the re-raise site.
			var childStack []byte
			// finishedAt records when the handler returned, so the deadline
			// branch can tell "finished inside the budget, and this
			// goroutine was late to notice" from "finished after the
			// deadline, while this goroutine was late". A closed done says
			// only that the handler is no longer running; it does not say
			// when it stopped, and the two cases deserve opposite answers.
			// Written before close(done) and read only after it, which
			// orders the two without a lock.
			var finishedAt time.Time
			go func() {
				defer func() {
					if v := recover(); v != nil {
						childPanic = v
						if v != http.ErrAbortHandler {
							childStack = debug.Stack()
						}
					}
					finishedAt = time.Now()
					close(done)
				}()
				next.ServeHTTP(tw, r.WithContext(ctx))
			}()

			// abandon handles "the request is over but the handler is
			// not": the deadline fired, or the client disconnected while
			// the handler was still running. cause completes the
			// handler's context when it hasn't completed already.
			// Reports whether the 504 was actually written (false for a
			// streaming response, where the deadline no longer applies).
			abandon := func(cause error) bool {
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
					tw.release()
					if childPanic != nil {
						panic(tw.reraise(childPanic, childStack))
					}
					return false
				}
				ctx.cancel(cause)
				i18n.HTTPError(w, r, http.StatusGatewayTimeout, i18n.StatusPhrase(http.StatusGatewayTimeout))
				// Parent abandoned the handler; the handler goroutine
				// is still running and may yet panic. Watch for that
				// late panic and surface it through slog.Default so it
				// doesn't vanish. Otherwise debugging "why does this
				// endpoint sometimes 504 with no further trace" is
				// impossible.
				go func() {
					<-done
					tw.release()
					if childPanic != nil {
						slog.Error("panic in timed-out handler",
							"error", textsafe.Truncate(textsafe.ScrubControlBytes(fmt.Sprint(childPanic)), maxRecoveryPanicLen),
							"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
							"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
							"stack", textsafe.Truncate(string(childStack), maxRecoveryStackLen),
						)
					}
				}()
				return true
			}

			select {
			case <-done:
				if childPanic != nil {
					tw.release()
					panic(tw.reraise(childPanic, childStack))
				}
				if !finishedAt.Before(deadline) {
					abandon(context.DeadlineExceeded)
					return
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
				// A select whose cases are BOTH ready picks between them
				// uniformly at random. When the handler closes done just
				// inside the budget but this goroutine is descheduled past
				// the deadline, that coin flip discards a finished response
				// and 504s a request that succeeded — visible only under
				// load, which is where a spurious 504 costs the most.
				// The handler winning is the answer the client should get,
				// so re-check done before abandoning.
				//
				// The r.Context().Done() case above needs no equivalent:
				// the client is gone, and delivering or abandoning both
				// write to a socket nobody is reading.
				if handlerBeatTheDeadline(done, &finishedAt, deadline) {
					if childPanic != nil {
						tw.release()
						panic(tw.reraise(childPanic, childStack))
					}
					tw.finish()
					return
				}
				if abandon(context.DeadlineExceeded) {
					// Name the route, not just the path: "why does this
					// endpoint 504" starts from this line. Streaming
					// responses don't get here; their deadline is shed.
					attrs := []any{
						"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
						"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
						"timeout", effective,
					}
					if hasRoute && rt.Pattern != "" {
						attrs = append(attrs, "pattern", rt.Pattern)
					}
					slog.Warn("request timeout", attrs...)
				}
			}
		})
	}
}
