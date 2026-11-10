package crud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Relation field whose target has an integer key holds that integer and
// is checked as an Int (entity.ValueFields), and every read answers it as
// a JSON number. The MCP tools type it the same way, like the record's own
// integer key: the number a tool returns for shop_id is an argument the
// create, update and list tools of the same entity take.
//
// The bug this holds: the tools described every Relation as a string, and
// tools/call enforces the schema, so the number items_get returned was
// refused as invalid params and only the string "1" passed.

// intReferenceEntities defines items, whose shop_id references the
// integer-keyed shops, and a tags entity with a text key for the control.
func intReferenceEntities() (shops, tags, items *entity.Entity) {
	public := &entity.ExposureConfig{Public: true}
	inc := schema.Field{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}
	shops = entity.Define("shops", entity.EntityConfig{
		Fields:   []schema.Field{inc, {Name: "name", Type: schema.String}},
		Exposure: public,
	}.WithTimestamps(false))
	tags = entity.Define("tags", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Exposure: public,
	}.WithTimestamps(false))
	items = entity.Define("items", entity.EntityConfig{
		Fields: []schema.Field{inc,
			{Name: "shop_id", Type: schema.Relation, To: "shops"},
			{Name: "tag_id", Type: schema.Relation, To: "tags"},
			{Name: "title", Type: schema.String},
		},
		Exposure: public,
	}.WithTimestamps(false))
	return shops, tags, items
}

