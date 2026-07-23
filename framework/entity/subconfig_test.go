package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestEntitySubConfigsNormalizeAndOverrideFlatCompatibilityFields(t *testing.T) {
	crud := false
	e := Define("tickets", EntityConfig{
		Fields:      []schema.Field{{Name: "tenant_key", Type: schema.String}},
		MultiTenant: false, OwnerField: "legacy_owner", MCP: false, Public: true,
		Scope:      &ScopeConfig{MultiTenant: true, TenantField: "tenant_key", SoftDelete: true, OwnerField: "user_id"},
		Pagination: &PaginationConfig{CursorFields: []string{"created_at", "id"}, MaxListLimit: 25},
		Exposure:   &ExposureConfig{CRUD: &crud, MCP: true, Public: false, Access: AccessControl{Read: "tickets:read"}},
	})

	cfg := e.Config
	if !cfg.MultiTenant || !cfg.SoftDelete || cfg.TenantField != "tenant_key" || cfg.OwnerField != "user_id" {
		t.Fatalf("scope not normalized: %+v", cfg)
	}
	if len(cfg.CursorFields) != 2 || cfg.MaxListLimit != 25 {
		t.Fatalf("pagination not normalized: %+v", cfg)
	}
	if cfg.CRUD == nil || *cfg.CRUD || !cfg.MCP || cfg.Public || cfg.Access.Read != "tickets:read" {
		t.Fatalf("exposure not normalized: %+v", cfg)
	}
}

func TestEntityDeclarationAcceptsGroupedSubConfigs(t *testing.T) {
	crud := false
	cfg, err := (EntityDeclaration{
		Name: "notes", Fields: []FieldDeclaration{{Name: "title", Type: "string"}},
		Scope:      &ScopeDeclaration{OwnerField: "user_id"},
		Pagination: &PaginationDeclaration{CursorField: "created_at", MaxListLimit: 50},
		Exposure:   &ExposureDeclaration{CRUD: &crud, Access: &AccessDeclaration{Create: "notes:create"}},
	}).Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	e := Define("notes", cfg)
	if e.Config.OwnerField != "user_id" || e.Config.CursorField != "created_at" || e.Config.MaxListLimit != 50 {
		t.Fatalf("grouped declaration not normalized: %+v", e.Config)
	}
	if e.Config.CRUD == nil || *e.Config.CRUD || e.Config.Access.Create != "notes:create" {
		t.Fatalf("grouped exposure not normalized: %+v", e.Config)
	}
}
