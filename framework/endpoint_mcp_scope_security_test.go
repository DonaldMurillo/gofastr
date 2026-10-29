package framework

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An endpoint's REST route sits under auth.RequireAPIScopes, which holds a
// token to "<table>:<verb>". The MCP twin is registered straight on the
// server, so a customers:read token called a posts endpoint twin its REST
// route refuses (2026-10-27 P35). The twin now asks for the same scope.
func TestEndpointMCPTwinRequiresTokenScope(t *testing.T) {
	ran := 0
	eps := []entity.Endpoint{
		{Method: "POST", Path: "{id}/publish", Name: "posts_publish", MCP: true,
			MCPHandler: func(context.Context, map[string]any) (any, error) { ran++; return "ok", nil }},
		{Method: "get", Path: "{id}/preview", Name: "posts_preview", MCP: true,
			MCPHandler: func(context.Context, map[string]any) (any, error) { ran++; return "ok", nil }},
		{Method: "POST", Path: "{id}/ping", Name: "posts_ping", MCP: true, Public: true,
			MCPHandler: func(context.Context, map[string]any) (any, error) { ran++; return "ok", nil }},
		{Method: "POST", Path: "{id}/archive", Name: "posts_archive", MCP: true,
			MCPGate:    func(context.Context) error { return nil },
			MCPHandler: func(context.Context, map[string]any) (any, error) { ran++; return "ok", nil }},
	}
	flat := NewApp()
	flat.Entity("posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}, Endpoints: eps})
	grouped := NewApp()
	grouped.GroupEntity(grouped.Group("/api/v1"), "posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}, Endpoints: eps})

	user := handler.SetUser(context.Background(), &endpointScopeUser{id: "u1"})
	wrong := access.WithHeldScopes(user, []string{"customers:*"})
	readOnly := access.WithHeldScopes(user, []string{"posts:read"})
	for name, a := range map[string]*App{"entity": flat, "group": grouped} {
		for _, tool := range []string{"posts_publish", "posts_preview", "posts_ping", "posts_archive"} {
			if callEndpointTool(a, wrong, tool) == nil {
				t.Errorf("SECURITY: %s %s ran for a customers:* token", name, tool)
			}
			if hasTool(toolNames(t, a, wrong), tool) {
				t.Errorf("SECURITY: %s %s listed to a customers:* token", name, tool)
			}
		}
		if callEndpointTool(a, readOnly, "posts_publish") == nil {
			t.Errorf("SECURITY: %s POST twin ran for a posts:read token", name)
		}
		if err := callEndpointTool(a, readOnly, "posts_preview"); err != nil {
			t.Errorf("%s GET twin refused a posts:read token: %v", name, err)
		}
		if err := callEndpointTool(a, access.WithHeldScopes(user, []string{"posts:write"}), "posts_publish"); err != nil {
			t.Errorf("%s POST twin refused a posts:write token: %v", name, err)
		}
		if err := callEndpointTool(a, user, "posts_publish"); err != nil {
			t.Errorf("%s twin refused an unscoped session caller: %v", name, err)
		}
		if err := callEndpointTool(a, context.Background(), "posts_ping"); err != nil {
			t.Errorf("%s Public twin refused an anonymous caller: %v", name, err)
		}
	}
	if ran != 8 {
		t.Errorf("handlers ran %d times, want 8 (the allowed calls only)", ran)
	}
}

func callEndpointTool(a *App, ctx context.Context, tool string) *mcp.RPCError {
	return a.MCP.HandleRequest(ctx, mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"` + tool + `","arguments":{}}`),
	}).Error
}

type endpointScopeUser struct{ id string }

func (u *endpointScopeUser) GetID() string { return u.id }
