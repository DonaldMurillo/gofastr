package cache

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// cachedResponse holds the serialized HTTP response for caching.
type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Vary       []string            `json:"vary,omitempty"`
}

// credentialHeaders are the request headers the framework treats as a
// caller's credential. A request carrying any of them is never served
// from or written to the cache by default. The list matches
// credentialFingerprint in framework/app.go; TestCacheCredentialCanon
// holds the two together.
//
// X-Gofastr-Embed is an embed grant: framework/embed's middleware resolves
// it into a user before the handler runs, and it carries no cookie and no
// Authorization by construction, so a check on those two alone stored a
// grant-authenticated response under the shared method/host/path/query key
// and served it to another grant subject as a HIT. X-API-Key had the same
// hole for API-key callers.
var credentialHeaders = []string{"Authorization", "X-API-Key", "X-Gofastr-Embed", "Cookie"}

// carriesCredential reports whether r carries a non-empty value of any
// credential header. Every field line counts: Header.Get reads only the
// first, and an empty first line would hide a credential on the second.
func carriesCredential(r *http.Request) bool {
	for _, name := range credentialHeaders {
		for _, v := range r.Header.Values(name) {
			if v != "" {
				return true
			}
		}
	}
	return false
}

// DefaultMaxCacheableBytes caps how large a response CacheMiddleware will
// buffer for caching (8 MiB). Larger responses stream straight to the client
// and are not cached, so a single huge response can't pin unbounded memory.
const DefaultMaxCacheableBytes = 8 << 20

// CacheMiddleware returns an HTTP middleware that caches GET responses
// using the provided Cache implementation. The X-Cache response header
// is set to "HIT" or "MISS" accordingly.
//
// The middleware is conservative and refuses to cache responses that
// carry user-specific data or that the origin marks as uncacheable:
//
//   - Requests other than GET are passed through untouched. After an
//     unsafe one (any method but GET, HEAD, OPTIONS and TRACE) runs,
//     the stored GET of its URL is evicted on every locale (RFC 9111
//     §4.4), whatever the status.
//   - Requests carrying a Range header are passed through untouched and
//     never cached: the key does not encode the Range, so a partial
//     (206) body must not be allowed to collide with the full variant.
//   - The cache key includes the request authority (Host) so distinct
//     virtual hosts sharing one Cache do not serve each other's content.
//   - Requests with a credential header (Authorization, Cookie,
//     X-API-Key or X-Gofastr-Embed) are not served from or written to
//     the cache by default (RFC 9111 §3.5 / §3 default).
//   - Requests with Cache-Control: no-cache or no-store bypass the
//     stored variant and force a fresh origin fetch (no-store also
//     skips writing the response back into the cache).
//   - Responses with Set-Cookie or with Cache-Control containing
//     private, no-store, or no-cache are never stored.
//   - Responses with non-2xx/3xx status (i.e. 4xx and 5xx) are never
//     stored, and so are 304 responses: a 304 is a conditional-reply
//     artifact, not a representation, and the key encodes no conditional
//     headers — a stored 304 would be replayed to every unconditional
//     GET as an empty-body HIT (RFC 9111 §4.3.1). Partial-content (206)
//     responses, or any response carrying a Content-Range header, are
//     likewise never stored.
//   - Responses carrying a Vary header are stored under a key that
//     includes the values of every listed request header so different
//     variants do not collide. A Vary: * response is treated as
//     uncacheable and is never stored (RFC 9111 §4.1).

func CacheMiddleware(cache Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return CacheMiddlewareWithLimit(cache, ttl, DefaultMaxCacheableBytes)
}

