package featureflag

import (
	"context"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A user allow-list entry names one tenant's user. Allow-listing "42"
// used to turn the flag on for user 42 in every tenant: user ids are
// per-tenant in a multi-tenant app, so that was every tenant's 42.

func userFlag(t *testing.T, users ...string) *Evaluator {
	t.Helper()
	s := NewMemoryStore()
	if err := s.Set(Flag{Key: "beta", Enabled: true, Users: users}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return NewEvaluator(s)
}

func evalUser(e *Evaluator, tenant, user string) bool {
	return e.Bool(WithContext(context.Background(), EvalContext{UserID: user, TenantID: tenant}), "beta")
}

func TestUserAllowListRefusesOtherTenants(t *testing.T) {
	bare := userFlag(t, "42")
	if evalUser(bare, "acme", "42") || evalUser(bare, "beta", "42") {
		t.Error("SECURITY: [featureflag] a bare allow-list id matched user 42 inside a tenant. " +
			"Attack: allow-listing one tenant's user 42 enables the flag for user 42 of every tenant.")
	}
	scoped := userFlag(t, "acme:42")
	if !evalUser(scoped, "acme", "42") {
		t.Error("acme:42 did not match tenant acme's user 42")
	}
	if evalUser(scoped, "beta", "42") {
		t.Error("SECURITY: [featureflag] acme:42 matched tenant beta's user 42")
	}
	if evalUser(scoped, "acme", "43") {
		t.Error("acme:42 matched user 43")
	}
	if evalUser(scoped, "", "42") {
		t.Error("acme:42 matched a context with no tenant")
	}
}

// With no tenant in the context (a single-tenant app), a bare id
// matches as it always did.
func TestUserAllowListBareIDWithoutTenant(t *testing.T) {
	e := userFlag(t, "42", "alice@example.com")
	if !evalUser(e, "", "42") || !evalUser(e, "", "alice@example.com") {
		t.Fatal("bare ids stopped matching in a single-tenant context")
	}
	if evalUser(e, "", "43") {
		t.Fatal("bare 42 matched user 43")
	}
}

// The tenant the framework resolved for the request (handler's tenant
// slot, which tenant.ResolveMiddleware fills) counts when the
// EvalContext leaves TenantID empty, so an app that forgets it does not
// fall back to every-tenant matching. An explicit TenantID wins.
func TestUserAllowListReadsRequestTenant(t *testing.T) {
	bare := userFlag(t, "42")
	scoped := userFlag(t, "acme:42")
	ctx := handler.SetTenantID(context.Background(), "acme")
	ctx = WithContext(ctx, EvalContext{UserID: "42"})
	if bare.Bool(ctx, "beta") {
		t.Error("SECURITY: [featureflag] bare 42 matched while the request resolved tenant acme")
	}
	if !scoped.Bool(ctx, "beta") {
		t.Error("acme:42 did not match the request's resolved tenant acme")
	}
	explicit := WithContext(ctx, EvalContext{UserID: "42", TenantID: "beta"})
	if scoped.Bool(explicit, "beta") {
		t.Error("the request tenant overrode an explicit EvalContext.TenantID")
	}
	// The Tenants list reads the same tenant.
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "beta", Enabled: true, Tenants: []string{"acme"}})
	if !NewEvaluator(s).Bool(WithContext(handler.SetTenantID(context.Background(), "acme"), EvalContext{}), "beta") {
		t.Error("Tenants list did not read the request's resolved tenant")
	}
}

