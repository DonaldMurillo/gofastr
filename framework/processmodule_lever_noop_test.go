package framework

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A revoke that takes nothing away is an error from the supervisor, not
// a quiet success: the admin's Modules page audits and reports a nil as
// a revoke that happened.
func TestRevokeGrantsOfAnUnheldGrantErrors(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	ctx := context.Background()
	d := validDescriptor()
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read", "articles:write"}); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetDesired(ctx, d.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []access.Permission{"users:read", "articles:delete", "not a scope"} {
		if gen, err := sup.RevokeGrants(ctx, d.Name, []access.Permission{grant}); err == nil {
			t.Errorf("revoking %q, which the module does not hold, answered generation %d and no error", grant, gen)
		}
	}
	after, err := store.GetDesired(ctx, d.Name)
	if err != nil || after.DesiredGeneration != before.DesiredGeneration ||
		!slices.Equal(after.EffectiveGrants, before.EffectiveGrants) || !slices.Equal(after.RevokedGrants, before.RevokedGrants) {
		t.Fatalf("refused revokes changed the module: before=%+v after=%+v err=%v", before, after, err)
	}
	gen, err := sup.RevokeGrants(ctx, d.Name, []access.Permission{"articles:write"})
	if err != nil || gen != before.DesiredGeneration+1 {
		t.Fatalf("revoking a held grant = %d, %v", gen, err)
	}
	if _, err := sup.RevokeGrants(ctx, d.Name, []access.Permission{"articles:write"}); err == nil {
		t.Error("revoking the same grant a second time answered no error")
	}
}

// The other levers have no nothing-happened answer to hide: enable and
// disable set a state that holds when they return, and a bump always
// moves the generation.
func TestOtherLeversDoWhatTheyAnswer(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	ctx := context.Background()
	d := validDescriptor()
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read", "articles:write"}); err != nil {
		t.Fatal(err)
	}
	enabled := func() bool {
		t.Helper()
		got, err := store.GetDesired(ctx, d.Name)
		if err != nil {
			t.Fatal(err)
		}
		return got.Enabled
	}
	for range 2 {
		if err := sup.Disable(ctx, d.Name); err != nil || enabled() {
			t.Fatalf("disable: err=%v enabled=%v", err, enabled())
		}
	}
	first, err := sup.BumpGeneration(ctx, d.Name)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := sup.BumpGeneration(ctx, d.Name); err != nil || second != first+1 {
		t.Fatalf("second bump = %d, %v; want %d", second, err, first+1)
	}
}
