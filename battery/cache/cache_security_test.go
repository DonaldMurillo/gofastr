package cache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cacheReplayTest drives the shared skeleton of responses that must not be
// cached: CacheMiddleware over a fresh MemoryCache around an origin whose
// body is "<bodyPrefix>-<hit>" (distinct per origin run, so a replayed prime
// is detectable). decorate may add response headers or cookies before the
// status is written. One prime GET to path, then a probe GET to the same path.
// It fails unless the probe bypassed the stored variant: no X-Cache: HIT and
// a body different from the prime.
func cacheReplayTest(t *testing.T, label string, decorate func(int, http.ResponseWriter), bodyPrefix, path string, status int) {
	t.Helper()
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if decorate != nil {
			decorate(int(n), w)
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(fmt.Sprintf("%s-%d", bodyPrefix, n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, path, nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, path, nil))

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] %s. body1=%q body2=%q", label, rec1.Body.String(), rec2.Body.String())
	}
}

// cacheHeaderPair drives the shared skeleton of Vary and credential-pair
// tests: CacheMiddleware over a fresh MemoryCache around an origin that
// echoes req.Header.Get(header) into its body. When setVary is true, the
// origin declares that header in Vary. When failOnHit is true, the
// credential-bearing request must run the origin instead of merely selecting
// a separate cached variant.
func cacheHeaderPair(t *testing.T, label, header, path, v1, v2, bodyPrefix string, setVary, failOnHit bool) {
	t.Helper()
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if setVary {
			w.Header().Set("Vary", header)
		}
		_, _ = w.Write([]byte(bodyPrefix + r.Header.Get(header)))
	}))

	req1 := httptest.NewRequest(http.MethodGet, path, nil)
	req1.Header.Set(header, v1)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, path, nil)
	req2.Header.Set(header, v2)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if (failOnHit && rec2.Header().Get("X-Cache") == "HIT") || rec2.Body.String() != bodyPrefix+v2 {
		t.Fatalf("SECURITY: [cache] %s: second response = %q (X-Cache=%q), want %q", label, rec2.Body.String(), rec2.Header().Get("X-Cache"), bodyPrefix+v2)
	}
}

// cacheRequestDirectiveTest drives the shared skeleton of request directives
// that bypass a stored response. It primes a cacheable response, then sends a
// second request with directive and requires a fresh body.
func cacheRequestDirectiveTest(t *testing.T, label, directive, bodyPrefix string) {
	t.Helper()
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("%s-%d", bodyPrefix, n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/refresh", nil))

	req2 := httptest.NewRequest(http.MethodGet, "/refresh", nil)
	req2.Header.Set("Cache-Control", directive)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] %s. body1=%q body2=%q", label, rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotCacheSetCookieResponses(t *testing.T) {
	cacheReplayTest(t, "response with Set-Cookie was cached and replayed", func(n int, w http.ResponseWriter) {
		http.SetCookie(w, &http.Cookie{Name: "session_id", Value: fmt.Sprintf("token-%d", n), Path: "/", HttpOnly: true})
	}, "request", "/account", http.StatusOK)
}

func TestCacheMiddleware_DoesNotCachePrivateResponses(t *testing.T) {
	cacheReplayTest(t, "Cache-Control: private response was cached and replayed", func(_ int, w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}, "private", "/profile", http.StatusOK)
}

func TestCacheMiddleware_DoesNotCacheNoStoreResponses(t *testing.T) {
	cacheReplayTest(t, "Cache-Control: no-store response was cached and replayed", func(_ int, w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "no-store")
	}, "nostore", "/billing", http.StatusOK)
}

func TestCacheMiddleware_HonorsVaryAuthorization(t *testing.T) {
	cacheHeaderPair(t, "cache key ignored Vary: Authorization and replayed another user's variant",
		"Authorization", "/me", "Bearer alice", "Bearer bob", "user=", true, false)
}

