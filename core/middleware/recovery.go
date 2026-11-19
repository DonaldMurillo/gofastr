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

const (
	maxRecoveryPanicLen  = 4 << 10
	maxRecoveryStackLen  = 64 << 10
	maxRecoveryPathLen   = 2 << 10
	maxRecoveryMethodLen = 1 << 5
)

// RecoveryFn returns middleware that logs a panic, bounded and scrubbed, through getLogger (slog.Default when it or its result is nil) and answers 500, or cuts the connection when the response already started.
func RecoveryFn(getLogger func() *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recoveryWriter{ResponseWriter: w}
			defer func() {
				if err := recover(); err != nil {
					if err == http.ErrAbortHandler {
						panic(err)
					}
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

var recoveryDroppedHeaders = [...]string{
	"Content-Encoding", "Content-Length", "Content-Type", "ETag", "Last-Modified",
	"Content-Range", "Accept-Ranges", "Content-Disposition",
}

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

type recoveryWriter struct {
	http.ResponseWriter
	started bool
	status  int
}

func (w *recoveryWriter) WriteHeader(code int) {
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

func (w *recoveryWriter) FlushError() error {
	w.markStarted(http.StatusOK)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *recoveryWriter) Flush() { _ = w.FlushError() }

func (w *recoveryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.started = true
	}
	return conn, brw, err
}

func (w *recoveryWriter) ReadFrom(src io.Reader) (int64, error) {
	w.markStarted(http.StatusOK)
	return io.Copy(w.ResponseWriter, src)
}

func (w *recoveryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *recoveryWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := w.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Recovery is RecoveryFn(nil).
func Recovery() Middleware {
	return RecoveryFn(nil)
}
