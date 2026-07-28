package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
)

// CORSConfig holds configuration for the CORS middleware.
type CORSConfig struct {
	// AllowedOrigins is the list of allowed origin patterns.
	// Use "*" to allow all origins.
	AllowedOrigins []string

	// AllowedMethods is the list of allowed HTTP methods.
	// Defaults to GET, POST, PUT, DELETE, PATCH, OPTIONS if empty.
	AllowedMethods []string

	// AllowedHeaders is the list of allowed request headers.
	AllowedHeaders []string
}

// CORS returns middleware that adds CORS headers to responses.
// It handles preflight OPTIONS requests by returning 204 with the
// appropriate headers.
// When multiple AllowedOrigins are configured, the request's Origin
// header is matched against the list and the matching origin is echoed
// back (Access-Control-Allow-Origin only accepts a single value).
func CORS(cfg CORSConfig) Middleware {
	methods := "GET, POST, PUT, DELETE, PATCH, OPTIONS"
	if len(cfg.AllowedMethods) > 0 {
		methods = strings.Join(sanitizeCORSTokens(cfg.AllowedMethods), ", ")
	}

	headers := "Content-Type, Authorization"
	if len(cfg.AllowedHeaders) > 0 {
		headers = strings.Join(sanitizeCORSTokens(cfg.AllowedHeaders), ", ")
	}

	// Build a set of allowed origins for O(1) lookup.
	// SECURITY: empty AllowedOrigins means deny-all (not allow-all).
	// Callers must explicitly set ["*"] or specific origins.
	allowAll := false
	originSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				allowed = true
			} else if origin != "" && originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// Add, not Set: an upstream middleware may already have
				// written Vary (compression writes Accept-Encoding), and
				// clobbering it makes a shared cache serve one variant to
				// clients that cannot read it.
				w.Header().Add("Vary", "Origin")
				allowed = true
			}

			// SECURITY: only emit Allow-Methods / Allow-Headers when the
			// origin is allowed. Echoing them to rejected origins leaks
			// API metadata and makes blocked preflights look successful.
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				if !allowed {
					// SECURITY: rejected-origin preflight must fail
					// outright rather than appearing to succeed.
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// SECURITY: wildcard ACAO is incompatible with credentialed
			// responses — browsers reject the combo. Strip the header so
			// a downstream handler can't accidentally enable it.
			if allowAll {
				next.ServeHTTP(stripCredsWriter{ResponseWriter: w}, r)
				// A handler that sets the header and returns without
				// writing reaches none of the wrapper's strip points:
				// net/http's implicit WriteHeader(200) runs on the real
				// response object, not on the wrapper. Nothing has been
				// committed to the wire yet at this point, so a final
				// strip closes that gap. Harmless when the handler did
				// write — the header map was already cleaned then.
				w.Header().Del("Access-Control-Allow-Credentials")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// stripCredsWriter prevents Access-Control-Allow-Credentials from being
// emitted by a downstream handler when the configured ACAO is "*". Browsers
// reject that combination, and a handler that sets it is asking for a
// credentialed cross-origin read.
//
// It forwards Flush and Hijack like every other wrapper in this package
// (metrics, logging, tracing, timeout). It did not, so behind a wildcard CORS
// policy the SSE bus lost its Flusher — and the framework's SSE constructor
// type-asserts http.Flusher, making that a hard failure rather than degraded
// buffering — while a WebSocket upgrade lost its Hijacker.
type stripCredsWriter struct {
	http.ResponseWriter
}

func (s stripCredsWriter) WriteHeader(code int) {
	s.ResponseWriter.Header().Del("Access-Control-Allow-Credentials")
	s.ResponseWriter.WriteHeader(code)
}

func (s stripCredsWriter) Write(b []byte) (int, error) {
	s.ResponseWriter.Header().Del("Access-Control-Allow-Credentials")
	return s.ResponseWriter.Write(b)
}

// Flush strips before flushing: a streaming handler that sets the header and
// flushes before its first Write would otherwise commit it to the wire.
func (s stripCredsWriter) Flush() {
	s.ResponseWriter.Header().Del("Access-Control-Allow-Credentials")
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer so a WebSocket upgrade behind a
// wildcard CORS policy still works. Once hijacked the connection is raw and
// this wrapper no longer mediates it — which is correct: there are no HTTP
// response headers left to strip.
func (s stripCredsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("middleware: underlying ResponseWriter does not implement http.Hijacker")
}

// Unwrap lets http.ResponseController reach the original writer for
// capabilities this wrapper does not name explicitly.
func (s stripCredsWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// sanitizeCORSTokens strips bytes that could terminate the Allow-Methods
// or Allow-Headers value and smuggle a second header line. The CORS
// config is developer-supplied, but defense-in-depth against config
// injection (env-driven lists, template-generated configs) is cheap.
func sanitizeCORSTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		clean := stripCtrlBytes(t)
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func stripCtrlBytes(s string) string {
	if !containsCtrl(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func containsCtrl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