func TestCacheMiddleware_DoesNotCacheVaryStar(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		// Vary: * means the response varies on unstated factors and must
		// never be reused (RFC 9111 §4.1). Users are distinguished by a
		// non-credential header here so hasCreds stays false.
		w.Header().Set("Vary", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("personalized-%d-for-%s", n, r.Header.Get("X-User"))))
	}))

	// Distinct attack shapes: bare "*", "*" mixed with named headers,
	// and lowercase/spaced "*". Each is the same property at the surface.
	for _, varyVal := range []string{"*", "Accept-Language, *", " * "} {
		hits.Store(0)
		store = NewMemoryCache()
		handler = CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Vary", varyVal)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf("personalized-%d-for-%s", n, r.Header.Get("X-User"))))
		}))

		req1 := httptest.NewRequest(http.MethodGet, "/me", nil)
		req1.Header.Set("X-User", "alice")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		req2 := httptest.NewRequest(http.MethodGet, "/me", nil)
		req2.Header.Set("X-User", "bob")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if rec2.Header().Get("X-Cache") == "HIT" {
			t.Fatalf("SECURITY: [cache] Vary:%q response was cached and replayed cross-user (X-Cache=HIT)", varyVal)
		}
		if rec2.Body.String() == rec1.Body.String() {
			t.Fatalf("SECURITY: [cache] Vary:%q response replayed alice's body to bob: %q", varyVal, rec2.Body.String())
		}
	}
}

func TestCacheMiddleware_DoesNotCacheNoCacheResponses(t *testing.T) {
	cacheReplayTest(t, "Cache-Control: no-cache response was cached and replayed", func(_ int, w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "no-cache")
	}, "nocache", "/statement", http.StatusOK)
}

func TestCacheMiddleware_HonorsVaryCookie(t *testing.T) {
	cacheHeaderPair(t, "cache key ignored Vary: Cookie and replayed another session's variant",
		"Cookie", "/dashboard", "session=alice", "session=bob", "cookie=", true, false)
}

func TestCacheMiddleware_DoesNotCacheAuthorizationRequestsByDefault(t *testing.T) {
	cacheHeaderPair(t, "middleware cached Authorization-bearing request by default",
		"Authorization", "/me", "Bearer alice", "Bearer bob", "auth=", false, true)
}

func TestCacheMiddleware_DoesNotCacheCookieAuthenticatedRequestsByDefault(t *testing.T) {
	cacheHeaderPair(t, "middleware cached cookie-authenticated request by default",
		"Cookie", "/account", "session=alice", "session=bob", "cookie=", false, true)
}

func TestCacheMiddleware_DoesNotCacheServerErrors(t *testing.T) {
	cacheReplayTest(t, "500 response was cached and replayed", nil,
		"db-down", "/healthz", http.StatusInternalServerError)
}

func TestCacheMiddleware_HonorsVaryAcceptLanguage(t *testing.T) {
	cacheHeaderPair(t, "cache key ignored Vary: Accept-Language and replayed another locale's variant",
		"Accept-Language", "/landing", "en-US", "fr-FR", "lang=", true, false)
}

func TestCacheMiddleware_HonorsVaryOrigin(t *testing.T) {
	cacheHeaderPair(t, "cache key ignored Vary: Origin and replayed another origin's variant",
		"Origin", "/cors", "https://alice.example", "https://bob.example", "origin=", true, false)
}

func TestCacheMiddleware_RequestNoCacheBypassesStoredVariant(t *testing.T) {
	cacheRequestDirectiveTest(t, "request Cache-Control: no-cache did not bypass stored variant",
		"no-cache", "refresh")
}

