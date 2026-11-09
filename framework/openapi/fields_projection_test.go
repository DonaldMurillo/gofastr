package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The spec documents ?fields= as a projection. crud's projection keeps
// every required column (and the key) in the rows, exactly because the
// response schema marks those keys required — a projected response that
// dropped them would fail the spec (projectFromRequestQ, #669). The
// description has to say so, or a reader sizing payloads from the doc
// expects required keys to vanish.
func TestFieldsParamDocumentsRequiredKeys(t *testing.T) {
	e := entity.Define("items", entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")
	listOp := getMap(t, getMap(t, paths, "/items"), "get")
	fp := findParam(listOp["parameters"], "fields")
	if fp == nil {
		t.Fatal("list op missing 'fields' parameter")
	}
	desc, _ := fp["description"].(string)
	if !strings.Contains(desc, "required fields are always included") {
		t.Errorf("fields description = %q; it must say required fields stay in the projection", desc)
	}
}
