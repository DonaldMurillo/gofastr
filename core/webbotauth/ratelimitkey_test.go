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

func TestRateLimitKeyNilFallbackIsRemoteHost(t *testing.T) {
	key := RateLimitKey(nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:4567"
	if got := key(r); got != "203.0.113.9" {
		t.Fatalf("nil-fallback key = %q, want the remote host", got)
	}
}
