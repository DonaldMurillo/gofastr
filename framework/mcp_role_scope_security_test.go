package framework

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// An API token carries its owner's roles, so an admin's token minted for a
// reporting job ("customers:read") passed every framework role gate: the
// control tools, battery/log's tools (both MCPRequireRole) and /.debug. A
// scope-restricted caller now acts on a role only when a held scope grants
// access.RoleScope(role) (2026-10-31 O11).
func TestRoleGatesNarrowByScope(t *testing.T) {
	admin := handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin"}})
	viaAccess := access.WithRoles(handler.SetUser(context.Background(), "u-1"), []string{"admin"})
	gate := MCPRequireRole("admin")

	cases := []struct {
		name   string
		ctx    context.Context
		passes bool
	}{
		{"session admin", admin, true},
		{"token for customers:read", access.WithHeldScopes(admin, []string{"customers:read"}), false},
		{"access-role token for customers:read", access.WithHeldScopes(viaAccess, []string{"customers:read"}), false},
		{"empty-scoped token", access.WithHeldScopes(admin, nil), false},
		{"token for another role", access.WithHeldScopes(admin, []string{"role:ops"}), false},
		{"token for role:admin", access.WithHeldScopes(admin, []string{"role:admin"}), true},
		{"token for role:*", access.WithHeldScopes(admin, []string{"role:*"}), true},
		{"token for *:*", access.WithHeldScopes(admin, []string{"*:*"}), true},
	}
	app := NewApp()
	app.registerDebugEndpoints()
	for _, tc := range cases {
		if err := gate(tc.ctx); (err == nil) != tc.passes {
			t.Errorf("SECURITY: %s: MCPRequireRole(admin) err = %v, want pass=%v", tc.name, err, tc.passes)
		}
		want := http.StatusForbidden
		if tc.passes {
			want = http.StatusOK
		}
		if got := debugStatus(app, "/.debug/stats", tc.ctx); got != want {
			t.Errorf("SECURITY: %s: /.debug/stats = %d, want %d", tc.name, got, want)
		}
	}

	// The scope must name the role that admits: a token scoped to ops does
	// not pass a gate on ops for a caller whose admitting role is admin.
	both := handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin", "ops"}})
	if err := MCPRequireRole("admin")(access.WithHeldScopes(both, []string{"role:ops"})); err == nil {
		t.Error("SECURITY: a role:ops token passed MCPRequireRole(admin)")
	}
	if err := MCPRequireRole("admin", "ops")(access.WithHeldScopes(both, []string{"role:ops"})); err != nil {
		t.Errorf("a role:ops token on MCPRequireRole(admin, ops): %v", err)
	}
}

// The control tools run behind MCPRequireRole("admin"): a narrow token from
// an admin neither calls nor sees app_module_disable.
func TestControlToolsRefuseNarrowToken(t *testing.T) {
	app := controlApp(t)
	token := access.WithHeldScopes(handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin"}}), []string{"customers:read"})
	if _, err := app.MCP.CallTool(token, "app_module_disable", map[string]any{"name": "reports"}); err == nil {
		t.Error("SECURITY: a customers:read token disabled a module")
	}
	if !app.Modules().Enabled("reports") {
		t.Fatal("SECURITY: the narrow token's call disabled the module")
	}
	if toolListedFor(app, token, "app_module_disable") {
		t.Error("app_module_disable listed to a customers:read token")
	}
}

// The scope narrows what a token may GRANT on, never what a Decider sees:
// the decider still gets every role the caller holds, so a deny keyed on a
// role the token's scopes leave out keeps binding.
func TestRoleGateDeciderSeesAllRoles(t *testing.T) {
	var seen []string
	user := handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin", "suspended"}})
	ctx := access.WithHeldScopes(access.WithDecider(user,
		func(_ context.Context, roles []string, _ access.Permission, _ access.Ref) access.Decision {
			seen = roles
			if slices.Contains(roles, "suspended") {
				return access.DecisionDeny
			}
			return access.DecisionAbstain
		}), []string{"role:admin"})
	if err := MCPRequireRole("admin")(ctx); err == nil {
		t.Error("SECURITY: a token without role:suspended slipped the decider's deny on suspended")
	}
	if !slices.Contains(seen, "suspended") {
		t.Errorf("decider saw roles %v, want suspended among them", seen)
	}
}
