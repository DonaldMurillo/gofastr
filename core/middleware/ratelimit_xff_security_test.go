package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Behind a trusted proxy that APPENDS to X-Forwarded-For (nginx
// $proxy_add_x_forwarded_for, httputil.ReverseProxy), everything left of
// the hop the proxy wrote is client-supplied. The key must be the
// rightmost untrusted hop, so a client rotating the leftmost entry gets
// no fresh bucket and cannot spend a named third party's bucket.

func appendingProxyLimiter() http.Handler {
	return RateLimit(RateLimitConfig{
		Capacity:          2,
		RefillEvery:       time.Minute,
		RefillBy:          1,
		TrustProxyHeaders: true,
		TrustedProxies:    []string{"10.0.0.1", "10.0.1.0/24"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func viaProxy(h http.Handler, xff ...string) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	for _, v := range xff {
		req.Header.Add("X-Forwarded-For", v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRateLimitAppendedXFFRotationCapped(t *testing.T) {
	h := appendingProxyLimiter()
	allowed := 0
	for i := 1; i <= 10; i++ {
		if viaProxy(h, fmt.Sprintf("198.51.100.%d, 203.0.113.7", i)) == http.StatusOK {
			allowed++
		}
	}
	if allowed > 2 {
		t.Fatalf("rotating the client-supplied XFF prefix got %d of 10 through, cap 2", allowed)
	}
}

func TestRateLimitAppendedXFFNoVictimLockout(t *testing.T) {
	h := appendingProxyLimiter()
	for range 3 {
		viaProxy(h, "8.8.4.4, 203.0.113.7")
	}
	if code := viaProxy(h, "8.8.4.4"); code != http.StatusOK {
		t.Fatalf("victim 8.8.4.4 got %d after an attacker named it in XFF", code)
	}
}

// Trusted inner hops (a second proxy tier in TrustedProxies) are skipped,
// and a split header (one value per line) is read as one list.
func TestRateLimitXFFSkipsTrustedInnerHops(t *testing.T) {
	h := appendingProxyLimiter()
	allowed := 0
	for i := 1; i <= 6; i++ {
		if viaProxy(h, fmt.Sprintf("198.51.100.%d, 203.0.113.9", i), "10.0.1.5") == http.StatusOK {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("client 203.0.113.9 behind two trusted tiers: %d allowed, want exactly 2", allowed)
	}
	// A different client through the same tiers has its own bucket; keying
	// on the inner tier would have spent it already.
	if code := viaProxy(h, "203.0.113.10", "10.0.1.5"); code != http.StatusOK {
		t.Fatalf("second client behind the same tiers got %d, want its own bucket", code)
	}
}
