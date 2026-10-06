package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// setupSoftDeleteWorld builds the owner-scoped soft-delete invoices table
// shared by the RestoreOne/PurgeOne tests.
func setupSoftDeleteWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE invoices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		title TEXT,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("invoices", entity.EntityConfig{
		Name:  "invoices",
		Table: "invoices",
		Scope: &entity.ScopeConfig{OwnerField: "user_id", SoftDelete: true},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	installOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	return ch, db
}

func seedInvoice(t *testing.T, db *sql.DB, id, user, deletedAt any) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO invoices (id, user_id, title, deleted_at) VALUES (?, ?, ?, ?)`,
		id, user, "row "+id.(string), deletedAt,
	); err != nil {
		t.Fatal(err)
	}
}

func rowDeletedAt(t *testing.T, db *sql.DB, table, id string) (isNull bool) {
	t.Helper()
	var deleted sql.NullString
	if err := db.QueryRow(`SELECT deleted_at FROM `+table+` WHERE id = ?`, id).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	return !deleted.Valid
}

func TestRestoreOneClearsDeletedAt(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")

	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); err != nil {
		t.Fatalf("RestoreOne: %v", err)
	}
	if !rowDeletedAt(t, db, "invoices", "a1") {
		t.Fatal("deleted_at not cleared")
	}
	// The restored row is visible again to an ordinary scoped list.
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "a1" {
		t.Fatalf("restored row not listed: %v", rows)
	}
}

// Another owner's id answers the read's not-found: no existence leak.
func TestRestoreOneOwnerIsolation(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")

	err := ch.RestoreOne(ctxWithUser("bob"), "a1")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: bob restoring alice's row = %v, want errNotFound", err)
	}
	if rowDeletedAt(t, db, "invoices", "a1") {
		t.Fatal("bob's attempt cleared alice's deleted_at")
	}
}

func TestRestoreOneTenantIsolation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE tdocs (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("tdocs", entity.EntityConfig{
		Name:  "tdocs",
		Table: "tdocs",
		Scope: &entity.ScopeConfig{MultiTenant: true, SoftDelete: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "tdocs", []map[string]any{
		{"id": "t2row", "tenant_id": "t2", "deleted_at": "2026-01-01T00:00:00Z"},
	})

	err = ch.RestoreOne(tenant.SetTenantID(context.Background(), "t1"), "t2row")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: t1 restoring t2's row = %v, want errNotFound", err)
	}
	if rowDeletedAt(t, db, "tdocs", "t2row") {
		t.Fatal("cross-tenant restore cleared the marker")
	}
}

func TestRestoreOneNotDeletedRefused(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "live", "alice", nil)

	if err := ch.RestoreOne(ctxWithUser("alice"), "live"); !errors.Is(err, ErrNotSoftDeleted) {
		t.Fatalf("restoring a live row = %v, want ErrNotSoftDeleted", err)
	}
}

func TestRestoreOneUnknownIdNotFound(t *testing.T) {
	ch, _ := setupSoftDeleteWorld(t)
	if err := ch.RestoreOne(ctxWithUser("alice"), "nope"); !errors.Is(err, errNotFound) {
		t.Fatalf("unknown id = %v, want errNotFound", err)
	}
}

func TestRestoreOneRefusesWithoutSoftDelete(t *testing.T) {
	ch, _ := setupOwnerScopedHandler(t) // logs: owner scope, no soft delete
	if err := ch.RestoreOne(ctxWithUser("alice"), "log-a1"); !errors.Is(err, ErrNoSoftDelete) {
		t.Fatalf("restore on non-soft-delete entity = %v, want ErrNoSoftDelete", err)
	}
	if err := ch.PurgeOne(ctxWithUser("alice"), "log-a1"); !errors.Is(err, ErrNoSoftDelete) {
		t.Fatalf("purge on non-soft-delete entity = %v, want ErrNoSoftDelete", err)
	}
}

// The update permission and a resource-aware Decider gate the restore the
// way the update route gates a PATCH.
func TestRestoreOneDeciderDeny(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Entity.Config.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{
		Update: "invoices:write",
	}}
	seedInvoice(t, db, "locked", "alice", "2026-01-01T00:00:00Z")

	policy := access.NewRolePolicy()
	policy.Grant("member", "invoices:write")
	ctx := access.WithPolicy(ctxWithUser("alice"), policy)
	ctx = access.WithRoles(ctx, []string{"member"})
	ctx = access.WithDecider(ctx, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "locked" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})

	err := ch.RestoreOne(ctx, "locked")
	if err == nil {
		t.Fatal("SECURITY: a Decider-denied restore went through")
	}
	if rowDeletedAt(t, db, "invoices", "locked") {
		t.Fatal("denied restore still cleared the marker")
	}
	// The abstain path lets the granted caller through.
	seedInvoice(t, db, "free", "alice", "2026-01-01T00:00:00Z")
	if err := ch.RestoreOne(ctx, "free"); err != nil {
		t.Fatalf("abstaining decider blocked a granted restore: %v", err)
	}
}

// Restore runs the update hook chain, the audit operation marker reaches
// the hooks, and the entity.updated event fires after commit.
func TestRestoreOneHooksAuditOpAndEvent(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")
	ch.Events = event.NewEventBus()

	var before, after, auditOp bool
	var preImageDeleted bool
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(_ context.Context, _ any) error {
		before = true
		return nil
	})
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
		after = true
		auditOp = AuditOperationFor(ctx, "invoices", "a1") == "restore"
		if pre := AuditPreImageFromContext(ctx); pre != nil {
			v, ok := pre["deleted_at"]
			preImageDeleted = ok && v != nil
		}
		return nil
	})
	updated := make(chan string, 1)
	cancel := ch.Events.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
		if d, ok := ev.Data.(map[string]any); ok {
			if m, _ := d[eventKeyRecord].(map[string]any); m != nil {
				updated <- m["id"].(string)
			}
		}
		return nil
	})
	defer cancel()

	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); err != nil {
		t.Fatal(err)
	}
	if !before || !after {
		t.Fatalf("update hooks did not both run: before=%v after=%v", before, after)
	}
	if !auditOp {
		t.Fatal("AfterUpdate hook did not see the restore audit operation")
	}
	if !preImageDeleted {
		t.Fatal("audit pre-image did not capture the soft-deleted row")
	}
	select {
	case id := <-updated:
		if id != "a1" {
			t.Fatalf("entity.updated carried id %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("entity.updated event not emitted")
	}
}

// Purge removes only soft-deleted rows: a live row is refused and stays.
func TestPurgeOneRefusesLiveRow(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "live", "alice", nil)

	if err := ch.PurgeOne(ctxWithUser("alice"), "live"); !errors.Is(err, ErrNotSoftDeleted) {
		t.Fatalf("purging a live row = %v, want ErrNotSoftDeleted", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "live").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("live row was removed by a refused purge")
	}
}

func TestPurgeOneRemovesTrashedRow(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "gone", "alice", "2026-01-01T00:00:00Z")

	if err := ch.PurgeOne(ctxWithUser("alice"), "gone"); err != nil {
		t.Fatalf("PurgeOne: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "gone").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("purged row still present")
	}
}

func TestPurgeOneOwnerIsolation(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")

	if err := ch.PurgeOne(ctxWithUser("bob"), "a1"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: bob purging alice's row = %v, want errNotFound", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "a1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("cross-owner purge removed the row")
	}
}

func TestPurgeOneTenantIsolation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE tdocs2 (id TEXT PRIMARY KEY, tenant_id TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("tdocs2", entity.EntityConfig{
		Name:  "tdocs2",
		Table: "tdocs2",
		Scope: &entity.ScopeConfig{MultiTenant: true, SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "tdocs2", []map[string]any{
		{"id": "t2row", "tenant_id": "t2", "deleted_at": "2026-01-01T00:00:00Z"},
	})

	if err := ch.PurgeOne(tenant.SetTenantID(context.Background(), "t1"), "t2row"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: t1 purging t2's row = %v, want errNotFound", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tdocs2`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("cross-tenant purge removed the row")
	}
}

