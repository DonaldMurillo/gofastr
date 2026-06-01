package framework

import (
	"context"
	"database/sql"
	"testing"

	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// TestTenantField_InvalidPanicsAtDefine: a TenantField that isn't a valid SQL
// identifier fails loud at definition time (with an actionable message), not as
// an opaque per-request panic when it's interpolated into the WHERE clause.
func TestTenantField_InvalidPanicsAtDefine(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic for an invalid TenantField")
		}
		if s, ok := r.(string); !ok || !strings.Contains(s, "TenantField") {
			t.Fatalf("panic should name TenantField, got: %v", r)
		}
	}()
	entity.Define("bad", entity.EntityConfig{
		MultiTenant: true,
		TenantField: "org id", // space → invalid identifier
		Fields:      []schema.Field{{Name: "x", Type: schema.String}},
	})
}

// TestWithMultiTenant_HonorsConfigField: tenant.WithMultiTenant flows
// TenantConfig.Field into EntityConfig.TenantField (the single source), so the
// previously-dead config knob now works.
func TestWithMultiTenant_HonorsConfigField(t *testing.T) {
	ent := entity.Define("acct", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	tenant.WithMultiTenant(ent, tenant.TenantConfig{Field: "org_id"})
	if !ent.Config.MultiTenant || ent.Config.TenantField != "org_id" {
		t.Fatalf("WithMultiTenant should set MultiTenant + TenantField, got %+v", ent.Config)
	}
	if ent.Config.TenantColumn() != "org_id" {
		t.Fatalf("TenantColumn() = %q, want org_id", ent.Config.TenantColumn())
	}
}

// TestTenantField_CustomColumnEndToEnd proves a custom MultiTenant column name
// (TenantField) is honored consistently: entity.Define injects it, AutoMigrate
// creates it, CRUD writes it, and CRUD reads are scoped by it.
func TestTenantField_CustomColumnEndToEnd(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ent := entity.Define("docs", entity.EntityConfig{
			Table:       "docs",
			MultiTenant: true,
			TenantField: "org_id", // not the default "tenant_id"
			Fields:      []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))

		// Define injected the custom column, not "tenant_id".
		var hasOrg, hasTenant bool
		for _, f := range ent.GetFields() {
			if f.Name == "org_id" {
				hasOrg = true
			}
			if f.Name == "tenant_id" {
				hasTenant = true
			}
		}
		if !hasOrg || hasTenant {
			t.Fatalf("expected org_id injected, not tenant_id; got %v", ent.GetFields())
		}

		reg := NewRegistry()
		reg.Register(ent)
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		// AutoMigrate created org_id, not tenant_id.
		cols := liveColumns(t, db, "docs")
		if _, ok := cols["org_id"]; !ok {
			t.Fatalf("org_id column not created; got %v", keysOf(cols))
		}
		if _, ok := cols["tenant_id"]; ok {
			t.Fatal("a tenant_id column should not exist for a custom TenantField")
		}

		ch := crud.NewCrudHandler(ent, db)
		ch.Registry = reg

		// Two tenants write a doc each.
		ctxA := tenant.SetTenantID(context.Background(), "org-A")
		ctxB := tenant.SetTenantID(context.Background(), "org-B")
		if _, err := ch.CreateOne(ctxA, map[string]any{"title": "alpha"}); err != nil {
			t.Fatalf("create A: %v", err)
		}
		if _, err := ch.CreateOne(ctxB, map[string]any{"title": "beta"}); err != nil {
			t.Fatalf("create B: %v", err)
		}

		// The row's org_id was set from the tenant context.
		var org string
		if err := db.QueryRow("SELECT org_id FROM docs WHERE title = $1", "alpha").Scan(&org); err != nil {
			t.Fatalf("read org_id: %v", err)
		}
		if org != "org-A" {
			t.Fatalf("org_id = %q, want org-A", org)
		}

		// READ ISOLATION through the custom column: tenant A sees only its row,
		// tenant B only its own. This is the gap that would expose a fail-open
		// if the read scope filtered on the wrong column.
		aRows, err := ch.ListAll(ctxA, crud.ListOptions{})
		if err != nil {
			t.Fatalf("ListAll A: %v", err)
		}
		if len(aRows) != 1 || aRows[0]["title"] != "alpha" {
			t.Fatalf("tenant A should see only its own row, got %+v", aRows)
		}
		bRows, _ := ch.ListAll(ctxB, crud.ListOptions{})
		if len(bRows) != 1 || bRows[0]["title"] != "beta" {
			t.Fatalf("tenant B should see only its own row, got %+v", bRows)
		}
	})
}
