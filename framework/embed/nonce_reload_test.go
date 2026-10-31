package embed

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A frame reloaded after the nonce's minute re-sends the same nonce, and
// embed.md promises the exchange is idempotent within the grant's
// lifetime. Exchange checked the nonce's expiry before the burn store, so
// any re-run after the first minute of a 15-minute grant answered 401
// (2026-10-27 bug bash U42). An expired nonce may now look up the grant
// its first exchange stored; it never mints one.

// waitNonceExpired blocks until tok no longer verifies as live.
func waitNonceExpired(t *testing.T, h *Host, tok string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := h.verifyNonce(tok, time.Now())
		if errors.Is(err, ErrExpired) {
			return
		}
		if err != nil {
			t.Fatalf("verifyNonce: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the nonce never expired")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// expiringHosts runs fn on a host with a one-second nonce over each burn
// store.
func expiringHosts(t *testing.T, mutate func(*Config), fn func(t *testing.T, h *Host)) {
	for _, s := range []struct {
		name string
		mk   func(*testing.T) BurnStore
	}{
		{"memory", func(*testing.T) BurnStore { return NewMemoryBurnStore() }},
		{"sqlite", func(t *testing.T) BurnStore { return newSQLiteBurnStore(t) }},
	} {
		t.Run(s.name, func(t *testing.T) {
			fn(t, testHost(t, func(c *Config) {
				c.NonceTTL = time.Second
				c.BurnStore = s.mk(t)
				if mutate != nil {
					mutate(c)
				}
			}))
		})
	}
}

func TestExpiredNonceReturnsLiveGrant(t *testing.T) {
	expiringHosts(t, nil, func(t *testing.T, h *Host) {
		ctx := context.Background()
		tok, err := h.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
		if err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
		first, err := h.Exchange(ctx, tok, "https://acme.com")
		if err != nil {
			t.Fatalf("first Exchange: %v", err)
		}
		waitNonceExpired(t, h, tok)
		again, err := h.Exchange(ctx, tok, "https://acme.com")
		if err != nil {
			t.Fatalf("Exchange after the nonce expired: %v", err)
		}
		if again.Grant != first.Grant || !again.Replay || !again.Expires.Equal(first.Expires) {
			t.Errorf("reload exchange = %+v, want the first grant %+v as a replay", again, first)
		}
	})
}

// TestExpiredUnusedNonceMintsNothing: a nonce never exchanged inside its
// minute stays expired, on every try.
func TestExpiredUnusedNonceMintsNothing(t *testing.T) {
	expiringHosts(t, nil, func(t *testing.T, h *Host) {
		ctx := context.Background()
		tok, err := h.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
		if err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
		waitNonceExpired(t, h, tok)
		for i := range 2 {
			if res, err := h.Exchange(ctx, tok, "https://acme.com"); !errors.Is(err, ErrExpired) || res.Grant != "" {
				t.Fatalf("try %d: Exchange of an unused expired nonce = %+v, %v; want ErrExpired", i, res, err)
			}
		}
	})
}

// TestExpiredNonceKeepsOriginChecks: the lookup runs only after the
// nonce passes every check a live one does.
func TestExpiredNonceKeepsOriginChecks(t *testing.T) {
	expiringHosts(t, nil, func(t *testing.T, h *Host) {
		ctx := context.Background()
		tok, err := h.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
		if err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
		if _, err := h.Exchange(ctx, tok, "https://acme.com"); err != nil {
			t.Fatalf("first Exchange: %v", err)
		}
		waitNonceExpired(t, h, tok)
		if res, err := h.Exchange(ctx, tok, "https://shop.acme.com"); err == nil || res.Grant != "" {
			t.Errorf("an expired nonce exchanged from another origin = %+v", res)
		}
		if res, err := h.Exchange(ctx, tok+"x", "https://acme.com"); err == nil || res.Grant != "" {
			t.Errorf("a tampered expired nonce exchanged = %+v", res)
		}
	})
}

// TestExpiredNonceAfterGrantExpiry: once the grant itself is dead the
// reload gets nothing.
func TestExpiredNonceAfterGrantExpiry(t *testing.T) {
	expiringHosts(t, func(c *Config) { c.GrantTTL = time.Second }, func(t *testing.T, h *Host) {
		ctx := context.Background()
		tok, err := h.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
		if err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
		first, err := h.Exchange(ctx, tok, "https://acme.com")
		if err != nil {
			t.Fatalf("first Exchange: %v", err)
		}
		waitNonceExpired(t, h, tok)
		time.Sleep(time.Until(first.Expires) + 50*time.Millisecond)
		if res, err := h.Exchange(ctx, tok, "https://acme.com"); err == nil || res.Grant != "" {
			t.Errorf("an exchange after the grant expired = %+v", res)
		}
	})
}

// deadGrantStore breaks the BurnStore contract: the expired-nonce lookup
// gets back a grant that has already expired.
type deadGrantStore struct {
	BurnStore
	dead string
}

func (s *deadGrantStore) Burn(ctx context.Context, id, grant string, expires time.Time) (string, bool, error) {
	if grant == "" {
		return s.dead, true, nil
	}
	return s.BurnStore.Burn(ctx, id, grant, expires)
}

// TestExpiredNonceRechecksStoredGrant: the lookup verifies what the store
// returns, so a store that hands back a dead grant hands the frame nothing.
func TestExpiredNonceRechecksStoredGrant(t *testing.T) {
	store := &deadGrantStore{BurnStore: NewMemoryBurnStore()}
	h := testHost(t, func(c *Config) { c.NonceTTL = time.Second; c.BurnStore = store })
	past := time.Now().Add(-2 * time.Hour)
	dead, err := MintGrant(h.grantKey, Nonce{Surface: "dashboard", Subject: "u-1", Origin: "https://acme.com"}, time.Minute, past.Add(time.Hour), past)
	if err != nil {
		t.Fatal(err)
	}
	store.dead = dead
	ctx := context.Background()
	tok, err := h.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	waitNonceExpired(t, h, tok)
	if res, err := h.Exchange(ctx, tok, "https://acme.com"); !errors.Is(err, ErrExpired) || res.Grant != "" {
		t.Fatalf("Exchange with a dead stored grant = %+v, %v; want ErrExpired", res, err)
	}
}
