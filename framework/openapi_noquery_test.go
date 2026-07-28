package framework

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	openapi "github.com/DonaldMurillo/gofastr/framework/openapi"
)

// TestOpenAPIOmitsNoQueryFilterParams pins that the published contract agrees
// with the parser. A NoQuery column belongs in the response schema — it is
// returned, just in whatever form a redaction hook leaves it — but must not
// appear as a filter parameter, or every generated SDK method and agent call
// built from the spec is a guaranteed 400.
func TestOpenAPIOmitsNoQueryFilterParams(t *testing.T) {
	reg := NewRegistry()
	ent := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "number", Type: schema.String, NoQuery: true},
		},
	})
	if err := reg.Register(ent); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(openapi.EntityOpenAPI(reg, "Test", "1.0.0").Build())
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}

	paths, _ := spec["paths"].(map[string]any)
	listPath, _ := paths["/cards"].(map[string]any)
	get, _ := listPath["get"].(map[string]any)
	params, _ := get["parameters"].([]any)

	var names []string
	for _, p := range params {
		if m, ok := p.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("no list parameters in spec: %s", raw)
	}

	for _, n := range names {
		if n == "number" || strings.HasPrefix(n, "number_") {
			t.Errorf("OpenAPI advertises filter param %q on a NoQuery column — every call "+
				"built from it gets 400 %q cannot be filtered", n, "number")
		}
	}

	var sawLabel bool
	for _, n := range names {
		if n == "label" {
			sawLabel = true
		}
	}
	if !sawLabel {
		t.Error("normal filterable column lost its parameter — the exclusion is too broad")
	}

	// The field must still be described in the response schema.
	if !strings.Contains(string(raw), `"number"`) {
		t.Error("NoQuery column vanished from the spec entirely; it must remain in the " +
			"response schema, only absent from filter parameters")
	}
}
