package handler

import (
	"context"
	"testing"
)

type recheckRolesKey struct{}

// The re-check runs the identity step, then each derivation step on the
// context it returned, and keeps itself installed on the refreshed context.
// SetUser drops it, so a later middleware that switches or clears the user
// cannot leave the earlier principal's check behind.
func TestRecheckPrincipalRunsAndResets(t *testing.T) {
	ctx := context.Background()
	if _, ok := RecheckPrincipal(ctx); !ok {
		t.Fatal("nothing installed must re-check as ok")
	}

	live := true
	ctx = SetUser(ctx, "alice")
	ctx = WithPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
		if !live {
			return c, false
		}
		return SetUser(c, "alice-fresh"), true
	})
	ctx = AddPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
		u, _ := GetUser(c)
		return context.WithValue(c, recheckRolesKey{}, "roles-of-"+u.(string)), true
	})

	got, ok := RecheckPrincipal(ctx)
	if !ok {
		t.Fatal("live principal failed the re-check")
	}
	if u, _ := GetUser(got); u != "alice-fresh" {
		t.Fatalf("user = %v, want the re-resolved principal", u)
	}
	if r := got.Value(recheckRolesKey{}); r != "roles-of-alice-fresh" {
		t.Fatalf("derivation step saw %v, want it run on the refreshed user", r)
	}
	if _, ok := RecheckPrincipal(got); !ok {
		t.Fatal("refreshed context lost its installed check")
	}

	live = false
	if _, ok := RecheckPrincipal(ctx); ok {
		t.Fatal("SECURITY: a revoked principal passed the re-check")
	}

	switched := SetUser(ctx, "bob")
	if _, ok := RecheckPrincipal(switched); !ok {
		t.Fatal("SetUser kept the previous principal's check installed")
	}
}
