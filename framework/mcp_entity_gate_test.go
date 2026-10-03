package framework

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity's MCP tools used to list for every caller: the route refused a
// viewer's notes_delete, but tools/list still handed them the tool and its
// input schema. Each tool now carries its operation's Access permission as
// a listing gate, judged for any caller the MCP request already resolved.
func TestEntityMCPToolsListOnlyWhatTheCallerMayDo(t *testing.T) {
	app := NewApp(WithDB(sqliteDB(t)))
	app.Entity("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		Exposure: &entity.ExposureConfig{
			MCP: true,
			Access: entity.AccessControl{
				Read: "notes:read", Create: "notes:write", Update: "notes:write", Delete: "notes:admin",
			},
		},
	})
	policy := NewRolePolicy()
	policy.Grant("viewer", "notes:read")
	policy.Grant("editor", "notes:read", "notes:write")
	policy.Grant("admin", "notes:read", "notes:write", "notes:admin")
	as := func(role string) context.Context {
		ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: role})
		return WithRoles(WithPolicy(ctx, policy), []string{role})
	}
	all := []string{"notes_create", "notes_delete", "notes_get", "notes_list", "notes_update"}

	cases := []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{"viewer", as("viewer"), []string{"notes_get", "notes_list"}},
		{"editor", as("editor"), []string{"notes_create", "notes_get", "notes_list", "notes_update"}},
		{"admin", as("admin"), all},
		{"no role", as("nobody"), nil},
		// No resolved user: the redispatch may still authenticate the
		// caller, so the listing stays as it was and the route decides.
		{"unresolved", context.Background(), all},
	}
	for _, tc := range cases {
		got := entityToolNames(t, app, tc.ctx, "notes_")
		if !slices.Equal(got, tc.want) {
			t.Errorf("SECURITY: [disclosure] %s tools/list = %v, want %v", tc.name, got, tc.want)
		}
	}

	// The gate also refuses the call, before the router is reached.
	raw, _ := json.Marshal(map[string]any{"name": "notes_create", "arguments": map[string]any{"title": "x"}})
	resp := app.MCP.HandleRequest(as("viewer"), mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: raw})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] a viewer's notes_create call was not refused")
	}
}

func entityToolNames(t *testing.T, app *App, ctx context.Context, prefix string) []string {
	t.Helper()
	var out []string
	for _, n := range toolNames(t, app, ctx) {
		if len(n) > len(prefix) && n[:len(prefix)] == prefix {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
