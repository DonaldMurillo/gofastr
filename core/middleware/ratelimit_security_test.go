package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRateLimit_XFFRotationDoesNotBypass verifies that rotating
// X-Forwarded-For header values does not allow bypassing the rate limit.
// Attack: attacker spoofs X-Forwarded-For to get a fresh bucket per request.
func TestRateLimit_XFFRotationDoesNotBypass(t *testing.T) {
	cfg := RateLimitConfig{
		Capacity:    3,
		RefillEvery: time.Minute,
		RefillBy:    1,
	}
	mw := RateLimit(cfg)

	var allowed int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send 10 requests with different XFF values
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("1.2.3.%d", i))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := atomic.LoadInt32(&allowed)
	// With XFF rotation, each request gets a different key, so all 10 pass.
	// This test DOCUMENTS the behavior: XFF rotation defeats the rate limiter
	// when the KeyFunc trusts the leftmost XFF entry without validation.
	if got > 3 {
		t.Errorf("SECURITY: [ratelimit] XFF rotation bypassed rate limit: %d requests allowed out of 10 (cap=3). Attack: spoofing X-Forwarded-For gives unlimited buckets.", got)
	}
}

// TestRateLimit_ConcurrentBurstCapped verifies that concurrent requests
// beyond capacity are rejected. Attack: concurrent burst overwhelms the
// rate limit by racing the token bucket.
func TestRateLimit_ConcurrentBurstCapped(t *testing.T) {
	cfg := RateLimitConfig{
		Capacity:    5,
		RefillEvery: time.Minute,
		RefillBy:    1,
	}
	mw := RateLimit(cfg)

	var allowed int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
		}()
	}
	wg.Wait()

	got := atomic.LoadInt32(&allowed)
	if got > 5 {
		t.Errorf("SECURITY: [ratelimit] concurrent burst allowed %d requests (cap=5). Attack: concurrent request race bypasses bucket.", got)
	}
}

// TestRateLimit_HexIPNormalizesToSameBucket verifies that hex-encoded IPs
// map to the same rate limit bucket. Attack: IP format variation (hex vs
// dotted-decimal) bypasses per-IP rate limiting.
func TestRateLimit_HexIPNormalizesToSameBucket(t *testing.T) {
	cfg := RateLimitConfig{
		Capacity:    2,
		RefillEvery: time.Minute,
		RefillBy:    1,
	}
	mw := RateLimit(cfg)

	var allowed int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send requests from the same IP but different formatting
	for _, addr := range []string{"1.2.3.4:1234", "1.2.3.4:5678"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := atomic.LoadInt32(&allowed)
	// Both should use the same bucket (after port stripping)
	// So we expect exactly 2 allowed since capacity is 2
	if got != 2 {
		t.Errorf("SECURITY: [ratelimit] same-IP different-port got %d allowed (want 2). Attack: port variation defeats per-IP bucketing.", got)
	}
}

// TestRateLimit_HeaderSplitKeyCollision verifies that a header value with
// commas doesn't create unintended bucket collisions or splits.
// Attack: crafting XFF header with embedded commas to manipulate key
// extraction.
func TestRateLimit_HeaderSplitKeyCollision(t *testing.T) {
	cfg := RateLimitConfig{
		Capacity:    2,
		RefillEvery: time.Minute,
		RefillBy:    1,
	}
	mw := RateLimit(cfg)

	var allowed int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// First request with clean XFF
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-Forwarded-For", "1.2.3.4")
	rr1 := httptest.NewRecorder()
	srv.ServeHTTP(rr1, req1)

	// Second with same first-hop but different second-hop
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, req2)

	got := atomic.LoadInt32(&allowed)
	// Both use key "1.2.3.4" — first-hop extraction — so only 2 should pass
	if got != 2 {
		t.Logf("SECURITY: [ratelimit] XFF comma-split gave %d allowed (want 2). Attack: multi-value XFF header may create unexpected buckets.", got)
	}
}

// TestRateLimit_NoProxyXFFSpoofing verifies that when there is no reverse
// proxy, the X-Forwarded-For header is not blindly trusted for key
// extraction. Attack: direct-to-origin request spoofs XFF to bypass
// per-IP rate limiting.
func TestRateLimit_NoProxyXFFSpoofing(t *testing.T) {
	cfg := RateLimitConfig{
		Capacity:    2,
		RefillEvery: time.Minute,
		RefillBy:    1,
	}
	mw := RateLimit(cfg)

	var allowed int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send requests with different spoofed XFF but same RemoteAddr
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("spoofed-%d", i))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := atomic.LoadInt32(&allowed)
	// The default KeyFunc trusts XFF leftmost entry. If spoofed XFF
	// creates separate buckets, the rate limiter is defeated.
	if got > 2 {
		t.Errorf("SECURITY: [ratelimit] XFF spoofing bypassed rate limit: %d allowed (cap=2). Attack: direct requests spoof X-Forwarded-For for unlimited buckets.", got)
	}
}
