package access

// Can, CanResource and CanResourceExact read the caller's roles. A scoped
// API token carries its owner's roles, so an orders:update token an admin
// minted could run an orders:approve transition or the state override: the
// CRUD route gate checked the token's scope, then crud/states.go checked the
// transition's permission with CanResourceExact, which never read scopes.
// Every Can-family check now also requires ScopeAllows for a scoped caller,
// as RequirePermission does, and a Decider's Allow does not lift it
// (issue #614).

import (
	"context"
	"testing"
)

func approverCtx(t *testing.T, scopes []string, d Decider) context.Context {
	t.Helper()
	p := NewRolePolicy()
	if err := p.Grant("admin", "orders:approve", "orders:update"); err != nil {
		t.Fatal(err)
	}
	ctx := WithRoles(WithPolicy(context.Background(), p), []string{"admin"})
	if d != nil {
		ctx = WithDecider(ctx, d)
	}
	if scopes != nil {
		ctx = WithHeldScopes(ctx, scopes)
	}
	return ctx
}

func TestCanFamilyHonorsTokenScopes(t *testing.T) {
	allow := func(context.Context, []string, Permission, Ref) Decision { return DecisionAllow }
	cases := []struct {
		name   string
		scopes []string
		d      Decider
		want   bool
	}{
		{"session", nil, nil, true},
		{"narrow token", []string{"orders:update"}, nil, false},
		{"empty token", []string{}, nil, false},
		{"granting token", []string{"orders:approve"}, nil, true},
		{"verb wildcard", []string{"orders:*"}, nil, true},
		{"decider allow", []string{"orders:update"}, allow, false},
	}
	ref := Ref{Type: "orders", ID: "1"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := approverCtx(t, tc.scopes, tc.d)
			if tc.d == nil {
				if got := Can(ctx, "orders:approve"); got != tc.want {
					t.Errorf("Can = %v, want %v", got, tc.want)
				}
			}
			if got := CanResource(ctx, "orders:approve", ref); got != tc.want {
				t.Errorf("CanResource = %v, want %v", got, tc.want)
			}
			if got := CanResourceExact(ctx, "orders:approve", ref); got != tc.want {
				t.Errorf("CanResourceExact = %v, want %v", got, tc.want)
			}
		})
	}
}
