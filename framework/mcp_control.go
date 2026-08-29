package framework

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// WithMCPControl installs the MUTATING MCP tools that let a connected
// agent control the running App. Separate from WithMCPIntrospection,
// which stays strictly read-only, so each surface opts into exactly
// the trust level its /mcp endpoint warrants:
//
//   - Introspection (read-only) is safe wherever disclosure is: docs
//     sites, staging, local dev.
//   - Control (mutating) belongs on /mcp endpoints reachable only by
//     trusted callers: the local dev loop, an authenticated tunnel.
//     Blueprint-generated apps wire it gated on the dev env for this
//     reason.
//
// Tools registered:
//
//   - app_module_enable:  enable a registered module at runtime. The
//     state persists through the module store and respects dependency
//     ordering (enabling a module whose dependency is disabled fails).
//   - app_module_disable: disable a registered module at runtime. Fails
//     closed if enabled modules depend on it: no cascades.
//
// Code-level changes (screens, entities, handlers) are NOT an MCP
// concern: that is the `gofastr dev` edit-rebuild-reload loop. MCP
// control mutates runtime STATE the app already models.
func WithMCPControl() AppOption {
	return func(a *App) {
		a.mcpControl = true
	}
}

// WithMCPGate installs a server-wide precondition over the /mcp data surface:
// tools/list, tools/call, resources/list, resources/read,
// resources/templates/list, prompts/list and prompts/get all run it first.
// Use it when the endpoint is private wholesale.
//
// Without it, /mcp discloses every registered tool's inputSchema to anyone who
// can reach the route, and for entity CRUD tools those schemas are built from
// live entity definitions, naming every entity and every non-Hidden field.
// Individual tools can be gated instead with mcp.WithToolGate, which filters
// the listing per caller rather than closing it for everyone.
//
// The `initialize` handshake and `ping` stay open by design: they carry only
// the protocol version, capability booleans and the server name, and a client
// that cannot handshake cannot present credentials.
//
//	app := framework.NewApp(
//	    framework.WithMCP(),
//	    framework.WithMCPGate(framework.MCPRequireUser()),
//	)
func WithMCPGate(gate func(ctx context.Context) error) AppOption {
	if gate == nil {
		panic("framework.WithMCPGate: nil gate: a nil precondition would silently allow every caller")
	}
	return func(a *App) {
		a.MCP.SetGate(gate)
	}
}

func (a *App) registerControlTools() error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name:        "app_module_enable",
			description: "Enable a registered module on the running app. Persists through the module store and re-serves the module's routes/tools. Fails if a declared dependency is disabled. Use app_modules to list modules and their current state.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Module name as reported by app_modules."},
				},
				"required": []string{"name"},
			},
			handler: a.toolModuleEnable,
		},
		{
			name:        "app_module_disable",
			description: "Disable a registered module on the running app. Persists through the module store; the module's routes/tools start refusing. Fails closed when enabled modules depend on it: disable dependents first. Use app_modules to list modules and their current state.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Module name as reported by app_modules."},
				},
				"required": []string{"name"},
			},
			handler: a.toolModuleDisable,
		},
	}
	gate := controlToolGate(a.mcpControlDevImplied)
	for _, t := range tools {
		var opts []mcp.ToolOption
		if gate != nil {
			// WithToolGate, not mcp.Gated: the wrapper only ever reached
			// tools/call, so an anonymous tools/list still returned these
			// tools' schemas, telling a stranger the app can be
			// reconfigured over MCP, and exactly how.
			opts = append(opts, mcp.WithToolGate(gate))
		}
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler, opts...); err != nil {
			return fmt.Errorf("framework: register MCP control tool %q: %w", t.name, err)
		}
	}
	return nil
}

// MCPRequireUser is the precondition the framework ships for MCP tools that
// register DIRECTLY on the server and so never see the router's middleware
// chain: entity.Endpoint.MCPHandler twins, app.MCP.RegisterTool calls, and
// battery-registered mutating tools.
//
// It asks only for an identity, not a role, because the framework layer
// cannot know the host's role vocabulary (and cannot import battery/auth).
// A host wanting more passes auth.MCPRole("admin") instead.
//
// Pair it with [mcp.WithToolGate] rather than [mcp.Gated] so the tool also
// disappears from tools/list for callers who cannot invoke it, the
// inputSchema is the disclosure, not the call.
func MCPRequireUser() func(ctx context.Context) error {
	return requireMCPUser
}

