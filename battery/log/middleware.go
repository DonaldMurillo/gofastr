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
			// Snapshot path/method up front, inner middleware may
			// rewrite r.URL; the access log records what the client
			// actually sent.
			method := scrubControlBytes(r.Method)
			path := truncateString(scrubControlBytes(r.URL.Path), maxPathLen)
			forwardedRaw := truncateString(scrubControlBytes(r.Header.Get("X-Forwarded-For")), maxPathLen)
			rw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http.access",
					slog.String("method", method),
					slog.String("path", path),
					slog.Int("status", rw.status),
					slog.Int64("bytes", rw.bytes),
					slog.Int64("dur_ms", time.Since(start).Milliseconds()),
					slog.String("request_id", scrubControlBytes(middleware.GetRequestID(r.Context()))),
					slog.String("remote", scrubControlBytes(remoteAddr(r, trustXFF))),
					slog.String("forwarded_for", forwardedRaw),
				)
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// c0AndDelSet is the full C0 control range plus DEL, the probe set for
// scrubControlBytes. Mirrors core/middleware's set (logging.go): the
// fast path must cover every byte the encoder handles, or a string
// carrying only an uncovered byte bypasses the encoder and is logged
// raw.
var c0AndDelSet = func() string {
	var b [33]byte
	for i := range 32 {
		b[i] = byte(i)
	}
	b[32] = 0x7f
	return string(b[:])
}()

// scrubControlBytes percent-encodes ASCII control bytes (the C0 range)
// and DEL in a request-derived log field. battery/log is the production
// access path: r.URL.Path is percent-DECODED, so %1b/%0d%0a/%00 in a
// request URL are real ESC/CRLF/NUL by the time accessMiddleware
// snapshots them, and entries fan out to the file sink, the webhook
// sink, the console sink and the MCP ring. A raw ESC is terminal-escape
// injection into every operator tail; a CRLF forges entries in any
// downstream line-oriented consumer. slog's JSON handler escapes these
// for valid JSON, but a JSON-escaped \r\n is still visible to text
// grep, and naive log shippers render the injected payload on its own
// line. Parity with core/middleware's scrubControlBytes, which guards
// the framework's own logging sinks the same way.
func scrubControlBytes(s string) string {
	if !strings.ContainsAny(s, c0AndDelSet) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range s {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// recoveryMiddleware recovers panics, reports them through the configured
// ErrorReporter (default: the log plugin's slog reporter, preserving the
// http.panic log line), then returns 500. A nil reporter is a safe no-op so
// the middleware never nil-derefs on the recovery path.
//
// The panic value and stack are capped (4 KiB / 64 KiB) so a handler that
// panics with a 100 MB string doesn't write a 100 MB log entry or POST one
// to an error collector, the file sink would serialize all of it before
// rotating, and the webhook sink would try to deliver it as a batch element.
func recoveryMiddleware(reporter ErrorReporter) middleware.Middleware {
	if reporter == nil {
		reporter = noopReporter{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					reporter.Report(ErrorReport{
						Message:   "http.panic",
						Error:     truncateString(fmt.Sprint(v), maxPanicValueLen),
						Stack:     truncateString(string(debug.Stack()), maxStackLen),
						Method:    r.Method,
						Path:      truncateString(r.URL.Path, maxPathLen),
						Route:     r.Pattern,
						RequestID: middleware.GetRequestID(r.Context()),
						ctx:       r.Context(),
					})
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
// battery/log returns 500 "streaming unsupported", its
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

// Unwrap lets http.NewResponseController walk through this wrapper so
// per-write deadlines reach the real connection. battery/log is the
// production access-log path, so without this every streaming handler
// behind it silently loses SetWriteDeadline (ErrNotSupported). Mirrors
// core/middleware's cors/metrics/tracing/logging wrappers.
func (rw *countingResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// remoteAddr returns the client address the access log should record.
// When trustXFF is false (default) it returns r.RemoteAddr only;
// X-Forwarded-For / X-Real-IP are still emitted as a separate
// `forwarded_for` field but never override `remote`.
//
// When trustXFF is true the returned value is the FIRST comma-separated
// segment of X-Forwarded-For (or X-Real-IP) with surrounding whitespace
// trimmed, without the trim, a value like "  attacker.example, real"
// could sneak past downstream allow-list string matching.
func remoteAddr(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First entry is the client; subsequent entries are proxies.
			if before, _, ok := strings.Cut(xff, ","); ok {
				return truncateString(strings.TrimSpace(before), maxPathLen)
			}
			return truncateString(strings.TrimSpace(xff), maxPathLen)
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return truncateString(strings.TrimSpace(real), maxPathLen)
		}
	}
	return r.RemoteAddr
}
