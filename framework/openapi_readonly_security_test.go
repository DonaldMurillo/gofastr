package framework

import (
	"testing"

	coreopenapi "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	frameworkopenapi "github.com/DonaldMurillo/gofastr/framework/openapi"
)

// TestFieldToSchema_MarksReadOnly pins that any field the runtime
// refuses to accept on write (ReadOnly or AutoGenerate) is marked
// readOnly in the OpenAPI doc. Without this, generated SDKs propose
// writable bindings for fields the server will reject — and clients
// like Insomnia / Postman cheerfully send them.
func TestFieldToSchema_MarksReadOnly(t *testing.T) {
	cases := []schema.Field{
		{Name: "title", Type: schema.String, ReadOnly: true},
		{Name: "score", Type: schema.Int, ReadOnly: true},
		{Name: "meta", Type: schema.JSON, ReadOnly: true},
		{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
		{Name: "created_at", Type: schema.Timestamp, AutoGenerate: schema.AutoTimestamp},
		{Name: "sequence", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
	}
	for _, f := range cases {
		t.Run(f.Name, func(t *testing.T) {
			if got := coreopenapi.FieldToSchema(f)["readOnly"]; got != true {
				t.Fatalf("FieldToSchema(%s) missing readOnly=true, got %#v", f.Name, got)
			}
		})
	}
}

// TestEntityOpenAPI_PreservesReadOnly is the integration shape of the
// same contract: the readOnly marker survives the framework-level
// entity schema build, not just the per-field helper.
func TestEntityOpenAPI_PreservesReadOnly(t *testing.T) {
	cases := []schema.Field{
		{Name: "title_lock", Type: schema.String, ReadOnly: true},
		{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
	}
	for _, field := range cases {
		t.Run(field.Name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			name := "ent_" + field.Name
			app.Entity(name, entity.EntityConfig{
				Table: name,
				Fields: []schema.Field{
					{Name: "name", Type: schema.String},
					field,
				},
			}.WithTimestamps(false))
			spec := frameworkopenapi.EntityOpenAPI(app.Registry, "Test", "1.0.0").Build()
			components := spec["components"].(map[string]any)
			schemas := components["schemas"].(map[string]map[string]any)
			ent := schemas[name]
			props := ent["properties"].(map[string]any)
			prop, ok := props[casing.ToCamel(field.Name)].(map[string]any)
			if !ok {
				t.Fatalf("property %q missing from schema %#v", field.Name, props)
			}
			if got := prop["readOnly"]; got != true {
				t.Fatalf("entity schema dropped readOnly on %s: %#v", field.Name, prop)
			}
		})
	}
}
