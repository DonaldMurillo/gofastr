package embed

import (
	"context"
	"testing"
)

// Property: a refreshed grant is a NEW token for the SAME claims — the
// absolute deadline never moves (that is what stops refresh being an
// immortal credential) and the scopes it carries are the surface's CURRENT
// set, so narrowing Surface.Scopes takes effect on the next refresh, not at
// grant expiry. The deadline half is the load-bearing clamp that
// TestMintGrantClampsExpiryToDeadline pins at mint time; this pins it across
// the refresh loop and through the scope intersection VerifyGrant performs.
func TestRefreshKeepsDeadlineAndScopes(t *testing.T) {
	ctx := context.Background()
	wide := testHost(t) // scopes: read, comment
	narrow := testHost(t, func(c *Config) {
		c.Surfaces[0].Scopes = []string{"read"}
	})

	tok, err := wide.MintNonce(ctx, "dashboard", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := wide.Exchange(ctx, tok, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	orig, err := narrow.VerifyGrant(ctx, res.Grant)
	if err != nil {
		t.Fatalf("VerifyGrant on narrow host: %v", err)
	}

	next, err := narrow.Refresh(ctx, res.Grant)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	rolled, err := narrow.VerifyGrant(ctx, next.Token)
	if err != nil {
		t.Fatalf("VerifyGrant(refreshed): %v", err)
	}
	if !rolled.Deadline.Equal(orig.Deadline) {
		t.Errorf("refresh moved the absolute deadline: %v -> %v (refresh must never extend it)", orig.Deadline, rolled.Deadline)
	}
	if !rolled.HasScope("read") {
		t.Error("refreshed grant lost the still-declared scope")
	}
	if rolled.HasScope("comment") {
		t.Error("refreshed grant kept a scope the surface no longer declares")
	}
}
