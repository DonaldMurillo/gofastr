package middleware

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// LoggingFn returns middleware that logs each request through getLogger, called per request; slog.Default when it or its result is nil.
func LoggingFn(getLogger func() *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := wrapResponseWriter(w)
			next.ServeHTTP(reveal(wrapped), r)
			duration := time.Since(start)

			logger := slog.Default()
			if getLogger != nil {
				if l := getLogger(); l != nil {
					logger = l
				}
			}
			logger.Info("request",
				"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
				"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
				"status", wrapped.statusCode,
				"duration", duration.String(),
			)
		})
	}
}

// Logging is LoggingFn(nil).
func Logging() Middleware {
	return LoggingFn(nil)
}

// LoggingWithWriter returns logging middleware that writes JSON to w, or uses slog.Default when w is nil.
func LoggingWithWriter(w io.Writer) Middleware {
	if w == nil {
		return LoggingFn(nil)
	}
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return LoggingFn(func() *slog.Logger { return logger })
}

// SampledLogging logs every request that errored (status >= 400) or took longer than slowThreshold, and one in sampleN of the rest; sampleN <= 1 logs all.
func SampledLogging(sampleN int, slowThreshold time.Duration) Middleware {
	return SampledLoggingFn(sampleN, slowThreshold, nil)
}

// SampledLoggingFn is SampledLogging with a logger source, as in LoggingFn.
func SampledLoggingFn(sampleN int, slowThreshold time.Duration, getLogger func() *slog.Logger) Middleware {
	if sampleN <= 1 {
		return LoggingFn(getLogger)
	}
	var counter atomic.Uint64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := wrapResponseWriter(w)
			next.ServeHTTP(reveal(wrapped), r)
			duration := time.Since(start)

			logger := slog.Default()
			if getLogger != nil {
				if l := getLogger(); l != nil {
					logger = l
				}
			}

			if wrapped.statusCode >= 400 || duration > slowThreshold {
				logger.Info("request",
					"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
					"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", false,
				)
				return
			}

			n := counter.Add(1)
			if n%uint64(sampleN) == 1 {
				logger.Info("request",
					"method", textsafe.Truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
					"path", textsafe.Truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", true,
				)
			}
		})
	}
}

func safeLogMethod(m string) string { return textsafe.ScrubControlBytes(m) }

func safeLogPath(p string) string { return textsafe.ScrubControlBytes(p) }

// DiscardLogging returns middleware that logs nothing.
func DiscardLogging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		rw.ResponseWriter.WriteHeader(code)
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) ReadFrom(src io.Reader) (int64, error) {
	copied, err := io.Copy(rw.ResponseWriter, src)
	if copied > 0 && !rw.wroteHeader {
		rw.statusCode = http.StatusOK
		rw.wroteHeader = true
	}
	return copied, err
}

func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

type flushWriter struct{ *responseWriter }

func (f flushWriter) Flush() {
	f.responseWriter.wroteHeader = true
	if fl, ok := f.responseWriter.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

type hijackWriter struct{ *responseWriter }

func (h hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := h.responseWriter.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("middleware: underlying ResponseWriter does not implement http.Hijacker")
}

type pushWriter struct{ *responseWriter }

func (p pushWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := p.responseWriter.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

type flushHijackWriter struct {
	*responseWriter
	flushWriter
	hijackWriter
}
type flushHijackPushWriter struct {
	*responseWriter
	flushWriter
	hijackWriter
	pushWriter
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func reveal(rw *responseWriter) http.ResponseWriter {
	_, fl := rw.ResponseWriter.(http.Flusher)
	_, hj := rw.ResponseWriter.(http.Hijacker)
	_, pu := rw.ResponseWriter.(http.Pusher)

	switch {
	case fl && hj && pu:
		return flushHijackPushWriter{
			responseWriter: rw,
			flushWriter:    flushWriter{rw},
			hijackWriter:   hijackWriter{rw},
			pushWriter:     pushWriter{rw},
		}
	case fl && hj:
		return flushHijackWriter{
			responseWriter: rw,
			flushWriter:    flushWriter{rw},
			hijackWriter:   hijackWriter{rw},
		}
	case fl:
		return struct {
			*responseWriter
			flushWriter
		}{rw, flushWriter{rw}}
	case hj:
		return struct {
			*responseWriter
			hijackWriter
		}{rw, hijackWriter{rw}}
	case pu:
		return struct {
			*responseWriter
			pushWriter
		}{rw, pushWriter{rw}}
	default:
		return rw
	}
}
