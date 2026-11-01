package access

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// atomicStore opens a SQLite GrantStore whose admin role holds
// users:delete in the database.
func atomicStore(t *testing.T) (*sql.DB, *GrantStore, *RolePolicy) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	policy := NewRolePolicy()
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if err := store.Grant(ctx, "admin", Permission("users:delete")); err != nil {
		t.Fatal(err)
	}
	return db, store, policy
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE role = 'admin' AND permission = 'users:delete'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Revoke deleted the grant row and then wrote the tombstone as separate
// statements, so a failed tombstone write left the row gone while this
// replica's policy still granted: a fresh boot denied, this one allowed
// (bug hunt r7 R7-067). Both writes are one transaction now.
func TestRevokeFailedTombstoneKeepsGrant(t *testing.T) {
	db, store, policy := atomicStore(t)
	if _, err := db.Exec(`CREATE TRIGGER no_stone BEFORE INSERT ON access_grants_revoked
		BEGIN SELECT RAISE(ABORT, 'tombstone write refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(context.Background(), "admin", Permission("users:delete")); err == nil {
		t.Fatal("Revoke succeeded with the tombstone write refused")
	}
	if n := countRows(t, db, "access_grants"); n != 1 {
		t.Errorf("grant rows after the failed revoke = %d, want the row kept", n)
	}
	if !hasPerm(policy.PermissionsOf("admin"), "users:delete") {
		t.Error("the failed revoke changed the live policy")
	}
}

// Grant has the mirror shape: the grant row went in, then lifting the
// tombstone failed, leaving a grant row the tombstone still cancels.
func TestGrantFailedLiftKeepsNoRow(t *testing.T) {
	db, store, policy := atomicStore(t)
	ctx := context.Background()
	if err := store.Revoke(ctx, "admin", Permission("users:delete")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER no_lift BEFORE DELETE ON access_grants_revoked
		BEGIN SELECT RAISE(ABORT, 'tombstone delete refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.Grant(ctx, "admin", Permission("users:delete")); err == nil {
		t.Fatal("Grant succeeded with the tombstone delete refused")
	}
	if n := countRows(t, db, "access_grants"); n != 0 {
		t.Errorf("grant rows after the failed grant = %d, want none written", n)
	}
	if hasPerm(policy.PermissionsOf("admin"), "users:delete") {
		t.Error("the failed grant changed the live policy")
	}
}
