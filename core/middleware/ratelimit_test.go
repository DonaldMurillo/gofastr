package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

// rateLimitBudgetHdrs lists the three IETF-draft budget headers in one place
// so the omit-style tests stay terse.
var rateLimitBudgetHdrs = []string{"RateLimit-Limit", "RateLimit-Remaining", "RateLimit-Reset"}

func TestRateLimit_BudgetHeadersAllowed(t *testing.T) {
	h, _ := newRateLimitedServer(RateLimitConfig{
		Capacity:    5,
		RefillEvery: time.Hour, // no refill within the test window
		RefillBy:    1,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1"

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("RateLimit-Limit"); got != "5" {
		t.Errorf("RateLimit-Limit = %q, want 5", got)
	}
	// Fresh bucket consumed its one token ⇒ remaining is capacity-1.
	if got := w.Header().Get("RateLimit-Remaining"); got != "4" {
		t.Errorf("RateLimit-Remaining = %q, want 4", got)
	}
	// A fresh bucket is missing exactly one token, so reset is a positive
	// integer (never 0) — assert the invariant, not a timing-sensitive value.
	reset := w.Header().Get("RateLimit-Reset")
	n, err := strconv.Atoi(reset)
	if err != nil {
		t.Fatalf("RateLimit-Reset missing or non-integer: %q", reset)
	}
	if n <= 0 {
		t.Errorf("RateLimit-Reset = %d, want > 0 on a non-full bucket", n)
	}
}

func TestRateLimit_BudgetHeaders429(t *testing.T) {
	h, _ := newRateLimitedServer(RateLimitConfig{
		Capacity:    1,
		RefillEvery: time.Hour,
		RefillBy:    1,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1"

	// Spend the only token.
	h.ServeHTTP(httptest.NewRecorder(), req)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if got := w.Header().Get("RateLimit-Limit"); got != "1" {
		t.Errorf("RateLimit-Limit = %q, want 1", got)
	}
	if got := w.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Errorf("RateLimit-Remaining = %q, want 0 on deny", got)
	}
	if w.Header().Get("RateLimit-Reset") == "" {
		t.Error("RateLimit-Reset header missing on 429")
	}
	// Existing behaviour is preserved: Retry-After still ships on the 429.
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429")
	}
}

func TestRateLimit_ResetFullIsZero(t *testing.T) {
	// capacity 10, +2 tokens/s: deficits cross tick boundaries at 2,4,6…
	s := newBucketStore(10, time.Second, 2)
	now := time.Now()

	// A full (or over-full) bucket reports 0 — the invariant the spec calls out.
	if d := s.timeToFull(10, now, now); d != 0 {
		t.Errorf("full bucket reset = %v, want 0", d)
	}
	if d := s.timeToFull(11, now, now); d != 0 {
		t.Errorf("over-full bucket reset = %v, want 0", d)
	}
	// Missing tokens ⇒ positive and monotonically non-decreasing in the deficit.
	prev := time.Duration(0)
	for rem := 9; rem >= 0; rem-- {
		d := s.timeToFull(rem, now, now)
		if d < prev {
			t.Errorf("reset decreased at remaining=%d: %v (prev %v)", rem, d, prev)
		}
		prev = d
	}
	// Deficit of 2 with refill=2/rate=1s needs exactly one tick ⇒ 1s.
	if d := s.timeToFull(8, now, now); d != time.Second {
		t.Errorf("reset for deficit 2 = %v, want 1s", d)
	}
}

func TestRateLimit_OmitBudgetHeaders(t *testing.T) {
	h, _ := newRateLimitedServer(RateLimitConfig{
		Capacity:          1,
		RefillEvery:       time.Hour,
		RefillBy:          1,
		OmitBudgetHeaders: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1"

	// Allowed path: none of the three budget headers leak.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	for _, hdr := range rateLimitBudgetHdrs {
		if v := w.Header().Get(hdr); v != "" {
			t.Errorf("%s present (%q) with OmitBudgetHeaders=true", hdr, v)
		}
	}

	// Deny path: still no budget headers, but Retry-After is unaffected.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	for _, hdr := range rateLimitBudgetHdrs {
		if v := w.Header().Get(hdr); v != "" {
			t.Errorf("%s present on 429 (%q) with OmitBudgetHeaders=true", hdr, v)
		}
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After must remain on 429 even when budget headers are omitted")
	}
}
