package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSpecBracketsControlNamedFilter(t *testing.T) {
	e := entity.Define("transfers", entity.EntityConfig{
		Table:  "transfers",
		Fields: []schema.Field{{Name: "direction", Type: schema.String}},
	}.WithTimestamps(false))
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	params := getMap(t, getMap(t, getMap(t, doc, "paths"), "/transfers"), "get")["parameters"]

	count := 0
	for _, p := range params.([]map[string]any) {
		if p["name"] == "direction" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("spec lists %d direction parameters, want the one control", count)
	}
	p := findParam(params, "direction[eq]")
	if p == nil {
		t.Fatal("spec has no direction[eq] filter")
	}
	if desc, _ := p["description"].(string); !strings.Contains(desc, "a bare direction is the list control") {
		t.Errorf("direction[eq] description = %q, want the reason for the brackets", desc)
	}
}
