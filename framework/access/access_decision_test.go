package access_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// ctxFor builds a context carrying the policy plus the given roles, the
// way RequirePermission middleware expects to find them.
func ctxFor(rp *access.RolePolicy, roles ...string) context.Context {
	return access.WithRoles(access.WithPolicy(context.Background(), rp), roles)
}

// Testimonial: documents that RolePolicy.Can makes a real allow/deny
// decision driven by Grant/Revoke; reported issue rbac-1 (Can ignores
// grants / always allows or denies) does not reproduce.
//
// TestCan_GrantedActionAllowed is the positive authorization decision:
// a role granted an action must have Can return true for it.
func TestCan_GrantedActionAllowed(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write")
	ctx := ctxFor(rp, "editor")

	if !rp.Can(ctx, "posts:write", nil) {
		t.Fatalf("Can(posts:write) = false for editor granted posts:write, want true")
	}
}

// Testimonial: documents that RolePolicy.Can denies actions that were
// never granted; reported issue rbac-1 does not reproduce.
//
// TestCan_UngrantedActionDenied is the negative authorization decision:
// an action the role was never granted must be denied.
func TestCan_UngrantedActionDenied(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write")
	ctx := ctxFor(rp, "editor")

	if rp.Can(ctx, "posts:delete", nil) {
		t.Fatalf("Can(posts:delete) = true for editor without that grant, want false")
	}
}

// TestCan_RevokedActionDenied confirms revoking a grant flips the
// decision from allow to deny.
func TestCan_RevokedActionDenied(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write", "posts:delete")
	rp.Revoke("editor", "posts:delete")
	ctx := ctxFor(rp, "editor")

	if !rp.Can(ctx, "posts:write", nil) {
		t.Fatalf("Can(posts:write) = false after revoking only posts:delete, want true")
	}
	if rp.Can(ctx, "posts:delete", nil) {
		t.Fatalf("Can(posts:delete) = true after revoke, want false")
	}
}

// TestCan_NoRolesDenied confirms an anonymous subject (no roles in ctx)
// is denied even for a permission that exists in the policy.
func TestCan_NoRolesDenied(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write")
	ctx := access.WithPolicy(context.Background(), rp) // no roles attached

	if rp.Can(ctx, "posts:write", nil) {
		t.Fatalf("Can(posts:write) = true with no roles in ctx, want false")
	}
}

// TestCan_PermissionFromOneOfManyRoles confirms the decision unions
// permissions across all of a subject's roles.
func TestCan_PermissionFromOneOfManyRoles(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("reader", "posts:read")
	rp.Grant("editor", "posts:write")
	ctx := ctxFor(rp, "reader", "editor")

	if !rp.Can(ctx, "posts:read", nil) {
		t.Fatalf("Can(posts:read) = false for reader+editor, want true")
	}
	if !rp.Can(ctx, "posts:write", nil) {
		t.Fatalf("Can(posts:write) = false for reader+editor, want true")
	}
	if rp.Can(ctx, "posts:delete", nil) {
		t.Fatalf("Can(posts:delete) = true for reader+editor, want false")
	}
}