func TestCacheMiddleware_RangeDoesNotPoisonFullGet(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	const full = "FULL-DOCUMENT-CONTENTS"
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if rng := r.Header.Get("Range"); rng != "" {
			// Emulate http.ServeContent's 206 Partial Content behaviour.
			w.Header().Set("Content-Range", "bytes 0-5/"+fmt.Sprint(len(full)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(full[:6]))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(full))
	}))

	// 1) Attacker primes the cache with a Range request -> 206 truncated body.
	reqRange := httptest.NewRequest(http.MethodGet, "/file", nil)
	reqRange.Header.Set("Range", "bytes=0-5")
	recRange := httptest.NewRecorder()
	handler.ServeHTTP(recRange, reqRange)

	// 2) Victim sends a plain full GET. It must NOT be served the cached 206.
	recFull := httptest.NewRecorder()
	handler.ServeHTTP(recFull, httptest.NewRequest(http.MethodGet, "/file", nil))

	if recFull.Code == http.StatusPartialContent || recFull.Body.String() != full {
		t.Fatalf("SECURITY: [cache] 206 Range response poisoned full GET. status=%d body=%q", recFull.Code, recFull.Body.String())
	}

	// 3) A subsequent identical Range request must also not get a HIT that
	// could leak a full body cached under the same bare key, and the
	// truncated 206 body must not be served as a HIT to non-Range GETs.
	recFull2 := httptest.NewRecorder()
	handler.ServeHTTP(recFull2, httptest.NewRequest(http.MethodGet, "/file", nil))
	if recFull2.Body.String() != full {
		t.Fatalf("SECURITY: [cache] later full GET served truncated body %q", recFull2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotLeakAcrossHosts(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("host=" + r.Host))
	}))

	// Anonymous GET /dashboard on tenant-a primes the cache.
	reqA := httptest.NewRequest(http.MethodGet, "http://tenant-a.app.com/dashboard", nil)
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)

	// Anonymous GET /dashboard on tenant-b must not be served tenant-a's body.
	reqB := httptest.NewRequest(http.MethodGet, "http://tenant-b.app.com/dashboard", nil)
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)

	if recB.Body.String() == "host=tenant-a.app.com" {
		t.Fatalf("SECURITY: [cache] cross-host leak: tenant-b served tenant-a's cached body %q (X-Cache=%s)", recB.Body.String(), recB.Header().Get("X-Cache"))
	}
	if recB.Body.String() != "host=tenant-b.app.com" {
		t.Fatalf("SECURITY: [cache] tenant-b got unexpected body %q", recB.Body.String())
	}

	// Same host repeated should still be cacheable (no regression).
	reqA2 := httptest.NewRequest(http.MethodGet, "http://tenant-a.app.com/dashboard", nil)
	recA2 := httptest.NewRecorder()
	handler.ServeHTTP(recA2, reqA2)
	if recA2.Body.String() != "host=tenant-a.app.com" {
		t.Fatalf("SECURITY: [cache] same-host caching regressed: %q", recA2.Body.String())
	}
}

func TestCacheMiddleware_RequestNoStoreBypassesStoredVariant(t *testing.T) {
	cacheRequestDirectiveTest(t, "request Cache-Control: no-store did not bypass stored variant",
		"no-store", "nostore-req")
}

// An embed grant is an app credential: framework/embed's middleware resolves it
// into a user before the handler runs. It deliberately travels without a cookie
// and without Authorization, that is the whole design, so a credential check
// that looks only at those two saw an authenticated response as anonymous,
// stored it under the shared method/host/path/query key, and served it to the
// next grant holder as a HIT without the handler ever running.
func TestCacheMiddleware_DoesNotCacheEmbedGrantResponses(t *testing.T) {
	store := NewMemoryCache()
	var n atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stand in for a per-subject render.
		subject := "alice"
		if n.Add(1) > 1 {
			subject = "bob"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(subject))
	}))

	get := func(grant string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/reports", nil)
		req.Header.Set("X-Gofastr-Embed", grant)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := get("emg_alice-grant")
	second := get("emg_bob-grant")

	if second.Body.String() == first.Body.String() {
		t.Fatalf("a second grant holder received the first one's response (%q). "+
			"X-Gofastr-Embed must count as a credential, or the cache replays one "+
			"embed subject's page to another.", second.Body.String())
	}
	if got := second.Header().Get("X-Cache"); got == "HIT" {
		t.Errorf("X-Cache = HIT for a grant-authenticated request")
	}
}

// stubRedisClient is a map-backed RedisClient standing in for one Redis
// server shared by several RedisCache instances, so no server is needed.
// A non-nil getErr makes every read fail while writes still succeed,
// simulating a read outage.
type stubRedisClient struct {
	kv     map[string]string
	getErr error
}

func (s *stubRedisClient) Get(_ context.Context, k string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	v, ok := s.kv[k]
	if !ok {
		// The interface's documented miss signal ("a redis nil error").
		return "", errors.New("redis: nil")
	}
	return v, nil
}

func (s *stubRedisClient) Set(_ context.Context, k, v string, _ time.Duration) error {
	if s.kv == nil {
		s.kv = map[string]string{}
	}
	s.kv[k] = v
	return nil
}

func (s *stubRedisClient) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(s.kv, k)
	}
	return nil
}

func (s *stubRedisClient) Exists(_ context.Context, k string) (bool, error) {
	_, ok := s.kv[k]
	return ok, nil
}

