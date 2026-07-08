package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ---------------------------------------------------------------------------
// oa-3: gated entities must declare components.securitySchemes and carry
//       per-operation security (bearerAuth + cookieAuth); ungated entities
//       stay public (no schemes, no per-op security, no global requirement).
// ---------------------------------------------------------------------------

// securitySchemesFor returns components.securitySchemes, or nil when the spec
// declares none (the all-ungated case).
func securitySchemesFor(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()
	components, ok := doc["components"].(map[string]any)
	if !ok {
		return nil
	}
	schemes, ok := components["securitySchemes"].(map[string]map[string]any)
	if !ok {
		return nil
	}
	return schemes
}

// hasOpScheme reports whether the operation's `security` block lists scheme.
func hasOpScheme(op map[string]any, scheme string) bool {
	sec, ok := op["security"].([]map[string][]string)
	if !ok {
		return false
	}
	for _, req := range sec {
		if _, ok := req[scheme]; ok {
			return true
		}
	}
	return false
}

// allGatedOps returns every operation a gated entity advertises: list, create,
// get, update, delete, the three _batch ops, and _events.
func allGatedOps(t *testing.T, doc map[string]any, table string) map[string]map[string]any {
	t.Helper()
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/"+table)
	byID := getMap(t, paths, "/"+table+"/{id}")
	batch := getMap(t, paths, "/"+table+"/_batch")
	events := getMap(t, paths, "/"+table+"/_events")
	return map[string]map[string]any{
		"list":        getMap(t, base, "get"),
		"create":      getMap(t, base, "post"),
		"get":         getMap(t, byID, "get"),
		"update":      getMap(t, byID, "put"),
		"delete":      getMap(t, byID, "delete"),
		"batchCreate": getMap(t, batch, "post"),
		"batchUpdate": getMap(t, batch, "patch"),
		"batchDelete": getMap(t, batch, "delete"),
		"events":      getMap(t, events, "get"),
	}
}

func TestGatedEntityDeclaresSchemes(t *testing.T) {
	e := entity.Define("notes", entity.EntityConfig{
		Table:      "notes",
		OwnerField: "user_id",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()

	schemes := securitySchemesFor(t, doc)
	if schemes == nil {
		t.Fatal("gated entity: expected components.securitySchemes, got none")
	}
	bearer := schemes["bearerAuth"]
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" || bearer["bearerFormat"] != "JWT" {
		t.Errorf("bearerAuth = %v, want http/bearer/JWT", bearer)
	}
	cookie := schemes["cookieAuth"]
	if cookie["type"] != "apiKey" || cookie["in"] != "cookie" || cookie["name"] != "__Host-session" {
		t.Errorf("cookieAuth = %v, want apiKey/cookie/__Host-session", cookie)
	}
	if cookie["description"] == nil {
		t.Error("cookieAuth should carry a description explaining the name override")
	}
}

// Every gating kind (owner-scoped, multi-tenant, RBAC) must attach both
// schemes to each gated operation. Custom endpoints are excluded.
func TestGatedOpsCarryBothSchemes(t *testing.T) {
	cases := []struct {
		name  string
		table string
		ent   *entity.Entity
	}{
		{
			name:  "owner-scoped",
			table: "notes",
			ent: entity.Define("notes", entity.EntityConfig{
				Table:      "notes",
				OwnerField: "user_id",
				Fields:     []schema.Field{{Name: "title", Type: schema.String}},
			}),
		},
		{
			name:  "multi-tenant",
			table: "invoices",
			ent: entity.Define("invoices", entity.EntityConfig{
				Table:       "invoices",
				MultiTenant: true,
				Fields:      []schema.Field{{Name: "amount", Type: schema.Int}},
			}),
		},
		{
			name:  "rbac",
			table: "rbac_items",
			ent:   rbacOnlyEntity(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := EntityOpenAPI(reg(tc.ent), "Test", "1.0.0").Build()
			for label, op := range allGatedOps(t, doc, tc.table) {
				if !hasOpScheme(op, "bearerAuth") {
					t.Errorf("%s op %q missing bearerAuth in security", tc.name, label)
				}
				if !hasOpScheme(op, "cookieAuth") {
					t.Errorf("%s op %q missing cookieAuth in security", tc.name, label)
				}
			}
		})
	}
}

// A registry of only ungated entities must declare no schemes, no per-op
// security, and no global security requirement.
func TestUngatedEntityHasNoSecurity(t *testing.T) {
	e := entity.Define("public_posts", entity.EntityConfig{
		Table: "public_posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()

	if schemes := securitySchemesFor(t, doc); schemes != nil {
		t.Errorf("ungated entity: expected no securitySchemes, got %v", schemes)
	}
	if _, ok := doc["security"]; ok {
		t.Error("ungated entity: expected no global security requirement")
	}
	paths := getMap(t, doc, "paths")
	base := getMap(t, paths, "/public_posts")
	for _, method := range []string{"get", "post"} {
		op := getMap(t, base, method)
		if _, ok := op["security"]; ok {
			t.Errorf("ungated entity: %s op should not carry security", method)
		}
	}
}

// Custom (ent.Config.Endpoints) operations are not gated today and must not
// gain a spurious security block.
func TestCustomEndpointCarriesNoSecurity(t *testing.T) {
	doc := EntityOpenAPI(reg(postsWithTypedEndpoint()), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")
	op := getMap(t, getMap(t, paths, "/posts/{id}/publish"), "post")
	if _, ok := op["security"]; ok {
		t.Error("custom endpoint should not carry per-operation security")
	}
}
