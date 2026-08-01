package access

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// openAccessDB opens an in-memory SQLite DB serialised to a single
// connection (the package's test posture), so two stores backed by it share
// one database — the multi-replica shape.
func openAccessDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedStore mirrors real boot: declare a code grant on the policy, then
// LoadInto so the store captures it as baseline. Returns the bound store
// and policy. All stores share db (one database, many replicas).
func seedStore(t *testing.T, db *sql.DB, role string, perms ...Permission) (*GrantStore, *RolePolicy) {
	t.Helper()
	ctx := context.Background()
	policy := NewRolePolicy()
	if err := policy.Grant(role, perms...); err != nil {
		t.Fatalf("policy.Grant: %v", err)
	}
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	return store, policy
}

// TestRevokePropagatesToPeer: stores A and B share one DB and both seed the
// same code grant (real-boot shape). Revoke on A; drive B's reload the way
// fanout would (reloadRole directly on B's store). B must drop the
// permission — its own baseline must not merge the revoked grant back.
func TestRevokePropagatesToPeer(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	storeA, policyA := seedStore(t, db, "editor", Permission("posts:read"))
	storeB, policyB := seedStore(t, db, "editor", Permission("posts:read"))

	if !hasPerm(policyA.PermissionsOf("editor"), "posts:read") {
		t.Fatal("A: seed grant missing before revoke")
	}
	if !hasPerm(policyB.PermissionsOf("editor"), "posts:read") {
		t.Fatal("B: seed grant missing before revoke")
	}

	if err := storeA.Revoke(ctx, "editor", Permission("posts:read")); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}

	// Drive B's reload the way fanout would — directly via reloadRole on B.
	if err := storeB.reloadRole(ctx, "editor"); err != nil {
		t.Fatalf("B reloadRole: %v", err)
	}
	if hasPerm(policyB.PermissionsOf("editor"), "posts:read") {
		t.Fatalf("B: peer resurrected revoked code-seeded grant: %v", policyB.PermissionsOf("editor"))
	}
}

// TestRevokeSurvivesPeerBoot: Revoke on A; a NEW store C boots against the
// same DB and re-seeds the same code grant. C must NOT allow — the tombstone
// outlives the code seed.
func TestRevokeSurvivesPeerBoot(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	storeA, _ := seedStore(t, db, "editor", Permission("posts:read"))
	if err := storeA.Revoke(ctx, "editor", Permission("posts:read")); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}

	// New replica C boots, re-seeding the same code grant.
	_, policyC := seedStore(t, db, "editor", Permission("posts:read"))
	if hasPerm(policyC.PermissionsOf("editor"), "posts:read") {
		t.Fatalf("C: fresh boot resurrected revoked grant: %v", policyC.PermissionsOf("editor"))
	}
}

// TestRegrantLiftsTombstone: after a revoke, Grant on A deletes the
// tombstone; a fresh boot D then allows again.
func TestRegrantLiftsTombstone(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	storeA, _ := seedStore(t, db, "editor", Permission("posts:read"))
	if err := storeA.Revoke(ctx, "editor", Permission("posts:read")); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}

	// B boots after the revoke → must deny (tombstone present).
	_, policyB := seedStore(t, db, "editor", Permission("posts:read"))
	if hasPerm(policyB.PermissionsOf("editor"), "posts:read") {
		t.Fatal("B: should deny after revoke before regrant")
	}

	// Re-grant on A lifts the tombstone.
	if err := storeA.Grant(ctx, "editor", Permission("posts:read")); err != nil {
		t.Fatalf("regrant on A: %v", err)
	}

	// A fresh boot D now allows again.
	_, policyD := seedStore(t, db, "editor", Permission("posts:read"))
	if !hasPerm(policyD.PermissionsOf("editor"), "posts:read") {
		t.Fatalf("D: regrant did not lift tombstone on fresh boot: %v", policyD.PermissionsOf("editor"))
	}
}

// TestTombstoneWinsOverGrantRow: with both a grant row AND a tombstone for
// the same (role, perm) in the DB, reloadRole must fail closed — the
// tombstone wins.
func TestTombstoneWinsOverGrantRow(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	policy := NewRolePolicy()
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := policy.Grant("editor", Permission("posts:read")); err != nil {
		t.Fatalf("policy.Grant: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	// Hand-insert BOTH a grant row and a tombstone (inconsistent write).
	if _, err := db.Exec(`INSERT INTO access_grants (role, permission) VALUES ('editor', 'posts:read')`); err != nil {
		t.Fatalf("insert grant row: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO access_grants_revoked (role, permission) VALUES ('editor', 'posts:read')`); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}

	if err := store.reloadRole(ctx, "editor"); err != nil {
		t.Fatalf("reloadRole: %v", err)
	}
	if hasPerm(policy.PermissionsOf("editor"), "posts:read") {
		t.Fatalf("tombstone did not win over grant row: %v", policy.PermissionsOf("editor"))
	}
}

// TestGrantRevokeReloadConcurrentRace exercises the transition lock under
// the race detector: concurrent Grant/Revoke/reloadRole on one store. The
// loop is for the race detector, not outcome assertions; after it joins we
// quiesce with one synchronous Revoke and assert that single permission is
// gone.
func TestGrantRevokeReloadConcurrentRace(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)

	policy := NewRolePolicy()
	store := NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := policy.Grant("editor", Permission("posts:read"), Permission("posts:write")); err != nil {
		t.Fatalf("policy.Grant: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}

	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			_ = store.Grant(ctx, "editor", Permission("posts:read"))
			_ = store.Revoke(ctx, "editor", Permission("posts:write"))
			_ = store.reloadRole(ctx, "editor")
		})
	}
	wg.Wait()

	// Quiesce: one final authoritative Revoke, then a reload, then assert.
	if err := store.Revoke(ctx, "editor", Permission("posts:write")); err != nil {
		t.Fatalf("final Revoke: %v", err)
	}
	if err := store.reloadRole(ctx, "editor"); err != nil {
		t.Fatalf("final reloadRole: %v", err)
	}
	if hasPerm(policy.PermissionsOf("editor"), "posts:write") {
		t.Fatalf("posts:write present after final revoke: %v", policy.PermissionsOf("editor"))
	}
}