func (s *stubRedisClient) FlushDB(context.Context) error {
	s.kv = map[string]string{}
	return nil
}

// Keys implements the optional KeyScanner capability, which is what lets
// RedisCache.Clear scope its wipe to one namespace instead of flushing
// the shared database. Only the trailing "*" form RedisCache emits is
// supported; that is all it asks for.
func (s *stubRedisClient) Keys(_ context.Context, pattern string) ([]string, error) {
	prefix := strings.TrimSuffix(pattern, "*")
	var out []string
	for k := range s.kv {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out) // deterministic order for a map-backed stub
	return out, nil
}

// CACHE-R1: the Cache interface documents ErrCacheMiss strictly as the
// not-found sentinel ("Returns ErrCacheMiss if the key does not exist or
// has expired", cache.go), but RedisCache.Get wraps EVERY client error
// with %w on ErrCacheMiss (redis.go), so a Redis outage — connection
// refused, timeout, auth failure — is indistinguishable from an absent
// key. Callers that fail closed on miss (negative caching, revocation
// lists) fail open for the whole outage. A backend failure must not
// satisfy errors.Is(err, ErrCacheMiss).
func TestRedisCache_BackendErrorNotMiss(t *testing.T) {
	outages := []struct {
		name string
		err  error
	}{
		{"connection refused", errors.New("dial tcp 127.0.0.1:6379: connect: connection refused")},
		{"timeout", context.DeadlineExceeded},
		{"auth rejected", errors.New("WRONGPASS invalid username-password pair")},
	}
	for _, o := range outages {
		t.Run(o.name, func(t *testing.T) {
			rc := NewRedisCache(&stubRedisClient{getErr: o.err})
			var got string
			err := rc.Get(context.Background(), "revocation:token-7", &got)
			if err == nil {
				t.Fatal("backend outage did not surface as an error")
			}
			if errors.Is(err, ErrCacheMiss) {
				t.Fatalf("SECURITY: [cache] a dead Redis is reported as ErrCacheMiss (%v). "+
					"ErrCacheMiss is the documented not-found sentinel; miss-means-absent "+
					"callers (negative caching, revocation data) treat the outage as "+
					"\"key not present\" and fail open.", err)
			}
		})
	}
}

// CACHE-R1 through the read-through path: GetOrSet's contract is that the
// loader runs "on a miss" (cache.go). During a read outage every Get
// fails, so with the sentinel conflated the loader result is stored and
// the final read-back still returns an error satisfying ErrCacheMiss —
// the caller is told "miss" for a key that was just loaded, and each new
// request re-runs the loader (origin stampede during the outage).
func TestGetOrSet_BackendOutageNotMiss(t *testing.T) {
	stub := &stubRedisClient{getErr: errors.New("connection refused")}
	var loads int
	var got string
	err := GetOrSet(context.Background(), NewRedisCache(stub), "hot-key", time.Minute, &got,
		func(context.Context) (any, error) { loads++; return "loaded", nil })
	if err == nil {
		t.Fatal("expected the outage to surface as an error")
	}
	if errors.Is(err, ErrCacheMiss) {
		t.Fatalf("SECURITY: [cache] GetOrSet reported a backend outage as ErrCacheMiss "+
			"(%v) even though the loader ran %d time(s); miss-means-absent callers fail "+
			"open during the outage.", err, loads)
	}
}

