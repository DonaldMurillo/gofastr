package openapi

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: a field marked Hidden never appears in any surface of the
// published spec. The spec is served to unauthenticated clients (the docs
// and /openapi.json handlers), so a Hidden column's name reaching it — as
// a schema property, a required entry, a filter parameter, or in the
// ?fields= projection blurb — publishes the existence and wire key of a
// column the entity declared invisible.
//
// entity.Define enforces the related SearchFields rule (a Hidden column
// there panics), but nothing pins the builder's own visibleFields walk.
// This sweeps every surface that names a field, including one whose wire
// key differs from its column name (WireName) — the exclusion must hold
// under the same key resolution the properties map uses.
func TestHiddenFieldAbsentFromEverySurface(t *testing.T) {
	ent := entity.Define("invoices", entity.EntityConfig{
		Table: "invoices",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			// Both hidden fields are Required on purpose. A hidden field
			// that is not required never reaches the required list, so the
			// required-list assertion below would hold no matter what the
			// filter did — the test would stay green through a regression
			// in exactly the code it exists to pin.
			{Name: "internal_note", Type: schema.String, Hidden: true, Required: true},
			{Name: "audit_meta", Type: schema.String, Hidden: true, Required: true, WireName: "auditMeta"},
		},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0").Build()

	// 1. Schema component properties.
	comps := getMap(t, doc, "components")
	schemas := getMap(t, comps, "schemas")
	inv := getMap(t, schemas, "invoices")
	props := getMap(t, inv, "properties")
	for _, key := range []string{"internal_note", "internalNote", "audit_meta", "auditMeta"} {
		if _, ok := props[key]; ok {
			t.Errorf("SECURITY: [openapi] hidden field %q appears in schema properties", key)
		}
	}

	// 2. Required list. Assert the visible required field is present before
	// checking the hidden ones are absent — a type assertion that quietly
	// failed, or a builder that stopped emitting `required` at all, would
	// otherwise make the absence check pass by finding nothing to look at.
	reqs, ok := inv["required"].([]string)
	if !ok {
		t.Fatalf("required list missing or not []string: %#v", inv["required"])
	}
	if !slices.Contains(reqs, "title") {
		t.Fatalf("required = %v, want the visible required field present", reqs)
	}
	for _, r := range reqs {
		if r == "internalNote" || r == "auditMeta" || r == "internal_note" || r == "audit_meta" {
			t.Errorf("SECURITY: [openapi] hidden field %q listed as required", r)
		}
	}

	// 3. Filter parameters on the list operation (raw and suffixed ops).
	listOp := getMap(t, getMap(t, doc, "paths"), "/invoices")
	raw, _ := json.Marshal(listOp)
	for _, param := range []string{
		`"internal_note"`, `"internal_note_like"`, `"internal_note_in"`,
		`"audit_meta"`, `"audit_meta_gte"`, `"auditMeta"`, `"auditMeta_like"`,
	} {
		if strings.Contains(string(raw), param) {
			t.Errorf("SECURITY: [openapi] hidden field advertised as filter parameter %s", param)
		}
	}

	// 4. The ?fields= projection description must not name the hidden
	// wire keys, and the visible one must (the description is the
	// discovery surface for projections).
	getOp := getMap(t, listOp, "get")
	fieldsParam := findParam(getOp["parameters"], "fields")
	if fieldsParam == nil {
		t.Fatal("?fields= parameter missing from list operation")
	}
	desc, _ := fieldsParam["description"].(string)
	if strings.Contains(desc, "internalNote") || strings.Contains(desc, "auditMeta") {
		t.Errorf("SECURITY: [openapi] ?fields= description names a hidden field: %s", desc)
	}
	if !strings.Contains(desc, "title") {
		t.Errorf("?fields= description lost the visible field: %s", desc)
	}

	// 5. Create/update request bodies exclude hidden fields too.
	createOp := getMap(t, getMap(t, doc, "paths"), "/invoices")
	createRaw, _ := json.Marshal(createOp)
	if strings.Contains(string(createRaw), "auditMeta") || strings.Contains(string(createRaw), "internalNote") {
		t.Errorf("SECURITY: [openapi] hidden field reachable in create/update body")
	}
}
