package openapi

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A relation field with Many set stores one target id in its own column:
// schema.Validate takes a string for it and refuses an array with
// not_reference, and a read answers the string. /openapi.json documented
// the property as an array of ids, so a client built from the document
// sent a body the API answered with 400. The documented type is the one
// the validator takes.
func TestManyRelationSchemaTakesWhatValidateTakes(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "tag_ids", Type: schema.Relation, To: "tags", Many: true},
	}
	reg := &testRegistry{}
	for name, cfg := range map[string]entity.EntityConfig{
		"tags":  {Fields: []schema.Field{{Name: "label", Type: schema.String}}, Exposure: &entity.ExposureConfig{CRUD: new(true)}},
		"posts": {Fields: fields, Exposure: &entity.ExposureConfig{CRUD: new(true)}},
	} {
		reg.ents = append(reg.ents, entity.Define(name, cfg))
	}
	raw, err := json.Marshal(EntityOpenAPI(reg, "t", "1", func(*entity.Entity) bool { return true }).Build())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	prop, ok := schemas["posts"].(map[string]any)["properties"].(map[string]any)["tagIds"].(map[string]any)
	if !ok {
		t.Fatalf("posts schema has no tagIds property: %v", schemas["posts"])
	}

	// What the validator takes decides what the document may say.
	values := map[string]any{
		"string": "5f0c1d1e-8a54-4f0e-9d51-6b7f6d3f2a10",
		"array":  []any{"5f0c1d1e-8a54-4f0e-9d51-6b7f6d3f2a10"},
	}
	accepted := map[string]bool{}
	for typ, v := range values {
		accepted[typ] = schema.Validate(fields[1], v) == nil
	}
	if !accepted["string"] || accepted["array"] {
		t.Fatalf("schema.Validate takes %v; this test assumes a Many relation is one string id", accepted)
	}

	documented := map[string]bool{}
	switch typ := prop["type"].(type) {
	case string:
		documented[typ] = true
	case []any:
		for _, v := range typ {
			documented[v.(string)] = true
		}
	}
	if documented["array"] || !documented["string"] {
		t.Fatalf("tagIds is documented as %v; the API takes a string and refuses an array", prop["type"])
	}
	if _, has := prop["items"]; has {
		t.Errorf("tagIds documents items, which only an array has: %v", prop)
	}
}
