package cache

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Vary       []string            `json:"vary,omitempty"`
	Generation string              `json:"generation,omitempty"`
}

var credentialHeaders = []string{"Authorization", "X-API-Key", "X-Gofastr-Embed", "Cookie"}

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
// buffer for caching (8 MiB).
const DefaultMaxCacheableBytes = 8 << 20

func CacheMiddleware(cache Cache, ttl time.Duration) func(http.Handler) http.Handler {
	return CacheMiddlewareWithLimit(cache, ttl, DefaultMaxCacheableBytes)
}

// CacheMiddlewareWithLimit is CacheMiddleware with an explicit cap on the
// response size that may be buffered for caching.
func CacheMiddlewareWithLimit(cache Cache, ttl time.Duration, maxBodyBytes int) func(http.Handler) http.Handler {
	if maxBodyBytes < 0 {
		panic(fmt.Sprintf("cache: CacheMiddlewareWithLimit: maxBodyBytes %d is negative (0 means unbounded)", maxBodyBytes))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("X-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				if !safeMethod(r.Method) {
					for _, k := range getKeys(r) {
						//gofastr:allow(discardmutator) the handler already answered; a failed eviction leaves the entry to its TTL, as before this eviction existed
						_ = cache.Delete(context.Background(), k)
					}
				}
				return
			}

			reqCC := parseCacheControl(allValues("Cache-Control", r.Header))
			reqNoCache := reqCC["no-cache"]
			reqNoStore := reqCC["no-store"]

			hasCreds := carriesCredential(r)

			isRange := r.Header.Get("Range") != ""
			isNavigate := r.Header.Get("X-Gofastr-Navigate") == "1"
			if isRange || isNavigate {
				w.Header().Set("X-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			baseKey := getKey(r)

			if !hasCreds && !reqNoCache && !reqNoStore {
				if cached := lookupStored(r.Context(), cache, r, baseKey); cached != nil {
					writeCached(w, cached, "HIT")
					return
				}
			}

			rec := &responseRecorder{
				header:     make(http.Header),
				body:       &bytes.Buffer{},
				statusCode: http.StatusOK,
				w:          w,
				maxBytes:   maxBodyBytes,
			}
			next.ServeHTTP(rec, r)

			if rec.overflow {
				return
			}

			varyHeader := strings.Join(append(w.Header().Values("Vary"), rec.header.Values("Vary")...), ",")
			responseTTL, fresh := originTTL(ttl, time.Now(), w.Header(), rec.header)
			storeable := fresh && isStoreable(rec, w.Header(), hasCreds, reqNoStore) && !varyHasStar(varyHeader)

			if storeable {
				vary := parseVary(varyHeader)
				headers := cloneHeader(rec.header)
				stored := cachedResponse{
					StatusCode: rec.statusCode,
					Headers:    headers,
					Body:       append([]byte(nil), rec.body.Bytes()...),
					Vary:       captureVariant(r, vary),
				}
				storeResponse(cache, baseKey, vary, stored, responseTTL)
			}

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

func lookupStored(ctx context.Context, cache Cache, r *http.Request, baseKey string) *cachedResponse {
	var stored cachedResponse
	if err := cache.Get(ctx, baseKey, &stored); err != nil {
		return nil
	}
	if stored.Generation != "" {
		key := variantKey(baseKey, stored.Generation, captureVariant(r, stored.Vary))
		var variant cachedResponse
		if err := cache.Get(ctx, key, &variant); err != nil {
			return nil
		}
		stored = variant
	}
	if stored.Generation != "" || stored.StatusCode == http.StatusNotModified ||
		stored.StatusCode < 200 || stored.StatusCode >= 400 {
		return nil
	}
	if !variantMatches(r, stored.Vary) {
		return nil
	}
	return &stored
}

type varyIndex struct {
	Vary       []string `json:"vary"`
	Generation string   `json:"generation"`
}

func storeResponse(cache Cache, baseKey string, vary []string, response cachedResponse, ttl time.Duration) {
	ctx := context.Background()
	if len(vary) == 0 {
		_ = cache.Set(ctx, baseKey, response, ttl)
		return
	}
	var index varyIndex
	if err := cache.Get(ctx, baseKey, &index); err != nil || index.Generation == "" {
		index.Generation = rand.Text()
	}
	index.Vary = vary
	if err := cache.Set(ctx, variantKey(baseKey, index.Generation, response.Vary), response, ttl); err != nil {
		return
	}
	_ = cache.Set(ctx, baseKey, index, ttl)
}

func variantKey(baseKey, generation string, variant []string) string {
	digest := sha256.New()
	for _, field := range variant {
		digest.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return baseKey + "|vary:" + generation + ":" + hex.EncodeToString(digest.Sum(nil))
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

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

func isStoreable(rec *responseRecorder, outer http.Header, hasCreds, reqNoStore bool) bool {
	if reqNoStore || hasCreds {
		return false
	}
	if rec.statusCode == http.StatusNotModified ||
		rec.statusCode < 200 || rec.statusCode >= 400 {
		return false
	}
	if rec.statusCode == http.StatusPartialContent || rec.header.Get("Content-Range") != "" {
		return false
	}
	if len(rec.header.Values("Set-Cookie")) > 0 || len(outer.Values("Set-Cookie")) > 0 {
		return false
	}
	cc := parseCacheControl(allValues("Cache-Control", outer, rec.header))
	if cc["private"] || cc["no-store"] || cc["no-cache"] {
		return false
	}
	if varyHasStar(allValues("Vary", rec.header)) {
		return false
	}
	return true
}

func originTTL(ttl time.Duration, now time.Time, headers ...http.Header) (time.Duration, bool) {
	if ttl < 0 {
		return ttl, false
	}
	limits := make(map[string]uint64)
	for part := range strings.SplitSeq(allValues("Cache-Control", headers...), ",") {
		name, value, assigned := strings.Cut(part, "=")
		name = asciicase.Lower(strings.TrimSpace(name))
		if name != "max-age" && name != "s-maxage" {
			continue
		}
		seconds, valid := deltaSeconds(strings.TrimSpace(value))
		if _, duplicate := limits[name]; duplicate || !assigned || !valid {
			return 0, false
		}
		limits[name] = seconds
	}
	lifetime, explicit := limits["s-maxage"]
	if !explicit {
		lifetime, explicit = limits["max-age"]
	}
	if !explicit {
		return ttl, allValues("Expires", headers...) == ""
	}
	var age uint64
	if raw := allValues("Age", headers...); raw != "" {
		var valid bool
		age, valid = deltaSeconds(strings.TrimSpace(raw))
		if !valid {
			return 0, false
		}
	}
	if raw := allValues("Date", headers...); raw != "" {
		date, err := http.ParseTime(raw)
		if err != nil {
			return 0, false
		}
		if elapsed := now.Sub(date); elapsed > 0 {
			apparent := uint64(elapsed / time.Second)
			if elapsed%time.Second != 0 {
				apparent++
			}
			age = max(age, apparent)
		}
	}
	if age >= lifetime {
		return 0, false
	}
	remaining := lifetime - age
	const maxSeconds = uint64((1<<63 - 1) / time.Second)
	bounded := time.Duration(min(remaining, maxSeconds)) * time.Second
	if ttl == 0 || bounded < ttl {
		ttl = bounded
	}
	return ttl, ttl > 0
}

func deltaSeconds(raw string) (uint64, bool) {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	if raw == "" {
		return 0, false
	}
	for i := range len(raw) {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(raw, 10, 64)
	return seconds, err == nil
}

func allValues(name string, hs ...http.Header) string {
	var vals []string
	for _, h := range hs {
		vals = append(vals, h.Values(name)...)
	}
	return strings.Join(vals, ",")
}

func varyHasStar(v string) bool {
	for p := range strings.SplitSeq(v, ",") {
		if strings.TrimSpace(p) == "*" {
			return true
		}
	}
	return false
}

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
		out[asciicase.Lower(part)] = true
	}
	return out
}

func parseVary(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
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

func headerFieldValue(r *http.Request, name string) string {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return ""
	}
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = strings.ReplaceAll(v, "\x00", "\\0")
	}
	return strings.Join(escaped, "\x00")
}

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

type responseRecorder struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
	wrote      bool

	w        http.ResponseWriter
	maxBytes int
	overflow bool
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) WriteHeader(code int) {
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
		r.streamThrough()
		return r.w.Write(b)
	}
	return r.body.Write(b)
}

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
	r.body.Reset()
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
	conn, rw, err := http.NewResponseController(r.w).Hijack()
	if err == nil {
		r.overflow = true
	}
	return conn, rw, err
}

// Unwrap lets http.ResponseController reach the client writer's
// deadlines; Flush and Hijack stop at the methods above.
func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.w }
