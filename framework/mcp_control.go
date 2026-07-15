package framework

import (
	"context"
	"fmt"
	"net/http"
)

// WithMCPControl installs the MUTATING MCP tools that let a connected
// agent control the running App. Separate from WithMCPIntrospection —
// which stays strictly read-only — so each surface opts into exactly
// the trust level its /mcp endpoint warrants:
//
//   - Introspection (read-only) is safe wherever disclosure is: docs
//     sites, staging, local dev.
//   - Control (mutating) belongs on /mcp endpoints reachable only by
//     trusted callers — the local dev loop, an authenticated tunnel.
//     Blueprint-generated apps wire it gated on the dev env for this
//     reason.
//
// Tools registered:
//
//   - app_module_enable:  enable a registered module at runtime. The
//     state persists through the module store and respects dependency
//     ordering (enabling a module whose dependency is disabled fails).
//   - app_module_disable: disable a registered module at runtime. Fails
//     closed if enabled modules depend on it — no cascades.
//
// Code-level changes (screens, entities, handlers) are NOT an MCP
// concern: that is the `gofastr dev` edit-rebuild-reload loop. MCP
// control mutates runtime STATE the app already models.
func WithMCPControl() AppOption {
	return func(a *App) {
		a.mcpControl = true
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
			description: "Disable a registered module on the running app. Persists through the module store; the module's routes/tools start refusing. Fails closed when enabled modules depend on it — disable dependents first. Use app_modules to list modules and their current state.",
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
	for _, t := range tools {
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("framework: register MCP control tool %q: %w", t.name, err)
		}
	}
	return nil
}

// routerHasMCPRoute reports whether the host already mounted a POST
// /mcp route by hand — the dev-implied auto-mount yields to it.
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
		return nil, fmt.Errorf("mcp control: `name` is required — call app_modules to list module names")
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