// CacheMiddlewareWithLimit is CacheMiddleware with an explicit cap on the
// response size that may be buffered for caching. A response exceeding
// maxBodyBytes is streamed to the client and never stored. Pass 0 for
// unbounded buffering (the pre-cap behaviour, not recommended in production).
func CacheMiddlewareWithLimit(cache Cache, ttl time.Duration, maxBodyBytes int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET requests. An unsafe method invalidates the
			// stored GET of its target URI (RFC 9111 §4.4) once the
			// handler ran: after a DELETE or PUT the anonymous GET kept
			// answering HIT with the old body until the TTL.
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				if !safeMethod(r.Method) {
					for _, k := range getKeys(r) {
						//gofastr:allow(discardmutator) the handler already answered; a failed eviction leaves the entry to its TTL, as before this eviction existed
						_ = cache.Delete(context.Background(), k)
					}
				}
				return
			}

			// Every field line counts: Header.Get reads only the first,
			// so "max-age=0" then "no-store" on two lines read as
			// max-age alone.
			reqCC := parseCacheControl(allValues("Cache-Control", r.Header))
			reqNoCache := reqCC["no-cache"]
			reqNoStore := reqCC["no-store"]

			// Requests that carry credentials are not cached by default.
			// This avoids replaying one user's response to another.
			hasCreds := carriesCredential(r)

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

			baseKey := getKey(r)

			// Try to fetch from cache only if we're allowed to serve a
			// stored variant for this request.
			if !hasCreds && !reqNoCache && !reqNoStore {
				var cached cachedResponse
				if err := cache.Get(r.Context(), baseKey, &cached); err == nil {
					// The replay side of the 304 refusal: an entry primed by
					// a pre-fix process on a shared backend (Redis) must
					// not be served either — fall through to the origin
					// and re-store a real representation.
					if cached.StatusCode != http.StatusNotModified && variantMatches(r, cached.Vary) {
						writeCached(w, &cached, "HIT")
						return
					}
				}
			}

			// Cache miss, capture the response into a buffer (not the client),
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

			// Overflowed the cap, flushed or hijacked: the response was
			// already streamed to the client. Nothing to store or replay.
			if rec.overflow {
				return
			}

			// The response varies on what the handler declared AND on
			// what middleware outside this one already set on w: the
			// i18n negotiation runs first and adds Vary: Accept-Language,
			// which the recorder never sees. Every Vary line counts.
			varyHeader := strings.Join(append(w.Header().Values("Vary"), rec.header.Values("Vary")...), ",")
			storeable := isStoreable(rec, w.Header(), hasCreds, reqNoStore) && !varyHasStar(varyHeader)

			if storeable {
				vary := parseVary(varyHeader)
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

			// Replay the captured response to the client. Nothing has
			// reached w yet: a response that did (overflow, a flush, a
			// hijack) returned above.
			//gofastr:allow(wrapwriter) the recorder buffered the whole response; w is unwritten until here
			w.Header().Set("X-Cache", "MISS")
			for k, vals := range rec.header {
				for _, v := range vals {
					//gofastr:allow(wrapwriter) the recorder buffered the whole response; w is unwritten until here
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(rec.statusCode)
			w.Write(rec.body.Bytes())
		})
	}
}

// getKey is the key r's GET is stored under. It includes the request
// authority (Host) so distinct virtual hosts / tenants sharing one Cache
// do not collide on the same path+query. The path is the escaped one:
// /a%2Fb (one segment) and /a/b (two) are different pages. Under locale
// prefixes the router strips /es before any middleware runs, so
// /es/pricing and /pricing arrive as the same URL: the locale the path
// named keeps them apart.
func getKey(r *http.Request) string {
	k := keyPrefix(r)
	if _, tag, ok := i18n.PathLocale(r.Context()); ok {
		k += "#" + tag
	}
	return k
}

func keyPrefix(r *http.Request) string {
	return http.MethodGet + ":" + r.Host + ":" + r.URL.EscapedPath() + "?" + r.URL.RawQuery
}

// getKeys lists every key a GET of r's target URI can be stored under:
// one per locale of the path's locale set, since a change to the
// resource changes every locale's page of it.
func getKeys(r *http.Request) []string {
	set, _, ok := i18n.PathLocale(r.Context())
	if !ok {
		return []string{keyPrefix(r)}
	}
	keys := []string{keyPrefix(r) + "#" + set.Default}
	for _, tag := range set.Prefixed {
		keys = append(keys, keyPrefix(r)+"#"+tag)
	}
	return keys
}

