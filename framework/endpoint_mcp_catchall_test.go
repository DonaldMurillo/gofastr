package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// R8-HD-03 (#669): an endpoint's MCP twin must accept what its HTTP route
// accepts. A catch-all route ({path...}) spans segments, so its twin's
// path parameter takes a slash-bearing value — while the route's
// sanitiser still refuses dot-dot segments, empty segments and control
// bytes, and a single-segment {id} route's twin stays strict.
func TestEndpointMCPTwinFollowsCatchAllRoute(t *testing.T) {
	type fileInput struct {
		Path string `path:"path"`
	}
	var gotPath string
	app := newEndpointMCPApp(t, entity.Endpoint{
		Method: "GET", Path: "files/{path...}", Name: "posts_files", MCP: true, Public: true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		MCPHandler: handler.ToolAdapter(func(ctx context.Context, in fileInput) (string, error) {
			gotPath = in.Path
			return in.Path, nil
		}),
	})
	call := func(args string) mcp.Response {
		t.Helper()
		return app.MCP.HandleRequest(context.Background(), mcp.Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call",
			Params: json.RawMessage(`{"name":"posts_files","arguments":` + args + `}`),
		})
	}

	if resp := call(`{"path":"docs/report.pdf"}`); resp.Error != nil {
		t.Fatalf("catch-all twin refused a value its route accepts: %v", resp.Error)
	}
	if gotPath != "docs/report.pdf" {
		t.Fatalf("catch-all twin bound %q, want %q", gotPath, "docs/report.pdf")
	}
	for name, args := range map[string]string{
		"dot-dot segment": `{"path":"docs/../secret"}`,
		"empty segment":   `{"path":"docs//report.pdf"}`,
		"leading slash":   `{"path":"/etc/passwd"}`,
		"NUL byte":        `{"path":"docs\u0000.pdf"}`,
	} {
		if resp := call(args); resp.Error == nil {
			t.Errorf("SECURITY: [mcp-catchall] catch-all twin accepted %s: the route's sanitiser refuses it", name)
		}
	}
}

// A single-segment wildcard stays strict on its twin too, and a tool
// registered by hand (no route) keeps every path parameter strict.
func TestEndpointMCPTwinSingleSegmentStaysStrict(t *testing.T) {
	type idInput struct {
		ID string `path:"id"`
	}
	app := newEndpointMCPApp(t, entity.Endpoint{
		Method: "GET", Path: "report/{id}", Name: "posts_report", MCP: true, Public: true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		MCPHandler: handler.ToolAdapter(func(ctx context.Context, in idInput) (string, error) {
			return in.ID, nil
		}),
	})
	app.MCP.RegisterTool("hand_made", "no route", nil, handler.ToolAdapter(func(ctx context.Context, in idInput) (string, error) {
		return in.ID, nil
	}))

	for name, tool := range map[string]string{"route twin": "posts_report", "hand-registered tool": "hand_made"} {
		resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call",
			Params: json.RawMessage(`{"name":"` + tool + `","arguments":{"id":"a/b"}}`),
		})
		if resp.Error == nil {
			t.Errorf("SECURITY: [mcp-catchall] %s accepted a slash in a single-segment path parameter", name)
		}
	}
}

// The grouped entity's endpoints register their twins at the second
// site (registerGroupEndpoints), under the group's prefix: the twin
// follows the catch-all there too, with the same refusals.
func TestGroupEndpointMCPTwinFollowsCatchAll(t *testing.T) {
	type fileInput struct {
		Path string `path:"path"`
	}
	var gotPath string
	app := NewApp()
	g := app.Group("/api")
	app.GroupEntity(g, "posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{{
			Method: "GET", Path: "files/{path...}", Name: "posts_files", MCP: true, Public: true,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
			MCPHandler: handler.ToolAdapter(func(ctx context.Context, in fileInput) (string, error) {
				gotPath = in.Path
				return in.Path, nil
			}),
		}},
	})
	call := func(args string) mcp.Response {
		t.Helper()
		return app.MCP.HandleRequest(context.Background(), mcp.Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call",
			Params: json.RawMessage(`{"name":"posts_files","arguments":` + args + `}`),
		})
	}
	if resp := call(`{"path":"docs/report.pdf"}`); resp.Error != nil {
		t.Fatalf("grouped catch-all twin refused a value its route accepts: %v", resp.Error)
	}
	if gotPath != "docs/report.pdf" {
		t.Fatalf("grouped catch-all twin bound %q, want %q", gotPath, "docs/report.pdf")
	}
	if resp := call(`{"path":"docs/../secret"}`); resp.Error == nil {
		t.Error("SECURITY: [mcp-catchall] grouped catch-all twin accepted a dot-dot segment")
	}
}
