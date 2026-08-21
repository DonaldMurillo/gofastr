package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestWireNameInFieldsDescAndRequestExclusion pins that every OpenAPI
// surface advertises the SAME wire key for a field that sets WireName.
// Before the fix, the ?fields= description recomputed casing.ToCamel
// (advertising "bodyText") while the response used the WireName
// ("content"), so ?fields=bodyText 400'd. Likewise, the request-schema
// exclusion tried to delete "roleCode" after the property was renamed
// to "role", leaving a ReadOnly field the runtime ignores in every
// POST/PUT/PATCH request schema.
func TestWireNameInFieldsDescAndRequestExclusion(t *testing.T) {
	e := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "body_text", Type: schema.String, WireName: "content"},
			{Name: "role_code", Type: schema.String, WireName: "role", ReadOnly: true},
		},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")

	// --- ?fields= description must advertise the WireName, not camelCase ---
	listOp := getMap(t, getMap(t, paths, "/posts"), "get")
	fp := findParam(listOp["parameters"], "fields")
	if fp == nil {
		t.Fatal("list op missing 'fields' parameter")
	}
	desc, _ := fp["description"].(string)
	if !strings.Contains(desc, "content") {
		t.Errorf("?fields= description should advertise WireName %q, got: %s", "content", desc)
	}
	if strings.Contains(desc, "bodyText") {
		t.Errorf("?fields= description should NOT advertise camelCase %q (runtime does not accept it): %s", "bodyText", desc)
	}

	// --- POST request body must exclude ReadOnly by WireName ---
	postOp := getMap(t, getMap(t, paths, "/posts"), "post")
	body := getMap(t, getMap(t, postOp["requestBody"].(map[string]any), "content"), "application/json")
	schemaMap := getMap(t, body, "schema")
	props := getMap(t, schemaMap, "properties")
	if _, present := props["role"]; present {
		t.Error("POST request schema must exclude ReadOnly field by its wire key 'role', but it is present — generated clients would send a value the runtime ignores")
	}
	if _, present := props["content"]; !present {
		t.Error("POST request schema should include writable WireName field 'content'")
	}

	// --- Response schema property must be the WireName ---
	components := getMap(t, doc, "components")
	schemas := getMap(t, components, "schemas")
	entSchema := getMap(t, schemas, "posts")
	entProps := getMap(t, entSchema, "properties")
	if _, present := entProps["content"]; !present {
		t.Error("response schema missing WireName property 'content'")
	}
	if _, present := entProps["bodyText"]; present {
		t.Error("response schema should not have camelCase property 'bodyText' for a WireName field")
	}
}
