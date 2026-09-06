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
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// LoggingFn returns middleware that logs each request via the *slog.Logger
// returned by getLogger. getLogger is called per request so the upstream
// (e.g. framework.App) can hand out a logger that was swapped after the
// middleware was attached; this is how plugins can replace the logger
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
				"method", truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
				"path", truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
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

			// Always log errors and slow requests
			if wrapped.statusCode >= 400 || duration > slowThreshold {
				logger.Info("request",
					"method", truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
					"path", truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", false,
				)
				return
			}

			// Sample 1-in-N normal requests
			n := counter.Add(1)
			if n%uint64(sampleN) == 1 {
				logger.Info("request",
					"method", truncate(safeLogMethod(r.Method), maxRecoveryMethodLen),
					"path", truncate(safeLogPath(r.URL.Path), maxRecoveryPathLen),
					"status", wrapped.statusCode,
					"duration", duration.String(),
					"sampled", true,
				)
			}
		})
	}
}

// needsControlScrub is the fast-path probe for scrubControlBytes. It
// must flag a SUPERSET of what the encoder rewrites: the C0 controls
// and DEL, plus every non-ASCII byte (which may open an unsafe rune or
// itself be a stray 8-bit C1 control). A clean ASCII string returns
// unchanged without entering the encoder; a shape the probe misses
// would be logged raw, so the superset rule is load-bearing — the
// earlier hand-written probe omitted most of the C0 range (SOH, EOT,
// FS, …) and those leaked.
func needsControlScrub(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r >= utf8.RuneSelf
	})
}

// scrubControlBytes percent-encodes every character that can forge,
// break, or reorder a rendered log line in a request-derived value, URL
// path, method, or a panic that embeds a request string: the C0
// controls and DEL, the C1 controls (U+0080–U+009F — the 8-bit CSI
// 0x9B and OSC 0x9D drive terminal escapes exactly as ESC-[ does, NEL
// 0x85 breaks the line), and the zero-width/bidi set from core/textsafe
// (RLO and friends visually rewrite the logged path). An attacker then
// can't forge a fake log entry, reorder one, or smuggle a
// terminal-control payload into an operator's tail/less session.
// slog's JSON handler escapes C0 for valid JSON but leaves C1/bidi
// runes raw (verified 2026-09-05: a raw C2 9B lands in the encoded
// line), and a JSON-escaped \r\n is still visible to text grep, with
// naive log shippers rendering the injected payload on its own line.
//
// r.URL.Path is percent-DECODED, so %0d%0a / %c2%9b / %e2%80%ae in the
// raw request are a real CRLF / U+009B / U+202E by the time they reach
// any sink here. Stray non-UTF-8 bytes in 0x80..0x9F are the 8-bit C1
// forms on the wire (a bare 0x9B from %9B) and are encoded like their
// rune counterparts; other invalid bytes pass through untouched.
func scrubControlBytes(s string) string {
	if !needsControlScrub(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 {
			// Stray invalid byte: in 0x80..0x9F it is an 8-bit
			// C1 control on the wire, encode it.
			if c <= 0x9f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if textsafe.IsUnsafe(r) {
			for j := i; j < i+size; j++ {
				fmt.Fprintf(&b, "%%%02x", s[j])
			}
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// safeLogMethod percent-encodes control bytes (and DEL) in the HTTP method.
func safeLogMethod(m string) string { return scrubControlBytes(m) }

// safeLogPath percent-encodes control characters in a URL path.
func safeLogPath(p string) string { return scrubControlBytes(p) }

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

// Unwrap lets http.NewResponseController walk through this wrapper so
// SetReadDeadline/SetWriteDeadline reach the real connection instead of
// failing with a silent ErrNotSupported (a streaming handler that
// ignores that error then has no per-write deadline, and a half-open
// client pins its goroutine). Every variant below embeds this type, so
// the one method covers the whole family — matching the Unwrap the
// cors/metrics/tracing wrappers already carry.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// optional-interface wrappers; we conditionally expose just what the
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

// 8 combinations of (Flusher, Hijacker, Pusher); only the relevant
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
