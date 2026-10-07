package crud

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// The Deleted list option: ListOptions.Deleted narrows a list (and its
// count) to soft-deleted rows ONLY, inverting the soft-delete filter
// while every other scope — owner, tenant, read scope, Where — still
// applies, and an entity without Scope.SoftDelete is refused with
// ErrNoSoftDelete before any SQL runs.

// setupSoftDeleteHandler builds a CrudHandler over an owner-scoped,
// soft-deleting "notes" table.
func setupSoftDeleteHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		title TEXT,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("notes", entity.EntityConfig{Fields: []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, Scope: &entity.ScopeConfig{OwnerField: "user_id", SoftDelete: true}}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// seedNote inserts one row directly; a non-nil deletedAt soft-deletes it.
func seedNote(t *testing.T, db *sql.DB, id, userID, title string, deletedAt any) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO notes (id, user_id, title, deleted_at) VALUES (?, ?, ?, ?)`,
		id, userID, title, deletedAt,
	); err != nil {
		t.Fatal(err)
	}
}

func noteIDs(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["id"].(string))
	}
	return out
}

// TestDeletedListsOnlyTrashedRows pins the inversion: a Deleted list
// returns the soft-deleted rows and never a live one, and CountAll
// agrees with the list.
func TestDeletedListsOnlyTrashedRows(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupSoftDeleteHandler(t)
	seedNote(t, db, "n-live", "alice", "live note", nil)
	seedNote(t, db, "n-dead", "alice", "dead note", "2026-01-01T00:00:00Z")

	ctx := ctxWithUser("alice")
	rows, err := ch.ListAll(ctx, ListOptions{Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(rows); len(got) != 1 || got[0] != "n-dead" {
		t.Fatalf("Deleted ListAll = %v, want [n-dead] only", got)
	}
	n, err := ch.CountAll(ctx, ListOptions{Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Deleted CountAll = %d, want 1", n)
	}
	// The ordinary list still hides the trashed row: the option inverted
	// nothing it does not own.
	live, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(live); len(got) != 1 || got[0] != "n-live" {
		t.Fatalf("ordinary ListAll = %v, want [n-live] only", got)
	}
}

// TestDeletedKeepsOwnerScope pins that owner scoping still applies to
// trashed rows: alice's Deleted list never answers bob's.
func TestDeletedKeepsOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupSoftDeleteHandler(t)
	seedNote(t, db, "a-dead", "alice", "alice dead", "2026-01-01T00:00:00Z")
	seedNote(t, db, "b-dead", "bob", "bob dead", "2026-01-01T00:00:00Z")
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(rows); len(got) != 1 || got[0] != "a-dead" {
		t.Fatalf("SECURITY: alice's Deleted list answered %v, want [a-dead] only", got)
	}
	n, err := ch.CountAll(ctxWithUser("alice"), ListOptions{Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("SECURITY: alice's Deleted count = %d, want 1", n)
	}
}

// TestDeletedKeepsTenantScope pins the same for multi-tenancy: the
// Deleted view stays inside the caller's tenant.
func TestDeletedKeepsTenantScope(t *testing.T) {
	installOwnerExtractor(t)
	_, db := setupSoftDeleteHandler(t)
	// The same table under a multi-tenant definition.
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, Scope: &entity.ScopeConfig{OwnerField: "user_id", SoftDelete: true, MultiTenant: true}}.WithTimestamps(false)
	tenantEnt := entity.Define("notes", cfg)
	tenantEnt.SetDB(db)
	if err := tenantEnt.Validate(); err != nil {
		t.Fatalf("tenant entity: %v", err)
	}
	mch := NewCrudHandler(tenantEnt, db).WithJSONCase(CaseSnake)
	seedNote(t, db, "a-dead", "alice", "tenant a dead", "2026-01-01T00:00:00Z")
	if _, err := db.Exec(`ALTER TABLE notes ADD COLUMN tenant_id TEXT`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE notes SET tenant_id = ? WHERE id = ?`, "t-a", "a-dead"); err != nil {
		t.Fatal(err)
	}
	seedNote(t, db, "tb-dead", "alice", "tenant b dead", "2026-01-01T00:00:00Z")
	if _, err := db.Exec(`UPDATE notes SET tenant_id = ? WHERE id = ?`, "t-b", "tb-dead"); err != nil {
		t.Fatal(err)
	}

	ctx := tenant.SetTenantID(ctxWithUser("alice"), "t-a")
	rows, err := mch.ListAll(ctx, ListOptions{Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(rows); len(got) != 1 || got[0] != "a-dead" {
		t.Fatalf("SECURITY: tenant t-a's Deleted list answered %v, want [a-dead] only", got)
	}
}

// TestDeletedKeepsWhere pins that a caller's Where narrows inside the
// Deleted view the same way it narrows the ordinary list.
func TestDeletedKeepsWhere(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupSoftDeleteHandler(t)
	seedNote(t, db, "d1", "alice", "urgent dead", "2026-01-01T00:00:00Z")
	seedNote(t, db, "d2", "alice", "quiet dead", "2026-01-01T00:00:00Z")
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{Deleted: true,
		Where: &filter.Predicate{Field: "title", Op: filter.OpEq, Value: "urgent dead"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(rows); len(got) != 1 || got[0] != "d1" {
		t.Fatalf("Deleted ListAll with Where = %v, want [d1]", got)
	}
}

// TestDeletedWithoutSoftDeleteRefused pins the refusal: Deleted on an
// entity that never declared Scope.SoftDelete answers ErrNoSoftDelete
// from both ListAll and CountAll, before any SQL could name a
// deleted_at column the table does not have.
func TestDeletedWithoutSoftDeleteRefused(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "a live row")

	ctx := ctxWithUser("alice")
	if _, err := ch.ListAll(ctx, ListOptions{Deleted: true}); !errors.Is(err, ErrNoSoftDelete) {
		t.Fatalf("ListAll Deleted on a hard-delete entity = %v, want ErrNoSoftDelete", err)
	}
	if _, err := ch.CountAll(ctx, ListOptions{Deleted: true}); !errors.Is(err, ErrNoSoftDelete) {
		t.Fatalf("CountAll Deleted on a hard-delete entity = %v, want ErrNoSoftDelete", err)
	}
}
