package log

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Caps on the size of pieces that flow into a log entry. Without these,
// a hostile or buggy caller can write multi-MB JSON lines per request:
// Go's default MaxHeaderBytes allows ~1 MiB request lines, so URL.Path
// or an X-Forwarded-For / X-Real-IP header alone can be that large; a
// panic with a giant value or stack compounds the problem across every
// sink. Every request-derived field (path, forwarded_for, remote, panic,
// stack) is therefore truncated before it reaches a log entry.
const (
	maxPanicValueLen = 4 << 10  // 4 KiB
	maxStackLen      = 64 << 10 // 64 KiB
	maxPathLen       = 2 << 10  // 2 KiB
)

// truncateString returns s truncated to max bytes with an explicit
// "... (truncated)" marker so consumers know the entry was capped.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = " … (truncated)"
	if max <= len(marker) {
		return s[:max]
	}
	return s[:max-len(marker)] + marker
}

// accessMiddleware emits one INFO record per request once the response
// has flushed. Fields: method, path, status, bytes, dur_ms, request_id,
// remote, forwarded_for.
//
// The emit runs in a defer so a panicking handler still gets an entry.
// When the inner recoveryMiddleware catches a panic it writes status 500
// to the response; we read that back from the wrapped writer.
//
// The URL.Path is snapshotted BEFORE next.ServeHTTP so inner middleware
// rewrites (StripPrefix, custom rewriters) don't change the logged path.
//
// `remote` is r.RemoteAddr by default; if trustXFF is true the first
// X-Forwarded-For / X-Real-IP value overrides it. Both forms always
// emit `forwarded_for` (raw) so operators can correlate without trust.
func accessMiddleware(logger *slog.Logger, trustXFF bool) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// Snapshot path/method up front — inner middleware may
			// rewrite r.URL; the access log records what the client
			// actually sent.
			method := r.Method
			path := truncateString(r.URL.Path, maxPathLen)
			forwardedRaw := truncateString(r.Header.Get("X-Forwarded-For"), maxPathLen)
			rw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http.access",
					slog.String("method", method),
					slog.String("path", path),
					slog.Int("status", rw.status),
					slog.Int64("bytes", rw.bytes),
					slog.Int64("dur_ms", time.Since(start).Milliseconds()),
					slog.String("request_id", middleware.GetRequestID(r.Context())),
					slog.String("remote", remoteAddr(r, trustXFF)),
					slog.String("forwarded_for", forwardedRaw),
				)
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recoveryMiddleware logs panics with full request context and a stack
// trace, then returns 500. Replaces middleware.Recovery for apps using
// the log plugin so panics flow through the configured sinks.
//
// The panic value and stack are capped (4 KiB / 64 KiB) so a handler
// that panics with a 100 MB string doesn't write a 100 MB log entry —
// the file sink would happily serialize all of it before rotating, and
// the webhook sink would try to POST it as a batch element.
func recoveryMiddleware(logger *slog.Logger) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					panicStr := truncateString(fmt.Sprint(v), maxPanicValueLen)
					stack := truncateString(string(debug.Stack()), maxStackLen)
					logger.LogAttrs(r.Context(), slog.LevelError, "http.panic",
						slog.String("panic", panicStr),
						slog.String("method", r.Method),
						slog.String("path", truncateString(r.URL.Path, maxPathLen)),
						slog.String("request_id", middleware.GetRequestID(r.Context())),
						slog.String("stack", stack),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
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
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *countingResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		// Implicit 200; record it but don't double-call WriteHeader.
		rw.wroteHeader = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

// Flush forwards to the underlying ResponseWriter's Flusher if it has one.
// Without this, any SSE / chunked-JSON / long-poll handler downstream of
// battery/log returns 500 "streaming unsupported" — its
// `w.(http.Flusher)` assertion fails against the wrapper. Mirrors the
// fix already present on core/middleware.metricsResponseWriter.
func (rw *countingResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying ResponseWriter's Hijacker if it has one.
// Without this, wrapping breaks any handler that performs a WebSocket upgrade
// or otherwise type-asserts http.Hijacker (e.g. core/stream/websocket.go),
// because the assertion would see the wrapper instead of the real writer and
// fail with "does not support hijacking". Mirrors the fix on
// core/middleware.metricsResponseWriter.
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

// remoteAddr returns the client address the access log should record.
// When trustXFF is false (default) it returns r.RemoteAddr only;
// X-Forwarded-For / X-Real-IP are still emitted as a separate
// `forwarded_for` field but never override `remote`.
//
// When trustXFF is true the returned value is the FIRST comma-separated
// segment of X-Forwarded-For (or X-Real-IP) with surrounding whitespace
// trimmed — without the trim, a value like "  attacker.example, real"
// could sneak past downstream allow-list string matching.
func remoteAddr(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First entry is the client; subsequent entries are proxies.
			if comma := strings.IndexByte(xff, ','); comma >= 0 {
				return truncateString(strings.TrimSpace(xff[:comma]), maxPathLen)
			}
			return truncateString(strings.TrimSpace(xff), maxPathLen)
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return truncateString(strings.TrimSpace(real), maxPathLen)
		}
	}
	return r.RemoteAddr
}
