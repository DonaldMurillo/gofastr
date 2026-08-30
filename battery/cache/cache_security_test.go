package cache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheMiddleware_DoesNotCacheSetCookieResponses(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "session_id", Value: fmt.Sprintf("token-%d", n), Path: "/", HttpOnly: true})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("request-%d", n)))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/account", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/account", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] response with Set-Cookie was cached and replayed. body1=%q body2=%q cookie2=%q", rec1.Body.String(), rec2.Body.String(), rec2.Header().Get("Set-Cookie"))
	}
}

func TestCacheMiddleware_DoesNotCachePrivateResponses(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("private-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/profile", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/profile", nil))

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] Cache-Control: private response was cached and replayed. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotCacheNoStoreResponses(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("nostore-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/billing", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/billing", nil))

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] Cache-Control: no-store response was cached and replayed. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheMiddleware_HonorsVaryAuthorization(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("user=" + r.Header.Get("Authorization")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req1.Header.Set("Authorization", "Bearer alice")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req2.Header.Set("Authorization", "Bearer bob")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Body.String() != "user=Bearer bob" {
		t.Fatalf("SECURITY: [cache] cache key ignored Vary: Authorization and replayed another user's variant: %q", rec2.Body.String())
	}
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
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("nocache-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/statement", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/statement", nil))

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] Cache-Control: no-cache response was cached and replayed. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheMiddleware_HonorsVaryCookie(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Cookie")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cookie=" + r.Header.Get("Cookie")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req1.Header.Set("Cookie", "session=alice")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req2.Header.Set("Cookie", "session=bob")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Body.String() != "cookie=session=bob" {
		t.Fatalf("SECURITY: [cache] cache key ignored Vary: Cookie and replayed another session's variant: %q", rec2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotCacheAuthorizationRequestsByDefault(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("auth=" + r.Header.Get("Authorization")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req1.Header.Set("Authorization", "Bearer alice")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req2.Header.Set("Authorization", "Bearer bob")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() != "auth=Bearer bob" {
		t.Fatalf("SECURITY: [cache] middleware cached Authorization-bearing request by default and replayed %q", rec2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotCacheCookieAuthenticatedRequestsByDefault(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cookie=" + r.Header.Get("Cookie")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/account", nil)
	req1.Header.Set("Cookie", "session=alice")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/account", nil)
	req2.Header.Set("Cookie", "session=bob")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() != "cookie=session=bob" {
		t.Fatalf("SECURITY: [cache] middleware cached cookie-authenticated request by default and replayed %q", rec2.Body.String())
	}
}

func TestCacheMiddleware_DoesNotCacheServerErrors(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf("db-down-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] 500 response was cached and replayed. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheMiddleware_HonorsVaryAcceptLanguage(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Accept-Language")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("lang=" + r.Header.Get("Accept-Language")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/landing", nil)
	req1.Header.Set("Accept-Language", "en-US")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/landing", nil)
	req2.Header.Set("Accept-Language", "fr-FR")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Body.String() != "lang=fr-FR" {
		t.Fatalf("SECURITY: [cache] cache key ignored Vary: Accept-Language and replayed another locale's variant: %q", rec2.Body.String())
	}
}

func TestCacheMiddleware_HonorsVaryOrigin(t *testing.T) {
	store := NewMemoryCache()
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin=" + r.Header.Get("Origin")))
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/cors", nil)
	req1.Header.Set("Origin", "https://alice.example")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/cors", nil)
	req2.Header.Set("Origin", "https://bob.example")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Body.String() != "origin=https://bob.example" {
		t.Fatalf("SECURITY: [cache] cache key ignored Vary: Origin and replayed another origin's variant: %q", rec2.Body.String())
	}
}

func TestCacheMiddleware_RequestNoCacheBypassesStoredVariant(t *testing.T) {
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("refresh-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/refresh", nil))

	req2 := httptest.NewRequest(http.MethodGet, "/refresh", nil)
	req2.Header.Set("Cache-Control", "no-cache")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] request Cache-Control: no-cache did not bypass stored variant. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
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
	store := NewMemoryCache()
	var hits atomic.Int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("nostore-req-%d", n)))
	}))

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/refresh", nil))

	req2 := httptest.NewRequest(http.MethodGet, "/refresh", nil)
	req2.Header.Set("Cache-Control", "no-store")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Cache") == "HIT" || rec2.Body.String() == rec1.Body.String() {
		t.Fatalf("SECURITY: [cache] request Cache-Control: no-store did not bypass stored variant. body1=%q body2=%q", rec1.Body.String(), rec2.Body.String())
	}
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
