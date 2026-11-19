package framework

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// WithMCPControl installs the mutating MCP tools app_module_enable and
// app_module_disable. They require the "admin" role unless
// WithMCPControlGate replaces the gate.
func WithMCPControl() AppOption {
	return func(a *App) {
		a.mcpControl = true
	}
}

// WithMCPControlGate replaces the control tools' gate,
// MCPRequireRole("admin") by default. The gate must refuse a caller with no
// identity itself. It panics on a nil gate.
func WithMCPControlGate(gate func(ctx context.Context) error) AppOption {
	if gate == nil {
		panic("framework.WithMCPControlGate: nil gate: a nil precondition would silently allow every caller")
	}
	return func(a *App) {
		a.mcpControlGate = gate
	}
}

// WithMCPGate installs a precondition that every /mcp tools, resources and
// prompts method runs first; initialize and ping stay open. It panics on a
// nil gate.
func WithMCPGate(gate func(ctx context.Context) error) AppOption {
	if gate == nil {
		panic("framework.WithMCPGate: nil gate: a nil precondition would silently allow every caller")
	}
	return func(a *App) {
		a.mcpGate = gate
	}
}

func (a *App) controlToolSpecs() []mcpToolSpec {
	return []mcpToolSpec{
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

func (a *App) controlToolNames() []string {
	specs := a.controlToolSpecs()
	out := make([]string, len(specs))
	for i, t := range specs {
		out[i] = t.name
	}
	return out
}

func (a *App) registerControlTools() error {
	var opts []mcp.ToolOption
	if gate := controlToolGate(a.mcpControlDevImplied, a.mcpControlGate); gate != nil {
		opts = append(opts, mcp.WithToolGate(gate))
	}
	return a.registerBuiltinTools("control tool", a.controlToolSpecs(), opts...)
}

// MCPRequireUser returns a gate that refuses a caller with no resolved user,
// a caller on an embed grant, and a caller the access Decider denies on
// access.RoleGateCapability. Pair it with [mcp.WithToolGate] so the tool
// is also hidden from tools/list.
func MCPRequireUser() func(ctx context.Context) error {
	return requireMCPUser
}

var errAuthRequired = errors.New("this tool requires an authenticated caller: " +
	"send the session cookie or Authorization header on the /mcp request, " +
	"and make sure the app's session middleware runs on the /mcp route")

func requireMCPUser(ctx context.Context) error {
	if u, ok := handler.GetUser(ctx); !ok || u == nil {
		return errAuthRequired
	}
	if _, embedded := fembed.GrantFromContext(ctx); embedded {
		return errEmbedNotAllowed
	}
	if deciderDenies(ctx) {
		return errDeciderDenied
	}
	return nil
}

// MCPRequireRole is MCPRequireUser plus a role: the caller must hold one
// of roles, and a scope-restricted caller must also hold access.RoleScope
// of that role. It panics when no role is given.
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

func callerRoles(ctx context.Context) []string {
	held := heldRoles(ctx)
	if _, scoped := access.HeldScopes(ctx); !scoped {
		return held
	}
	return slices.DeleteFunc(held, func(r string) bool {
		return !access.ScopeAllows(ctx, access.RoleScope(r))
	})
}

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

func deciderDenies(ctx context.Context) bool {
	d := access.GetDecider(ctx)
	return d != nil && d(ctx, heldRoles(ctx), access.RoleGateCapability, access.Ref{}) == access.DecisionDeny
}

var (
	errRoleRequired  = errors.New("this tool requires a role the caller does not hold")
	errDeciderDenied = errors.New("this tool call was refused by the access decider for the caller's roles")
)

var errScopeNotHeld = errors.New("this tool needs a scope the caller's credential does not hold")

var errEmbedNotAllowed = errors.New("this tool is not reachable from an embedded surface: " +
	"an embed grant is delegated, scoped, and lives in a page the app does not control")

func controlToolGate(devImplied bool, override func(ctx context.Context) error) func(ctx context.Context) error {
	if devImplied {
		return nil
	}
	if override != nil {
		return override
	}
	return MCPRequireRole("admin")
}

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
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: "mcp control: `name` is required: call app_modules to list module names"}
	}
	var err error
	if enable {
		err = a.Modules().Enable(ctx, name)
	} else {
		err = a.Modules().Disable(ctx, name)
	}
	if err != nil {
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: err.Error()}
	}
	return map[string]any{
		"name":    name,
		"enabled": enable,
	}, nil
}
