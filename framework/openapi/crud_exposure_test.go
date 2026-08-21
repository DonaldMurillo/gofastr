package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// boolPtr is local to the test file; the framework's own helper lives in
// package framework and importing it here would be a cycle.
//
//go:fix inline
func boolPtr(b bool) *bool { return new(b) }

// An entity that opted out of auto-CRUD must not appear in the spec. The
// router refuses every generated path for it (404), so advertising them
// documents an API the server does not have, and a generated SDK ships
// methods that cannot work. Entities usually opt out because their rows
// are sensitive or their invariants belong to a server-side workflow, so
// this is the worst place for the spec to over-promise.
func TestCRUDDisabledEntityEmitsNoPaths(t *testing.T) {
	off := entity.Define("secrets", entity.EntityConfig{
		Table:    "secrets",
		Fields:   []schema.Field{{Name: "value", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(false)},
	}.WithTimestamps(false))
	on := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(off, on), "Test", "1.0.0").Build()
	paths, ok := asMap(doc["paths"])
	if !ok {
		t.Fatalf("spec has no paths object")
	}

	for p := range paths {
		if strings.Contains(p, "/secrets") {
			t.Errorf("CRUD-disabled entity advertised path %q — the router answers 404 for it", p)
		}
	}
	if _, ok := paths["/posts"]; !ok {
		t.Errorf("CRUD-enabled entity lost its path; emitted %v", mapKeys(paths))
	}
}

// A nil CRUD pointer means "auto", the router enables CRUD for it, so the
// spec must too. Only an explicit false opts out.
func TestNilCRUDStillEmitsPaths(t *testing.T) {
	auto := entity.Define("posts", entity.EntityConfig{
		Table:    "posts",
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(auto), "Test", "1.0.0").Build()
	paths, ok := asMap(doc["paths"])
	if !ok {
		t.Fatalf("spec has no paths object")
	}
	if _, ok := paths["/posts"]; !ok {
		t.Errorf("nil Exposure.CRUD must mean enabled; emitted %v", mapKeys(paths))
	}
}

// The opt-out hides the paths, not the type. A CRUD-disabled entity is
// still reachable through hand-written Endpoints that speak its shape, so
// the schema component stays, dropping it would leave those endpoints
// referencing a component the spec never defines.
func TestCRUDDisabledEntityKeepsSchema(t *testing.T) {
	off := entity.Define("secrets", entity.EntityConfig{
		Table:    "secrets",
		Fields:   []schema.Field{{Name: "value", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(false)},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(off), "Test", "1.0.0").Build()
	components := getMap(t, doc, "components")
	schemas, ok := asMap(components["schemas"])
	if !ok {
		t.Fatalf("spec has no schemas object")
	}
	if _, ok := schemas["secrets"]; !ok {
		t.Errorf("schema component dropped with the paths; emitted %v", mapKeys(schemas))
	}
}

// Custom Endpoints are mounted by the router whether or not auto-CRUD is
// on (App.registerEntityEndpoints sits outside the crudEnabled branch),
// so the spec must advertise them either way. A CRUD-disabled entity is
// the case most likely to carry them: opting out of generated CRUD is how
// an app says "reach this through the endpoints I declared". Dropping
// them re-creates the very spec/server mismatch this file exists to close,
// pointing the other way, a live route the spec never mentions.
func TestCRUDDisabledEntityKeepsEndpoints(t *testing.T) {
	off := entity.Define("secrets", entity.EntityConfig{
		Table:    "secrets",
		Fields:   []schema.Field{{Name: "value", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(false)},
		Endpoints: []entity.Endpoint{{
			Method:      "POST",
			Path:        "reveal",
			Description: "Reveal one secret",
		}},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(off), "Test", "1.0.0").Build()
	paths, ok := asMap(doc["paths"])
	if !ok {
		t.Fatalf("spec has no paths object")
	}
	if _, ok := paths["/secrets/reveal"]; !ok {
		t.Errorf("custom endpoint on a CRUD-disabled entity is missing from the spec; emitted %v", mapKeys(paths))
	}
	// The generated CRUD surface still stays out.
	if _, ok := paths["/secrets"]; ok {
		t.Errorf("CRUD-disabled entity still advertised its generated list path")
	}

	// The endpoint's tag has to be declared, or it renders under an
	// undefined group.
	tags, _ := doc["tags"].([]map[string]any)
	found := false
	for _, tm := range tags {
		if tm["name"] == "secrets" {
			found = true
		}
	}
	if !found {
		t.Errorf("entity tag missing while an operation still carries it: %v", tags)
	}
}
