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

type timeoutWriter struct {
	w http.ResponseWriter

	mu          sync.Mutex
	h           http.Header
	body        *bytes.Buffer
	wroteHeader bool
	code        int
	streaming   bool
	hijacking   bool
	hijackDone  *sync.Cond
	timedOut    bool
	returned    bool
}

var timeoutBodies = sync.Pool{New: func() any { return new(bytes.Buffer) }}

const maxPooledBody = 1 << 20

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

func (tw *timeoutWriter) Unwrap() http.ResponseWriter { return tw.w }

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.returned || tw.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
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

func (tw *timeoutWriter) copyHeadersToUnderlyingLocked() {
	dst := tw.w.Header()
	for k := range dst {
		if _, ok := tw.h[k]; !ok {
			delete(dst, k)
		}
	}
	maps.Copy(dst, tw.h)
}

func (tw *timeoutWriter) commitBufferedLocked() {
	tw.copyHeadersToUnderlyingLocked()
	if tw.wroteHeader {
		tw.w.WriteHeader(tw.code)
	}
	if tw.body != nil && tw.body.Len() > 0 {
		_, _ = tw.body.WriteTo(tw.w)
	}
}

func (tw *timeoutWriter) finish() {
	tw.mu.Lock()
	if !tw.timedOut && !tw.streaming {
		tw.commitBufferedLocked()
	}
	tw.mu.Unlock()
	tw.release()
}

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

type handlerPanic struct {
	v     any
	stack []byte
}

func (p handlerPanic) Error() string { return fmt.Sprint(p.v) }

func (p handlerPanic) Unwrap() error { e, _ := p.v.(error); return e }

func (tw *timeoutWriter) reraise(v any, stack []byte) any {
	if v == http.ErrAbortHandler {
		return v
	}
	if inner, ok := v.(handlerPanic); ok {
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

type timeoutCtx struct {
	parent context.Context

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

func (c *timeoutCtx) cancel(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
}

// RouteTimeout is the router's per-route timeout for a request: Budget > 0 replaces the Timeout duration, Budget <= 0 exempts the request; Method and Pattern name the route in the timeout log.
type RouteTimeout struct {
	Method  string
	Pattern string
	Budget  time.Duration
}

type routeTimeoutKey struct{}

// WithRouteTimeout returns ctx carrying rt for the Timeout middleware.
func WithRouteTimeout(ctx context.Context, rt RouteTimeout) context.Context {
	return context.WithValue(ctx, routeTimeoutKey{}, rt)
}

// RouteTimeoutFromContext returns the RouteTimeout the router put on ctx, if any.
func RouteTimeoutFromContext(ctx context.Context) (RouteTimeout, bool) {
	rt, ok := ctx.Value(routeTimeoutKey{}).(RouteTimeout)
	return rt, ok
}

func handlerAlreadyFinished(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func handlerBeatTheDeadline(done <-chan struct{}, finishedAt *time.Time, deadline time.Time) bool {
	select {
	case <-done:
		return finishedAt.Before(deadline)
	default:
		return false
	}
}

// Timeout returns middleware that answers 504 when the handler has not finished within d (or the route's RouteTimeout); zero or less means no deadline, and a response that flushed or hijacked is no longer bound by it.
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
			if effective <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			ctx := newTimeoutCtx(r.Context())
			defer ctx.cancel(context.Canceled)
			stop := context.AfterFunc(r.Context(), func() {
				ctx.cancel(r.Context().Err())
			})
			defer stop()
			deadline := time.Now().Add(effective)
			timer := time.NewTimer(effective)
			defer timer.Stop()

			tw := newTimeoutWriter(w)
			done := make(chan struct{})
			var childPanic any
			var childStack []byte
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

			abandon := func(cause error) bool {
				if !tw.expire() {
					<-done
					tw.release()
					if childPanic != nil {
						panic(tw.reraise(childPanic, childStack))
					}
					return false
				}
				ctx.cancel(cause)
				i18n.HTTPError(w, r, http.StatusGatewayTimeout, i18n.StatusPhrase(http.StatusGatewayTimeout))
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
				abandon(r.Context().Err())
			case <-timer.C:
				if handlerBeatTheDeadline(done, &finishedAt, deadline) {
					if childPanic != nil {
						tw.release()
						panic(tw.reraise(childPanic, childStack))
					}
					tw.finish()
					return
				}
				if abandon(context.DeadlineExceeded) {
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
