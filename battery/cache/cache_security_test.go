package cache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheMiddleware_DoesNotCacheSetCookieResponses(t *testing.T) {
	store := NewMemoryCache()
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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

func TestCacheMiddleware_DoesNotCacheNoCacheResponses(t *testing.T) {
	store := NewMemoryCache()
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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

func TestCacheMiddleware_RequestNoStoreBypassesStoredVariant(t *testing.T) {
	store := NewMemoryCache()
	var hits int32
	handler := CacheMiddleware(store, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
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
