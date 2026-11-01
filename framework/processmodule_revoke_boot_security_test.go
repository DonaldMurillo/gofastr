package framework

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A reboot used to undo a runtime revoke: InstallOrUpdate compared the
// descriptor with the stored row, saw the revoked grant as a grant change,
// wrote it back and disabled the module. The store now remembers revokes,
// subtracts them from the descriptor at boot, and disables only for a grant
// the descriptor adds (issue #614).

func bootRow(t *testing.T, store *SQLProcessModuleStore, grants ...access.Permission) InstallResult {
	t.Helper()
	res, err := store.InstallOrUpdate(context.Background(), DesiredState{
		Module: "demo", ArtifactSHA256: hex256("aa"), EffectiveGrants: grants,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// SetEffectiveGrants is a replacement: a grant it drops is remembered as
// revoked, and a grant it gives back is not.
func TestSetGrantsTracksRevokes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bootRow(t, store, "x:read", "x:write")
	if _, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{"x:write"}); err != nil {
		t.Fatal(err)
	}
	// Giving x:write back clears its revoke: the next boot keeps it.
	if _, err := store.SetEffectiveGrants(ctx, "demo", []access.Permission{"x:read", "x:write"}); err != nil {
		t.Fatal(err)
	}
	if res := bootRow(t, store, "x:read", "x:write"); res.Outcome != InstallUnchanged {
		t.Errorf("after a re-grant, boot outcome = %v, want unchanged", res.Outcome)
	}
	// Dropping x:read through a replacement revokes it: the next boot does
	// not hand it back from the descriptor.
	if _, err := store.SetEffectiveGrants(ctx, "demo", []access.Permission{"x:write"}); err != nil {
		t.Fatal(err)
	}
	res := bootRow(t, store, "x:read", "x:write")
	if res.Outcome != InstallUnchanged || !slices.Equal(res.Row.EffectiveGrants, []access.Permission{"x:write"}) {
		t.Errorf("SECURITY: boot = %v %v, want unchanged [x:write]: a replacement's drop was undone", res.Outcome, res.Row.EffectiveGrants)
	}
}

// A give-back narrower than its revoke survives a reboot: orders:* was
// revoked and orders:read handed back, and the boot's pattern subtraction
// used to drop orders:read with the revoke it still matched.
func TestNarrowGiveBackSurvivesBoot(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bootRow(t, store, "orders:*")
	if _, err := store.RemoveEffectiveGrants(ctx, "demo", []access.Permission{"orders:*"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetEffectiveGrants(ctx, "demo", []access.Permission{"orders:read"}); err != nil {
		t.Fatal(err)
	}
	res := bootRow(t, store, "orders:*")
	if res.Outcome != InstallUnchanged || !slices.Equal(res.Row.EffectiveGrants, []access.Permission{"orders:read"}) {
		t.Errorf("boot = %v %v, want unchanged [orders:read]", res.Outcome, res.Row.EffectiveGrants)
	}
	// A descriptor that no longer covers it drops it.
	res = bootRow(t, store, "invoices:read")
	if slices.Contains(res.Row.EffectiveGrants, "orders:read") {
		t.Errorf("SECURITY: a grant the descriptor dropped survived boot: %v", res.Row.EffectiveGrants)
	}
}

// SetEffectiveGrants on a module with no row reports it, as before.
func TestSetGrantsUnknownModule(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.SetEffectiveGrants(context.Background(), "nope", []access.Permission{"x:read"}); err == nil {
		t.Error("SetEffectiveGrants on a missing module succeeded")
	}
}

// A desired-state table an older build created has no revoked_grants
// column; EnsureSchema adds it and a revoke then sticks.
func TestEnsureSchemaAddsRevokedColumn(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE gofastr_process_modules (
		module TEXT PRIMARY KEY,
		desired_generation INTEGER NOT NULL DEFAULT 1,
		enabled INTEGER NOT NULL DEFAULT 0,
		artifact_sha256 TEXT NOT NULL,
		effective_grants TEXT NOT NULL DEFAULT '[]',
		migrations_applied_at INTEGER NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gofastr_process_modules (module, artifact_sha256, effective_grants) VALUES ('demo', ?, '["x:read","x:write"]')`, hex256("aa")); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLProcessModuleStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // idempotent on the second boot
		if err := store.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("ensure schema on an old table: %v", err)
		}
	}
	if _, err := store.RemoveEffectiveGrants(context.Background(), "demo", []access.Permission{"x:write"}); err != nil {
		t.Fatal(err)
	}
	if res := bootRow(t, store, "x:read", "x:write"); !slices.Equal(res.Row.EffectiveGrants, []access.Permission{"x:read"}) {
		t.Errorf("SECURITY: grants after boot = %v, want [x:read]", res.Row.EffectiveGrants)
	}
}
