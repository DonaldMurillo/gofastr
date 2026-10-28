package framework

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// denyAll is a Decider that refuses every check, the one an operator
// installs to lock a caller out.
func denyAll(context.Context, []string, access.Permission, access.Ref) access.Decision {
	return access.DecisionDeny
}

func controlApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(append([]AppOption{WithMCPControl()}, opts...)...)
	app.RegisterModule(&modStub{name: "reports", manifest: ModuleManifest{}, init: noopInit})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	return app
}

func toolListedFor(app *App, ctx context.Context, name string) bool {
	resp := app.MCP.HandleRequest(ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	blob, _ := json.Marshal(resp.Result)
	return strings.Contains(string(blob), `"`+name+`"`)
}

// app_module_disable turns a module off for every user, and on an app with
// open sign-up any account could call it: the gate asked for an identity
// alone. It now needs the admin role, the same default /.debug uses, and a
// signed-in member neither calls nor sees it (2026-10-27 P26).
func TestControlToolsRequireAdmin(t *testing.T) {
	app := controlApp(t)
	bg := context.Background()
	member := handler.SetUser(bg, debugRoleUser{roles: []string{"member"}})
	viaAccess := access.WithRoles(handler.SetUser(bg, "u-9"), []string{"admin"})

	for _, tool := range []string{"app_module_disable", "app_module_enable"} {
		if _, err := app.MCP.CallTool(member, tool, map[string]any{"name": "reports"}); err == nil {
			t.Errorf("SECURITY: %s ran for a signed-in member", tool)
		}
	}
	if !app.Modules().Enabled("reports") {
		t.Fatal("SECURITY: a member's call disabled the module")
	}
	if toolListedFor(app, member, "app_module_disable") {
		t.Error("app_module_disable listed to a member")
	}

	if _, err := app.MCP.CallTool(viaAccess, "app_module_disable", map[string]any{"name": "reports"}); err != nil {
		t.Fatalf("admin via access roles: %v", err)
	}
	if app.Modules().Enabled("reports") {
		t.Fatal("admin's disable did not land")
	}
	if !toolListedFor(app, viaAccess, "app_module_disable") {
		t.Error("app_module_disable hidden from an admin")
	}
}

// WithMCPControlGate replaces the admin default, for an app whose operators
// hold another role.
func TestMCPControlGateOverridesRole(t *testing.T) {
	ops := MCPRequireRole("ops")
	app := controlApp(t, WithMCPControlGate(ops))
	bg := context.Background()
	admin := handler.SetUser(bg, debugRoleUser{roles: []string{"admin"}})
	operator := handler.SetUser(bg, debugRoleUser{roles: []string{"ops"}})

	if _, err := app.MCP.CallTool(admin, "app_module_disable", map[string]any{"name": "reports"}); err == nil {
		t.Error("the replaced admin default still admitted an admin")
	}
	if _, err := app.MCP.CallTool(operator, "app_module_disable", map[string]any{"name": "reports"}); err != nil {
		t.Fatalf("ops caller under WithMCPControlGate(ops): %v", err)
	}
}

// A Decider that answers DecisionDeny binds every gate (access.Decider's
// contract). The framework's MCP gates asked only for an identity and a
// role, so an operator's deny never reached them, and /.debug skipped it
// on both branches (2026-10-27 P22, P27).
func TestMCPGatesHonorDeciderDeny(t *testing.T) {
	admin := handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin"}})
	denied := access.WithDecider(admin, denyAll)

	for name, gate := range map[string]func(context.Context) error{
		"MCPRequireUser": MCPRequireUser(),
		"MCPRequireRole": MCPRequireRole("admin"),
	} {
		if err := gate(admin); err != nil {
			t.Errorf("%s refused an admin with no decider: %v", name, err)
		}
		if err := gate(denied); err == nil {
			t.Errorf("SECURITY: %s passed a caller the decider denied", name)
		}
		abstain := access.WithDecider(admin, func(context.Context, []string, access.Permission, access.Ref) access.Decision {
			return access.DecisionAbstain
		})
		if err := gate(abstain); err != nil {
			t.Errorf("%s refused on an abstaining decider: %v", name, err)
		}
	}

	app := NewApp()
	app.registerDebugEndpoints()
	if got := debugStatus(app, "/.debug/stats", denied); got != http.StatusForbidden {
		t.Errorf("/.debug/stats for a denied admin = %d, want 403", got)
	}
	custom := NewApp(WithDebugAuthorize(func(context.Context) bool { return true }))
	custom.registerDebugEndpoints()
	if got := debugStatus(custom, "/.debug/stats", denied); got != http.StatusForbidden {
		t.Errorf("/.debug/stats under WithDebugAuthorize for a denied caller = %d, want 403", got)
	}
}

// The role gate passes the caller's roles to the decider, so a rule keyed
// on a role works.
func TestMCPRequireRolePassesRoles(t *testing.T) {
	var seen []string
	ctx := access.WithDecider(handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin"}}),
		func(_ context.Context, roles []string, capability access.Permission, ref access.Ref) access.Decision {
			seen = roles
			if capability != access.RoleGateCapability || ref != (access.Ref{}) {
				return access.DecisionDeny
			}
			return access.DecisionAbstain
		})
	if err := MCPRequireRole("admin")(ctx); err != nil {
		t.Fatalf("MCPRequireRole: %v", err)
	}
	if len(seen) != 1 || seen[0] != "admin" {
		t.Errorf("decider saw roles %v, want [admin]", seen)
	}
	if err := MCPRequireRole("admin")(handler.SetUser(context.Background(), debugRoleUser{roles: []string{"member"}})); !errors.Is(err, errRoleRequired) {
		t.Errorf("member refused with %v, want errRoleRequired", err)
	}
}
