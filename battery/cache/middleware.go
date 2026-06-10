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
//   - Requests carrying a Range header are passed through untouched and
//     never cached: the key does not encode the Range, so a partial
//     (206) body must not be allowed to collide with the full variant.
//   - The cache key includes the request authority (Host) so distinct
//     virtual hosts sharing one Cache do not serve each other's content.
//   - Requests with Authorization or Cookie headers are not served from
//     or written to the cache by default (RFC 9111 §3.5 / §3 default).
//   - Requests with Cache-Control: no-cache or no-store bypass the
//     stored variant and force a fresh origin fetch (no-store also
//     skips writing the response back into the cache).
//   - Responses with Set-Cookie or with Cache-Control containing
//     private, no-store, or no-cache are never stored.
//   - Responses with non-2xx/3xx status (i.e. 4xx and 5xx) are never
//     stored. Partial-content (206) responses, or any response carrying
//     a Content-Range header, are likewise never stored.
//   - Responses carrying a Vary header are stored under a key that
//     includes the values of every listed request header so different
//     variants do not collide. A Vary: * response is treated as
//     uncacheable and is never stored (RFC 9111 §4.1).
//
// DefaultMaxCacheableBytes caps how large a response CacheMiddleware will
// buffer for caching (8 MiB). Larger responses stream straight to the client
// and are not cached, so a single huge response can't pin unbounded memory.
const DefaultMaxCacheableBytes = 8 << 20

func CacheMiddleware(cache Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return CacheMiddlewareWithLimit(cache, ttl, DefaultMaxCacheableBytes)
}

// CacheMiddlewareWithLimit is CacheMiddleware with an explicit cap on the
// response size that may be buffered for caching. A response exceeding
// maxBodyBytes is streamed to the client and never stored. Pass 0 for
// unbounded buffering (the pre-cap behaviour — not recommended in production).
func CacheMiddlewareWithLimit(cache Cache, ttl time.Duration, maxBodyBytes int) func(http.Handler) http.Handler {
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

			// Range requests are partial-content requests. The cache key does
			// not encode the Range, so serving from or writing to the cache
			// for a Range request would let a truncated 206 body collide with
			// (and poison) the full-document variant. Pass them straight
			// through, untouched and uncached.
			isRange := r.Header.Get("Range") != ""
			if isRange {
				next.ServeHTTP(w, r)
				return
			}

			// Include the request authority (Host) in the key so distinct
			// virtual hosts / tenants sharing one Cache do not collide on the
			// same method+path+query.
			baseKey := r.Method + ":" + r.Host + ":" + r.URL.Path + "?" + r.URL.RawQuery

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

			// Cache miss — capture the response into a buffer (not the client),
			// up to maxBodyBytes. Beyond that the recorder streams straight to
			// the client and marks overflow.
			rec := &responseRecorder{
				header:     make(http.Header),
				body:       &bytes.Buffer{},
				statusCode: http.StatusOK,
				w:          w,
				maxBytes:   maxBodyBytes,
			}
			next.ServeHTTP(rec, r)

			// Overflowed the cap: the response was already streamed to the
			// client and is too big to cache. Nothing to store or replay.
			if rec.overflow {
				return
			}

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
	// Partial-content responses are not full representations and would
	// poison the full-document variant under the shared key.
	if rec.statusCode == http.StatusPartialContent || rec.header.Get("Content-Range") != "" {
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
	// Vary: * declares the response varies on factors not in the request
	// and must never be reused (RFC 9111 §4.1). Refuse to store it.
	if varyHasStar(rec.header.Get("Vary")) {
		return false
	}
	return true
}

// varyHasStar reports whether a Vary header value contains the "*" token,
// which per RFC 9111 §4.1 marks the response as uncacheable.
func varyHasStar(v string) bool {
	for _, p := range strings.Split(v, ",") {
		if strings.TrimSpace(p) == "*" {
			return true
		}
	}
	return false
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
		p = strings.TrimSpace(p)
		// "*" is uncacheable (handled by isStoreable / varyHasStar) and
		// must not become a stored variant dimension.
		if p == "*" {
			continue
		}
		p = http.CanonicalHeaderKey(p)
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

// responseRecorder captures an HTTP response for caching. It buffers up to
// maxBytes; if the response grows past that, it stops buffering, flushes what
// it has straight to the client, and streams the rest through (overflow=true).
// This bounds memory: a pathological multi-GB response can't be fully buffered
// just to discover it's too big to cache.
type responseRecorder struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
	wrote      bool

	w        http.ResponseWriter // client; written to only on/after overflow
	maxBytes int                 // 0 = unbounded buffering
	overflow bool                // true once we gave up buffering and streamed
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
	if r.overflow {
		return r.w.Write(b)
	}
	if r.maxBytes > 0 && r.body.Len()+len(b) > r.maxBytes {
		// Over the cap — give up on caching this response and switch to
		// streaming. Flush headers + already-buffered bytes to the client,
		// then write this chunk and everything after straight through.
		r.overflow = true
		dst := r.w.Header()
		for k, vals := range r.header {
			for _, v := range vals {
				dst.Add(k, v)
			}
		}
		dst.Set("X-Cache", "MISS")
		r.w.WriteHeader(r.statusCode)
		if r.body.Len() > 0 {
			_, _ = r.w.Write(r.body.Bytes())
		}
		r.body.Reset() // release the buffered memory
		return r.w.Write(b)
	}
	return r.body.Write(b)
}
