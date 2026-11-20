package log

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

const (
	maxPanicValueLen = 4 << 10
	maxStackLen      = 64 << 10
	maxPathLen       = 2 << 10
)

func accessMiddleware(logger *slog.Logger, trustXFF bool) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			method := textsafe.ScrubControlBytes(r.Method)
			path := textsafe.Truncate(textsafe.ScrubControlBytes(r.URL.Path), maxPathLen)
			forwardedRaw := textsafe.Truncate(textsafe.ScrubControlBytes(strings.Join(r.Header.Values("X-Forwarded-For"), ", ")), maxPathLen)
			rw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http.access",
					slog.String("method", method),
					slog.String("path", path),
					slog.Int("status", rw.status),
					slog.Int64("bytes", rw.bytes),
					slog.Int64("dur_ms", time.Since(start).Milliseconds()),
					slog.String("request_id", textsafe.ScrubControlBytes(middleware.GetRequestID(r.Context()))),
					slog.String("remote", textsafe.ScrubControlBytes(remoteAddr(r, trustXFF))),
					slog.String("forwarded_for", forwardedRaw),
				)
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

func recoveryMiddleware(reporter ErrorReporter) middleware.Middleware {
	if reporter == nil {
		reporter = noopReporter{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					reporter.Report(ErrorReport{
						Message:   "http.panic",
						Error:     textsafe.Truncate(textsafe.ScrubControlBytes(fmt.Sprint(v)), maxPanicValueLen),
						Stack:     textsafe.Truncate(string(debug.Stack()), maxStackLen),
						Method:    textsafe.ScrubControlBytes(r.Method),
						Path:      textsafe.Truncate(textsafe.ScrubControlBytes(r.URL.Path), maxPathLen),
						Route:     textsafe.ScrubControlBytes(r.Pattern),
						RequestID: textsafe.ScrubControlBytes(middleware.GetRequestID(r.Context())),
						ctx:       r.Context(),
					})
					if rw.wroteHeader {
						panic(http.ErrAbortHandler)
					}
					i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

type countingResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (rw *countingResponseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		rw.ResponseWriter.WriteHeader(code)
		return
	}
	rw.ResponseWriter.WriteHeader(code)
	rw.status = code
	rw.wroteHeader = true
}

func (rw *countingResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

// ReadFrom keeps the connection's sendfile path: io.Copy and
// http.ServeContent use the writer's ReaderFrom when it has one, and
// embedding http.ResponseWriter does not promote it.
func (rw *countingResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	rw.wroteHeader = true
	n, err := io.Copy(rw.ResponseWriter, src)
	rw.bytes += n
	return n, err
}

// Flush forwards to the underlying ResponseWriter's Flusher if it has
// one.
func (rw *countingResponseWriter) Flush() {
	rw.wroteHeader = true
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying ResponseWriter's Hijacker if it has
// one.
func (rw *countingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push forwards to the underlying ResponseWriter's Pusher if it has one.
func (rw *countingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := rw.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap lets http.NewResponseController walk through this wrapper so
// per-write deadlines reach the real connection.
func (rw *countingResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

func remoteAddr(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if len(r.Header.Values("X-Forwarded-For")) > 0 {
			if ip, ok := clientip.Forwarded(r.Header, clientip.Proxies{}); ok {
				return ip.String()
			}
			return r.RemoteAddr
		}
		if ip := clientip.ParseHop(r.Header.Get("X-Real-IP")); ip != nil {
			return ip.String()
		}
	}
	return r.RemoteAddr
}
