package framework

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// Revoking a grant takes that grant away and keeps the rest; it used to
// replace the set with the grant it named, so revoking write from a
// read-and-write module left it write-only (2026-10-27 A14).
func TestRevokeKeepsOtherGrants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"),
		EffectiveGrants: []access.Permission{"x:read", "x:write", "y:read"}})
	gen, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{"x:write"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetDesired(ctx, "demo")
	if !slices.Equal(got.EffectiveGrants, []access.Permission{"x:read", "y:read"}) {
		t.Errorf("grants = %v, want [x:read y:read]", got.EffectiveGrants)
	}
	if gen != 2 || got.DesiredGeneration != 2 {
		t.Errorf("gen = %d, stored %d, want 2", gen, got.DesiredGeneration)
	}
}

// A write that lands between the revoke's read and its write is kept:
// the revoke retries on the generation it read, so two revokes at once
// both stick.
func TestRevokeKeepsConcurrentRevoke(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"),
		EffectiveGrants: []access.Permission{"x:read", "x:write", "y:read"}})
	raced := false
	store.afterGrantsRead = func() {
		if !raced {
			raced = true
			if _, err := store.SetEffectiveGrants(ctx, "demo", []access.Permission{"x:write", "y:read"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{"x:write"}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetDesired(ctx, "demo")
	if !slices.Equal(got.EffectiveGrants, []access.Permission{"y:read"}) {
		t.Errorf("SECURITY: grants = %v, want [y:read]: a concurrent revoke was undone", got.EffectiveGrants)
	}
}

// A revoke that names nothing is refused: under the old replace reading
// an empty list revoked everything, and a caller still passing one must
// not silently keep every grant.
func TestRevokeNamingNothingRefused(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"),
		EffectiveGrants: []access.Permission{"x:read"}})
	if _, err := store.RemoveEffectiveGrants(ctx, "demo", nil); err == nil {
		t.Error("a revoke naming no grant succeeded")
	}
}
