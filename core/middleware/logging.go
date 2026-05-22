package middleware

import (
	"io"
	"log/slog"
	"net/http"
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
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			logger := slog.Default()
			if getLogger != nil {
				if l := getLogger(); l != nil {
					logger = l
				}
			}
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
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
func SampledLogging(sampleN int, slowThreshold time.Duration) Middleware {
	if sampleN <= 1 {
		return LoggingFn(nil)
	}
	var counter uint64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			// Always log errors and slow requests
			if wrapped.statusCode >= 400 || duration > slowThreshold {
				slog.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", false,
				)
				return
			}

			// Sample 1-in-N normal requests
			n := atomic.AddUint64(&counter, 1)
			if n%uint64(sampleN) == 1 {
				slog.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", true,
				)
			}
		})
	}
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
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before delegating.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
