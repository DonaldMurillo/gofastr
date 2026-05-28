package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newProxyAwareRateLimitedServer(capacity int, handler http.HandlerFunc) (http.Handler, *int32) {
	var allowed int32
	cfg := RateLimitConfig{
		Capacity:          capacity,
		RefillEvery:       time.Minute,
		RefillBy:          1,
		TrustProxyHeaders: true,
	}
	srv := RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		handler(w, r)
	}))
	return srv, &allowed
}

func TestRateLimit_TrustProxyHeadersXFFRotationDoesNotBypass(t *testing.T) {
	srv, allowed := newProxyAwareRateLimitedServer(2, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	if got := atomic.LoadInt32(allowed); got > 2 {
		t.Fatalf("SECURITY: [ratelimit-proxy] rotating X-Forwarded-For bypassed proxy-aware rate limit: allowed=%d cap=2.", got)
	}
}

func TestRateLimit_TrustProxyHeadersRejectsPrivateXFFSpoofing(t *testing.T) {
	srv, allowed := newProxyAwareRateLimitedServer(2, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	if got := atomic.LoadInt32(allowed); got > 2 {
		t.Fatalf("SECURITY: [ratelimit-proxy] private X-Forwarded-For values were trusted and bypassed rate limit: allowed=%d cap=2.", got)
	}
}

func TestRateLimit_TrustProxyHeadersRejectsArbitraryXFFValues(t *testing.T) {
	srv, allowed := newProxyAwareRateLimitedServer(2, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("attacker-%d", i))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	if got := atomic.LoadInt32(allowed); got > 2 {
		t.Fatalf("SECURITY: [ratelimit-proxy] arbitrary X-Forwarded-For strings were trusted and bypassed rate limit: allowed=%d cap=2.", got)
	}
}

func TestRateLimit_TrustProxyHeadersXRealIPRotationDoesNotBypass(t *testing.T) {
	srv, allowed := newProxyAwareRateLimitedServer(2, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	if got := atomic.LoadInt32(allowed); got > 2 {
		t.Fatalf("SECURITY: [ratelimit-proxy] rotating X-Real-IP bypassed proxy-aware rate limit: allowed=%d cap=2.", got)
	}
}