func TestPurgeOneDeciderDeny(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Entity.Config.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{
		Delete: "invoices:delete",
	}}
	seedInvoice(t, db, "locked", "alice", "2026-01-01T00:00:00Z")

	policy := access.NewRolePolicy()
	policy.Grant("member", "invoices:delete")
	ctx := access.WithPolicy(ctxWithUser("alice"), policy)
	ctx = access.WithRoles(ctx, []string{"member"})
	ctx = access.WithDecider(ctx, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		return access.DecisionDeny
	})

	if err := ch.PurgeOne(ctx, "locked"); err == nil {
		t.Fatal("SECURITY: a Decider-denied purge went through")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE id = ?`, "locked").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("denied purge still removed the row")
	}
}

func TestPurgeOneHooksAuditOpAndEvent(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "gone", "alice", "2026-01-01T00:00:00Z")
	ch.Events = event.NewEventBus()

	var before, after, auditOp bool
	ch.Hooks.RegisterHook(hook.BeforeDelete, func(_ context.Context, _ any) error {
		before = true
		return nil
	})
	ch.Hooks.RegisterHook(hook.AfterDelete, func(ctx context.Context, _ any) error {
		after = true
		auditOp = AuditOperationFor(ctx, "invoices", "gone") == "purge"
		return nil
	})

	deleted := make(chan string, 1)
	cancel := ch.Events.Subscribe(event.EntityDeleted, func(_ context.Context, ev event.Event) error {
		if d, ok := ev.Data.(map[string]any); ok {
			if m, _ := d[eventKeyRecord].(map[string]any); m != nil {
				deleted <- m["id"].(string)
			}
		}
		return nil
	})
	defer cancel()

	if err := ch.PurgeOne(ctxWithUser("alice"), "gone"); err != nil {
		t.Fatal(err)
	}
	if !before || !after {
		t.Fatalf("delete hooks did not both run: before=%v after=%v", before, after)
	}
	if !auditOp {
		t.Fatal("AfterDelete hook did not see the purge audit operation")
	}
	select {
	case id := <-deleted:
		if id != "gone" {
			t.Fatalf("entity.deleted carried id %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("entity.deleted event not emitted")
	}
}

// A purged row is gone for good: not even the trashed view sees it.
func TestPurgeOneResultInvisible(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "gone", "alice", "2026-01-01T00:00:00Z")
	if err := ch.PurgeOne(ctxWithUser("alice"), "gone"); err != nil {
		t.Fatal(err)
	}
	// Direct read of the trashed view shape: nothing with that id exists.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("table not empty after purge")
	}
}
