package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The list route's data items were a bare {type: object}, so a typed
// client generated from the spec saw List() returning untyped maps
// while Get() returned the entity (#669, OpenAPI and SDK). Each
// entity's list envelopes now $ref its own schema component, one
// envelope per pagination mode, and the shared untyped ListResponse /
// CursorPage components are gone.
func TestListEnvelopesRefEntitySchema(t *testing.T) {
	e := entity.Define("items", entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
	comps := getMap(t, doc, "components")
	schemas := getMap(t, comps, "schemas")
	listOp := getMap(t, getMap(t, getMap(t, doc, "paths"), "/items"), "get")
	resp := resp200(t, listOp)
	schema := getMap(t, getMap(t, getMap(t, resp, "content"), "application/json"), "schema")
	oneOf, _ := schema["oneOf"].([]any)
	if len(oneOf) != 2 {
		t.Fatalf("list 200 has %d oneOf arms, want 2 (offset and cursor envelopes)", len(oneOf))
	}

	for i, arm := range oneOf {
		env, ok := arm.(map[string]any)
		if !ok {
			t.Fatalf("oneOf arm %d is not a map", i)
		}
		refName, _ := env["$ref"].(string)
		if refName == "" {
			t.Fatalf("oneOf arm %d is inline, want a $ref", i)
		}
		name := compName(t, refName)
		envSchema := getMap(t, schemas, name)
		data := getMap(t, getMap(t, envSchema, "properties"), "data")
		items := getMap(t, data, "items")
		if got, _ := items["$ref"].(string); got != "#/components/schemas/items" {
			t.Fatalf("envelope %s data.items = %v, want $ref #/components/schemas/items", name, items)
		}
	}

	// The shared untyped envelopes are gone: a reader must not find a
	// component whose data items are a bare object.
	for _, gone := range []string{"ListResponse", "CursorPage"} {
		if _, dup := schemas[gone]; dup {
			t.Errorf("shared untyped component %s still registered", gone)
		}
	}
}

// An entity whose own name is another entity's envelope spelling
// ("itemsListResponse" beside "items") owns that component name; the
// envelope must not fight it — AddSchema panics on duplicates, and
// whichever entity the loop reached first must not decide whose spec
// build dies. The colliding envelope degrades to an inline schema
// with data.items still a $ref to the entity (#669 review finding).
func TestListEnvelopeNameCollidingEntityGoesInline(t *testing.T) {
	def := func(name string) *entity.Entity {
		return entity.Define(name, entity.EntityConfig{
			Table: name,
			Fields: []schema.Field{
				{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
	}
	doc := EntityOpenAPI(reg(def("items"), def("itemsListResponse")), "Test", "1.0.0", nil).Build()
	comps := getMap(t, doc, "components")
	schemas := getMap(t, comps, "schemas")
	// The colliding component belongs to the entity that owns the name.
	owner := getMap(t, schemas, "itemsListResponse")
	if _, hasData := getMap(t, owner, "properties")["data"]; hasData {
		t.Fatal("itemsListResponse component is the list envelope; the entity owns that name")
	}
	// items' offset envelope degrades to inline instead of $ref-ing the
	// stolen component; the cursor envelope keeps its own free name.
	// The inline arm still types data.items as the entity.
	listOp := getMap(t, getMap(t, getMap(t, doc, "paths"), "/items"), "get")
	resp := resp200(t, listOp)
	spec := getMap(t, getMap(t, getMap(t, resp, "content"), "application/json"), "schema")
	oneOf, _ := spec["oneOf"].([]any)
	if len(oneOf) != 2 {
		t.Fatalf("list 200 has %d oneOf arms, want 2", len(oneOf))
	}
	listArm, ok := oneOf[0].(map[string]any)
	if !ok {
		t.Fatal("offset arm is not a map")
	}
	if _, hasRef := listArm["$ref"]; hasRef {
		t.Fatalf("offset arm is a $ref into a name the entity owns: %v", listArm)
	}
	data := getMap(t, getMap(t, listArm, "properties"), "data")
	items := getMap(t, data, "items")
	if got, _ := items["$ref"].(string); got != "#/components/schemas/items" {
		t.Fatalf("inline offset arm data.items = %v, want $ref #/components/schemas/items", items)
	}
	cursorArm, ok := oneOf[1].(map[string]any)
	if !ok {
		t.Fatal("cursor arm is not a map")
	}
	if ref, _ := cursorArm["$ref"].(string); ref != "#/components/schemas/itemsCursorPage" {
		t.Fatalf("cursor arm = %v, want $ref itemsCursorPage (its name is free)", cursorArm)
	}
}

func compName(t *testing.T, ref string) string {
	t.Helper()
	const p = "#/components/schemas/"
	if len(ref) <= len(p) || ref[:len(p)] != p {
		t.Fatalf("bad $ref %q", ref)
	}
	return ref[len(p):]
}
