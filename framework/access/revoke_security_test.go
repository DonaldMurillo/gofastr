package access

import (
	"context"
	"testing"
)

// Pins the grant/revoke wildcard asymmetry, found by the 2026-09-04
// red-probe round; fixed in RolePolicy.Revoke and GrantStore.Revoke by
// running wildcard-shaped revokes through the same expansion Grant uses
// (prepareRevokes keeps the raw literal alongside its expansion).
// Property: Revoke must remove at least what Grant installed — revoking the
// wildcard form "teams:*" of a grant that Grant expanded to the registered
// "teams:*" capabilities must remove those capabilities (or reject the
// revoke), never return success while every expanded permission stays
// granted.
// Surfaces: framework/access/access.go::RolePolicy.Grant (expands via
// prepareGrants), framework/access/access.go::RolePolicy.Revoke (expands via
// prepareRevokes), framework/access/store.go::GrantStore.Grant (persists the
// expanded set), framework/access/store.go::GrantStore.Revoke (deletes and
// tombstones the expanded set plus the raw literal),
// battery/admin/rbac_admin.go::handleRBACRevoke (free-text permission field
// feeds GrantStore.Revoke, which now expands).
func TestRevokeWildcardUngantsExpansion(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	policy := NewRolePolicy()
	policy.Register("teams:read", "teams:write", "teams:invite")
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	// Grant the wildcard: Grant expands it against the registry and persists
	// the EXPANDED set (pinned by TestStoreGrantExpandsWildcard).
	if err := store.Grant(ctx, "editor", "teams:*"); err != nil {
		t.Fatalf("Grant(teams:*): %v", err)
	}
	if got := policy.PermissionsOf("editor"); len(got) != 3 {
		t.Fatalf("sanity: Grant(teams:*) installed %v, want the 3 expanded capabilities", got)
	}

	// Revoke the SAME wildcard through the store the admin RPC drives.
	if err := store.Revoke(ctx, "editor", "teams:*"); err != nil {
		t.Fatalf("Revoke(teams:*): %v", err)
	}

	// The property: after revoking the wildcard, none of the capabilities the
	// wildcard granted may survive, in memory...
	for _, p := range []string{"teams:read", "teams:write", "teams:invite"} {
		if hasPerm(policy.PermissionsOf("editor"), p) {
			t.Fatalf("SECURITY: [rbac-revoke] Revoke(editor, \"teams:*\") returned nil but the role still holds %q — the wildcard grant's expanded set survived the revoke (held: %v)", p, policy.PermissionsOf("editor"))
		}
	}

	// ...and not after a full reload from the DB either.
	if err := store.reloadRole(ctx, "editor"); err != nil {
		t.Fatalf("reloadRole: %v", err)
	}
	for _, p := range []string{"teams:read", "teams:write", "teams:invite"} {
		if hasPerm(policy.PermissionsOf("editor"), p) {
			t.Fatalf("SECURITY: [rbac-revoke] %q survived a full reload after Revoke(editor, \"teams:*\") (held: %v)", p, policy.PermissionsOf("editor"))
		}
	}

	// The in-memory twin of the same invariant: RolePolicy.Grant/Revoke
	// agree on what a wildcard means without the store in between.
	mem := NewRolePolicy()
	mem.Register("teams:read", "teams:write", "teams:invite")
	if err := mem.Grant("editor", "teams:*"); err != nil {
		t.Fatalf("RolePolicy.Grant(teams:*): %v", err)
	}
	if got := mem.PermissionsOf("editor"); len(got) != 3 {
		t.Fatalf("sanity: RolePolicy.Grant(teams:*) installed %v, want 3 expanded capabilities", got)
	}
	mem.Revoke("editor", "teams:*")
	for _, p := range []string{"teams:read", "teams:write", "teams:invite"} {
		if hasPerm(mem.PermissionsOf("editor"), p) {
			t.Fatalf("SECURITY: [rbac-revoke] RolePolicy.Revoke(editor, \"teams:*\") left %q granted — the in-memory grant/revoke pair disagrees on the wildcard (held: %v)", p, mem.PermissionsOf("editor"))
		}
	}

	// A wildcard held LITERALLY (granted before the registry existed) is
	// revoked by the same call, so a code-seeded literal wildcard cannot
	// outlive its revoke once capabilities are registered.
	lit := NewRolePolicy()
	if err := lit.Grant("editor", "teams:*"); err != nil { // empty registry: stays literal
		t.Fatalf("empty-registry Grant(teams:*): %v", err)
	}
	if got := lit.PermissionsOf("editor"); len(got) != 1 || got[0] != "teams:*" {
		t.Fatalf("sanity: empty-registry Grant kept %v, want the literal [teams:*]", got)
	}
	lit.Register("teams:read", "teams:write", "teams:invite")
	lit.Revoke("editor", "teams:*")
	if got := lit.PermissionsOf("editor"); len(got) != 0 {
		t.Fatalf("SECURITY: [rbac-revoke] the literal wildcard grant survived Revoke(editor, \"teams:*\") after capabilities were registered (held: %v)", got)
	}
}
