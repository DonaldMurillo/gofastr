package embed

import (
	"context"
	"errors"
	"testing"
	"time"
)

// clockJumpStore moves the host's clock to the real present just before
// the claim, so a nonce verified live is past its retention deadline by
// the time the store sees it.
type clockJumpStore struct {
	BurnStore
	jump func()
}

func (s *clockJumpStore) Burn(ctx context.Context, id, grant string, expires time.Time) (string, bool, error) {
	s.jump()
	return s.BurnStore.Burn(ctx, id, grant, expires)
}

// TestFreshNonceExpiringDuringBurnIsExpired: a nonce never exchanged
// whose retention deadline passes between verification and the burn
// answers ErrExpired, not ErrSpent, over every burn store.
func TestFreshNonceExpiringDuringBurnIsExpired(t *testing.T) {
	for _, s := range []struct {
		name string
		mk   func(*testing.T) BurnStore
	}{
		{"memory", func(*testing.T) BurnStore { return NewMemoryBurnStore() }},
		{"sqlite", func(t *testing.T) BurnStore { return newSQLiteBurnStore(t) }},
	} {
		t.Run(s.name, func(t *testing.T) {
			past := time.Now().Add(-time.Hour)
			clock := past
			store := &clockJumpStore{BurnStore: s.mk(t), jump: func() { clock = time.Now() }}
			h := testHost(t, func(c *Config) { c.BurnStore = store })
			h.now = func() time.Time { return clock }
			tok, err := MintNonce(h.nonceKey, "dashboard", "u-1", "https://acme.com", nil, time.Minute, past)
			if err != nil {
				t.Fatal(err)
			}
			res, err := h.Exchange(context.Background(), tok, "https://acme.com")
			if !errors.Is(err, ErrExpired) || res.Grant != "" {
				t.Fatalf("Exchange of a nonce that expired during the burn = %+v, %v; want ErrExpired", res, err)
			}
		})
	}
}
