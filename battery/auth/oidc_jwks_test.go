package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestJWKSCache_PerKidRefetchNotWedged proves an unknown kid that resolves
// after a rotation is NOT blocked by a DIFFERENT unknown kid having just
// spent "the" forced-refetch slot. A global (non-per-kid) rate limit would
// wedge legitimate rotated-key logins for the whole window.
func TestJWKSCache_PerKidRefetchNotWedged(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	keyB := mustRSAKey(t, 2048)

	var hits atomic.Int32
	// The server publishes kid-b only after the first forced refetch, modeling
	// a rotation replica catching up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		keys := []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}
		if n >= 3 {
			keys = append(keys, rsaJWKMap("kid-b", &keyB.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	defer srv.Close()

	c := &jwksCache{httpClient: srv.Client(), ttl: time.Hour}
	ctx := context.Background()

	// 1. Warm the cache (hit 1): kid-a resolves.
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
		t.Fatalf("warmup kid-a: %v", err)
	}
	// 2. A bogus kid forces a refetch (hit 2) and spends ITS OWN slot.
	if _, err := c.getKey(ctx, srv.URL, "bogus"); err == nil {
		t.Fatal("bogus kid should not resolve")
	}
	// 3. kid-b (legitimately rotated in) must still get its own forced
	//    refetch (hit 3) despite bogus having just refetched, under a global
	//    limiter this returns an error with no new fetch.
	if _, err := c.getKey(ctx, srv.URL, "kid-b"); err != nil {
		t.Fatalf("kid-b wedged by bogus kid's spent slot: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("jwks hits = %d, want 3 (warmup + bogus refetch + kid-b refetch)", got)
	}
}

// TestJWKSCache_SameKidRateLimited confirms the per-kid limiter still throttles
// a flood of the SAME unknown kid to one refetch per window.
func TestJWKSCache_SameKidRateLimited(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)},
		})
	}))
	defer srv.Close()

	c := &jwksCache{httpClient: srv.Client(), ttl: time.Hour}
	ctx := context.Background()
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil { // hit 1
		t.Fatalf("warmup: %v", err)
	}
	for range 5 {
		_, _ = c.getKey(ctx, srv.URL, "same-bogus") // only the first forces hit 2
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("jwks hits = %d, want 2 (warmup + one throttled refetch)", got)
	}
}

func TestJWKSForcedKidsBounded(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)},
		})
	}))
	defer srv.Close()
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Hour}
	ctx := context.Background()
	for i := range 2*maxForcedKids + 1 {
		_, _ = c.getKey(ctx, srv.URL, fmt.Sprintf("bogus-%d", i))
	}
	if n := len(c.forcedByKid); n > maxForcedKids {
		t.Fatalf("forced-refetch map holds %d kids, want at most %d", n, maxForcedKids)
	}
}

func TestOIDCClaimsCacheBounded(t *testing.T) {
	p, err := NewOIDCProvider(OIDCConfig{Issuer: "https://idp.example", ClientID: "c", ClientSecret: "s", RedirectURL: "https://app.example/cb"}) // not-a-secret: test fixture
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1025 {
		p.claims.put(fmt.Sprintf("tok-%d", i), map[string]any{"sub": "u"})
	}
	if n := len(p.claims.m); n != 1024 {
		t.Errorf("claims cache holds %d entries, want 1024", n)
	}
	if _, ok := p.claims.get("tok-0"); ok {
		t.Error("the oldest entry survived past the cap")
	}
}
