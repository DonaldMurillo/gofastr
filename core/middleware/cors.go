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
func CORS(cfg CORSConfig) Middleware {
	origins := "*"
	if len(cfg.AllowedOrigins) > 0 {
		origins = strings.Join(cfg.AllowedOrigins, ", ")
	}

	methods := "GET, POST, PUT, DELETE, PATCH, OPTIONS"
	if len(cfg.AllowedMethods) > 0 {
		methods = strings.Join(cfg.AllowedMethods, ", ")
	}

	headers := "Content-Type, Authorization"
	if len(cfg.AllowedHeaders) > 0 {
		headers = strings.Join(cfg.AllowedHeaders, ", ")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origins)
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
