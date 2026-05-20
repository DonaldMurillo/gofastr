package middleware

import (
	"io"
	"log/slog"
	"net/http"
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
