package admin

import (
	"context"
	"net/url"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// An admin could post their own account with roles that drop admin: the
// only admin then lost every admin page, and nonAssignableRole refused
// granting admin back from a tier that no longer held it. Removing your
// own admin role is refused; another admin may still remove it.
func TestAssignRefusesOwnAdminRemoval(t *testing.T) {
	r := newRBACEnv(t, Config{})
	stored, _, err := r.mgr.UserStore().FindByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	self := roleUser{id: stored.GetID(), roles: []string{"admin"}}
	vals := url.Values{"user_id": {self.id}, "roles": {"editor"}}
	if got := resultOf(t, post(r.as(self), "/admin/rbac/_assign", vals)); got != "own-admin-refused" {
		t.Fatalf("self-demotion result = %q, want own-admin-refused", got)
	}
	if got := r.rolesOf(self.id); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("roles after a refused self-demotion = %v", got)
	}
	if ops := r.auditOps("access"); !slices.Equal(ops, []string{"assign-roles-refused"}) {
		t.Fatalf("audit ops = %v", ops)
	}
	// Keeping admin while adding a role is no removal.
	vals = url.Values{"user_id": {self.id}, "roles": {"admin,editor"}}
	if got := resultOf(t, post(r.as(self), "/admin/rbac/_assign", vals)); got != "roles-saved" {
		t.Fatalf("self-assign keeping admin = %q", got)
	}
	// A second admin may remove the first one's role.
	other := roleUser{id: "admin-2", roles: []string{"admin"}}
	vals = url.Values{"user_id": {self.id}, "roles": {"editor"}}
	if got := resultOf(t, post(r.as(other), "/admin/rbac/_assign", vals)); got != "roles-saved" {
		t.Fatalf("another admin's removal = %q", got)
	}
}

// The last admin could revoke a permission from their own role (no
// Wildcard) on the Roles grid: callerHoldsPermission then refused every
// grant of it back, and the tombstone outlived reloads. A revoke that
// leaves the caller's own roles without the permission is refused; one
// another held role still covers goes through.
func TestRevokeRefusesOwnLastPermission(t *testing.T) {
	r := newRBACEnv(t, Config{})
	ctx := context.Background()
	r.policy.Revoke("admin", access.Wildcard)
	_ = r.policy.Grant("admin", "posts:read", "queue:read")
	if err := r.store.LoadInto(ctx, r.policy); err != nil {
		t.Fatal(err)
	}
	stored, _, err := r.mgr.UserStore().FindByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	self := roleUser{id: stored.GetID(), roles: []string{"admin"}}
	// admin keeps posts:read (0:0) and drops queue:read.
	grid := url.Values{"role": {"admin"}, "permission": {"posts:read", "queue:read"}, "grant": {"0:0"}}
	if got := resultOf(t, post(r.as(self), "/admin/rbac/_permissions", grid)); got != "own-permission-refused" {
		t.Fatalf("revoke of own last path = %q, want own-permission-refused", got)
	}
	if !canAs(r.policy, "admin", "queue:read") {
		t.Fatal("a refused revoke still took queue:read from admin")
	}
	// A tombstone written behind the refusal would take it back on the
	// next boot, so read the stored grants again.
	if err := r.store.LoadInto(ctx, r.policy); err != nil {
		t.Fatal(err)
	}
	if !canAs(r.policy, "admin", "queue:read") {
		t.Fatal("a refused revoke left a tombstone: queue:read is gone after a reload")
	}
	if ops := r.auditOps("access"); !slices.Equal(ops, []string{"revoke-refused"}) {
		t.Fatalf("audit ops = %v", ops)
	}
	// A second held role that keeps the permission makes the revoke safe.
	_ = r.policy.Grant("support", "queue:read")
	both := roleUser{id: self.id, roles: []string{"admin", "support"}}
	if got := resultOf(t, post(r.as(both), "/admin/rbac/_permissions", grid)); got != "permissions-saved" {
		t.Fatalf("revoke covered by another held role = %q", got)
	}
}
