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

func TestPatternRevokeRemovesHeldGrants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	initial := DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"), EffectiveGrants: []access.Permission{"orders:read", "orders:write", "users:read"}}
	if err := store.Install(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{"orders:*"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetDesired(ctx, "demo")
	if err != nil || !slices.Equal(got.EffectiveGrants, []access.Permission{"users:read"}) {
		t.Errorf("effective grants=%v err=%v, want users:read only", got.EffectiveGrants, err)
	}
	if _, err := store.InstallOrUpdate(ctx, initial); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetDesired(ctx, "demo")
	if err != nil || !slices.Equal(got.EffectiveGrants, []access.Permission{"users:read"}) {
		t.Errorf("reinstalled grants=%v err=%v, want users:read only", got.EffectiveGrants, err)
	}
}

func TestIneffectiveRevokeRefused(t *testing.T) {
	for name, grants := range map[string][]access.Permission{
		"absent":   {"users:read"},
		"wildcard": {"orders:*"},
		"overlap":  {"orders:read", "orders:*"},
		"crossed":  {"*:read"},
	} {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			if err := store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"), EffectiveGrants: grants}); err != nil {
				t.Fatal(err)
			}
			before, err := store.GetDesired(ctx, "demo")
			if err != nil {
				t.Fatal(err)
			}
			revoke := access.Permission("orders:read")
			if name == "crossed" {
				revoke = "orders:*"
			}
			if _, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{revoke}); err == nil {
				t.Error("a revoke that cannot remove the permission succeeded")
			}
			after, err := store.GetDesired(ctx, "demo")
			if err != nil || after.DesiredGeneration != before.DesiredGeneration || !slices.Equal(after.EffectiveGrants, before.EffectiveGrants) || !slices.Equal(after.RevokedGrants, before.RevokedGrants) {
				t.Errorf("refused revoke changed state: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}
