package admin

import (
	"context"
	"net/url"
	"slices"
	"testing"
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
