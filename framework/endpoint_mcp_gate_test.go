package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity Endpoint declares one operation with two front doors: an HTTP
// handler that inherits the route's middleware chain, and an MCPHandler
// registered straight onto the MCP server. The MCP twin saw no middleware, so
// an endpoint behind auth.RequireRole("editor") was role-checked over HTTP and
// wide open over MCP.
//
// The twin now defaults to requiring an authenticated caller. An endpoint that
// really is public says so with MCPPublic.
func TestEndpointMCPTwinRequiresUserByDefault(t *testing.T) {
	app := newEndpointMCPApp(t, entity.Endpoint{
		Method: "POST", Path: "{id}/publish", Name: "posts_publish", MCP: true,
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
		MCPHandler: func(context.Context, map[string]any) (any, error) { return "published", nil },
	})

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"posts_publish","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] endpoint MCP twin ran for an unauthenticated caller")
	}
	if names := toolNames(t, app, context.Background()); hasTool(names, "posts_publish") {
		t.Errorf("SECURITY: [disclosure] gated endpoint twin still listed to an anonymous caller: %v", names)
	}
}

// The opt-out has to actually work, or hosts will reach for something worse.
func TestEndpointMCPPublicOptsOut(t *testing.T) {
	app := newEndpointMCPApp(t, entity.Endpoint{
		Method: "GET", Path: "{id}/preview", Name: "posts_preview", MCP: true, MCPPublic: true,
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
		MCPHandler: func(context.Context, map[string]any) (any, error) { return "preview", nil },
	})

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"posts_preview","arguments":{}}`),
	})
	if resp.Error != nil {
		t.Fatalf("MCPPublic endpoint refused an anonymous caller: %v", resp.Error)
	}
	if names := toolNames(t, app, context.Background()); !hasTool(names, "posts_preview") {
		t.Errorf("MCPPublic endpoint missing from the listing: %v", names)
	}
}

// A host that wants a stricter predicate than "any authenticated caller"
// supplies one, and it must win over the default.
func TestEndpointMCPGateOverridesDefault(t *testing.T) {
	var called bool
	app := newEndpointMCPApp(t, entity.Endpoint{
		Method: "POST", Path: "{id}/archive", Name: "posts_archive", MCP: true,
		MCPGate:    func(context.Context) error { called = true; return errAuthRequired },
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
		MCPHandler: func(context.Context, map[string]any) (any, error) { return "archived", nil },
	})

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"posts_archive","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Fatal("custom MCPGate did not refuse")
	}
	if !called {
		t.Error("custom MCPGate never ran; the default gate shadowed it")
	}
}

func newEndpointMCPApp(t *testing.T, ep entity.Endpoint) *App {
	t.Helper()
	app := NewApp()
	app.Entity("posts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{ep},
	})
	return app
}

func hasTool(hay []string, needle string) bool {
	return slices.Contains(hay, needle)
}
