package embed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testHost(t *testing.T, mutate ...func(*Config)) *Host {
	t.Helper()
	cfg := Config{
		Surfaces: []Surface{{
			Name:    "dashboard",
			Screen:  testScreen{"/embed/dashboard"},
			Origins: []string{"https://acme.com", "https://shop.acme.com"},
			Scopes:  []string{"read", "comment"},
			// The surface posts to an API route outside its own subtree, which
			// is the ordinary case and the one Reach exists for.
			Reach: []string{"/api/posts"},
		}},
		BurnStore: NewMemoryBurnStore(),
	}
	for _, m := range mutate {
		m(&cfg)
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.SetKeys(testNonceKey, testGrantKey)
	return h
}

func TestNewRejectsBadConfig(t *testing.T) {
	ok := Surface{Name: "dash", Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}
	bad := []struct {
		why string
		cfg Config
	}{
		{"no surfaces", Config{BurnStore: NewMemoryBurnStore()}},
		{"no burn store", Config{Surfaces: []Surface{ok}}},
		{"no origins", Config{Surfaces: []Surface{{Name: "dash", Screen: testScreen{"/d"}}}, BurnStore: NewMemoryBurnStore()}},
		{"relative path", Config{Surfaces: []Surface{{Name: "dash", Screen: testScreen{"d"}, Origins: []string{"https://acme.com"}}}, BurnStore: NewMemoryBurnStore()}},
		{"uppercase name", Config{Surfaces: []Surface{{Name: "Dash", Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}}, BurnStore: NewMemoryBurnStore()}},
		{"traversal name", Config{Surfaces: []Surface{{Name: "../x", Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}}, BurnStore: NewMemoryBurnStore()}},
		{"duplicate name", Config{Surfaces: []Surface{ok, ok}, BurnStore: NewMemoryBurnStore()}},
	}
	for _, c := range bad {
		if _, err := New(c.cfg); err == nil {
			t.Errorf("New with %s: want an error", c.why)
		}
	}
}

func TestMintNonceEnforcesSurfaceOriginAndScopes(t *testing.T) {
	h := testHost(t)

	if _, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com/", nil); err != nil {
		t.Fatalf("a listed origin (with a trailing slash) was rejected: %v", err)
	}
	if _, err := h.MintNonce(context.Background(), "nope", "u-1", "https://acme.com", nil); err == nil {
		t.Error("minting for an undeclared surface succeeded")
	}
	if _, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://evil.com", nil); err == nil {
		t.Error("minting for an origin not on the allowlist succeeded")
	}
	if _, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", []string{"read"}); err != nil {
		t.Errorf("narrowing scopes was rejected: %v", err)
	}
	if _, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", []string{"admin"}); err == nil {
		t.Error("minting a scope the surface does not declare succeeded — a silent drop makes an over-broad call site look correct")
	}

	// Default (nil) scopes grant the surface's full declared set.
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	n, err := VerifyNonce(testNonceKey, tok, time.Now())
	if err != nil {
		t.Fatalf("VerifyNonce: %v", err)
	}
	if len(n.Scopes) != 2 {
		t.Fatalf("scopes = %v, want the surface's declared set", n.Scopes)
	}
}

