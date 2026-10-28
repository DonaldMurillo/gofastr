package log

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
)

type roleUser struct{ roles []string }

func (u roleUser) GetRoles() []string { return u.roles }

func callLogTool(app *framework.App, ctx context.Context, name, args string) *mcp.RPCError {
	resp := app.MCP.HandleRequest(ctx, mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"` + name + `","arguments":` + args + `}`),
	})
	return resp.Error
}

// The log tools hand out every caller's request paths, remote IPs and
// request ids, and log_set_level changes what the app records. Any
// signed-in account passed their identity-only gate, and on an app with
// open sign-up that is anyone. They now need the admin role by default
// (2026-10-27 P22).
func TestLogToolsRequireAdmin(t *testing.T) {
	app, p := newLogMCPApp(t)
	before := p.level.Level()
	member := handler.SetUser(context.Background(), roleUser{roles: []string{"member"}})
	admin := handler.SetUser(context.Background(), roleUser{roles: []string{"admin"}})

	for _, tool := range []string{"log_recent", "log_filter", "log_metrics"} {
		if err := callLogTool(app, member, tool, `{}`); err == nil {
			t.Errorf("SECURITY: %s ran for a signed-in member", tool)
		}
		if err := callLogTool(app, admin, tool, `{}`); err != nil {
			t.Errorf("%s refused an admin: %v", tool, err)
		}
	}
	if err := callLogTool(app, member, "log_set_level", `{"level":"DEBUG"}`); err == nil {
		t.Error("SECURITY: log_set_level ran for a signed-in member")
	}
	if got := p.level.Level(); got != before {
		t.Fatalf("SECURITY: a member changed the level to %v", got)
	}
	if err := callLogTool(app, admin, "log_set_level", `{"level":"DEBUG"}`); err != nil {
		t.Errorf("log_set_level refused an admin: %v", err)
	}
}

// Config.MCPGate replaces the admin default for an app whose operators
// hold another role.
func TestLogMCPGateOverridesRole(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv("GOFASTR_DEV_MCP", "0")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{
		Sinks: []Sink{discardSink{}}, EnableMCP: true, AllowMCPMutation: true, Level: slog.LevelInfo,
		MCPGate: framework.MCPRequireRole("ops"),
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	admin := handler.SetUser(context.Background(), roleUser{roles: []string{"admin"}})
	ops := handler.SetUser(context.Background(), roleUser{roles: []string{"ops"}})
	for _, tool := range []string{"log_recent", "log_set_level"} {
		args := `{}`
		if tool == "log_set_level" {
			args = `{"level":"WARN"}`
		}
		if err := callLogTool(app, admin, tool, args); err == nil {
			t.Errorf("%s: the replaced admin default still admitted an admin", tool)
		}
		if err := callLogTool(app, ops, tool, args); err != nil {
			t.Errorf("%s refused ops under MCPGate: %v", tool, err)
		}
	}
}
