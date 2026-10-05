package access

import (
	"context"
	"testing"
)

// A grant made while the registry did not yet know a resource persists the
// wildcard literally ("reports:*"). Once a later release registers
// reports:read and reports:delete, a revoke of reports:delete must stay
// revoked through every reload path, not only LoadInto: the reload has to
// expand the literal row before it subtracts the tombstone.
func TestReloadKeepsWildcardRevoke(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	p0 := NewRolePolicy()
	p0.Register("posts:read")
	s0 := NewGrantStore(db, p0)
	if err := s0.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s0.LoadInto(ctx, p0); err != nil {
		t.Fatal(err)
	}
	if err := s0.Grant(ctx, "analyst", "reports:*"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	boot := func() (*GrantStore, *RolePolicy) {
		p := NewRolePolicy()
		p.Register("posts:read", "reports:read", "reports:delete")
		s := NewGrantStore(db, p)
		if err := s.LoadInto(ctx, p); err != nil {
			t.Fatal(err)
		}
		return s, p
	}
	sa, pa := boot()
	sb, pb := boot()

	if err := sa.Revoke(ctx, "analyst", "reports:delete"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	holds := func(p *RolePolicy, perm Permission) bool {
		return permIn(p.PermissionsOf("analyst"), perm)
	}
	steps := []struct {
		name   string
		reload func() error
		policy *RolePolicy
	}{
		{"peer reloadRole", func() error { return sb.reloadRole(ctx, "analyst") }, pb},
		{"origin reloadRole", func() error { return sa.reloadRole(ctx, "analyst") }, pa},
		{"peer reloadAll", func() error { return sb.reloadAll(ctx) }, pb},
	}
	for _, st := range steps {
		if err := st.reload(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if holds(st.policy, "reports:delete") {
			t.Fatalf("%s resurrected revoked reports:delete: %v", st.name, st.policy.PermissionsOf("analyst"))
		}
		if !holds(st.policy, "reports:read") {
			t.Fatalf("%s dropped reports:read, which was never revoked", st.name)
		}
	}
}
