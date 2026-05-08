package cache

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// cachedResponse holds the serialized HTTP response for caching.
type cachedResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
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
				for k, v := range cached.Headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(cached.StatusCode)
				w.Write(cached.Body)
				return
			}

			// Cache miss — capture the response into a buffer (not the client).
			rec := &responseRecorder{
				header:     make(http.Header),
				body:       &bytes.Buffer{},
				statusCode: http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			// Store in cache (best-effort, ignore errors).
			headers := make(map[string]string, len(rec.header))
			for k, vals := range rec.header {
				if len(vals) > 0 {
					headers[k] = vals[0]
				}
			}
			resp := cachedResponse{
				StatusCode: rec.statusCode,
				Headers:    headers,
				Body:       rec.body.Bytes(),
			}
			_ = cache.Set(context.Background(), cacheKey, resp, ttl)

			// Replay the captured response to the client.
			w.Header().Set("X-Cache", "MISS")
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(rec.statusCode)
			w.Write(rec.body.Bytes())
		})
	}
}

// responseRecorder captures an HTTP response without writing to the client.
type responseRecorder struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
	wrote      bool
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(b)
}
