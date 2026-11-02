package tenant

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestCrossTenantMarker(t *testing.T) {
	ctx := context.Background()
	if IsCrossTenant(ctx) {
		t.Fatal("a plain context reads as cross-tenant")
	}
	if !IsCrossTenant(AllowCrossTenant(ctx)) {
		t.Fatal("AllowCrossTenant did not mark the context")
	}
}

func tenantFields(ent *entity.Entity, name string) []schema.Field {
	var out []schema.Field
	for _, f := range ent.Config.Fields {
		if f.Name == name {
			out = append(out, f)
		}
	}
	return out
}

// WithMultiTenant runs after entity.Define, so it declares the tenant
// column itself: AutoMigrate creates only declared columns.
func TestWithMultiTenantDeclaresTheColumn(t *testing.T) {
	t.Run("default column is added once", func(t *testing.T) {
		ent := WithMultiTenant(entity.Define("wmt_a", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}), DefaultTenantConfig())
		got := tenantFields(ent, "tenant_id")
		if len(got) != 1 || !got[0].Hidden || !got[0].ReadOnly || got[0].Type != schema.String {
			t.Fatalf("tenant_id fields = %+v; want one hidden read-only string", got)
		}
		if again := tenantFields(WithMultiTenant(ent, DefaultTenantConfig()), "tenant_id"); len(again) != 1 {
			t.Fatalf("a second call added the column again: %+v", again)
		}
	})
	t.Run("custom field is added", func(t *testing.T) {
		ent := WithMultiTenant(entity.Define("wmt_b", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}), TenantConfig{Field: "org_id"})
		if len(tenantFields(ent, "org_id")) != 1 || len(tenantFields(ent, "tenant_id")) != 0 {
			t.Fatalf("fields = %+v; want org_id alone", ent.Config.Fields)
		}
	})
	t.Run("Define's injected tenant_id is renamed to the custom field", func(t *testing.T) {
		ent := WithMultiTenant(entity.Define("wmt_c", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Scope:  &entity.ScopeConfig{MultiTenant: true},
		}), TenantConfig{Field: "org_id"})
		if len(tenantFields(ent, "org_id")) != 1 || len(tenantFields(ent, "tenant_id")) != 0 {
			t.Fatalf("fields = %+v; want the injected tenant_id renamed to org_id", ent.Config.Fields)
		}
	})
	t.Run("an app's own tenant_id is not renamed", func(t *testing.T) {
		ent := WithMultiTenant(entity.Define("wmt_d", entity.EntityConfig{
			Fields: []schema.Field{{Name: "tenant_id", Type: schema.String, Required: true}},
		}), TenantConfig{Field: "org_id"})
		if len(tenantFields(ent, "tenant_id")) != 1 || len(tenantFields(ent, "org_id")) != 1 {
			t.Fatalf("fields = %+v; want the app's tenant_id kept and org_id added", ent.Config.Fields)
		}
	})
	t.Run("an invalid column name panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("WithMultiTenant accepted an invalid column name")
			}
		}()
		WithMultiTenant(entity.Define("wmt_e", entity.EntityConfig{}), TenantConfig{Field: "bad name;"})
	})
}
