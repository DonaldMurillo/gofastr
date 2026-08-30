package openapi

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: a field marked Hidden never appears in any surface of the
// published spec. The spec is served to unauthenticated clients (the docs
// and /openapi.json handlers), so a Hidden column's name reaching it, as
// a schema property, a required entry, a filter parameter, or in the
// ?fields= projection blurb, publishes the existence and wire key of a
// column the entity declared invisible.
//
// entity.Define enforces the related SearchFields rule (a Hidden column
// there panics), but nothing pins the builder's own visibleFields walk.
// This sweeps every surface that names a field, including one whose wire
// key differs from its column name (WireName), the exclusion must hold
// under the same key resolution the properties map uses.
//
// The entity also declares a custom Endpoint whose InputSchema and
// OutputSchema reuse the entity's own field slice — the exact reuse
// entity.Endpoint's docs invite ("the same representation the entity's
// own CRUD schema is built from"). The endpoint-schema conversion
// (EndpointInputSchema/EndpointOutputSchema) is a separate code path from
// the CRUD visibleFields walk, so the sweep extends to the custom
// endpoint's request body and 200 response, surfaces the CRUD-only walk
// never reached.
func TestHiddenFieldAbsentFromEverySurface(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		// Both hidden fields are Required on purpose. A hidden field
		// that is not required never reaches the required list, so the
		// required-list assertion below would hold no matter what the
		// filter did, the test would stay green through a regression
		// in exactly the code it exists to pin.
		{Name: "internal_note", Type: schema.String, Hidden: true, Required: true},
		{Name: "audit_meta", Type: schema.String, Hidden: true, Required: true, WireName: "auditMeta"},
	}
	ent := entity.Define("invoices", entity.EntityConfig{
		Table:  "invoices",
		Fields: fields,
		Endpoints: []entity.Endpoint{{
			// Relative path: mounted (and published) under the
			// entity's table, POST /invoices/export.
			Method:       "POST",
			Path:         "export",
			InputSchema:  fields,
			OutputSchema: fields,
		}},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0", nil).Build()

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
	// checking the hidden ones are absent, a type assertion that quietly
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

	// 3. Filter parameters on the list operation. Checked structurally
	// rather than against a hand-written list of suffixes: the builder
	// generates a parameter per operator, so an enumerated list silently
	// stops covering any operator added later. Every parameter whose name
	// is a hidden key, or a hidden key plus an operator suffix, is a leak.
	listOp := getMap(t, getMap(t, doc, "paths"), "/invoices")
	getListOp := getMap(t, listOp, "get")
	params, ok := getListOp["parameters"].([]map[string]any)
	if !ok {
		t.Fatalf("list parameters are %T, not []map[string]any", getListOp["parameters"])
	}
	sawVisibleFilter := false
	for _, p := range params {
		name, _ := p["name"].(string)
		for _, hidden := range hiddenKeys {
			if name == hidden || strings.HasPrefix(name, hidden+"_") {
				t.Errorf("SECURITY: [openapi] hidden field advertised as filter parameter %q", name)
			}
		}
		if name == "title" || strings.HasPrefix(name, "title_") {
			sawVisibleFilter = true
		}
	}
	// Positive control: if the builder emitted no field filters at all, the
	// loop above would pass while proving nothing.
	if !sawVisibleFilter {
		t.Error("no filter parameter for the visible field — the exclusion check proves nothing")
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

	// 5. Every request body that accepts entity fields, create, the two
	// item updates, the batch variants, and the custom endpoint's typed
	// body, excludes the hidden ones.
	//
	// The previous version serialized the /invoices path item and grepped
	// it, which covered the create body by accident (bodies are inlined)
	// and missed /invoices/{id} put and patch plus /invoices/_batch
	// entirely. Walking every body's schema for property NAMES also stops a
	// hidden key from hiding inside a nested allOf branch, which is exactly
	// the shape the batch-patch body uses.
	bodies := 0
	sawVisibleProp := false
	for path, item := range getMap(t, doc, "paths") {
		ops, ok := asMap(item)
		if !ok {
			continue
		}
		for method, op := range ops {
			o, ok := asMap(op)
			if !ok {
				continue
			}
			rb, has := o["requestBody"]
			if !has {
				continue
			}
			bodies++
			names := schemaPropertyNames(rb)
			for _, hidden := range hiddenKeys {
				if slices.Contains(names, hidden) {
					t.Errorf("SECURITY: [openapi] %s %s request body accepts hidden field %q",
						strings.ToUpper(method), path, hidden)
				}
			}
			if slices.Contains(names, "title") {
				sawVisibleProp = true
			}
		}
	}
	if bodies == 0 {
		t.Fatal("no request bodies found — the exclusion check proves nothing")
	}
	// Positive control on the walker itself: if schemaPropertyNames returned
	// nothing, every Contains above would pass regardless of the document.
	if !sawVisibleProp {
		t.Error("no request body exposed the visible field — the schema walk is not reading properties")
	}

	// 6. Response schemas. A custom endpoint with a typed OutputSchema
	// publishes it as its 200 response (addCustomEndpoints ->
	// AddResponse(200, ..., EndpointOutputSchema(...))), and the
	// requestBody-only walk above never looks there. The entity-CRUD
	// responses reference the (filtered) component schema, so the only
	// inline field properties under "responses" come from the endpoint
	// path — the surface this section pins.
	responses := 0
	sawVisibleResponseProp := false
	for path, item := range getMap(t, doc, "paths") {
		ops, ok := asMap(item)
		if !ok {
			continue
		}
		for method, op := range ops {
			o, ok := asMap(op)
			if !ok {
				continue
			}
			resps, has := o["responses"]
			if !has {
				continue
			}
			responses++
			names := schemaPropertyNames(resps)
			for _, hidden := range hiddenKeys {
				if slices.Contains(names, hidden) {
					t.Errorf("SECURITY: [openapi] %s %s response schema publishes hidden field %q",
						strings.ToUpper(method), path, hidden)
				}
			}
			if slices.Contains(names, "title") {
				sawVisibleResponseProp = true
			}
		}
	}
	if responses == 0 {
		t.Fatal("no responses found — the exclusion check proves nothing")
	}
	if !sawVisibleResponseProp {
		t.Error("no response schema exposed the visible field — the schema walk is not reading properties")
	}
}

// hiddenKeys is every spelling of the two hidden fields: the database
// column name and the wire key, since either one identifies the column to
// a reader of the spec.
var hiddenKeys = []string{"internal_note", "internalNote", "audit_meta", "auditMeta"}

// schemaPropertyNames collects every key declared under a "properties"
// object anywhere in v, descending through allOf/items/nested schemas so a
// name cannot hide one level down.
func schemaPropertyNames(v any) []string {
	var out []string
	var walk func(any)
	walk = func(n any) {
		switch t := n.(type) {
		case map[string]any:
			for k, child := range t {
				if k == "properties" {
					if props, ok := asMap(child); ok {
						for name := range props {
							out = append(out, name)
						}
					}
				}
				walk(child)
			}
		case map[string]map[string]any:
			for k, child := range t {
				if k == "properties" {
					for name := range child {
						out = append(out, name)
					}
				}
				walk(child)
			}
		case map[int]map[string]any:
			// Operation.Responses is keyed by int status code; no
			// int key can be "properties", so just descend.
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		case []map[string]any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

// Property: a Hidden field never reaches the exported endpoint-schema
// conversions. EndpointInputSchema and EndpointOutputSchema are the single
// source the OpenAPI requestBody/response AND the generated MCP tool input
// schema both consume (framework/app.go registers an endpoint's MCP twin
// with openapi.EndpointInputSchema(endpoint); per WithToolGate's own doc,
// "the inputSchema is the disclosure"). Passing them the entity's field
// slice verbatim must not publish the hidden column's name, neither as a
// property nor as a required entry.
func TestEndpointSchemaHelpersExcludeHidden(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		// Required so the required-list assertion has something to
		// catch: a non-required hidden field never reaches the list.
		{Name: "internal_note", Type: schema.String, Hidden: true, Required: true},
		{Name: "audit_meta", Type: schema.String, Hidden: true, Required: true, WireName: "auditMeta"},
	}
	ep := entity.Endpoint{
		Method:       "POST",
		Path:         "export",
		InputSchema:  fields,
		OutputSchema: fields,
	}
	for name, sch := range map[string]map[string]any{
		"EndpointInputSchema":  EndpointInputSchema(ep),
		"EndpointOutputSchema": EndpointOutputSchema(ep),
	} {
		names := schemaPropertyNames(sch)
		if !slices.Contains(names, "title") {
			t.Fatalf("%s lost the visible field — the conversion is not reading the slice", name)
		}
		for _, hidden := range hiddenKeys {
			if slices.Contains(names, hidden) {
				t.Errorf("SECURITY: [openapi] %s publishes hidden field %q (this schema is also the MCP tool input)", name, hidden)
			}
		}
		if reqs, ok := sch["required"].([]string); ok {
			for _, r := range reqs {
				if slices.Contains(hiddenKeys, r) {
					t.Errorf("SECURITY: [openapi] %s lists hidden field %q as required", name, r)
				}
			}
		} else {
			t.Fatalf("%s required list missing or not []string: %#v", name, sch["required"])
		}
	}
}