// intReferenceMCP mounts the three entities and registers the items tools.
// known names the entities the registry holds when the tools are
// registered; the rest are added to it afterwards, the way an app that
// declares items before shops registers them.
func intReferenceMCP(t *testing.T, known ...string) (*mcp.Server, context.Context) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE shops (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, shop_id INTEGER, tag_id TEXT, title TEXT)`,
	)
	shops, tags, items := intReferenceEntities()
	all := map[string]*entity.Entity{"shops": shops, "tags": tags, "items": items}
	reg := stubRegistry{byName: map[string]*entity.Entity{}}
	for _, name := range known {
		reg.byName[name] = all[name]
	}
	r := router.New()
	srv := mcp.NewServer()
	for _, e := range []*entity.Entity{shops, tags, items} {
		e.SetDB(db)
		ch := NewCrudHandler(e, db).WithJSONCase(CaseSnake)
		ch.Registry = reg
		RegisterCrudRoutes(r, ch, "/"+e.GetName())
		if e == items {
			if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
				t.Fatalf("register mcp tools: %v", err)
			}
		}
	}
	for name, e := range all {
		reg.byName[name] = e
	}
	seedRows(t, db, "shops", []map[string]any{{"id": 1, "name": "s1"}, {"id": 2, "name": "s2"}})
	seedRows(t, db, "tags", []map[string]any{{"id": "red", "name": "Red"}})
	seedRows(t, db, "items", []map[string]any{
		{"id": 1, "shop_id": 1, "tag_id": "red", "title": "a"},
		{"id": 2, "shop_id": 2, "tag_id": "red", "title": "b"},
	})
	return srv, handler.SetUser(context.Background(), &testUser{id: "u1"})
}

// listedTitles reads the titles an items_list result holds.
func listedTitles(t *testing.T, out any) string {
	t.Helper()
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("encode list result: %v", err)
	}
	var page struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &page); err != nil {
		t.Fatalf("decode list result %s: %v", b, err)
	}
	var titles []string
	for _, row := range page.Data {
		titles = append(titles, row.Title)
	}
	return strings.Join(titles, ",")
}

func TestMCPToolsAcceptTheIntegerReferenceTheyReturn(t *testing.T) {
	srv, ctx := intReferenceMCP(t, "shops", "tags", "items")

	got, err := srv.CallTool(ctx, "items_get", map[string]any{"id": float64(1)})
	if err != nil {
		t.Fatalf("items_get: %v", err)
	}
	row, _ := got.(map[string]any)
	raw, _ := json.Marshal(row["shop_id"])
	if string(raw) != "1" {
		t.Fatalf("control: items_get returned shop_id %s; the test needs the JSON number 1", raw)
	}
	// What a client sends back: the JSON number, which the transport
	// hands a tool as float64 (core/mcp normalizeArgNumbers).
	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(1), "title": "renamed", "shop_id": float64(2)}); err != nil {
		t.Errorf("items_update refuses an integer shop_id: %v", err)
	}
	if _, err := srv.CallTool(ctx, "items_create", map[string]any{"title": "new", "shop_id": float64(1)}); err != nil {
		t.Errorf("items_create refuses an integer shop_id: %v", err)
	}
	if _, err := srv.CallTool(ctx, "items_create", map[string]any{"title": "loose", "shop_id": nil}); err != nil {
		t.Errorf("items_create refuses null for the optional shop_id: %v", err)
	}
	// The writes landed, and the filters select by the integer.
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"shop_id": float64(1)}, "new"},
		{map[string]any{"shop_id": float64(2), "sort": "id"}, "renamed,b"},
		{map[string]any{"shop_id_ne": float64(2)}, "new"},
		{map[string]any{"shop_id_in": []any{float64(1), float64(2)}, "sort": "id"}, "renamed,b,new"},
		{map[string]any{"shop_id_gt": float64(1), "sort": "id"}, "renamed,b"},
	} {
		out, err := srv.CallTool(ctx, "items_list", c.args)
		if err != nil {
			t.Errorf("items_list %v: %v", c.args, err)
			continue
		}
		if titles := listedTitles(t, out); titles != c.want {
			t.Errorf("items_list %v selected %q, want %q", c.args, titles, c.want)
		}
	}

	// It is typed as the record's own integer key is: the quoted spelling
	// is not an integer, on a write or a filter.
	for tool, args := range map[string]map[string]any{
		"items_update": {"id": float64(1), "shop_id": "1"},
		"items_create": {"title": "quoted", "shop_id": "1"},
		"items_list":   {"shop_id": "1"},
	} {
		if _, err := srv.CallTool(ctx, tool, args); err == nil || !strings.Contains(err.Error(), "expected type integer") {
			t.Errorf("%s took the string \"1\" for the integer shop_id (err %v)", tool, err)
		}
	}
	// A reference to a text key stays a string.
	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(2), "tag_id": "red"}); err != nil {
		t.Errorf("items_update refuses the text tag_id: %v", err)
	}
	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(2), "tag_id": float64(7)}); err == nil {
		t.Errorf("items_update took a number for tag_id, a reference to a text key")
	}
}

// The schemas say what the calls above showed: an integer for the
// reference to an integer key, in the write bodies and in every filter
// prop, and a string for the reference to a text key. A handler with no
// registry cannot tell the two apart, and neither can its route, which
// then checks the reference as declared: both stay strings.
func TestMCPToolSchemasTypeAReferenceByItsTargetKey(t *testing.T) {
	shops, tags, items := intReferenceEntities()
	reg := stubRegistry{byName: map[string]*entity.Entity{"shops": shops, "tags": tags, "items": items}}
	typeOf := func(props map[string]any, name string) any {
		t.Helper()
		p, ok := props[name].(map[string]any)
		if !ok {
			t.Fatalf("no %s prop in %v", name, props)
		}
		if name == "shop_id_in" || name == "tag_id_in" {
			return p["items"].(map[string]any)["type"]
		}
		return p["type"]
	}
	encode := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}

	create := schemaProps(t, writeToolSchema(items, reg))
	update := schemaProps(t, updateToolSchema(items, reg, []string{"id"}))
	list := schemaProps(t, listToolSchema(items, reg))
	for name, props := range map[string]map[string]any{"create": create, "update": update} {
		if got := encode(typeOf(props, "shop_id")); got != `["integer","null"]` {
			t.Errorf("%s shop_id typed %s, want integer or null", name, got)
		}
		if got := encode(typeOf(props, "tag_id")); got != `["string","null"]` {
			t.Errorf("%s tag_id typed %s, want string or null", name, got)
		}
	}
	for _, name := range []string{"shop_id", "shop_id_ne", "shop_id_in", "shop_id_gt", "shop_id_lte"} {
		if got := typeOf(list, name); got != "integer" {
			t.Errorf("list %s typed %v, want integer", name, got)
		}
	}
	for _, name := range []string{"tag_id", "tag_id_in"} {
		if got := typeOf(list, name); got != "string" {
			t.Errorf("list %s typed %v, want string", name, got)
		}
	}

	bare := schemaProps(t, writeToolSchema(items, nil))
	if got := encode(typeOf(bare, "shop_id")); got != `["string","null"]` {
		t.Errorf("with no registry shop_id typed %s, want the declared string or null", got)
	}
}

// The schemas are built when the tools are registered, and an app may
// declare items before the shops it references. A reference whose target
// is not registered yet takes an integer or a string, and the route,
// which knows the target by the time a call arrives, refuses the wrong
// one: the integer key a read answers is never refused by a schema that
// had to guess.
func TestMCPToolsTakeAnIntegerReferenceRegisteredBeforeItsTarget(t *testing.T) {
	srv, ctx := intReferenceMCP(t, "items")

	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(1), "shop_id": float64(2)}); err != nil {
		t.Errorf("items_update refuses an integer shop_id when shops registers after items: %v", err)
	}
	if _, err := srv.CallTool(ctx, "items_create", map[string]any{"title": "new", "shop_id": float64(2), "tag_id": "red"}); err != nil {
		t.Errorf("items_create refuses an integer shop_id when shops registers after items: %v", err)
	}
	if _, err := srv.CallTool(ctx, "items_create", map[string]any{"title": "loose", "shop_id": nil}); err != nil {
		t.Errorf("items_create refuses null for the optional shop_id: %v", err)
	}
	out, err := srv.CallTool(ctx, "items_list", map[string]any{"shop_id": float64(2), "sort": "id"})
	if err != nil {
		t.Fatalf("items_list refuses an integer shop_id filter: %v", err)
	}
	if titles := listedTitles(t, out); titles != "a,b,new" {
		t.Errorf("items_list shop_id=2 selected %q, want a,b,new", titles)
	}
	// The route still decides: a value that names no integer is its 400.
	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(1), "shop_id": "nope"}); err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Errorf("items_update with shop_id \"nope\": err %v, want the route's 400", err)
	}
	if _, err := srv.CallTool(ctx, "items_update", map[string]any{"id": float64(1), "shop_id": true}); err == nil || !strings.Contains(err.Error(), "expected type") {
		t.Errorf("items_update with a boolean shop_id: err %v, want the schema's refusal", err)
	}
}
