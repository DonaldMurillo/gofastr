package mcp_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	mcpcore "github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/agent/mcp"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

func setup(t *testing.T) *protocol.Tools {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-mcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l)
}

func TestRegisterAllTools(t *testing.T) {
	tools := setup(t)
	srv := mcpcore.NewServer()
	if err := mcp.Register(srv, tools); err != nil {
		t.Fatalf("Register: %v", err)
	}
	listed := srv.ListTools()
	if len(listed) < 10 {
		t.Errorf("expected many tools, got %d", len(listed))
	}
	want := map[string]bool{
		"world_get": false, "add_entity": false, "delete_entity": false, "undo": false,
	}
	for _, tt := range listed {
		if _, ok := want[tt.Name]; ok {
			want[tt.Name] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("missing tool %q", n)
		}
	}
}

func TestMCPDispatchAddEntity(t *testing.T) {
	tools := setup(t)
	srv, err := mcp.NewServer(tools)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Build a tools/call JSON-RPC request.
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "add_entity",
			"arguments": map[string]any{
				"entity": map[string]any{
					"name":   "posts",
					"fields": []any{map[string]any{"name": "title", "type": "string"}},
				},
			},
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(buf)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := tools.Live().Session().World.Entities["posts"]; !ok {
		t.Errorf("posts not added via MCP")
	}
}

func TestMCPDispatchPreservesErrorKind(t *testing.T) {
	tools := setup(t)
	srv, err := mcp.NewServer(tools)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "add_field",
			"arguments": map[string]any{"entity": "missing", "field": map[string]any{"name": "x", "type": "string"}},
		},
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(buf)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	out := rec.Body.String()
	if !strings.Contains(out, "not_found") {
		t.Errorf("error kind not preserved through MCP: %s", out)
	}
	if !strings.Contains(out, "missing") {
		// Sanity that the inner Result text is present.
		t.Errorf("error message lost: %s", out)
	}
}

// Sanity that the dispatch layer is shared with the native loop.
func TestSameDispatchAsNative(t *testing.T) {
	tools := setup(t)
	srv, _ := mcp.NewServer(tools)
	listed := srv.ListTools()
	for _, descr := range tools.List() {
		var found bool
		for _, l := range listed {
			if l.Name == descr.Name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MCP missing %q from protocol descriptor list", descr.Name)
		}
	}
	_ = context.Background
}
