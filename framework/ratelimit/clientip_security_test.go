package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Behind an appending proxy the leftmost X-Forwarded-For entry is whatever
// the client sent. ClientIP feeds the auth login and register limiters, so
// keying on it let one client rotate a fresh budget per request.

func xffRequest(peer string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = peer
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIPIgnoresClientXFFPrefix(t *testing.T) {
	r := xffRequest("10.0.0.1:5555", "198.51.100.1, 203.0.113.7")
	if got := ClientIP(r, true); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want the hop the proxy wrote, 203.0.113.7", got)
	}
}

func TestLimiterAppendedXFFRotationCapped(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute, TrustForwardedFor: true})
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	allowed := 0
	for i := 1; i <= 10; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, xffRequest("10.0.0.1:5555", fmt.Sprintf("198.51.100.%d, 203.0.113.7", i)))
		if rec.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed > 2 {
		t.Fatalf("rotating the XFF prefix got %d of 10 through, cap 2", allowed)
	}
}

// With TrustedProxies set, inner tiers are skipped and only a listed peer
// may speak for the header at all.
func TestLimiterClientIPSkipsTrustedTiers(t *testing.T) {
	rl := NewLimiter(Config{TrustForwardedFor: true, TrustedProxies: []string{"10.0.0.0/8"}})
	r := xffRequest("10.0.0.1:5555", "198.51.100.1, 203.0.113.7, 10.2.3.4")
	if got := rl.ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("Limiter.ClientIP = %q, want 203.0.113.7", got)
	}
	direct := xffRequest("203.0.113.50:1234", "198.51.100.1")
	if got := rl.ClientIP(direct); got != "203.0.113.50" {
		t.Fatalf("untrusted peer spoke for XFF: got %q, want the peer", got)
	}
}
