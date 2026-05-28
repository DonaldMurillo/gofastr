package cache

import (
	"bytes"
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

// cachedResponse holds the serialized HTTP response for caching.
type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Vary       []string            `json:"vary,omitempty"`
}

// CacheMiddleware returns an HTTP middleware that caches GET responses
// using the provided Cache implementation. The X-Cache response header
// is set to "HIT" or "MISS" accordingly.
//
// The middleware is conservative and refuses to cache responses that
// carry user-specific data or that the origin marks as uncacheable:
//
//   - Requests other than GET are passed through untouched.
//   - Requests with Authorization or Cookie headers are not served from
//     or written to the cache by default (RFC 9111 §3.5 / §3 default).
//   - Requests with Cache-Control: no-cache or no-store bypass the
//     stored variant and force a fresh origin fetch (no-store also
//     skips writing the response back into the cache).
//   - Responses with Set-Cookie or with Cache-Control containing
//     private, no-store, or no-cache are never stored.
//   - Responses with non-2xx/3xx status (i.e. 4xx and 5xx) are never
//     stored.
//   - Responses carrying a Vary header are stored under a key that
//     includes the values of every listed request header so different
//     variants do not collide.
func CacheMiddleware(cache Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET requests.
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			reqCC := parseCacheControl(r.Header.Get("Cache-Control"))
			reqNoCache := reqCC["no-cache"]
			reqNoStore := reqCC["no-store"]

			// Requests that carry credentials are not cached by default.
			// This avoids replaying one user's response to another.
			hasCreds := r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != ""

			baseKey := r.Method + ":" + r.URL.Path + "?" + r.URL.RawQuery

			// Try to fetch from cache only if we're allowed to serve a
			// stored variant for this request.
			if !hasCreds && !reqNoCache && !reqNoStore {
				var cached cachedResponse
				if err := cache.Get(r.Context(), baseKey, &cached); err == nil {
					if variantMatches(r, cached.Vary) {
						writeCached(w, &cached, "HIT")
						return
					}
				}
			}

			// Cache miss — capture the response into a buffer (not the client).
			rec := &responseRecorder{
				header:     make(http.Header),
				body:       &bytes.Buffer{},
				statusCode: http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			storeable := isStoreable(rec, hasCreds, reqNoStore)

			if storeable {
				vary := parseVary(rec.header.Get("Vary"))
				headers := cloneHeader(rec.header)
				// Bake the request's variant headers into the stored
				// entry so we can verify on read.
				stored := cachedResponse{
					StatusCode: rec.statusCode,
					Headers:    headers,
					Body:       append([]byte(nil), rec.body.Bytes()...),
					Vary:       captureVariant(r, vary),
				}
				_ = cache.Set(context.Background(), baseKey, stored, ttl)
			}

			// Replay the captured response to the client.
			w.Header().Set("X-Cache", "MISS")
			for k, vals := range rec.header {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(rec.statusCode)
			w.Write(rec.body.Bytes())
		})
	}
}

// writeCached writes a previously cached response to the client.
func writeCached(w http.ResponseWriter, cached *cachedResponse, cacheStatus string) {
	for k, vals := range cached.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache", cacheStatus)
	w.WriteHeader(cached.StatusCode)
	w.Write(cached.Body)
}

// isStoreable decides whether a recorded response is safe to cache.
func isStoreable(rec *responseRecorder, hasCreds, reqNoStore bool) bool {
	if reqNoStore || hasCreds {
		return false
	}
	// Only 2xx and 3xx responses are stored; never 4xx/5xx.
	if rec.statusCode < 200 || rec.statusCode >= 400 {
		return false
	}
	// Set-Cookie responses are inherently user-specific.
	if len(rec.header.Values("Set-Cookie")) > 0 {
		return false
	}
	cc := parseCacheControl(rec.header.Get("Cache-Control"))
	if cc["private"] || cc["no-store"] || cc["no-cache"] {
		return false
	}
	return true
}

// parseCacheControl returns a set of directive names from a
// Cache-Control header value (lowercased, parameter values stripped).
func parseCacheControl(v string) map[string]bool {
	out := map[string]bool{}
	if v == "" {
		return out
	}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.IndexByte(part, '='); i >= 0 {
			part = part[:i]
		}
		out[strings.ToLower(part)] = true
	}
	return out
}

// parseVary returns the canonicalised list of header names from a
// Vary response header (excluding "*", which is uncacheable but
// handled separately).
func parseVary(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = http.CanonicalHeaderKey(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// captureVariant produces a flat list of "header:value" pairs that
// uniquely identifies the request variant under a Vary policy.
func captureVariant(r *http.Request, vary []string) []string {
	if len(vary) == 0 {
		return nil
	}
	out := make([]string, 0, len(vary))
	for _, h := range vary {
		out = append(out, h+":"+r.Header.Get(h))
	}
	return out
}

// variantMatches checks whether a request's variant headers match
// those recorded on the cached entry.
func variantMatches(r *http.Request, vary []string) bool {
	for _, hv := range vary {
		i := strings.IndexByte(hv, ':')
		if i < 0 {
			return false
		}
		name, want := hv[:i], hv[i+1:]
		if r.Header.Get(name) != want {
			return false
		}
	}
	return true
}

func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
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
