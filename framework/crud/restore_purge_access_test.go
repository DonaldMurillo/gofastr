package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A RestoreOne/PurgeOne permission refusal carries crud.ErrForbidden, so
// an in-process caller (an admin screen) can tell "not allowed" from
// "no such row" without matching on message text. The deny arm uses a
// role policy that grants nothing; CanResource fails closed either way.
func TestRestoreDenialWrapsErrForbidden(t *testing.T) {
	restore, db := setupSoftDeleteWorld(t)
	restore.Entity.Config.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{
		Update: "invoices:write",
	}}
	seedInvoice(t, db, "locked", "alice", "2026-01-01T00:00:00Z")
	policy := access.NewRolePolicy() // grants nothing
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("alice"), policy), []string{"member"})

	err := restore.RestoreOne(ctx, "locked")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("denied restore = %v, want crud.ErrForbidden", err)
	}
	if rowDeletedAt(t, db, "invoices", "locked") {
		t.Fatal("denied restore cleared deleted_at")
	}

	purge, _ := setupSoftDeleteWorld(t)
	purge.Entity.Config.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{
		Delete: "invoices:delete",
	}}
	seedInvoice(t, db, "gone", "alice", "2026-01-01T00:00:00Z")
	if err := purge.PurgeOne(ctx, "gone"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("denied purge = %v, want crud.ErrForbidden", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "gone").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("denied purge still removed the row")
	}
}

// CrossOwnerRead may read another owner's rows; it must not revive or
// destroy them. Bob holds alice's entity-wide read grant — the control
// proves he can read her live row — yet RestoreOne and PurgeOne of her
// trashed row answer not found and change nothing. This pins
// selectTrashedRow's owner-WRITE scope (`read=false`, the same posture
// ApplyOwnerScopeUpdate never lifts). owner.AllowCrossOwner is the
// separate deliberate server-side escape that lifts every in-process
// write, UpdateOne and DeleteOne included; it lifting restore/purge is
// that documented contract, not a hole.
func TestCrossOwnerReadCannotRestoreForeign(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Entity.Config.Scope.CrossOwnerRead = "invoices:read:all"
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")
	seedInvoice(t, db, "a2", "alice", nil)
	// A hook that must never run: the trashed row was not visible to the
	// caller under the owner WRITE scope, so the refusal has to arrive
	// before any hook fires — a BeforeUpdate that ran would prove the
	// read scope (not the write scope) selected the row.
	hookRan := false
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(_ context.Context, _ any) error {
		hookRan = true
		return nil
	})

	policy := access.NewRolePolicy()
	policy.Grant("auditor", "invoices:read:all")
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("bob"), policy), []string{"auditor"})

	// Control: the grant really is held — bob reads alice's live row.
	row, err := ch.GetOne(ctx, "a2", nil)
	if err != nil {
		t.Fatalf("cross-owner read control failed, grant not held: %v", err)
	}
	if row["user_id"] != "alice" {
		t.Fatalf("control read %v, want alice's row", row)
	}

	if err := ch.RestoreOne(ctx, "a1"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: cross-owner read restored a foreign trashed row: %v", err)
	}
	if hookRan {
		t.Fatal("SECURITY: the update hooks ran for a foreign trashed row — the read scope, not the write scope, selected it")
	}
	if rowDeletedAt(t, db, "invoices", "a1") {
		t.Fatal("the foreign restore cleared alice's deleted_at")
	}
}
func TestCrossOwnerReadCannotPurgeForeign(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Entity.Config.Scope.CrossOwnerRead = "invoices:read:all"
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")

	policy := access.NewRolePolicy()
	policy.Grant("auditor", "invoices:read:all")
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("bob"), policy), []string{"auditor"})

	if err := ch.PurgeOne(ctx, "a1"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: cross-owner read purged a foreign trashed row: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "a1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the foreign purge removed alice's trashed row")
	}
}
