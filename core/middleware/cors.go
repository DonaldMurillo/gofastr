package middleware

import (
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
		methods = strings.Join(cfg.AllowedMethods, ", ")
	}

	headers := "Content-Type, Authorization"
	if len(cfg.AllowedHeaders) > 0 {
		headers = strings.Join(cfg.AllowedHeaders, ", ")
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
				w.Header().Set("Vary", "Origin")
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
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// stripCredsWriter prevents Access-Control-Allow-Credentials from being
// emitted by a downstream handler when the configured ACAO is "*".
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
