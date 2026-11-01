package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/net/http/httpguts"
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

	// AllowAnyHeader answers a preflight from an allowed origin with the
	// headers it asked for (Access-Control-Request-Headers) instead of
	// AllowedHeaders, for a browser client that sends headers the server
	// does not list one by one (X-Request-Tag, tracing headers). Only
	// well-formed header names are echoed, at most 64 of them. A
	// rejected origin still gets nothing.
	AllowAnyHeader bool

	// AllowAnyMethod answers a preflight from an allowed origin with the
	// method it asked for (Access-Control-Request-Method) instead of
	// AllowedMethods, when that method is a well-formed token.
	AllowAnyMethod bool
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
			} else {
				// Under an allow-list the response depends on Origin whether
				// or not it matched, so every response says so: a shared
				// cache that stored the no-ACAO variant must not replay it
				// to an allowed origin. Add, not Set: an upstream middleware
				// may already have written Vary (compression writes
				// Accept-Encoding).
				w.Header().Add("Vary", "Origin")
				if origin != "" && originSet[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					allowed = true
				}
			}

			// SECURITY: only emit Allow-Methods / Allow-Headers when the
			// origin is allowed. Echoing them to rejected origins leaks
			// API metadata and makes blocked preflights look successful.
			if allowed {
				m, h := methods, headers
				if cfg.AllowAnyMethod {
					w.Header().Add("Vary", "Access-Control-Request-Method")
					if req := requestedMethod(r); req != "" {
						m = req
					}
				}
				if cfg.AllowAnyHeader {
					w.Header().Add("Vary", "Access-Control-Request-Headers")
					if req := requestedHeaders(r); req != "" {
						h = req
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", m)
				w.Header().Set("Access-Control-Allow-Headers", h)
			}

			// A preflight is OPTIONS carrying Origin and
			// Access-Control-Request-Method. Any other OPTIONS (curl, WebDAV,
			// a gateway probe) is the app's own request and reaches next.
			preflight := r.Method == http.MethodOptions && origin != "" &&
				r.Header.Get("Access-Control-Request-Method") != ""
			if preflight {
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
			// responses; browsers reject the combo. Defer the final check
			// so it also runs while a panicking handler unwinds to Recovery.
			sw := stripCredsWriter{ResponseWriter: w}
			defer sw.stripCredentialsForWildcard()
			next.ServeHTTP(sw, r)
		})
	}
}

// stripCredsWriter prevents Access-Control-Allow-Credentials from being
// emitted when the response's actual ACAO value is "*". A downstream handler
// can replace the value set from config, so the emitted header is the only
// reliable gate.
//
// It forwards Flush and Hijack like every other wrapper in this package
// (metrics, logging, tracing, timeout). It did not, so behind a wildcard CORS
// policy the SSE bus lost its Flusher, and the framework's SSE constructor
// type-asserts http.Flusher, making that a hard failure rather than degraded
// buffering, while a WebSocket upgrade lost its Hijacker.
type stripCredsWriter struct {
	http.ResponseWriter
}

func (s stripCredsWriter) stripCredentialsForWildcard() {
	if s.ResponseWriter.Header().Get("Access-Control-Allow-Origin") == "*" {
		s.ResponseWriter.Header().Del("Access-Control-Allow-Credentials")
	}
}

func (s stripCredsWriter) WriteHeader(code int) {
	s.stripCredentialsForWildcard()
	s.ResponseWriter.WriteHeader(code)
}

func (s stripCredsWriter) Write(b []byte) (int, error) {
	s.stripCredentialsForWildcard()
	return s.ResponseWriter.Write(b)
}

// Flush strips before flushing: a streaming handler that sets the header and
// flushes before its first Write would otherwise commit it to the wire.
func (s stripCredsWriter) Flush() {
	s.stripCredentialsForWildcard()
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer so a WebSocket upgrade behind a
// wildcard CORS policy still works. Once hijacked the connection is raw and
// this wrapper no longer mediates it, which is correct: there are no HTTP
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
// maxEchoedHeaders caps how many requested header names a preflight
// answer echoes.
const maxEchoedHeaders = 64

// requestedHeaders is the preflight's Access-Control-Request-Headers
// reduced to well-formed header names, lowercased and de-duplicated, at
// most maxEchoedHeaders of them: never a byte that could split the
// response header.
func requestedHeaders(r *http.Request) string {
	var out []string
	seen := map[string]bool{}
	for _, line := range r.Header.Values("Access-Control-Request-Headers") {
		for _, name := range strings.Split(line, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" || seen[name] || !httpguts.ValidHeaderFieldName(name) {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) == maxEchoedHeaders {
				return strings.Join(out, ", ")
			}
		}
	}
	return strings.Join(out, ", ")
}

// requestedMethod is the preflight's Access-Control-Request-Method when
// it is a well-formed method token, else "".
func requestedMethod(r *http.Request) string {
	m := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
	if m == "" || !httpguts.ValidHeaderFieldName(m) {
		return ""
	}
	return m
}

func sanitizeCORSTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		clean := textsafe.SanitizeControlBytes(t)
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}
