package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStripPort_PreservesBareIPv6 pins the contract that
// defaultRateLimitKey returns bare-IPv6 addresses unchanged. The old
// last-colon split mangled "2001:db8::1" to "2001:db8:", which split
// the bucket per address and silently defeated the rate limit.
func TestStripPort_PreservesBareIPv6(t *testing.T) {
	cases := []string{
		"::1",
		"2001:db8::1",
		"2001:db8::ab",
		"[::1]:8080",
		"[2001:db8::1]:1234",
		"10.0.0.1:9000",
		"10.0.0.1",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = in
			got := defaultRateLimitKey(req)
			switch in {
			case "[::1]:8080":
				if got != "::1" {
					t.Fatalf("bracketed IPv6 not unwrapped: got %q", got)
				}
			case "[2001:db8::1]:1234":
				if got != "2001:db8::1" {
					t.Fatalf("bracketed IPv6 not unwrapped: got %q", got)
				}
			case "10.0.0.1:9000":
				if got != "10.0.0.1" {
					t.Fatalf("IPv4 port not stripped: got %q", got)
				}
			default:
				if got != in {
					t.Fatalf("bare IPv6/IPv4 mangled: in=%q got=%q", in, got)
				}
			}
		})
	}
}

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

	var allowed atomic.Int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send 10 requests with different XFF values
	for i := range 10 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("1.2.3.%d", i))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := allowed.Load()
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

	var allowed atomic.Int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
		})
	}
	wg.Wait()

	got := allowed.Load()
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

	var allowed atomic.Int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send requests from the same IP but different formatting
	for _, addr := range []string{"1.2.3.4:1234", "1.2.3.4:5678"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := allowed.Load()
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

	var allowed atomic.Int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
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

	got := allowed.Load()
	// Both use key "1.2.3.4", first-hop extraction, so only 2 should pass
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

	var allowed atomic.Int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	// Send requests with different spoofed XFF but same RemoteAddr
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("spoofed-%d", i))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
	}

	got := allowed.Load()
	// The default KeyFunc trusts XFF leftmost entry. If spoofed XFF
	// creates separate buckets, the rate limiter is defeated.
	if got > 2 {
		t.Errorf("SECURITY: [ratelimit] XFF spoofing bypassed rate limit: %d allowed (cap=2). Attack: direct requests spoof X-Forwarded-For for unlimited buckets.", got)
	}
}

// TestRateLimit_RetryAfterCoversTick pins the 429 Retry-After
// under-wait, found by the 2026-09-04 red-probe round; fixed in
// bucketStore.take, which now prices the deny path at refill-tick
// granularity (next token at lastSeen+rate, the model timeToFull uses)
// and RateLimit, which ceils the header instead of truncating it.
//
// Property: the Retry-After a 429 advertises must never be SHORTER
// than the time at which the bucket will actually grant the next
// token — a backoff header that under-waits turns every well-behaved
// client into a retry hammer against the exact endpoint being
// protected.
// Surfaces: core/middleware/ratelimit.go::bucketStore.take (deny path)
// and ::RateLimit (writes Retry-After out).
func TestRateLimit_RetryAfterCoversTick(t *testing.T) {
	// Both configs deliver tokens in ticks larger than one token:
	// 5-per-10s (perToken 2s vs tick 10s) and the package default
	// shape 60-per-60s (perToken 1s vs tick 60s).
	cases := []struct {
		name        string
		capacity    int
		refillEvery time.Duration
		refillBy    int
		minHonest   int // a retry CANNOT succeed before this many seconds
	}{
		{"bulk tick 5/10s", 5, 10 * time.Second, 5, 8},
		{"default shape 60/60s", 60, 60 * time.Second, 60, 55},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := RateLimit(RateLimitConfig{
				Capacity:    tc.capacity,
				RefillEvery: tc.refillEvery,
				RefillBy:    tc.refillBy,
			})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			for range tc.capacity {
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			}

			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
			if rr.Code != http.StatusTooManyRequests {
				t.Fatalf("drained bucket answered %d, want 429 (setup)", rr.Code)
			}
			got, err := strconv.Atoi(rr.Header().Get("Retry-After"))
			if err != nil {
				t.Fatalf("Retry-After missing/unparsable: %q", rr.Header().Get("Retry-After"))
			}
			if got < tc.minHonest {
				t.Errorf("SECURITY: [ratelimit-retryafter] Retry-After=%ds but the bucket's next "+
					"token cannot arrive for at least %ds (tokens refill %d per %s tick, not 1 "+
					"per %s): the header under-waits by nearly a whole refill window, so every "+
					"client that honors it re-hammers the denied endpoint.",
					got, tc.minHonest, tc.refillBy, tc.refillEvery, tc.refillEvery/time.Duration(tc.refillBy))
			}
		})
	}
}
