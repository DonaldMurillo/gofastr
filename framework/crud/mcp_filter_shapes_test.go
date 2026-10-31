package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The list tool sends each filter in the shape the list route reads
// (2026-10-27 bug bash D28, D29, D30). A JSON field's value went out as
// its first array element or fmt.Sprint's map[x:1]; an empty _in array
// dropped the filter and listed every row; and an _in element holding a
// comma was split by the route into two values.
func TestMCPListToolFilterShapes(t *testing.T) {
	installSecurityOwnerExtractor(t)

	db := setupDB(t, `CREATE TABLE mcp_docs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, payload TEXT)`)
	ent := entity.Define("mcp_docs", makeEntityConfig("mcp_docs", "mcp_docs", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
		{Name: "payload", Type: schema.JSON},
	}))
	ent.SetDB(db)
	seedRows(t, db, "mcp_docs", []map[string]any{
		{"id": "pair", "user_id": "alice", "name": "Doe, Jane", "payload": `[1,2]`},
		{"id": "one", "user_id": "alice", "name": "Doe", "payload": `[1]`},
		{"id": "obj", "user_id": "alice", "name": "Jane", "payload": `{"x":1}`},
	})

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/mcp_docs")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := handler.SetUser(mcp.WithRequest(context.Background(), orig), &testUser{id: "alice"})

	// call answers the ids the list returned, or ok=false for a refusal.
	call := func(rawArgs string) (ids []string, ok bool) {
		t.Helper()
		p, err := json.Marshal(map[string]any{"name": "mcp_docs_list", "arguments": json.RawMessage(rawArgs)})
		if err != nil {
			t.Fatal(err)
		}
		resp := srv.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
		if resp.Error != nil {
			return nil, false
		}
		out, err := json.Marshal(resp.Result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), `"isError":true`) {
			return nil, false
		}
		for _, id := range []string{"pair", "one", "obj"} {
			if strings.Contains(string(out), `\"id\":\"`+id+`\"`) {
				ids = append(ids, id)
			}
		}
		return ids, true
	}
	expect := func(args string, want ...string) {
		t.Helper()
		ids, ok := call(args)
		if !ok {
			t.Errorf("%s was refused, want %v", args, want)
		} else if strings.Join(ids, ",") != strings.Join(want, ",") {
			t.Errorf("%s listed %v, want %v", args, ids, want)
		}
	}
	refuse := func(args string) {
		t.Helper()
		if ids, ok := call(args); ok {
			t.Errorf("%s was answered with %v, want a refusal", args, ids)
		}
	}

	expect(`{"name_in": ["Doe", "Jane"]}`, "one", "obj")
	expect(`{"payload": [1, 2]}`, "pair")
	expect(`{"payload": {"x": 1}}`, "obj")
	refuse(`{"name_in": []}`)
	refuse(`{"name_in": ["Doe, Jane"]}`)
	refuse(`{"name": ["Doe", "Jane"]}`)
	refuse(`{"name": {"x": 1}}`)
}

// The tools/call validator refuses a list or object for a string filter
// and a scalar for an _in key before the tool runs, so these shapes
// reach toolFilterValues only from a caller that skips it. The tool
// still refuses them rather than sending a cut or garbled value.
func TestToolFilterValuesRefusesOffSchemaShapes(t *testing.T) {
	name := schema.Field{Name: "name", Type: schema.String}
	payload := schema.Field{Name: "payload", Type: schema.JSON}
	for _, tc := range []struct {
		key   string
		field schema.Field
		in    bool
		v     any
	}{
		{"name", name, false, []any{"Doe", "Jane"}},
		{"name", name, false, map[string]any{"x": 1.0}},
		{"name_in", name, true, []any{map[string]any{"x": 1.0}}},
		{"payload_in", payload, true, map[string]any{"x": 1.0, "y": 2.0}},
	} {
		if vs, err := toolFilterValues(tc.key, tc.field, tc.in, tc.v); err == nil {
			t.Errorf("%s = %#v was sent as %q, want a refusal", tc.key, tc.v, vs)
		}
	}
	// A scalar _in on a plain field keeps the route's comma form.
	if vs, err := toolFilterValues("name_in", name, true, "Doe,Jane"); err != nil || len(vs) != 1 || vs[0] != "Doe,Jane" {
		t.Errorf("name_in = \"Doe,Jane\" became %q, %v", vs, err)
	}
}