// CACHE-R3: prefixedKey concatenates prefix + ":" + key with no escaping,
// so distinct (prefix, key) pairs alias the same effective Redis key:
// ("u:alice", "admin:x") and ("u:alice:admin", "x") both address
// "u:alice:admin:x". With per-tenant prefixes over one shared Redis —
// the "prefix namespacing" the battery documents — a namespace whose
// prefix or key contains ':' can read (or poison, or Delete) another
// namespace's entries. Distinct namespaces must not alias.
func TestRedisCache_PrefixNoKeyCollision(t *testing.T) {
	srv := &stubRedisClient{}
	tenantA := NewRedisCache(srv, WithPrefix("u:alice"))
	tenantB := NewRedisCache(srv, WithPrefix("u:alice:admin"))

	const secret = "tenant-A-secret"
	if err := tenantA.Set(context.Background(), "admin:x", secret, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got string
	err := tenantB.Get(context.Background(), "x", &got)
	if err == nil || got == secret {
		t.Fatalf("SECURITY: [cache] namespaces WithPrefix(%q) and WithPrefix(%q) alias: "+
			"Get(%q) through the second returned the first's value %q (err=%v). "+
			"':'-delimited concatenation is not injective, so one tenant can read, "+
			"overwrite, or Delete another tenant's entries.", "u:alice", "u:alice:admin",
			"x", got, err)
	}
}

// CACHE-R4: the middleware documents that a Vary'd response is "stored
// under a key that includes the values of every listed request header
// so different variants do not collide" (middleware.go), but
// captureVariant and variantMatches use r.Header.Get, which sees only
// the FIRST value of a repeated header. A variant primed with
// X-Team: [alpha, omega] is then served as a HIT to a request carrying
// only X-Team: alpha — the victim receives a body computed under a
// header value it never sent (RFC 9111 §4.1 defines the selecting data
// as the complete field value).
func TestCacheMiddleware_VaryAllHeaderValues(t *testing.T) {
	mk := func() http.Handler {
		store := NewMemoryCache()
		return CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Vary", "X-Team")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Join(r.Header.Values("X-Team"), ",")))
		}))
	}
	get := func(h http.Handler, vals ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		for _, v := range vals {
			req.Header.Add("X-Team", v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("single-value request must not receive multi-value variant", func(t *testing.T) {
		h := mk()
		get(h, "alpha", "omega") // primes the cached variant
		rec := get(h, "alpha")
		if rec.Body.String() != "alpha" {
			t.Fatalf("SECURITY: [cache] request with X-Team=[alpha] got body %q "+
				"(X-Cache=%s): the variant computed under [alpha omega] was served to a "+
				"request that never sent omega; Vary selection must use the complete "+
				"field value.", rec.Body.String(), rec.Header().Get("X-Cache"))
		}
	})

	t.Run("multi-value request must not receive single-value variant", func(t *testing.T) {
		h := mk()
		get(h, "alpha") // primes the cached variant
		rec := get(h, "alpha", "omega")
		if rec.Body.String() != "alpha,omega" {
			t.Fatalf("SECURITY: [cache] request with X-Team=[alpha omega] got body %q "+
				"(X-Cache=%s): the variant computed under [alpha] was served to a request "+
				"that also sent omega; Vary selection must use the complete field value.",
				rec.Body.String(), rec.Header().Get("X-Cache"))
		}
	})
}

// CLEAR-BLAST-RADIUS: the Cache interface scopes Clear to "removes all
// entries from the cache" (cache.go) — the entries THIS cache instance owns.
// MemoryCache.Clear wipes exactly its own map (memory.go), but RedisCache.Clear
// ignores cfg.prefix entirely and issues FlushDB (redis.go: "Clear removes all
// keys from the current Redis database"), every key in the selected Redis
// database, not just this cache's namespace. Every other RedisCache op routes
// through prefixedKey(); Clear is the one operation whose blast radius differs
// between the twins. With per-tenant prefixes over one shared Redis — the
// "prefix namespacing" the battery documents — one tenant's Clear destroys
// every other tenant's entries (cross-tenant data destruction) and any foreign
// keys sharing the database. Clear's wipe must stay scoped to the keys
// prefixedKey() can produce.
func TestRedisCache_ClearScopedToPrefix(t *testing.T) {
	srv := &stubRedisClient{}
	tenantA := NewRedisCache(srv, WithPrefix("appA"))
	tenantB := NewRedisCache(srv, WithPrefix("appB"))
	ctx := context.Background()

	// Plant one entry per namespace plus a foreign key owned by neither
	// cache (another service sharing the same Redis database).
	if err := tenantA.Set(ctx, "session", "a-secret", time.Minute); err != nil {
		t.Fatalf("tenantA.Set: %v", err)
	}
	if err := tenantB.Set(ctx, "cart", `{"sku":"X"}`, time.Minute); err != nil {
		t.Fatalf("tenantB.Set: %v", err)
	}
	srv.kv["other-service:lock"] = "held"

	if err := tenantA.Clear(ctx); err != nil {
		t.Fatalf("tenantA.Clear: %v", err)
	}

	// Clear's contract does remove the instance's own entries.
	var a string
	if err := tenantA.Get(ctx, "session", &a); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Clear left the cache's own entry in place (err=%v)", err)
	}

	// Another namespace's entry must SURVIVE this cache's Clear: it is not
	// an entry of this cache.
	var b string
	if err := tenantB.Get(ctx, "cart", &b); err != nil {
		t.Fatalf("SECURITY: [cache] Clear on WithPrefix(%q) destroyed another "+
			"namespace's entry: tenantB Get(%q) = %v. The contract is \"removes all "+
			"entries from the cache\", but the Redis backend FlushDBs the whole "+
			"database, so one tenant's Clear is every tenant's data-loss event.",
			"appA", "cart", err)
	}
	// A foreign key owned by neither cache must survive too.
	if v, ok := srv.kv["other-service:lock"]; !ok || v != "held" {
		t.Fatalf("SECURITY: [cache] Clear on a prefixed cache destroyed the foreign "+
			"key other-service:lock owned by neither cache (present=%v). The wipe "+
			"must be scoped to the keys prefixedKey() can produce, not the whole "+
			"database.", ok)
	}
}

// TestCacheMiddleware_DoesNotCache304 pins the stored-304 poison,
// found by the 2026-09-04 red-probe round; fixed in isStoreable, which
// now refuses 304 outright, and in the HIT path, which declines to
// replay a stored 304 (entries primed by a pre-fix process on a shared
// backend).
//
// Property: a cache must never answer a request that is not itself
// conditional with a stored 304 (Not Modified); a 304 is not a
// representation, it is a conditional-reply artifact (RFC 9111 §4.3.1:
// a cache MUST NOT generate a 304 reply to a request unless that
// request is conditional and its validators match the stored response).
// Surfaces: battery/cache/middleware.go::isStoreable, ::CacheMiddlewareWithLimit
// (forwards conditional GETs to the origin and stores what comes back),
// and ::writeCached (replays the stored status verbatim to every caller).
func TestCacheMiddleware_DoesNotCache304(t *testing.T) {
	const fullBody = "FULL-REPRESENTATION"
	origin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", "max-age=60")
		switch r.Header.Get("If-None-Match") {
		case `"v1"`, "*":
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	})

	// Attack shapes: the three ways a client can make an origin answer
	// 304 with an empty body. All three must leave the shared variant
	// storing a servable representation, never the bare 304.
	primes := []struct {
		name string
		hdr  map[string]string
	}{
		{"if-none-match exact", map[string]string{"If-None-Match": `"v1"`}},
		{"if-none-match wildcard", map[string]string{"If-None-Match": "*"}},
		{"if-modified-since", map[string]string{"If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT"}},
	}

	for _, p := range primes {
		t.Run(p.name, func(t *testing.T) {
			c := NewMemoryCache()
			defer c.Close()
			h := CacheMiddleware(c, time.Minute)(origin)

			prime := httptest.NewRequest(http.MethodGet, "/page", nil)
			for k, v := range p.hdr {
				prime.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, prime)
			if rec.Code != http.StatusNotModified {
				t.Fatalf("prime: status = %d, want 304 (setup)", rec.Code)
			}

			// The victim sends no conditional headers at all.
			victim := httptest.NewRecorder()
			h.ServeHTTP(victim, httptest.NewRequest(http.MethodGet, "/page", nil))
			if victim.Code != http.StatusOK {
				t.Errorf("SECURITY: [cache-304] unconditional GET received status %d "+
					"(X-Cache=%q, body=%q) after a conditional 304 primed the key: "+
					"a stored 304 was replayed to a request that is not conditional, "+
					"poisoning the shared variant with an empty non-representation "+
					"(RFC 9111 §4.3.1). Every anonymous client now gets a bare 304 "+
					"until the entry expires.", victim.Code, victim.Header().Get("X-Cache"),
					victim.Body.String())
			}
			if victim.Body.String() != fullBody {
				t.Errorf("SECURITY: [cache-304] victim body = %q, want the full representation %q",
					victim.Body.String(), fullBody)
			}
		})
	}
}

