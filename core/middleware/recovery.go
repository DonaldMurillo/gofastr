package middleware

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Caps on log-entry pieces so a handler that panics with a 100 MB
// string doesn't write a 100 MB log line through slog.Default for
// apps that don't load a structured-logging plugin.
const (
	maxRecoveryPanicLen  = 4 << 10  // 4 KiB
	maxRecoveryStackLen  = 64 << 10 // 64 KiB
	maxRecoveryPathLen   = 2 << 10  // 2 KiB
	maxRecoveryMethodLen = 1 << 5   // 32 B; real HTTP methods are ≤ ~10 chars
)

// RecoveryFn returns recovery middleware that logs panics via the
// *slog.Logger returned by getLogger. The accessor is called per
// request so a downstream `app.SetLogger` swap takes effect without
// rewiring the middleware chain.
//
// If getLogger is nil or returns nil, slog.Default() is used.
//
// The panic value, stack trace, and URL.Path are truncated to
// reasonable caps (4 KiB / 64 KiB / 2 KiB) so an attacker (or a buggy
// handler) can't drive multi-MB log entries through this middleware.
//
// A panic before the response started gets a 500. The 500 drops the
// handler's representation and caching headers first (Content-Type,
// Content-Encoding, Content-Length, ETag, Last-Modified, Content-Range,
// Accept-Ranges, Content-Disposition) and sets Cache-Control: no-store,
// so the error page is not served as a gzip body, cached for a year, or
// filed under the asset's tag; Set-Cookie and the other headers stay. A
// panic after the response started (a status sent, a flush, body bytes,
// a hijack) cannot be answered with a 500: the panic is logged and the
// connection cut (http.ErrAbortHandler), so the client sees a truncated
// response instead of error text spliced onto a 200 body.
//
// A panic Timeout carried out of its handler goroutine is logged with
// the stack captured in that goroutine, so the log names the code that
// panicked rather than Timeout's re-raise.
func RecoveryFn(getLogger func() *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recoveryWriter{ResponseWriter: w}
			defer func() {
				if err := recover(); err != nil {
					// net/http's own signal to cut the connection quietly.
					if err == http.ErrAbortHandler {
						panic(err)
					}
					// Compress or Timeout raised it for a response already
					// on the wire, or this writer saw the response start:
					// a 500 cannot follow. Log the handler's panic, then
					// cut the connection.
					err, stack, cut := unwrapPanic(err)
					cut = cut || rw.started
					if stack == nil {
						stack = debug.Stack()
					}
					logger := slog.Default()
					if getLogger != nil {
						if l := getLogger(); l != nil {
							logger = l
						}
					}
					logger.Error("panic recovered",
						"error", textsafe.Truncate(textsafe.ScrubControlBytes(fmt.Sprint(err)), maxRecoveryPanicLen),
						"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
						"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
						"stack", textsafe.Truncate(string(stack), maxRecoveryStackLen),
					)
					if cut {
						panic(http.ErrAbortHandler)
					}
					// Nothing was sent: the headers are still ours to set.
					h := w.Header()
					for _, k := range recoveryDroppedHeaders {
						h.Del(k)
					}
					h.Set("Cache-Control", "no-store")
					i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recoveryDroppedHeaders are the handler's headers that describe the
// response it meant to send, not the 500 Recovery sends in its place.
// HTTPError sets its own Content-Type.
var recoveryDroppedHeaders = [...]string{
	"Content-Encoding", "Content-Length", "Content-Type", "ETag", "Last-Modified",
	"Content-Range", "Accept-Ranges", "Content-Disposition",
}

// unwrapPanic peels the wrappers this package re-raises a panic in:
// handlerPanic (Timeout's goroutine hop, carrying its stack) and
// abortedStream (a response already on the wire, so cut). Either order
// nests, depending on whether Compress sits inside or outside Timeout.
func unwrapPanic(v any) (val any, stack []byte, cut bool) {
	for {
		switch p := v.(type) {
		case handlerPanic:
			v = p.v
			if stack == nil {
				stack = p.stack
			}
		case abortedStream:
			v, cut = p.v, true
		default:
			return v, stack, cut
		}
	}
}

// recoveryWriter records whether the response started: a final status
// sent, a body byte written, a flush, or a hijack. After that a panic
// cannot become a 500. Flush and Hijack forward through
// http.ResponseController, and Unwrap lets the controller reach the
// connection's deadlines.
type recoveryWriter struct {
	http.ResponseWriter
	started bool
	// status is the final status that reached the client (200 for an
	// implicit one), 0 before that or after a bare hijack. Metrics and
	// Tracing read it for a panicked request (panickedStatus).
	status int
}

func (w *recoveryWriter) WriteHeader(code int) {
	// net/http panics on a code outside 100-999 before sending anything,
	// so the response starts only once the call returned. An
	// informational status (103 Early Hints) is not the response: the
	// final status, a 500 included, can still follow it.
	w.ResponseWriter.WriteHeader(code)
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.markStarted(code)
	}
}

func (w *recoveryWriter) markStarted(code int) {
	w.started = true
	if w.status == 0 {
		w.status = code
	}
}

func (w *recoveryWriter) Write(b []byte) (int, error) {
	w.markStarted(http.StatusOK)
	return w.ResponseWriter.Write(b)
}

// FlushError sends the headers (200 unless a status was set) and the
// buffered body.
func (w *recoveryWriter) FlushError() error {
	w.markStarted(http.StatusOK)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Flush is FlushError for handlers that assert http.Flusher.
func (w *recoveryWriter) Flush() { _ = w.FlushError() }

// Hijack hands the connection to the handler; nothing more can be sent
// through the writer, so a later panic cuts rather than answers.
func (w *recoveryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.started = true
	}
	return conn, brw, err
}

// ReadFrom keeps the underlying writer's sendfile path (io.Copy uses its
// ReaderFrom when it has one): hiding it would turn every file a handler
// serves through Recovery into a userspace copy.
func (w *recoveryWriter) ReadFrom(src io.Reader) (int64, error) {
	w.markStarted(http.StatusOK)
	return io.Copy(w.ResponseWriter, src)
}

func (w *recoveryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Push forwards to the underlying ResponseWriter's Pusher if it has one,
// like the metrics and tracing wrappers.
func (w *recoveryWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := w.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Recovery returns middleware that catches panics in downstream handlers
// using slog.Default at request time. Kept as a convenience for code
// without a logger to inject; new framework code should prefer
// RecoveryFn with an explicit logger source.
func Recovery() Middleware {
	return RecoveryFn(nil)
}
