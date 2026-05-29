package middleware

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// LoggingFn returns middleware that logs each request via the *slog.Logger
// returned by getLogger. getLogger is called per request so the upstream
// (e.g. framework.App) can hand out a logger that was swapped after the
// middleware was attached — this is how plugins can replace the logger
// after the chain is already wired.
//
// If getLogger is nil or returns nil, slog.Default() is used.
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
				"method", safeLogMethod(r.Method),
				"path", safeLogPath(r.URL.Path),
				"status", wrapped.statusCode,
				"duration", duration.String(),
			)
		})
	}
}

// Logging returns middleware that logs each request using slog.Default()
// at request time (not at construction). Kept as a convenience for code
// that doesn't have a logger to inject; new framework code should wire
// LoggingFn with an explicit logger source.
func Logging() Middleware {
	return LoggingFn(nil)
}

// LoggingWithWriter returns logging middleware that writes to w as
// structured JSON. If w is nil, slog.Default() is used at request time.
//
// Retained for tests and ad-hoc tooling that wants a fixed-destination
// logger; new framework code should prefer LoggingFn.
func LoggingWithWriter(w io.Writer) Middleware {
	if w == nil {
		return LoggingFn(nil)
	}
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return LoggingFn(func() *slog.Logger { return logger })
}

// SampledLogging returns middleware that logs only a fraction of requests.
// It ALWAYS logs requests that are slow (> slowThreshold) or errored
// (status >= 400). Otherwise it logs 1-in-every-sampleN requests.
//
// This addresses the ~200× overhead benchmark where Logging() dominates
// the default middleware chain cost. Use this as the default in
// production; switch to Logging() in dev or when debugging.
//
// When sampleN is 0 or 1, every request is logged (equivalent to Logging()).
//
// SampledLogging uses slog.Default(); use SampledLoggingFn to inject a
// specific logger source the same way LoggingFn does.
func SampledLogging(sampleN int, slowThreshold time.Duration) Middleware {
	return SampledLoggingFn(sampleN, slowThreshold, nil)
}

// SampledLoggingFn is the injected-logger variant of SampledLogging.
// getLogger is called per logged event; nil or nil-returning getLogger
// falls back to slog.Default().
func SampledLoggingFn(sampleN int, slowThreshold time.Duration, getLogger func() *slog.Logger) Middleware {
	if sampleN <= 1 {
		return LoggingFn(getLogger)
	}
	var counter uint64
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

			// Always log errors and slow requests
			if wrapped.statusCode >= 400 || duration > slowThreshold {
				logger.Info("request",
					"method", safeLogMethod(r.Method),
					"path", safeLogPath(r.URL.Path),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", false,
				)
				return
			}

			// Sample 1-in-N normal requests
			n := atomic.AddUint64(&counter, 1)
			if n%uint64(sampleN) == 1 {
				logger.Info("request",
					"method", safeLogMethod(r.Method),
					"path", safeLogPath(r.URL.Path),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", true,
				)
			}
		})
	}
}

// safeLogMethod percent-encodes control bytes (and DEL) in the HTTP
// method so an attacker who got a CRLF / ESC sequence into r.Method
// can't forge a fake log entry or smuggle a terminal-control payload
// into an operator's tail/less session.
func safeLogMethod(m string) string {
	if !strings.ContainsAny(m, "\x00\r\n\t\v\f\b\x1b") {
		return m
	}
	var b strings.Builder
	b.Grow(len(m))
	for i := 0; i < len(m); i++ {
		c := m[i]
		if c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// safeLogPath re-encodes control characters in a URL path so an
// attacker can't forge a fake log entry by injecting CRLF (or other
// terminal-escape sequences). slog's JSON handler already escapes
// these for valid JSON, but a JSON-escaped \r\n is still visible to
// text grep — and naive log shippers / console viewers can be tricked
// into rendering the injected payload on its own line.
func safeLogPath(p string) string {
	if !strings.ContainsAny(p, "\x00\r\n\t\v\f\b\x1b") {
		return p
	}
	var b strings.Builder
	b.Grow(len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// DiscardLogging returns middleware that tracks request timing but
// writes no log output. Useful for benchmarks and high-throughput
// production paths where structured logging is handled externally
// (e.g. by a reverse proxy or APM agent).
func DiscardLogging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
// To preserve optional interfaces (Flusher, Hijacker, Pusher), use
// wrapResponseWriter which returns a wrapper that exposes only the
// interfaces the underlying writer actually supports.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before delegating.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// optional-interface wrappers — we conditionally expose just what the
// underlying writer actually supports, so a caller's interface assertion
// reflects the real capabilities of the stack.

type flushWriter struct{ *responseWriter }

func (f flushWriter) Flush() {
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

// 8 combinations of (Flusher, Hijacker, Pusher) — only the relevant
// few are common enough to be worth a dedicated type; we collapse the
// rest into the dominant patterns below.

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

// wrapResponseWriter picks a wrapper that exposes the same optional
// interfaces (Flusher, Hijacker, Pusher) as the underlying writer.
func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

// reveal returns a writer that exposes only the optional interfaces the
// underlying ResponseWriter w actually supports.
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