// TestCacheMiddleware_StoreableStatuses sweeps every status isStoreable
// accepts: 304 must be refused (conditional artifact), 206 must be
// refused (partial representation), and the remaining 2xx/3xx
// representation statuses stay cacheable. The store-side twin of
// TestCacheMiddleware_DoesNotCache304, looping the invariant across the
// whole status range instead of one prime shape.
func TestCacheMiddleware_StoreableStatuses(t *testing.T) {
	cases := []struct {
		status    int
		storeable bool
	}{
		{http.StatusOK, true},
		{http.StatusCreated, true},
		{http.StatusNoContent, true},
		{http.StatusPartialContent, false},
		{http.StatusMultiStatus, true},
		{http.StatusMultipleChoices, true},
		{http.StatusMovedPermanently, true},
		{http.StatusFound, true},
		{http.StatusSeeOther, true},
		{http.StatusNotModified, false},
		{http.StatusTemporaryRedirect, true},
		{http.StatusPermanentRedirect, true},
		{http.StatusBadRequest, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			rec := &responseRecorder{header: make(http.Header), statusCode: tc.status}
			if got := isStoreable(rec, false, false); got != tc.storeable {
				t.Errorf("SECURITY: [cache-304] isStoreable(status=%d) = %v, want %v",
					tc.status, got, tc.storeable)
			}
		})
	}
}

