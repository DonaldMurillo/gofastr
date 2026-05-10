package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// helper: build a handler chain that returns 200 OK + counts hits to make
// rate-limit assertions trivial.
func newRateLimitedServer(cfg RateLimitConfig) (http.Handler, *int) {
	var hits int
	var mu sync.Mutex
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return RateLimit(cfg)(inner), &hits
}

func TestRateLimit_BurstThenDenied(t *testing.T) {
	h, _ := newRateLimitedServer(RateLimitConfig{
		Capacity:    3,
		RefillEvery: time.Hour, // effectively no refill in the test window
		RefillBy:    1,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	// First 3 succeed (burst).
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("burst %d: expected 200, got %d", i, w.Code)
		}
	}
	// 4th is denied with 429 + Retry-After.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRateLimit_KeyIsolation(t *testing.T) {
	h, hits := newRateLimitedServer(RateLimitConfig{
		Capacity:    1,
		RefillEvery: time.Hour,
		RefillBy:    1,
	})

	// Two different IPs each get their burst of 1.
	for _, addr := range []string{"10.0.0.1:1234", "10.0.0.2:5678"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("addr %s: expected 200, got %d", addr, w.Code)
		}
	}
	if *hits != 2 {
		t.Fatalf("expected 2 hits across distinct keys, got %d", *hits)
	}

	// A third request from the first IP is denied.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected reused IP to be denied, got %d", w.Code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	h, _ := newRateLimitedServer(RateLimitConfig{
		Capacity:    1,
		RefillEvery: 50 * time.Millisecond,
		RefillBy:    1,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:1234"

	// Use the only token.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d", w.Code)
	}

	// Immediate retry: denied.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate retry should be 429, got %d", w.Code)
	}

	// After a refill window: allowed again.
	time.Sleep(75 * time.Millisecond)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post-refill: expected 200, got %d", w.Code)
	}
}