// safeMethod reports whether method is safe in RFC 9110 §9.2.1's sense:
// it changes nothing, so it leaves stored responses alone.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
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
//
// outer is the header map middleware outside the cache already wrote on
// the real ResponseWriter. A session middleware that set Set-Cookie, or
// an auth layer that set Cache-Control: private, ran before the handler
// and its headers never reach the recorder, yet they are part of the
// response the client gets. Reading the recorder alone stored that
// response and replayed it to other callers.
func isStoreable(rec *responseRecorder, outer http.Header, hasCreds, reqNoStore bool) bool {
	if reqNoStore || hasCreds {
		return false
	}
	// Only 2xx and 3xx responses are stored; never 4xx/5xx. A 304 is
	// refused even though it sits inside that range: it is a
	// conditional-reply artifact, not a representation (RFC 9111
	// §4.3.1 — a cache MUST NOT generate a 304 unless the request is
	// conditional and its validators match), and the key encodes no
	// conditional headers. Storing one let a single conditional GET
	// prime the shared variant with {304, ""} and serve the empty
	// non-representation to every subsequent unconditional GET as a
	// HIT.
	if rec.statusCode == http.StatusNotModified ||
		rec.statusCode < 200 || rec.statusCode >= 400 {
		return false
	}
	// Partial-content responses are not full representations and would
	// poison the full-document variant under the shared key.
	if rec.statusCode == http.StatusPartialContent || rec.header.Get("Content-Range") != "" {
		return false
	}
	// Set-Cookie responses are inherently user-specific.
	if len(rec.header.Values("Set-Cookie")) > 0 || len(outer.Values("Set-Cookie")) > 0 {
		return false
	}
	// Every Cache-Control field line, outer and inner: Header.Get reads
	// only the first, so a no-store on a second line was missed.
	cc := parseCacheControl(allValues("Cache-Control", outer, rec.header))
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

// allValues joins every field line of name across hs with ",", the
// list form RFC 9110 §5.3 makes equivalent to repeated lines.
func allValues(name string, hs ...http.Header) string {
	var vals []string
	for _, h := range hs {
		vals = append(vals, h.Values(name)...)
	}
	return strings.Join(vals, ",")
}

// varyHasStar reports whether a Vary header value contains the "*" token,
// which per RFC 9111 §4.1 marks the response as uncacheable.
func varyHasStar(v string) bool {
	for p := range strings.SplitSeq(v, ",") {
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
	for part := range strings.SplitSeq(v, ",") {
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
//
// The value is the COMPLETE field, every value the request sent under
// that name, not Header.Get's first one. A header may repeat, and
// selecting on the first value alone made "X-Team: alpha" and
// "X-Team: alpha, omega" the same variant: whichever request populated
// the entry first decided what both of them received. Vary exists to
// keep exactly those two apart.
func captureVariant(r *http.Request, vary []string) []string {
	if len(vary) == 0 {
		return nil
	}
	out := make([]string, 0, len(vary))
	for _, h := range vary {
		out = append(out, h+":"+headerFieldValue(r, h))
	}
	return out
}

// headerFieldValue joins every value the request sent under name, in
// order, with a separator no single value can contain unescaped.
func headerFieldValue(r *http.Request, name string) string {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return ""
	}
	escaped := make([]string, len(values))
	for i, v := range values {
		// Escape the separator so ["a\x00b"] and ["a", "b"] cannot
		// collapse to the same string. A raw NUL in a header value is
		// already illegal, but the escape costs nothing and removes the
		// question.
		escaped[i] = strings.ReplaceAll(v, "\x00", "\\0")
	}
	return strings.Join(escaped, "\x00")
}

// variantMatches checks whether a request's variant headers match
// those recorded on the cached entry.
func variantMatches(r *http.Request, vary []string) bool {
	for _, hv := range vary {
		before, after, ok := strings.Cut(hv, ":")
		if !ok {
			return false
		}
		name, want := before, after
		if headerFieldValue(r, name) != want {
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
	// An informational status (103 Early Hints) is advisory and precedes
	// the final one, which a buffered response has not sent yet: drop it
	// rather than record it as the response.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
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
		// Over the cap, give up on caching this response and switch to
		// streaming, then write this chunk and everything after straight
		// through.
		r.streamThrough()
		return r.w.Write(b)
	}
	return r.body.Write(b)
}

// streamThrough gives up on caching: it sends the headers and the bytes
// buffered so far to the client and passes every later write through.
func (r *responseRecorder) streamThrough() {
	if r.overflow {
		return
	}
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
}

// FlushError is a streaming response's signal that its bytes must reach
// the client now, so the response streams through and is not cached.
func (r *responseRecorder) FlushError() error {
	r.streamThrough()
	return http.NewResponseController(r.w).Flush()
}

// Flush is FlushError for handlers that assert http.Flusher.
func (r *responseRecorder) Flush() { _ = r.FlushError() }

// Hijack hands the connection to the handler (a websocket upgrade); the
// response is the handler's from then on, and nothing is cached.
func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.overflow = true
	return http.NewResponseController(r.w).Hijack()
}

// Unwrap lets http.ResponseController reach the client writer's
// deadlines; Flush and Hijack stop at the methods above.
func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.w }