// TestMemoryCache_NegativeTTLNotImmortal pins the negative-TTL
// inversion, found by the 2026-09-04 red-probe round; fixed in
// MemoryCache.Set, which now treats a negative TTL as already expired
// (stores nothing, drops any existing entry) instead of mapping it to
// hasExpiry=false, i.e. never-expiring retention.
//
// Property: a negative TTL must never widen into maximal retention — a
// caller that computes a lifetime and gets a negative number (deadline
// already passed, clock skew across replicas, a unit mix-up) must see
// the entry absent, never an immortal one; only ttl exactly == 0 is
// documented as "fall back to the default".
// Surfaces: battery/cache/memory.go::Set, battery/cache/cache.go::GetOrSet
// (forwards its ttl into Set after the loader ran), and
// battery/cache/redis.go::Set (the Redis twin already fails closed:
// the server refuses a non-positive expiry).
func TestMemoryCache_NegativeTTLNotImmortal(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache()
	defer c.Close()

	// Control: a positive TTL expires and the entry reads as a miss.
	if err := c.Set(ctx, "pos", "v", 50*time.Millisecond); err != nil {
		t.Fatalf("Set(pos): %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if err := c.Get(ctx, "pos", new(string)); err == nil {
		t.Errorf("positive-TTL entry outlived its expiry (control failed)")
	}

	// The finding: a negative TTL used to mint an immortal entry.
	if err := c.Set(ctx, "neg", "v", -time.Second); err != nil {
		t.Fatalf("Set(neg): %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	var got string
	if err := c.Get(ctx, "neg", &got); err == nil {
		t.Errorf("SECURITY: [cache-negttl] MemoryCache.Set(ttl=-1s) minted a NEVER-EXPIRING "+
			"entry (Get succeeded with %q after the negative lifetime elapsed): a negative TTL "+
			"is the caller saying \"already gone\", but hasExpiry=ttl>0 silently maps it to "+
			"immortal retention — the maximal inversion of the caller's intent, and the "+
			"opposite of the Redis twin, which refuses non-positive expiries.", got)
	}

	// Sweep, same invariant: a negative Set over an existing entry must
	// drop it (the caller just said the key is already gone), and
	// GetOrSet forwarding a negative ttl must leave the key absent
	// (loader runs, nothing stored, miss surfaced).
	if err := c.Set(ctx, "overwrite", "old", time.Hour); err != nil {
		t.Fatalf("Set(overwrite): %v", err)
	}
	if err := c.Set(ctx, "overwrite", "new", -time.Second); err != nil {
		t.Fatalf("Set(overwrite, neg): %v", err)
	}
	if err := c.Get(ctx, "overwrite", new(string)); err == nil {
		t.Errorf("SECURITY: [cache-negttl] Set(ttl<0) left the previous long-TTL entry " +
			"live under the key: the negative lifetime must retire it, not keep it.")
	}

	if err := GetOrSet(ctx, c, "gos", -time.Second, new(string), func(context.Context) (any, error) {
		return "loaded", nil
	}); err == nil {
		t.Errorf("SECURITY: [cache-negttl] GetOrSet(ttl<0) reported success; the miss must be surfaced")
	}
	if err := c.Get(ctx, "gos", new(string)); err == nil {
		t.Errorf("SECURITY: [cache-negttl] GetOrSet(ttl<0) stored a live entry; nothing may be stored")
	}
}
