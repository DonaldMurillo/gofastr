package embed

import (
	"context"
	"errors"
	"testing"
	"time"
)

// MintGrant must never let a grant's Expires run past its Deadline.
//
// The deadline is the absolute cap on a credential's life, fixed when the nonce
// is exchanged and carried unchanged across every later refresh. Refreshing near
// the deadline with a TTL wider than the remaining window would otherwise mint a
// grant whose Expires is a full TTL past the deadline, and VerifyGrant checks
// only Expires, so the credential would outlive its absolute cap. This is the
// load-bearing clamp on the otherwise-immortal refresh path.
func TestMintGrantClampsExpiryToDeadline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// Only one minute of life remains under the absolute cap.
	deadline := now.Add(time.Minute)
	n := Nonce{
		Surface: "dashboard",
		Subject: "u-1",
		Origin:  "https://acme.com",
		Scopes:  []string{"read"},
	}
	// ...but the per-grant TTL is fifteen minutes. Without the clamp the minted
	// Expires is fourteen minutes past the deadline.
	tok, err := MintGrant(testGrantKey, n, 15*time.Minute, deadline, now)
	if err != nil {
		t.Fatalf("MintGrant: %v", err)
	}
	g, err := VerifyGrant(testGrantKey, tok, now)
	if err != nil {
		t.Fatalf("VerifyGrant: %v", err)
	}
	if g.Expires.After(deadline) {
		t.Fatalf("Expires %v runs past Deadline %v — the credential outlives its absolute cap by %s",
			g.Expires, deadline, g.Expires.Sub(deadline))
	}
	// Deadline itself round-trips unchanged.
	if !g.Deadline.Equal(deadline) {
		t.Fatalf("Deadline = %v, want %v (Refresh must move Expires, never Deadline)", g.Deadline, deadline)
	}
}

// Refresh must refuse a grant whose Deadline has lapsed, even if its Expires is
// still in the future.
//
// In a correctly-clamped grant Expires <= Deadline, so a lapsed Deadline always
// coincides with a lapsed Expires and VerifyGrant refuses first. The state
// below, future Expires and past Deadline, is exactly what a removed or buggy
// expiry clamp produces, and Refresh is the backstop that keeps such a grant
// from being rolled forward instead of refused. Deleting the ErrGrantExhausted
// guard makes Refresh return a refreshed credential for a grant that should be
// dead.
func TestRefreshRefusesAGrantPastItsDeadline(t *testing.T) {
	h := testHost(t)
	now := time.Now()
	// A grant whose expiry is still good for an hour, but whose absolute
	// deadline passed a second ago. VerifyGrant checks only Expires, so this
	// token verifies; the lapsed deadline is for Refresh to catch.
	claims := grantClaims{
		Surface:  "dashboard",
		Subject:  "u-1",
		Origin:   "https://acme.com",
		Scopes:   []string{"read"},
		Expires:  now.Add(time.Hour).Unix(),
		Deadline: now.Add(-time.Second).Unix(),
	}
	tok, err := sign(GrantPrefix, testGrantKey, claims)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := h.Refresh(context.Background(), tok); !errors.Is(err, ErrGrantExhausted) {
		t.Fatalf("Refresh of a grant past its deadline: err = %v, want ErrGrantExhausted", err)
	}
}
