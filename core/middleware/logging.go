package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Logging returns middleware that logs each request using log/slog.
// It records method, path, response status code, and duration.
// Output is structured JSON written to stderr by default.
func Logging() Middleware {
	return LoggingWithWriter(nil)
}

// LoggingWithWriter returns logging middleware that writes to w.
// If w is nil, os.Stderr is used.
func LoggingWithWriter(w io.Writer) Middleware {
	var logger *slog.Logger
	if w == nil {
		logger = slog.Default()
	} else {
		logger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"duration", duration.String(),
			)
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
