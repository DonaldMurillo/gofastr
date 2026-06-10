package crud

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// typedTNote mirrors the tnotes table from setupTenantScopedHandler
// (tenant_security_test.go uses the "items" table; here we reuse the helper's
// shape via a fresh handler).
type typedTNote struct {
	ID    string `json:"id"`
	Notes string `json:"notes"`
}

// TestTypedQuery_FindFailsClosedNoTenant pins the fix for the audit's HIGH
// finding: a typed-repo query on a MultiTenant entity with NO tenant in
// context must fail closed (errTenantRequired), not silently span every
// tenant. ApplyTenantScope no-ops on an empty tenant, so without an explicit
// gate the typed query leaked all tenants' rows — the one in-process path the
// HTTP/crud_api gates missed.
func TestTypedQuery_FindFailsClosedNoTenant(t *testing.T) {
	ch, db := setupTenantScopedHandlerTQ(t)
	seedTenantRowTQ(t, db, "n1", "t1", "a")

	if _, err := NewTypedQuery[typedTNote](ch).Find(context.Background()); err == nil {
		t.Fatal("Find with no tenant context should fail closed, got nil error")
	}
}

func TestTypedQuery_CountFailsClosedNoTenant(t *testing.T) {
	ch, _ := setupTenantScopedHandlerTQ(t)
	if _, err := NewTypedQuery[typedTNote](ch).Count(context.Background()); err == nil {
		t.Fatal("Count with no tenant context should fail closed")
	}
}

func TestTypedQuery_DeleteAllFailsClosedNoTenant(t *testing.T) {
	ch, db := setupTenantScopedHandlerTQ(t)
	seedTenantRowTQ(t, db, "n1", "t1", "keep")
	if _, err := NewTypedQuery[typedTNote](ch).DeleteAll(context.Background()); err == nil {
		t.Fatal("DeleteAll with no tenant context should fail closed")
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tnotes WHERE id='n1'`).Scan(&count)
	if count != 1 {
		t.Errorf("cross-tenant DeleteAll removed the row (count=%d)", count)
	}
}

func TestTypedQuery_UpdateAllFailsClosedNoTenant(t *testing.T) {
	ch, _ := setupTenantScopedHandlerTQ(t)
	if _, err := NewTypedQuery[typedTNote](ch).UpdateAll(context.Background(), map[string]any{"notes": "x"}); err == nil {
		t.Fatal("UpdateAll with no tenant context should fail closed")
	}
}

// TestTypedQuery_FindScopedWithTenant confirms the gate doesn't break the happy
// path: with a tenant in context, Find returns only that tenant's rows.
func TestTypedQuery_FindScopedWithTenant(t *testing.T) {
	ch, db := setupTenantScopedHandlerTQ(t)
	seedTenantRowTQ(t, db, "n1", "t1", "mine")
	seedTenantRowTQ(t, db, "n2", "t2", "theirs")

	ctx := tenant.SetTenantID(context.Background(), "t1")
	got, err := NewTypedQuery[typedTNote](ch).Find(ctx)
	if err != nil {
		t.Fatalf("Find with tenant ctx: %v", err)
	}
	if len(got) != 1 || got[0].ID != "n1" {
		t.Fatalf("tenant scope leaked: got %d rows %+v", len(got), got)
	}
}

// TestTypedQuery_FindCrossTenantOptIn confirms the documented admin escape
// hatch works for typed repos too.
func TestTypedQuery_FindCrossTenantOptIn(t *testing.T) {
	ch, db := setupTenantScopedHandlerTQ(t)
	seedTenantRowTQ(t, db, "n1", "t1", "a")
	seedTenantRowTQ(t, db, "n2", "t2", "b")

	ctx := tenant.AllowCrossTenant(context.Background())
	got, err := NewTypedQuery[typedTNote](ch).Find(ctx)
	if err != nil {
		t.Fatalf("cross-tenant Find: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("cross-tenant Find want 2 rows, got %d", len(got))
	}
}

// setupTenantScopedHandlerTQ builds a CrudHandler over an in-memory sqlite
// tnotes table with MultiTenant on — local to the typed-query tenant tests.
func setupTenantScopedHandlerTQ(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE tnotes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, notes TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("tnotes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String, Required: true},
			{Name: "notes", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func seedTenantRowTQ(t *testing.T, db *sql.DB, id, tenantID, notes string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO tnotes (id, tenant_id, notes) VALUES (?, ?, ?)`, id, tenantID, notes); err != nil {
		t.Fatal(err)
	}
}
