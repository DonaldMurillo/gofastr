package cache

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// cachedResponse holds the serialized HTTP response for caching.
type cachedResponse struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
}

// CacheMiddleware returns an HTTP middleware that caches GET responses
// using the provided Cache implementation. The X-Cache response header
// is set to "HIT" or "MISS" accordingly.
func CacheMiddleware(cache Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET requests.
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			cacheKey := r.Method + ":" + r.URL.Path + "?" + r.URL.RawQuery

			// Try to fetch from cache.
			var cached cachedResponse
			if err := cache.Get(r.Context(), cacheKey, &cached); err == nil {
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(cached.StatusCode)
				w.Write(cached.Body)
				return
			}

			// Cache miss — capture the response.
			rec := &responseRecorder{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
				statusCode:     http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			// Store in cache (best-effort, ignore errors).
			resp := cachedResponse{
				StatusCode: rec.statusCode,
				Body:       rec.body.Bytes(),
			}
			_ = cache.Set(context.Background(), cacheKey, resp, ttl)

			w.Header().Set("X-Cache", "MISS")
			// The response has already been written by ServeHTTP via rec.
		})
	}
}

// responseRecorder wraps an http.ResponseWriter to capture the response body
// and status code.
type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	wrote      bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