func TestMintRefusesWithoutKeys(t *testing.T) {
	h, err := New(Config{
		Surfaces:  []Surface{{Name: "dash", Screen: testScreen{"/d"}, Origins: []string{"https://acme.com"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if h.Ready() {
		t.Fatal("a keyless host reports ready")
	}
	if _, err := h.MintNonce(context.Background(), "dash", "u", "https://acme.com", nil); err == nil {
		t.Error("minting without a key succeeded")
	}
	if _, err := h.Exchange(context.Background(), "emb_x.y", ""); err == nil {
		t.Error("exchanging without a key succeeded")
	}
}

// The core property: a nonce buys exactly one identity. A repeat inside the
// grant's lifetime returns the SAME grant (so a prefetch or a double mount does
// not break the embed) and never a second, independent one.
func TestExchangeIsSingleUseAndIdempotent(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	first, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("first Exchange: %v", err)
	}
	if first.Replay {
		t.Fatal("the first exchange reported a replay")
	}
	if !strings.HasPrefix(first.Grant, GrantPrefix) {
		t.Fatalf("grant %q missing prefix", first.Grant)
	}

	second, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("second Exchange: %v", err)
	}
	if !second.Replay {
		t.Error("the second exchange did not report a replay")
	}
	if second.Grant != first.Grant {
		t.Error("the second exchange minted a DIFFERENT grant — the nonce bought two identities")
	}
	if !second.Expires.Equal(first.Expires) {
		t.Errorf("replay expiry %v != original %v — the frame would refresh on the wrong schedule", second.Expires, first.Expires)
	}
}

// Concurrent exchanges of one nonce are covered by burnStoreContract's
// "one winner under contention" subtest, which counts WINNERS.
//
// A host-level version of this used to live here, asserting that 24 racers saw
// one distinct grant. It could not fail for the reason it claimed: a grant is a
// deterministic function of its claims and both time fields are whole seconds,
// so racers inside one wall-clock second mint byte-identical tokens whether or
// not Exchange honours the burn store's answer. An implementation that ignored
// the store entirely would have passed it.

func TestExchangeRejectsSpentNonceAfterWindow(t *testing.T) {
	h := testHost(t, func(c *Config) { c.GrantTTL = 30 * time.Millisecond })
	ctx := context.Background()
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	if _, err := h.Exchange(ctx, tok, "https://acme.com"); err != nil {
		t.Fatalf("first Exchange: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := h.Exchange(ctx, tok, "https://acme.com"); !errors.Is(err, ErrSpent) {
		t.Fatalf("after the idempotency window: err = %v, want ErrSpent", err)
	}
}

func TestExchangeChecksFramedOrigin(t *testing.T) {
	ctx := context.Background()

	h := testHost(t)
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	// A differently-spelled but identical origin must match.
	if _, err := h.Exchange(ctx, tok, "https://ACME.com:443/"); err != nil {
		t.Fatalf("normalized-equal framed origin rejected: %v", err)
	}

	h2 := testHost(t)
	tok2, err := h2.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	// A different allowlisted origin is still the wrong one for THIS nonce.
	if _, err := h2.Exchange(ctx, tok2, "https://shop.acme.com"); err == nil {
		t.Error("a nonce minted for one origin exchanged from another")
	}
	if _, err := h2.Exchange(ctx, tok2, "https://evil.com"); err == nil {
		t.Error("a nonce exchanged from an unlisted origin")
	}
	// The nonce must still be unspent after those rejections. Verification
	// happens before the burn, so a failed probe cannot consume it.
	if res, err := h2.Exchange(ctx, tok2, "https://acme.com"); err != nil || res.Replay {
		t.Errorf("a rejected exchange consumed the nonce: res=%+v err=%v", res, err)
	}
}

func TestExchangeFailsClosedWhenConfigChanges(t *testing.T) {
	ctx := context.Background()
	minter := testHost(t)
	tok, err := minter.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	// Surface removed after the nonce was minted.
	gone, err := New(Config{
		Surfaces:  []Surface{{Name: "other", Screen: testScreen{"/o"}, Origins: []string{"https://acme.com"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gone.SetKeys(testNonceKey, testGrantKey)
	if _, err := gone.Exchange(ctx, tok, "https://acme.com"); err == nil {
		t.Error("a nonce for a removed surface still exchanged")
	}

	// Origin de-listed after the nonce was minted.
	delisted, err := New(Config{
		Surfaces:  []Surface{{Name: "dashboard", Screen: testScreen{"/embed/dashboard"}, Origins: []string{"https://other.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	delisted.SetKeys(testNonceKey, testGrantKey)
	if _, err := delisted.Exchange(ctx, tok, "https://acme.com"); err == nil {
		t.Error("a nonce for a de-listed origin still exchanged")
	}
}

func TestHostVerifyGrantTracksConfig(t *testing.T) {
	ctx := context.Background()
	h := testHost(t)
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", []string{"read"})
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	g, err := h.VerifyGrant(context.Background(), res.Grant)
	if err != nil {
		t.Fatalf("VerifyGrant: %v", err)
	}
	if g.Subject != "u-1" || !g.HasScope("read") || g.HasScope("comment") {
		t.Fatalf("grant claims wrong: %+v", g)
	}
	// A nonce is not a grant, on the host surface too.
	if _, err := h.VerifyGrant(context.Background(), tok); err == nil {
		t.Error("Host.VerifyGrant accepted a nonce")
	}

	delisted, err := New(Config{
		Surfaces:  []Surface{{Name: "dashboard", Screen: testScreen{"/embed/dashboard"}, Origins: []string{"https://other.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	delisted.SetKeys(testNonceKey, testGrantKey)
	if _, err := delisted.VerifyGrant(context.Background(), res.Grant); err == nil {
		t.Error("a grant for a de-listed origin still verified — de-listing must take effect for live frames too")
	}
}

func TestPrunesExpiredBurns(t *testing.T) {
	s := NewMemoryBurnStore()
	ctx := context.Background()
	now := time.Now()
	if _, _, err := s.Burn(ctx, "live", "g1", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn: %v", err)
	}
	if _, _, err := s.Burn(ctx, "dead", "g2", now.Add(-time.Hour)); err != nil {
		t.Fatalf("Burn: %v", err)
	}
	if err := s.Prune(ctx, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, replay, _ := s.Burn(ctx, "live", "g3", now.Add(time.Hour)); !replay {
		t.Error("Prune removed a live burn — the nonce became reusable")
	}
	if _, replay, _ := s.Burn(ctx, "dead", "g4", now.Add(time.Hour)); replay {
		t.Error("Prune left an expired burn behind")
	}
}

// The burn row has to outlive the NONCE, not the grant.
//
// Burning stores the row under the grant's expiry, and Prune deletes rows past
// it. But the nonce's own lifetime is an independent knob: with NonceTTL longer
// than GrantTTL, a Prune between two exchanges deletes the evidence while the
// nonce is still valid: the second exchange's INSERT wins and mints a SECOND,
// distinct grant. One nonce, two identities: exactly what single-use exists to
// make impossible.
func TestSpentNonceSurvivesAPruneWhenItOutlivesItsGrant(t *testing.T) {
	store := NewMemoryBurnStore()
	h := testHost(t, func(c *Config) {
		c.BurnStore = store
		c.NonceTTL = time.Hour             // long-lived nonce
		c.GrantTTL = 50 * time.Millisecond // short-lived grant
		c.GrantMaxAge = time.Hour
	})
	ctx := context.Background()

	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	first, err := h.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("first Exchange: %v", err)
	}

	// The grant lapses, then the operator's cron prunes.
	time.Sleep(80 * time.Millisecond)
	if err := h.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	second, err := h.Exchange(ctx, tok, "https://acme.com")
	if err == nil {
		t.Fatalf("a pruned burn let the nonce be spent again: it minted %q after %q — one nonce bought two identities",
			second.Grant, first.Grant)
	}
	if !errors.Is(err, ErrSpent) {
		t.Fatalf("err = %v, want ErrSpent", err)
	}
}

// The frame's reported ancestor origin is required, not optional. An empty
// value used to skip the comparison, which meant the only caller the check
// could plausibly catch, a direct POST that never went through a browser, was
// also the one caller it waved through.
func TestExchangeRequiresAReportedOrigin(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	tok, err := h.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	if _, err := h.Exchange(ctx, tok, ""); err == nil {
		t.Fatal("an exchange with no reported origin succeeded")
	}
	// And the nonce survives that rejection. Verification precedes the burn.
	if res, err := h.Exchange(ctx, tok, "https://acme.com"); err != nil || res.Replay {
		t.Fatalf("the rejected exchange consumed the nonce: res=%+v err=%v", res, err)
	}
}

// Removing a surface and de-listing an origin already took effect for grants
// in flight. Narrowing a surface's scopes did not: a grant kept the revoked
// scope for up to GrantMaxAge, twelve hours by default, refreshing the whole
// way. All three are the operator taking something away.
func TestVerifyGrantIntersectsWithCurrentScopes(t *testing.T) {
	ctx := context.Background()
	wide := testHost(t, func(c *Config) {
		c.Surfaces[0].Scopes = []string{"read", "admin"}
	})
	tok, err := wide.MintNonce(context.Background(), "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := wide.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if g, err := wide.VerifyGrant(context.Background(), res.Grant); err != nil || !g.HasScope("admin") {
		t.Fatalf("baseline: the grant should carry admin: %+v %v", g, err)
	}

	narrowed := testHost(t, func(c *Config) {
		c.Surfaces[0].Scopes = []string{"read"}
	})
	g, err := narrowed.VerifyGrant(context.Background(), res.Grant)
	if err != nil {
		t.Fatalf("VerifyGrant on the narrowed host: %v", err)
	}
	if g.HasScope("admin") {
		t.Fatalf("a revoked scope survived on a live grant: %v", g.Scopes)
	}
	if !g.HasScope("read") {
		t.Fatalf("the still-declared scope was dropped: %v", g.Scopes)
	}

	// And a refresh must not resurrect it.
	ref, err := narrowed.Refresh(context.Background(), res.Grant)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	after, err := narrowed.VerifyGrant(context.Background(), ref.Token)
	if err != nil {
		t.Fatalf("VerifyGrant after refresh: %v", err)
	}
	if after.HasScope("admin") {
		t.Fatalf("refresh restored a revoked scope: %v", after.Scopes)
	}
}
