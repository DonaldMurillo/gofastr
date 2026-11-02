package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// tenant.WithMultiTenant runs on an entity Define already built, so it set
// MultiTenant and TenantField but never added the column Define injects
// for a tenant-scoped entity: AutoMigrate created a table without it, and
// the first create failed on the column CRUD writes. With the default
// tenant_id injected at Define and Field "org_id" given after, the table
// held tenant_id and CRUD wrote org_id (2026-11-01 bug bash).
func TestWithMultiTenantAddsTheTenantColumn(t *testing.T) {
	cases := []struct {
		name       string
		atDefine   bool
		field, col string
	}{
		{"default column on a plain entity", false, "", "tenant_id"},
		{"custom column on a plain entity", false, "org_id", "org_id"},
		{"custom column over the injected default", true, "org_id", "org_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				ent := entity.Define("notes", entity.EntityConfig{
					Table:  "notes",
					Scope:  &entity.ScopeConfig{MultiTenant: tc.atDefine},
					Fields: []schema.Field{{Name: "title", Type: schema.String}},
				}.WithTimestamps(false))
				tenant.WithMultiTenant(ent, tenant.TenantConfig{Field: tc.field})

				reg := NewRegistry()
				if err := reg.Register(ent); err != nil {
					t.Fatal(err)
				}
				if err := AutoMigrate(db, reg); err != nil {
					t.Fatalf("AutoMigrate: %v", err)
				}
				cols := liveColumns(t, db, "notes")
				if _, ok := cols[tc.col]; !ok {
					t.Fatalf("%s column not created; got %v", tc.col, keysOf(cols))
				}
				if _, ok := cols["tenant_id"]; ok && tc.col != "tenant_id" {
					t.Fatal("a tenant_id column was created beside the custom column")
				}

				ch := crud.NewCrudHandler(ent, db)
				ch.Registry = reg
				ctxA := tenant.SetTenantID(context.Background(), "org-A")
				if _, err := ch.CreateOne(ctxA, map[string]any{"title": "alpha"}); err != nil {
					t.Fatalf("create: %v", err)
				}
				var got string
				if err := db.QueryRow("SELECT " + tc.col + " FROM notes").Scan(&got); err != nil || got != "org-A" {
					t.Fatalf("%s = %q (%v), want org-A", tc.col, got, err)
				}
				rows, err := ch.ListAll(tenant.SetTenantID(context.Background(), "org-B"), crud.ListOptions{})
				if err != nil || len(rows) != 0 {
					t.Fatalf("tenant B listed %v (%v), want nothing", rows, err)
				}
			})
		})
	}
}
