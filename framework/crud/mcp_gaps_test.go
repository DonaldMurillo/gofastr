package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// #669 parked gaps on the MCP surface, one test each.

// The record-key arguments were typed {"type": "string"} whatever the key
// column holds: an integer-keyed entity offered an id prop a client fills
// with a string. They are typed from the key column now, the single-key
// {id} included.
func TestMCPKeyArgsTypedFromKeyColumn(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name:       "widgets",
		Table:      "widgets",
		PrimaryKey: []string{"serial"},
		Fields: []schema.Field{
			{Name: "serial", Type: schema.Int, AutoGenerate: schema.AutoNone},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	props := idToolSchema(ent, nil, ent.KeyColumns())["properties"].(map[string]any)
	serial := props["serial"].(map[string]any)
	if serial["type"] != "integer" {
		t.Fatalf("integer key typed %v, want integer", serial)
	}

	// The single-column key is named id and typed from its column too.
	props = idToolSchema(ent, nil, []string{"id"})["properties"].(map[string]any)
	id := props["id"].(map[string]any)
	if id["type"] != "integer" {
		t.Fatalf("single {id} typed %v, want integer from the key column", id)
	}
}

// The single-key {id} argument names the KEY column, so an unrelated
// field literally named "id" must not win the lookup: the tool would
// advertise that field's type for the record key (review finding on
// #669: a String "id" beside an Int "serial" key typed the key a
// string, and a client either rejects valid keys or sends the wrong
// type).
func TestMCPKeyArgIdResolvesToKeyColumnNotIdField(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name:       "widgets",
		Table:      "widgets",
		PrimaryKey: []string{"serial"},
		Fields: []schema.Field{
			{Name: "serial", Type: schema.Int, AutoGenerate: schema.AutoNone},
			{Name: "id", Type: schema.String}, // a display slug, not the key
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	props := idToolSchema(ent, nil, []string{"id"})["properties"].(map[string]any)
	if got := props["id"].(map[string]any)["type"]; got != "integer" {
		t.Fatalf("{id} typed %v, want integer from the serial key column", got)
	}
}

// A composite key with a text leg types that leg as a string: only the
// integer columns become integer.
func TestMCPKeyArgsMixedComposite(t *testing.T) {
	ent := entity.Define("pairs", entity.EntityConfig{
		Name:       "pairs",
		Table:      "pairs",
		PrimaryKey: []string{"n", "slug"},
		Fields: []schema.Field{
			{Name: "n", Type: schema.Int},
			{Name: "slug", Type: schema.String},
		},
	}.WithTimestamps(false))
	props := idToolSchema(ent, nil, ent.KeyColumns())["properties"].(map[string]any)
	if got := props["n"].(map[string]any)["type"]; got != "integer" {
		t.Errorf("n typed %v, want integer", got)
	}
	if got := props["slug"].(map[string]any)["type"]; got != "string" {
		t.Errorf("slug typed %v, want string", got)
	}
}

// The update tool's key arguments are typed from the key column as well,
// not hardcoded strings.
func TestMCPUpdateKeyArgsTypedFromKeyColumn(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name:       "widgets",
		Table:      "widgets",
		PrimaryKey: []string{"serial"},
		Fields: []schema.Field{
			{Name: "serial", Type: schema.Int},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	args := (&CrudHandler{Entity: ent}).toolKeyArgs()
	props := updateToolSchema(ent, nil, args)["properties"].(map[string]any)
	id := props["id"].(map[string]any)
	if id["type"] != "integer" {
		t.Fatalf("update tool id typed %v, want integer from the key column", id)
	}
}

// The list tool forwards and documents ?per_page= and ?offset= like the
// HTTP route (#669 item 3's gap for MCP): schema first, then the
// forwarding it promises.
func TestMCPListToolDocumentsPerPageOffset(t *testing.T) {
	ent := entity.Define("items", entity.EntityConfig{
		Name:  "items",
		Table: "items",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	props := listToolSchema(ent, nil)["properties"].(map[string]any)
	for _, name := range []string{"per_page", "offset"} {
		p, ok := props[name].(map[string]any)
		if !ok {
			t.Fatalf("list tool schema omits %s", name)
		}
		if p["type"] != "integer" {
			t.Errorf("%s typed %v, want integer", name, p["type"])
		}
	}
}

// An integer key arrives from the MCP server as float64 (the JSON number
// the integer-typed id prop asks for), and fmt.Sprint spells 1234567 as
// 1.234567e+06: the tool fetched /widgets/1.234567e+06, a record that
// does not exist. The key segment is the integer's own digits.
func TestMCPIntegerKeyPathKeepsDigits(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name:       "widgets",
		Table:      "widgets",
		PrimaryKey: []string{"serial"},
		Fields: []schema.Field{
			{Name: "serial", Type: schema.Int, AutoGenerate: schema.AutoNone},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ch := &CrudHandler{Entity: ent}
	path, args, err := ch.toolRecordPath(map[string]any{"id": float64(1234567)})
	if err != nil {
		t.Fatal(err)
	}
	if want := ch.mcpBase() + "/1234567"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if args["id"] != "1234567" {
		t.Fatalf("args[id] = %q, want 1234567", args["id"])
	}
}
