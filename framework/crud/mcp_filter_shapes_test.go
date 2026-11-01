package crud

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The list tool sends each filter in the shape the list route reads
// (2026-10-27 bug bash D28, D29, D30). A JSON field's value went out as
// its first array element or fmt.Sprint's map[x:1], and a JSON field now
// takes no filter at all (D17); an empty _in array dropped the filter and
// listed every row; and an _in element holding a comma was split by the
// route into two values.
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

	// An argument refusal is the agent's to act on: invalid params with
	// its message, never redacted to "internal tool error" and never
	// logged at Error as a server fault.
	errorLogs := countErrorLogs(t)
	// call answers the ids the list returned, or ok=false for a refusal.
	call := func(rawArgs string) (ids []string, ok bool) {
		t.Helper()
		p, err := json.Marshal(map[string]any{"name": "mcp_docs_list", "arguments": json.RawMessage(rawArgs)})
		if err != nil {
			t.Fatal(err)
		}
		resp := srv.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
		if resp.Error != nil {
			if resp.Error.Code != mcp.ErrInvalidParams || resp.Error.Message == "internal tool error" {
				t.Errorf("%s refused as %d %q; want invalid params with the reason", rawArgs, resp.Error.Code, resp.Error.Message)
			}
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
	// A JSON field takes no filter (D17): the engines disagree on what
	// equal JSON is, so the tool refuses it as the route does.
	refuse(`{"payload": [1, 2]}`)
	refuse(`{"payload": {"x": 1}}`)
	refuse(`{"payload": 3}`)
	refuse(`{"name_in": []}`)
	refuse(`{"name_in": ["Doe, Jane"]}`)
	refuse(`{"name": ["Doe", "Jane"]}`)
	refuse(`{"name": {"x": 1}}`)

	// The get tool's record key, sent empty, is an argument refusal too.
	p, _ := json.Marshal(map[string]any{"name": "mcp_docs_get", "arguments": map[string]any{"id": ""}})
	resp := srv.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
	if resp.Error == nil || resp.Error.Code != mcp.ErrInvalidParams || !strings.Contains(resp.Error.Message, "must not be empty") {
		t.Errorf("get with an empty id answered %+v; want invalid params naming the empty id", resp.Error)
	}
	if n := errorLogs.Load(); n != 0 {
		t.Errorf("argument refusals logged %d records at Error", n)
	}
}

// countErrorLogs counts records logged at Error or above on the default
// logger until the test ends.
func countErrorLogs(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	prev := slog.Default()
	slog.SetDefault(slog.New(errorCounter{n: &n}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &n
}

type errorCounter struct{ n *atomic.Int32 }

func (errorCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c errorCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		c.n.Add(1)
	}
	return nil
}
func (c errorCounter) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c errorCounter) WithGroup(string) slog.Handler      { return c }

// The tools/call validator refuses a list or object for a string filter
// and a scalar for an _in key before the tool runs, so these shapes
// reach toolFilterValues only from a caller that skips it. The tool
// still refuses them rather than sending a cut or garbled value.
func TestToolFilterValuesRefusesOffSchemaShapes(t *testing.T) {
	for _, tc := range []struct {
		key string
		in  bool
		v   any
	}{
		{"name", false, []any{"Doe", "Jane"}},
		{"name", false, map[string]any{"x": 1.0}},
		{"name_in", true, []any{map[string]any{"x": 1.0}}},
		{"name_in", true, map[string]any{"x": 1.0, "y": 2.0}},
	} {
		if vs, err := toolFilterValues(tc.key, tc.in, tc.v); err == nil {
			t.Errorf("%s = %#v was sent as %q, want a refusal", tc.key, tc.v, vs)
		}
	}
	// A scalar _in on a plain field keeps the route's comma form.
	if vs, err := toolFilterValues("name_in", true, "Doe,Jane"); err != nil || len(vs) != 1 || vs[0] != "Doe,Jane" {
		t.Errorf("name_in = \"Doe,Jane\" became %q, %v", vs, err)
	}
}
