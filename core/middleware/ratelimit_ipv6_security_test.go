package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// One IPv6 subscriber is routinely handed a whole /64. RateLimit keyed the
// full peer address, so a single client sending each request from a fresh
// source address in its /64 met no limit at all: the 2026-11-01 bug bash
// fixed framework/ratelimit (be0acf07) and this middleware kept the old
// key. Found by the ratelimitkey analyzer. Both default keys now collapse
// IPv6 to its /64; IPv4 stays the address itself.
func TestRateLimit_IPv6Slash64SharesOneBucket(t *testing.T) {
	for name, cfg := range map[string]RateLimitConfig{
		"default key": {Capacity: 3, RefillEvery: time.Minute, RefillBy: 1},
		"proxy-aware key": {
			Capacity: 3, RefillEvery: time.Minute, RefillBy: 1,
			TrustProxyHeaders: true, TrustedProxies: []string{"10.0.0.1"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var allowed atomic.Int32
			srv := RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				allowed.Add(1)
			}))
			for i := range 10 {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				addr := fmt.Sprintf("2001:db8:1:2::%x", i+1)
				if cfg.TrustProxyHeaders {
					req.RemoteAddr = "10.0.0.1:443"
					req.Header.Set("X-Forwarded-For", "2600:1f18:1:2::"+fmt.Sprintf("%x", i+1))
				} else {
					req.RemoteAddr = "[" + addr + "]:4711"
				}
				srv.ServeHTTP(httptest.NewRecorder(), req)
			}
			if got := allowed.Load(); got != 3 {
				t.Fatalf("SECURITY: [ratelimit-ipv6] 10 requests rotating source addresses inside one /64 let %d through (cap 3): the key is the full address, so one subscriber mints a fresh bucket per request", got)
			}
		})
	}
}

func TestDefaultRateLimitKey_CollapsesIPv6Only(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9:4711":          "203.0.113.9",
		"[2001:db8:1:2::5]:4711":    "2001:db8:1:2::/64",
		"[::ffff:203.0.113.9]:4711": "203.0.113.9",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = in
		if got := defaultRateLimitKey(req); got != want {
			t.Errorf("defaultRateLimitKey(%q) = %q, want %q", in, got, want)
		}
	}
}
