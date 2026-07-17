package access_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "github.com/mattn/go-sqlite3"
)

func newGrantDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestGrantStore_EnsureSchemaIdempotent(t *testing.T) {
	db := newGrantDB(t)
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	ctx := context.Background()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	// Second call must not error — CREATE TABLE IF NOT EXISTS is idempotent.
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestGrantStore_LoadIntoHydratesPolicy(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	// Seed grants directly into the table.
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := store.Grant(ctx, "admin", "posts:read", "posts:write"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := store.Grant(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("Grant editor: %v", err)
	}

	// Fresh policy, load from DB.
	fresh := access.NewRolePolicy()
	store2 := access.NewGrantStore(db, fresh)
	if err := store2.LoadInto(ctx, fresh); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	roles := fresh.Roles()
	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "editor" {
		t.Fatalf("roles = %v, want [admin editor]", roles)
	}
	adminPerms := fresh.PermissionsOf("admin")
	if len(adminPerms) != 2 {
		t.Fatalf("admin perms = %v, want 2", adminPerms)
	}
}

func TestGrantStore_GrantUpdatesLivePolicy(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Before grant: Can denies.
	c := ctxFor(policy, "editor")
	if access.Can(c, "posts:read") {
		t.Fatal("expected Can=false before grant")
	}

	// Grant via store — should flip the live policy.
	if err := store.Grant(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if !access.Can(c, "posts:read") {
		t.Fatal("expected Can=true after grant")
	}
}

func TestGrantStore_RevokeRemovesFromBoth(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policy := access.NewRolePolicy()
	policy.Grant("editor", "posts:read", "posts:write")
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Persist the code-defined grants.
	if err := store.Grant(ctx, "editor", "posts:read", "posts:write"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	c := ctxFor(policy, "editor")
	if !access.Can(c, "posts:write") {
		t.Fatal("expected Can=true before revoke")
	}

	// Revoke via store — should remove from DB + live policy.
	if err := store.Revoke(ctx, "editor", "posts:write"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if access.Can(c, "posts:write") {
		t.Fatal("expected Can=false after revoke")
	}
	// posts:read should still be held.
	if !access.Can(c, "posts:read") {
		t.Fatal("expected posts:read still held after revoking posts:write")
	}

	// Verify the DB row is gone: reload into a fresh policy.
	fresh := access.NewRolePolicy()
	store2 := access.NewGrantStore(db, fresh)
	if err := store2.LoadInto(ctx, fresh); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	perms := fresh.PermissionsOf("editor")
	if len(perms) != 1 || perms[0] != "posts:read" {
		t.Fatalf("after revoke, DB has %v, want [posts:read]", perms)
	}
}

func TestGrantStore_GrantIdempotent(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Grant the same permission twice — ON CONFLICT DO NOTHING makes it a no-op.
	if err := store.Grant(ctx, "admin", "posts:read"); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := store.Grant(ctx, "admin", "posts:read"); err != nil {
		t.Fatalf("second grant: %v", err)
	}

	perms := policy.PermissionsOf("admin")
	if len(perms) != 1 {
		t.Fatalf("after duplicate grant, perms = %v, want 1", perms)
	}
}

func TestGrantStore_SQLInjectionLiteral(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// A role string containing SQL metacharacters must be stored literally —
	// no injection, no statement execution.
	evil := "x'; DROP TABLE access_grants; --"
	if err := store.Grant(ctx, evil, "perm:test"); err != nil {
		t.Fatalf("Grant evil role: %v", err)
	}

	// The table must still exist (no DROP executed).
	perms := policy.PermissionsOf(evil)
	if len(perms) != 1 || perms[0] != "perm:test" {
		t.Fatalf("evil role perms = %v, want [perm:test]", perms)
	}

	// Verify the row round-trips from the DB.
	fresh := access.NewRolePolicy()
	store2 := access.NewGrantStore(db, fresh)
	if err := store2.LoadInto(ctx, fresh); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	perms = fresh.PermissionsOf(evil)
	if len(perms) != 1 || perms[0] != "perm:test" {
		t.Fatalf("DB round-trip evil role perms = %v, want [perm:test]", perms)
	}

	// Confirm the table still exists by querying it directly.
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM access_grants").Scan(&n)
	if err != nil {
		t.Fatalf("table query failed (injection?): %v", err)
	}
	if n < 1 {
		t.Fatalf("expected >=1 row, got %d", n)
	}
}

func TestRolePolicy_RolesSorted(t *testing.T) {
	rp := access.NewRolePolicy()
	rp.Grant("zebra", "a:read")
	rp.Grant("alpha", "a:read")
	rp.Grant("monkey", "a:read")

	roles := rp.Roles()
	if len(roles) != 3 {
		t.Fatalf("roles = %v, want 3", roles)
	}
	if roles[0] != "alpha" || roles[1] != "monkey" || roles[2] != "zebra" {
		t.Fatalf("roles not sorted: %v", roles)
	}
}

func TestRolePolicy_PermissionsOfReturnsCopy(t *testing.T) {
	rp := access.NewRolePolicy()
	rp.Grant("admin", "a:read", "a:write")

	perms := rp.PermissionsOf("admin")
	if len(perms) != 2 {
		t.Fatalf("perms = %v, want 2", perms)
	}

	// Mutate the returned slice — must not affect the policy.
	perms[0] = "tampered"
	again := rp.PermissionsOf("admin")
	if again[0] != "a:read" {
		t.Fatalf("policy was mutated via returned slice: %v", again)
	}
}

func TestRolePolicy_RolesEmptyWhenNoGrants(t *testing.T) {
	rp := access.NewRolePolicy()
	roles := rp.Roles()
	if len(roles) != 0 {
		t.Fatalf("expected empty roles, got %v", roles)
	}
}

func TestStoreGrantExpandsWildcard(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	policy := access.NewRolePolicy()
	policy.Register("teams:write", "posts:read", "teams:read")
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	if err := store.Grant(ctx, "editor", "teams:*"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	if got := strings.Join(grantRows(t, db, "editor"), ","); got != "teams:read,teams:write" {
		t.Fatalf("persisted grants = %q, want expanded capabilities", got)
	}
	if got := policy.PermissionsOf("editor"); len(got) != 2 || got[0] != "teams:read" || got[1] != "teams:write" {
		t.Fatalf("policy grants = %v, want expanded capabilities", got)
	}
}

func TestStoreLoadExpandsWildcard(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	seed := access.NewGrantStore(db, access.NewRolePolicy())
	if err := seed.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO access_grants (role, permission) VALUES ($1, $2)`,
		"editor", "teams:*"); err != nil {
		t.Fatalf("seed wildcard: %v", err)
	}

	policy := access.NewRolePolicy()
	policy.Register("teams:write", "posts:read", "teams:read")
	store := access.NewGrantStore(db, policy)
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	if got := policy.PermissionsOf("editor"); len(got) != 2 || got[0] != "teams:read" || got[1] != "teams:write" {
		t.Fatalf("policy grants = %v, want loaded wildcard expansion", got)
	}
}

func TestStoreStrictRejectsUnknown(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	policy := access.NewRolePolicy().StrictCapabilities()
	policy.Register("teams:read")
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	if err := store.Grant(ctx, "editor", "temas:read"); err == nil {
		t.Fatal("Grant returned nil for unknown strict capability")
	}
	if got := grantRows(t, db, "editor"); len(got) != 0 {
		t.Fatalf("strict rejection persisted rows: %v", got)
	}
}

func TestStoreLoadRejectsUnknown(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	seed := access.NewGrantStore(db, access.NewRolePolicy())
	if err := seed.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO access_grants (role, permission) VALUES ($1, $2)`,
		"editor", "temas:read"); err != nil {
		t.Fatalf("seed unknown grant: %v", err)
	}

	policy := access.NewRolePolicy().StrictCapabilities()
	policy.Register("teams:read")
	store := access.NewGrantStore(db, policy)
	if err := store.LoadInto(ctx, policy); err == nil {
		t.Fatal("LoadInto returned nil for unknown strict capability")
	}
	if got := policy.PermissionsOf("editor"); len(got) != 0 {
		t.Fatalf("strict load retained rejected capability: %v", got)
	}
}

func grantRows(t *testing.T, db *sql.DB, role string) []string {
	t.Helper()
	rows, err := db.Query(
		`SELECT permission FROM access_grants WHERE role = $1 ORDER BY permission`,
		role,
	)
	if err != nil {
		t.Fatalf("query grants: %v", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			t.Fatalf("scan grant: %v", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate grants: %v", err)
	}
	return permissions
}