// errAuthRequired is the refusal MCPRequireUser returns. It names the fix
// (send credentials, check the middleware runs on /mcp) without disclosing
// anything about the tool or the app's state.
var errAuthRequired = errors.New("this tool requires an authenticated caller: " +
	"send the session cookie or Authorization header on the /mcp request, " +
	"and make sure the app's session middleware runs on the /mcp route")

func requireMCPUser(ctx context.Context) error {
	if u, ok := handler.GetUser(ctx); !ok || u == nil {
		return errAuthRequired
	}
	// An embed grant resolves to the same context user a session does, so
	// without this every entity endpoint's MCP twin, and the control tools
	// that enable and disable modules, is reachable with a credential that
	// lives in a third party's page and can be read by anyone with devtools.
	// MCP tools act on the caller's behalf; no surface declares that.
	//
	// This is the gate that is actually wired (Endpoint.MCPGate defaults to it,
	// as do controlToolGate and battery/log's setLevelGate). battery/auth's
	// MCPUser/MCPRole carry the same refusal for callers that opt into them.
	if _, embedded := fembed.GrantFromContext(ctx); embedded {
		return errEmbedNotAllowed
	}
	return nil
}

// errEmbedNotAllowed is deliberately distinct from errAuthRequired: the caller
// IS authenticated, just not with a credential that may drive tools.
var errEmbedNotAllowed = errors.New("this tool is not reachable from an embedded surface: " +
	"an embed grant is delegated, scoped, and lives in a page the app does not control")

// controlToolGate returns the precondition the MUTATING control tools
// run behind, or nil for no gate.
//
// mcp.Gated and battery/auth's MCPUser/MCPRole existed but had zero
// production call sites: every shipped tool was ungated. For the
// read-only introspection tools that is a documented posture; for
// app_module_enable / app_module_disable, which change what the running
// app serves, it made a reachable /mcp a control plane for whoever
// found it.
//
// The gate asks only for an identity, not a role, because the framework
// layer cannot know the host's role vocabulary (and cannot import
// battery/auth). A host wanting more wraps its own handlers with
// mcp.Gated(auth.MCPRole("admin"), …).
//
// devImplied tools are NOT gated: `gofastr dev` turns them on with no
// auth configured at all, so a gate would only lock the dev loop out of
// its own app. Dev exposure is bounded on the other axis instead, the
// listener must be loopback (guardDevMCPBind).
func controlToolGate(devImplied bool) func(ctx context.Context) error {
	if devImplied {
		return nil
	}
	return requireMCPUser
}

// routerHasMCPRoute reports whether the host already mounted a POST
// /mcp route by hand, the dev-implied auto-mount yields to it.
func (a *App) routerHasMCPRoute() bool {
	for _, r := range a.router.Routes() {
		if r.Pattern == "/mcp" && r.Method == http.MethodPost {
			return true
		}
	}
	return false
}

// routerHasWidgetClientRoute reports whether the host already serves the
// MCP Apps widget client script, so the WithMCPApp auto-mount can yield
// to it instead of panicking with a route conflict.
func (a *App) routerHasWidgetClientRoute() bool {
	for _, r := range a.router.Routes() {
		if r.Pattern == mcp.WidgetClientScriptURL && r.Method == http.MethodGet {
			return true
		}
	}
	return false
}

func (a *App) toolModuleEnable(ctx context.Context, params map[string]any) (any, error) {
	return a.toggleModule(ctx, params, true)
}

func (a *App) toolModuleDisable(ctx context.Context, params map[string]any) (any, error) {
	return a.toggleModule(ctx, params, false)
}

func (a *App) toggleModule(ctx context.Context, params map[string]any, enable bool) (any, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("mcp control: `name` is required: call app_modules to list module names")
	}
	var err error
	if enable {
		err = a.Modules().Enable(ctx, name)
	} else {
		err = a.Modules().Disable(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name":    name,
		"enabled": enable,
	}, nil
}
