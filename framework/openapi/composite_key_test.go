package openapi

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func keyedEntity(name string, key []string, fields ...schema.Field) *entity.Entity {
	return entity.Define(name, entity.EntityConfig{
		Table:      name,
		Fields:     fields,
		PrimaryKey: key,
	}.WithTimestamps(false))
}

// pathParams lists an operation's path parameter names in order.
func pathParams(op map[string]any) []string {
	var out []string
	params, _ := op["parameters"].([]map[string]any)
	for _, p := range params {
		if p["in"] == "path" {
			out = append(out, p["name"].(string))
		}
	}
	return out
}

// A composite-keyed record's path takes one segment per key column, named
// by its wire key, and a batch update names each item by every key column.
// A key column that shares its name with a query parameter keeps its path
// parameter.
func TestCompositeKeyRecordPaths(t *testing.T) {
	ent := keyedEntity("enrollments", []string{"student_id", "fields"},
		schema.Field{Name: "student_id", Type: schema.String, Required: true},
		schema.Field{Name: "fields", Type: schema.String, Required: true},
		schema.Field{Name: "grade", Type: schema.String},
	)
	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")
	if _, has := paths["/enrollments/{id}"]; has {
		t.Fatal("composite entity still documents /enrollments/{id}")
	}
	item := getMap(t, paths, "/enrollments/{studentId}/{fields}")
	for _, verb := range []string{"get", "put", "patch", "delete"} {
		if got := pathParams(getMap(t, item, verb)); !slices.Equal(got, []string{"studentId", "fields"}) {
			t.Errorf("%s path params = %v, want [studentId fields]", verb, got)
		}
	}

	batch := requestBodyProps(t, getMap(t, getMap(t, paths, "/enrollments/_batch"), "patch"))
	items := getMap(t, getMap(t, batch, "properties"), "items")
	allOf := getMap(t, items, "items")["allOf"].([]any)
	key := allOf[1].(map[string]any)
	if got := key["required"].([]string); !slices.Equal(got, []string{"studentId", "fields"}) {
		t.Errorf("batch item required = %v, want [studentId fields]", got)
	}
}

// A one-column key not named id keeps the {id} path, and a batch update
// item names the record by the key column the runtime reads.
func TestSingleKeyBatchItemNamesKeyColumn(t *testing.T) {
	ent := keyedEntity("products", []string{"sku"},
		schema.Field{Name: "sku", Type: schema.String, Required: true},
		schema.Field{Name: "title", Type: schema.String},
	)
	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")
	getMap(t, paths, "/products/{id}")
	batch := requestBodyProps(t, getMap(t, getMap(t, paths, "/products/_batch"), "patch"))
	items := getMap(t, getMap(t, batch, "properties"), "items")
	allOf := getMap(t, items, "items")["allOf"].([]any)
	if got := allOf[1].(map[string]any)["required"].([]string); !slices.Equal(got, []string{"sku"}) {
		t.Errorf("batch item required = %v, want [sku]", got)
	}
}

// An integer key column is a number in a batch item's body, the type its
// field declares, while its path parameter stays a string.
func TestCompositeIntKeyBatchItemTypes(t *testing.T) {
	ent := keyedEntity("seats", []string{"room_no", "seat_no"},
		schema.Field{Name: "room_no", Type: schema.Int, Required: true},
		schema.Field{Name: "seat_no", Type: schema.Int, Required: true},
	)
	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")
	batch := requestBodyProps(t, getMap(t, getMap(t, paths, "/seats/_batch"), "patch"))
	items := getMap(t, getMap(t, batch, "properties"), "items")
	allOf := getMap(t, items, "items")["allOf"].([]any)
	props := allOf[1].(map[string]any)["properties"].(map[string]any)
	for _, k := range []string{"roomNo", "seatNo"} {
		p, _ := props[k].(map[string]any)
		if p["type"] != "integer" {
			t.Errorf("batch item %s = %v, want type integer", k, p)
		}
		if _, ro := p["readOnly"]; ro {
			t.Errorf("batch item %s is readOnly: %v", k, p)
		}
	}
}

// A batch update item is sparse, like a single PATCH: only its key
// columns are required, never the entity's other required fields.
func TestBatchItemRequiresOnlyKey(t *testing.T) {
	ent := keyedEntity("posts", []string{"id"},
		schema.Field{Name: "id", Type: schema.String, Required: true},
		schema.Field{Name: "title", Type: schema.String, Required: true},
		schema.Field{Name: "views", Type: schema.Int},
	)
	doc := EntityOpenAPI(reg(ent), "Test", "1.0.0", nil).Build()
	batch := requestBodyProps(t, getMap(t, getMap(t, getMap(t, doc, "paths"), "/posts/_batch"), "patch"))
	items := getMap(t, getMap(t, batch, "properties"), "items")
	allOf := getMap(t, items, "items")["allOf"].([]any)
	if req, has := allOf[0].(map[string]any)["required"]; has {
		t.Errorf("batch item body requires %v, want only the key", req)
	}
}