// Ids may hold ':'. The entry splits at the FIRST ':', the tenant part
// percent-escapes ':' and '%', and the user part is the raw id, so
// every (tenant, user) pair has one spelling and parses back to itself.
func TestUserEntrySpellingIsUnambiguous(t *testing.T) {
	cases := []struct{ tenant, user, entry string }{
		{"", "42", "42"},
		{"", "google:42", ":google:42"},
		{"acme", "42", "acme:42"},
		{"acme", "google:42", "acme:google:42"},
		{"a:b", "c", "a%3Ab:c"},
		{"50%", "x", "50%25:x"},
	}
	for _, c := range cases {
		if got := UserEntry(c.tenant, c.user); got != c.entry {
			t.Errorf("UserEntry(%q, %q) = %q, want %q", c.tenant, c.user, got, c.entry)
		}
		tenant, user, err := ParseUserEntry(c.entry)
		if err != nil || tenant != c.tenant || user != c.user {
			t.Errorf("ParseUserEntry(%q) = %q, %q, %v; want %q, %q", c.entry, tenant, user, err, c.tenant, c.user)
		}
		e := userFlag(t, c.entry)
		if !evalUser(e, c.tenant, c.user) {
			t.Errorf("%q did not match tenant %q user %q", c.entry, c.tenant, c.user)
		}
	}
	// "a:b:c" is tenant a, user b:c, never tenant a:b, user c.
	e := userFlag(t, "a:b:c")
	if evalUser(e, "a:b", "c") || !evalUser(e, "a", "b:c") {
		t.Error("a:b:c was not read as tenant a, user b:c")
	}
	// A bare id that holds ':' is spelled with an empty tenant part; the
	// unprefixed spelling names a tenant.
	if evalUser(userFlag(t, "google:42"), "", "google:42") {
		t.Error("google:42 matched user google:42 with no tenant; it names tenant google")
	}
}

// A malformed entry is refused when a flag is stored, and never matches
// when a store hands one over anyway.
func TestMalformedUserEntries(t *testing.T) {
	bad := []string{"", "acme:", ":", "%zz:42", "%3:42"}
	for _, entry := range bad {
		if _, _, err := ParseUserEntry(entry); err == nil {
			t.Errorf("ParseUserEntry(%q) accepted a malformed entry", entry)
		}
		if err := NewMemoryStore().Set(Flag{Key: "k", Users: []string{"ok", entry}}); err == nil {
			t.Errorf("MemoryStore.Set accepted users entry %q", entry)
		}
		_, s := openSQLStore(t)
		if err := s.Set(Flag{Key: "k", Users: []string{entry}}); err == nil {
			t.Errorf("SQLStore.Set accepted users entry %q", entry)
		}
	}
	raw := rawStore{f: &Flag{Key: "beta", Enabled: true, Users: []string{"acme:", "%zz:42", ""}}}
	e := NewEvaluator(raw)
	for _, c := range [][2]string{{"acme", ""}, {"", "acme:"}, {"%zz", "42"}, {"", ""}} {
		if evalUser(e, c[0], c[1]) {
			t.Errorf("malformed entry matched tenant %q user %q", c[0], c[1])
		}
	}
}

type rawStore struct{ f *Flag }

func (r rawStore) Get(context.Context, string) (*Flag, error) { f := *r.f; return &f, nil }

// The rollout bucket is keyed on tenant + user when the context has a
// tenant, so user 42 of two tenants are bucketed independently. With no
// tenant the subject is the bare user id, as before (cohorts in
// single-tenant apps do not reshuffle), and a tenant alone buckets on
// the tenant id.
func TestRolloutBucketsPerTenantUser(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set(Flag{Key: "ramp", Enabled: true, Rollout: 50})
	e := NewEvaluatorWithSalt(s, "pepper")
	on := func(tenant, user string) bool {
		return e.Bool(WithContext(context.Background(), EvalContext{UserID: user, TenantID: tenant}), "ramp")
	}
	differ := 0
	for i := range 200 {
		u := fmt.Sprint("u", i)
		if on("acme", u) != on("beta", u) {
			differ++
		}
		if on("acme", u) != (bucket("pepper", "ramp", UserEntry("acme", u)) < 50) {
			t.Fatalf("tenant user %s not bucketed on acme:%s", u, u)
		}
		if on("", u) != (bucket("pepper", "ramp", u) < 50) {
			t.Fatalf("single-tenant user %s no longer bucketed on the bare id", u)
		}
	}
	if differ == 0 {
		t.Fatal("SECURITY: [featureflag] user ids share one rollout bucket across tenants")
	}
	if on("acme", "") != (bucket("pepper", "ramp", "acme") < 50) {
		t.Fatal("a tenant-only subject is no longer bucketed on the tenant id")
	}
}
