package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The List route accepts ?offset=, ?per_page= and ?stream=, but the spec
// documented none of them (#669, OpenAPI and SDK): a client reading the
// document could not page by row skip, use the per_page convention, or
// know stream's refusal contract. All three are described here, next to
// page/limit, with the semantics parsePaginationValues and the stream
// branch actually apply.
func TestListParamsDocumentOffsetPerPageStream(t *testing.T) {
	e := entity.Define("items", entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	listOp := getMap(t, getMap(t, getMap(t, doc, "paths"), "/items"), "get")

	for _, tc := range []struct {
		name   string
		needle string
	}{
		{"per_page", "alias"},
		{"per_page", "limit"},
		{"offset", "rows to skip"},
		{"offset", "offset mode"},
		{"offset", "before cursor mode is chosen"},
		{"stream", "stream"},
	} {
		p := findParam(listOp["parameters"], tc.name)
		if p == nil {
			t.Fatalf("list op missing ?%s= parameter", tc.name)
		}
		desc, _ := p["description"].(string)
		if !strings.Contains(strings.ToLower(desc), tc.needle) {
			t.Errorf("?%s= description %q does not mention %q", tc.name, desc, tc.needle)
		}
	}

	// The types are integers/boolean, not strings.
	if p := findParam(listOp["parameters"], "per_page"); p != nil {
		if sch := getMap(t, p, "schema"); sch["type"] != "integer" {
			t.Errorf("per_page schema = %v, want integer", sch)
		}
	}
	if p := findParam(listOp["parameters"], "offset"); p != nil {
		if sch := getMap(t, p, "schema"); sch["type"] != "integer" {
			t.Errorf("offset schema = %v, want integer", sch)
		}
	}
	if p := findParam(listOp["parameters"], "stream"); p != nil {
		if sch := getMap(t, p, "schema"); sch["type"] != "boolean" {
			t.Errorf("stream schema = %v, want boolean", sch)
		}
	}
}
