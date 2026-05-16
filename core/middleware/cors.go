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
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" && originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}

			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
