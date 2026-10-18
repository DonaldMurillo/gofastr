package openapi

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A write may omit a required field that has a default, so no request
// body lists it as required; the response keeps it, since every stored
// row carries it. Update bodies leave out the owner and tenant columns,
// which an update never sets; the create body keeps them.
func TestWriteSchemasFollowCrudWrites(t *testing.T) {
	notes := entity.Define("notes", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "kind", Type: schema.Enum, Values: []string{"note", "page"}, Required: true, Default: "note"},
			{Name: "user_id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
		},
		Scope: &entity.ScopeConfig{OwnerField: "user_id", MultiTenant: true},
	}.WithTimestamps(false))
	doc := EntityOpenAPI(reg(notes), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")

	create := requestBodyProps(t, getMap(t, getMap(t, paths, "/notes"), "post"))
	if req, _ := create["required"].([]string); !slices.Equal(req, []string{"body"}) {
		t.Errorf("create required = %v, want [body]: kind has a default", req)
	}
	createProps := create["properties"].(map[string]any)
	for _, key := range []string{"kind", "userId", "tenantId"} {
		if _, has := createProps[key]; !has {
			t.Errorf("create body lost %q", key)
		}
	}

	put := requestBodyProps(t, getMap(t, getMap(t, paths, "/notes/{id}"), "put"))
	if req, _ := put["required"].([]string); !slices.Equal(req, []string{"body"}) {
		t.Errorf("put required = %v, want [body]", req)
	}
	for _, verb := range []string{"put", "patch"} {
		props := requestBodyProps(t, getMap(t, getMap(t, paths, "/notes/{id}"), verb))["properties"].(map[string]any)
		for _, key := range []string{"userId", "tenantId"} {
			if _, has := props[key]; has {
				t.Errorf("%s body offers %q, which an update never sets", verb, key)
			}
		}
		if _, has := props["kind"]; !has {
			t.Errorf("%s body lost kind", verb)
		}
	}

	resp := getMap(t, getMap(t, getMap(t, doc, "components"), "schemas"), "notes")
	if req, _ := resp["required"].([]string); !slices.Contains(req, "kind") {
		t.Errorf("response required = %v, want kind kept", req)
	}
}
