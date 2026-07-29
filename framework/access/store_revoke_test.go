package access

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRevokeSurvivesReload(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	policy := NewRolePolicy()
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := policy.Grant("admin", Permission("users:delete"), Permission("users:read")); err != nil {
		t.Fatalf("policy.Grant: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	if err := store.Revoke(ctx, "admin", Permission("users:delete")); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// A peer refresh must not restore a code-seeded grant revoked locally.
	if err := store.reloadRole(ctx, "admin"); err != nil {
		t.Fatalf("reloadRole after revoke: %v", err)
	}
	if hasPerm(policy.PermissionsOf("admin"), "users:delete") {
		t.Fatalf("revoked grant restored after reload: %v", policy.PermissionsOf("admin"))
	}
	if !hasPerm(policy.PermissionsOf("admin"), "users:read") {
		t.Fatalf("unrevoked baseline grant lost after reload: %v", policy.PermissionsOf("admin"))
	}

	if err := store.Grant(ctx, "admin", Permission("users:delete")); err != nil {
		t.Fatalf("regrant: %v", err)
	}
	if err := store.reloadRole(ctx, "admin"); err != nil {
		t.Fatalf("reloadRole after regrant: %v", err)
	}
	if !hasPerm(policy.PermissionsOf("admin"), "users:delete") {
		t.Fatalf("regranted permission lost after reload: %v", policy.PermissionsOf("admin"))
	}
}

func hasPerm(perms []Permission, want string) bool {
	for _, p := range perms {
		if string(p) == want {
			return true
		}
	}
	return false
}
