package webbotauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitKeyVerifiedAgentIsKeyedByURL(t *testing.T) {
	key := RateLimitKey(func(*http.Request) string { return "ip:1.2.3.4" })
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(WithAgent(r.Context(), &Agent{URL: "https://bot.example/.well-known/http-message-signatures-directory", KeyID: "k1"}))
	if got, want := key(r), "agent:https://bot.example/.well-known/http-message-signatures-directory"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	// A rotated key is the same agent: the key id is not in the key.
	r2 := r.WithContext(WithAgent(r.Context(), &Agent{URL: "https://bot.example/.well-known/http-message-signatures-directory", KeyID: "k2"}))
	if key(r2) != key(r) {
		t.Fatalf("rotating the key changed the limiter key: %q vs %q", key(r2), key(r))
	}
}

func TestRateLimitKeyUnverifiedUsesFallback(t *testing.T) {
	key := RateLimitKey(func(*http.Request) string { return "ip:1.2.3.4" })
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := key(r); got != "ip:1.2.3.4" {
		t.Fatalf("unverified key = %q, want the fallback", got)
	}
	// An Agent with no URL is not an identity; it must not collapse every
	// such request into one shared "agent:" bucket.
	r = r.WithContext(WithAgent(r.Context(), &Agent{}))
	if got := key(r); got != "ip:1.2.3.4" {
		t.Fatalf("empty-URL agent key = %q, want the fallback", got)
	}
}

// An IPv6 subscriber holds a whole /64; the nil fallback keyed the full
// address, so rotating source addresses minted a fresh budget per request.
// Found by the ratelimitkey analyzer.
func TestRateLimitKeyNilFallbackCollapsesIPv6(t *testing.T) {
	key := RateLimitKey(nil)
	seen := map[string]bool{}
	for _, addr := range []string{"[2001:db8:1:2::1]:4567", "[2001:db8:1:2:ffff::9]:80"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = addr
		seen[key(r)] = true
	}
	if len(seen) != 1 || !seen["2001:db8:1:2::/64"] {
		t.Fatalf("SECURITY: nil-fallback keys inside one /64 = %v, want the single key 2001:db8:1:2::/64", seen)
	}
}

func TestRateLimitKeyNilFallbackIsRemoteHost(t *testing.T) {
	key := RateLimitKey(nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:4567"
	if got := key(r); got != "203.0.113.9" {
		t.Fatalf("nil-fallback key = %q, want the remote host", got)
	}
}
