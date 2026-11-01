package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Per-IP limits keyed IPv6 clients by their full address. One subscriber
// is routinely handed a /64, so a single client rotated past every per-IP
// limit with a fresh source address per request (2026-11-01 battery/auth
// bug bash). Keys collapse IPv6 to its /64.
func TestIPKeyCollapsesIPv6ToSlash64(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":              "203.0.113.9",
		"::ffff:203.0.113.9":       "203.0.113.9",
		"2001:db8:1:2::1":          "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ffff::": "2001:db8:1:2::/64",
		"fe80::1%eth0":             "fe80::/64",
		"not-an-ip":                "not-an-ip",
	} {
		if got := IPKey(in); got != want {
			t.Errorf("IPKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMiddlewareLimitsAWholeSlash64(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 3, Window: time.Minute, BlockDuration: time.Minute})
	h := rl.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	allowed := 0
	for i := range 10 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("SECURITY: one /64 rotating its source address got %d requests through a 3-per-minute limit", allowed)
	}
	// Another /64 has its own budget.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8:9:9::1]:443"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Error("a different /64 was limited")
	}
}
