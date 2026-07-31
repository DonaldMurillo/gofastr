package entity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestEntityConfigResolvedGroupsAreCanonical(t *testing.T) {
	crud := false
	e := Define("tickets", EntityConfig{
		Fields: []schema.Field{{Name: "tenant_key", Type: schema.String}},
		Scope: &ScopeConfig{
			MultiTenant: true, TenantField: "tenant_key",
			SoftDelete: true, OwnerField: "user_id",
		},
		Pagination: &PaginationConfig{
			CursorFields: []string{"created_at", "id"},
			MaxListLimit: 25,
		},
		Exposure: &ExposureConfig{
			CRUD: &crud, MCP: true,
			Access: AccessControl{Read: "tickets:read"},
		},
	})

	cfg := e.Config
	if cfg.Scope == nil || !cfg.Scope.MultiTenant || !cfg.Scope.SoftDelete ||
		cfg.Scope.TenantField != "tenant_key" || cfg.Scope.OwnerField != "user_id" {
		t.Fatalf("scope not resolved: %+v", cfg.Scope)
	}
	if cfg.Pagination == nil || len(cfg.Pagination.CursorFields) != 2 ||
		cfg.Pagination.MaxListLimit != 25 {
		t.Fatalf("pagination not resolved: %+v", cfg.Pagination)
	}
	if cfg.Exposure == nil || cfg.Exposure.CRUD == nil || *cfg.Exposure.CRUD ||
		!cfg.Exposure.MCP || cfg.Exposure.Access.Read != "tickets:read" {
		t.Fatalf("exposure not resolved: %+v", cfg.Exposure)
	}

	zero := Define("empty", EntityConfig{})
	if zero.Config.Scope == nil || zero.Config.Pagination == nil || zero.Config.Exposure == nil {
		t.Fatalf("resolved groups must always be populated: %+v", zero.Config)
	}

	typ := reflect.TypeOf(EntityConfig{})
	for _, removed := range []string{
		"SoftDelete", "MultiTenant", "TenantField", "CRUD", "MCP",
		"CursorField", "CursorFields", "MaxListLimit", "OwnerField",
		"CrossOwnerRead", "Access", "Public",
	} {
		if _, ok := typ.FieldByName(removed); ok {
			t.Errorf("EntityConfig still exposes removed field %s", removed)
		}
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
	if e.Config.Scope.OwnerField != "user_id" ||
		e.Config.Pagination.CursorField != "created_at" ||
		e.Config.Pagination.MaxListLimit != 50 {
		t.Fatalf("grouped declaration not preserved: %+v", e.Config)
	}
	if e.Config.Exposure.CRUD == nil || *e.Config.Exposure.CRUD ||
		e.Config.Exposure.Access.Create != "notes:create" {
		t.Fatalf("grouped exposure not preserved: %+v", e.Config.Exposure)
	}
}

func TestEntityDeclarationFlatJSONNormalizesImmediatelyToGroups(t *testing.T) {
	raw := []byte(`{
		"name": "tickets",
		"soft_delete": true,
		"multi_tenant": true,
		"tenant_field": "account_id",
		"owner_field": "user_id",
		"cross_owner_read": "tickets:read:all",
		"cursor_fields": ["created_at", "id"],
		"max_list_limit": 25,
		"crud": false,
		"mcp": true,
		"public": false,
		"access": {"read": "tickets:read"},
		"fields": [{"name": "title", "type": "string"}]
	}`)
	var d EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if d.Scope == nil || !d.Scope.SoftDelete || !d.Scope.MultiTenant ||
		d.Scope.TenantField != "account_id" || d.Scope.OwnerField != "user_id" ||
		d.Scope.CrossOwnerRead != "tickets:read:all" {
		t.Fatalf("flat scope did not normalize: %+v", d.Scope)
	}
	if d.Pagination == nil || len(d.Pagination.CursorFields) != 2 ||
		d.Pagination.MaxListLimit != 25 {
		t.Fatalf("flat pagination did not normalize: %+v", d.Pagination)
	}
	if d.Exposure == nil || d.Exposure.CRUD == nil || *d.Exposure.CRUD ||
		!d.Exposure.MCP || d.Exposure.Public || d.Exposure.Access == nil ||
		d.Exposure.Access.Read != "tickets:read" {
		t.Fatalf("flat exposure did not normalize: %+v", d.Exposure)
	}
}

func TestEntityDeclarationRejectsConflictingFlatAndGroupedJSON(t *testing.T) {
	tests := map[string]string{
		"soft delete":      `"soft_delete":true,"scope":{"soft_delete":false}`,
		"multi tenant":     `"multi_tenant":true,"scope":{"multi_tenant":false}`,
		"tenant field":     `"tenant_field":"tenant_id","scope":{"tenant_field":"account_id"}`,
		"owner field":      `"owner_field":"user_id","scope":{"owner_field":"account_id"}`,
		"cross owner read": `"cross_owner_read":"read:a","scope":{"cross_owner_read":"read:b"}`,
		"cursor field":     `"cursor_field":"id","pagination":{"cursor_field":"created_at"}`,
		"cursor fields":    `"cursor_fields":["id"],"pagination":{"cursor_fields":["created_at","id"]}`,
		"max list limit":   `"max_list_limit":10,"pagination":{"max_list_limit":20}`,
		"crud":             `"crud":true,"exposure":{"crud":false}`,
		"mcp":              `"mcp":true,"exposure":{"mcp":false}`,
		"public":           `"public":true,"exposure":{"public":false}`,
		"access":           `"access":{"read":"read:a"},"exposure":{"access":{"read":"read:b"}}`,
	}
	for name, pair := range tests {
		t.Run(name, func(t *testing.T) {
			raw := []byte(`{"name":"tickets","fields":[{"name":"title","type":"string"}],` + pair + `}`)
			var d EntityDeclaration
			err := json.Unmarshal(raw, &d)
			if err == nil {
				t.Fatal("expected conflicting declaration to fail")
			}
			if !strings.Contains(err.Error(), "conflicting") {
				t.Fatalf("error %q does not explain the conflict", err)
			}
		})
	}
}

func TestEntityConfigTimestampsUsesPointerTriState(t *testing.T) {
	field, ok := reflect.TypeOf(EntityConfig{}).FieldByName("Timestamps")
	if !ok {
		t.Fatal("EntityConfig.Timestamps is missing")
	}
	if field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.Bool {
		t.Fatalf("Timestamps type = %v, want *bool", field.Type)
	}
}
