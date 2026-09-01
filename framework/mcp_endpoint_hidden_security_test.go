package framework

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: a Hidden field never reaches a published tool schema. An
// entity Endpoint with MCP: true registers its twin via
// openapi.EndpointInputSchema(endpoint) (app.registerEntityEndpoints and
// the routegroup variant), and tools/list serves that inputSchema to any
// caller the gate lets see the tool at all — per WithToolGate's own doc,
// "the inputSchema is the disclosure". Reusing the entity's field slice
// for the endpoint's InputSchema — the pattern entity.Endpoint's docs
// invite ("the same representation the entity's own CRUD schema is built
// from") — must not publish the hidden column's name or wire key there,
// neither as a property nor as a required entry.
//
// The CRUD-generated tools build their schemas from VisibleFields; only
// the custom-endpoint path feeds an unfiltered developer slice into a
// published tool schema.
func TestMCPEndpointToolSchemaExcludesHidden(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		// Required so the required-list assertion has something to
		// catch: a non-required hidden field never reaches the list.
		{Name: "internal_note", Type: schema.String, Hidden: true, Required: true},
		{Name: "audit_meta", Type: schema.String, Hidden: true, Required: true, WireName: "auditMeta"},
	}
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("invoices", entity.EntityConfig{
		Table:    "invoices",
		Fields:   fields,
		Exposure: &entity.ExposureConfig{CRUD: new(false)},
		Endpoints: []entity.Endpoint{{
			Method: "POST",
			Path:   "export",
			Name:   "export_invoices", // explicit tool name, no default-name coupling
			MCP:    true,
			MCPHandler: func(ctx context.Context, params map[string]any) (any, error) {
				return "ok", nil
			},
			InputSchema: fields,
		}},
	})

	// ListTools is the unfiltered in-process introspection call, so the
	// default authenticated-caller gate cannot hide the tool from the
	// check: the schema is what the gate would disclose, not protect.
	var tool map[string]any
	for _, tl := range app.MCP.ListTools() {
		if tl.Name == "export_invoices" {
			tool = tl.InputSchema
			break
		}
	}
	if tool == nil {
		t.Fatal("export_invoices tool not registered — the endpoint MCP twin is missing")
	}

	props, ok := tool["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool inputSchema properties are %T, not map[string]any", tool["properties"])
	}
	// Positive control first: a schema with no visible property would make
	// every absence check below pass by finding nothing to look at.
	if _, ok := props["title"]; !ok {
		t.Fatal("visible field title missing from tool inputSchema — the schema is not reading the slice")
	}
	for _, hidden := range []string{"internal_note", "internalNote", "audit_meta", "auditMeta"} {
		if _, ok := props[hidden]; ok {
			t.Errorf("SECURITY: [framework] MCP tool export_invoices inputSchema publishes hidden field %q", hidden)
		}
	}
	if reqs, ok := tool["required"].([]string); ok {
		for _, r := range reqs {
			if slices.Contains([]string{"internal_note", "audit_meta"}, r) {
				t.Errorf("SECURITY: [framework] MCP tool export_invoices inputSchema lists hidden field %q as required", r)
			}
		}
	} else {
		t.Fatalf("tool inputSchema required list missing or not []string: %#v", tool["required"])
	}
}
