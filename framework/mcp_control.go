package framework

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/access"
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
//     Generated apps wire it gated on the dev env for this
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
//
// Both tools need a caller holding the "admin" role (MCPRequireRole), the
// default /.debug uses too; WithMCPControlGate replaces it. The dev loop's
// implied tools stay ungated.
func WithMCPControl() AppOption {
	return func(a *App) {
		a.mcpControl = true
	}
}

// WithMCPControlGate replaces the precondition the control tools run
// behind, MCPRequireRole("admin") by default, for an app whose operators
// hold another role:
//
//	framework.WithMCPControlGate(framework.MCPRequireRole("ops"))
//
// The gate replaces the default whole, so it should refuse a caller with
// no identity itself, as MCPRequireRole and auth.MCPRole do.
func WithMCPControlGate(gate func(ctx context.Context) error) AppOption {
	if gate == nil {
		panic("framework.WithMCPControlGate: nil gate: a nil precondition would silently allow every caller")
	}
	return func(a *App) {
		a.mcpControlGate = gate
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
	// NewApp installs it after every option has run, so WithMCPServer
	// may come before or after this option.
	return func(a *App) {
		a.mcpGate = gate
	}
}

// controlToolSpec is one mutating control tool. The set is built in a
// single place so guardDevMCPBind can withdraw exactly what
// registerControlTools installed: two hand-maintained lists would drift,
// and the failure mode of that drift is an exposed dev bind still serving
// a tool nobody remembered to remove.
type controlToolSpec struct {
	name        string
	description string
	schema      map[string]any
	handler     func(ctx context.Context, params map[string]any) (any, error)
}

func (a *App) controlToolSpecs() []controlToolSpec {
	return []controlToolSpec{
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
}

// controlToolNames returns the names registerControlTools installs, for
// the bind guard to withdraw.
func (a *App) controlToolNames() []string {
	specs := a.controlToolSpecs()
	out := make([]string, len(specs))
	for i, t := range specs {
		out[i] = t.name
	}
	return out
}

func (a *App) registerControlTools() error {
	tools := a.controlToolSpecs()
	gate := controlToolGate(a.mcpControlDevImplied, a.mcpControlGate)
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
// It asks for an identity, not a role; MCPRequireRole adds one. A Decider
// that answers DecisionDeny on access.RoleGateCapability refuses the call.
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
	// The Decider binds every gate (access.Decider), so an operator's deny
	// closes the MCP tools as it closes battery/auth's role gates.
	if deciderDenies(ctx) {
		return errDeciderDenied
	}
	return nil
}

// MCPRequireRole is MCPRequireUser plus a role: the caller must hold any of
// roles, read from the user's GetRoles() []string or from the roles access
// middleware put on the context. A scope-restricted caller (an API token)
// must also hold access.RoleScope of the role that admits it ("role:admin",
// "role:*" or "*:*"): the token carries its owner's roles, and one minted
// for another surface must not drive these tools. It is the default gate of
// the control tools and battery/log's MCP tools.
//
//	app.MCP.RegisterTool("cache_flush", desc, schema, flush,
//	    mcp.WithToolGate(framework.MCPRequireRole("admin")))
func MCPRequireRole(roles ...string) func(ctx context.Context) error {
	if len(roles) == 0 {
		panic("framework.MCPRequireRole: no roles given: use MCPRequireUser() to require just authentication")
	}
	roles = slices.Clone(roles)
	return func(ctx context.Context) error {
		if err := requireMCPUser(ctx); err != nil {
			return err
		}
		held := callerRoles(ctx)
		for _, r := range roles {
			if slices.Contains(held, r) {
				return nil
			}
		}
		return errRoleRequired
	}
}

// callerRoles is every role the caller may act on at a role gate: the ones
// access middleware resolved onto the context and the user's own
// GetRoles(), narrowed for a scope-restricted caller (an API token, an embed
// grant) to the roles a held scope grants (access.RoleScope). A token
// carries its owner's roles, and one an admin minted for "customers:read"
// must not pass MCPRequireRole("admin") or /.debug on them.
func callerRoles(ctx context.Context) []string {
	held := heldRoles(ctx)
	if _, scoped := access.HeldScopes(ctx); !scoped {
		return held
	}
	return slices.DeleteFunc(held, func(r string) bool {
		return !access.ScopeAllows(ctx, access.RoleScope(r))
	})
}

// heldRoles is every role the caller holds, before any scope narrowing.
// The Decider sees this set: scopes narrow what a credential may grant on,
// and dropping a role the scopes leave out would lift a deny keyed on it.
func heldRoles(ctx context.Context) []string {
	roles := slices.Clone(access.GetRoles(ctx))
	u, _ := handler.GetUser(ctx)
	if rh, ok := u.(interface{ GetRoles() []string }); ok {
		for _, r := range rh.GetRoles() {
			if !slices.Contains(roles, r) {
				roles = append(roles, r)
			}
		}
	}
	return roles
}

// deciderDenies consults the Decider installed in ctx the way a role gate
// does: the caller's roles, access.RoleGateCapability and the zero Ref.
func deciderDenies(ctx context.Context) bool {
	d := access.GetDecider(ctx)
	return d != nil && d(ctx, heldRoles(ctx), access.RoleGateCapability, access.Ref{}) == access.DecisionDeny
}

var (
	errRoleRequired  = errors.New("this tool requires a role the caller does not hold")
	errDeciderDenied = errors.New("this tool call was refused by the access decider for the caller's roles")
)

// errScopeNotHeld refuses a scope-restricted caller (an API token) whose
// scopes do not name the resource and verb a tool acts on.
var errScopeNotHeld = errors.New("this tool needs a scope the caller's credential does not hold")

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
// An identity was not enough: on an app with open sign-up any account
// could turn a module off for everyone. The gate is override
// (WithMCPControlGate) when set, else MCPRequireRole("admin").
//
// devImplied tools are NOT gated: `gofastr dev` turns them on with no
// auth configured at all, so a gate would only lock the dev loop out of
// its own app. Dev exposure is bounded on the other axis instead, the
// listener must be loopback (guardDevMCPBind).
func controlToolGate(devImplied bool, override func(ctx context.Context) error) func(ctx context.Context) error {
	if devImplied {
		return nil
	}
	if override != nil {
		return override
	}
	return MCPRequireRole("admin")
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

func (a *App) toolModuleEnable(ctx context.Context, params map[string]any) (any, error) {
	return a.toggleModule(ctx, params, true)
}

func (a *App) toolModuleDisable(ctx context.Context, params map[string]any) (any, error) {
	return a.toggleModule(ctx, params, false)
}

func (a *App) toggleModule(ctx context.Context, params map[string]any, enable bool) (any, error) {
	name, _ := params["name"].(string)
	if name == "" {
		// Caller-facing domain errors are returned as *RPCError so the
		// tools/call dispatch surfaces them verbatim; a bare error()
		// there is treated as internal detail and genericised to
		// "internal tool error" (TestPlainHandlerErrorNotEchoed).
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: "mcp control: `name` is required: call app_modules to list module names"}
	}
	var err error
	if enable {
		err = a.Modules().Enable(ctx, name)
	} else {
		err = a.Modules().Disable(ctx, name)
	}
	if err != nil {
		// A module toggle failure names the unknown module — a domain
		// error the caller must see, not an internal leak.
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: err.Error()}
	}
	return map[string]any{
		"name":    name,
		"enabled": enable,
	}, nil
}
