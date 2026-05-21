package framework

import (
	"context"
	"fmt"
)

// WithMCPIntrospection installs a set of MCP tools that expose the
// running App's structure to an agent: registered routes, plugins,
// batteries, app config, readiness status. Opt-in because:
//
//   - The tools are read-only but cumulatively reveal a lot about the
//     app's shape (every mounted route, every loaded module, every
//     config field). Production apps that expose MCP to untrusted
//     callers should weigh the disclosure surface before enabling.
//   - The framework should stay zero-config-friendly: nothing should
//     show up on the MCP server unless the operator asked for it.
//
// Tools registered:
//
//   - app_routes:     list every (method, pattern) registered on the router.
//   - app_plugins:    list registered plugins (name only).
//   - app_batteries:  list registered batteries with deps + lifecycle status.
//   - app_config:     return the AppConfig snapshot (Name, JSONCase, timeouts…).
//   - app_readiness:  run every registered readiness check and report results.
func WithMCPIntrospection() AppOption {
	return func(a *App) {
		a.mcpIntrospection = true
	}
}

func (a *App) registerIntrospectionTools() error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name:        "app_routes",
			description: "List every HTTP route registered on the App's router. Each entry has method (GET/POST/…) and pattern (Go 1.22 ServeMux syntax). Use to discover the surface area before issuing requests.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolRoutes,
		},
		{
			name:        "app_plugins",
			description: "List registered plugin names in registration order. Plugins are lightweight modules — see app_batteries for heavier modules with dependency declarations.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolPlugins,
		},
		{
			name:        "app_batteries",
			description: "List registered batteries with their dependency declarations and initialized state. Batteries are heavyweight modules with dependency-ordered Init and optional OnStart/OnStop lifecycle.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolBatteries,
		},
		{
			name:        "app_config",
			description: "Return the current AppConfig snapshot: Name, JSONCase, DebugEndpoints, NoLLMMD, RequestTimeout, DisableRequestTimeout. Read-only.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolConfig,
		},
		{
			name:        "app_readiness",
			description: "Run every registered readiness check (the same set /readyz consults) and return per-check status. Use to verify the app is ready to serve traffic before issuing real requests.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolReadiness,
		},
	}
	for _, t := range tools {
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("framework: register MCP tool %q: %w", t.name, err)
		}
	}
	return nil
}

// ---- handlers ------------------------------------------------------------

func (a *App) toolRoutes(_ context.Context, _ map[string]any) (any, error) {
	routes := a.router.Routes()
	out := make([]map[string]any, 0, len(routes))
	for _, r := range routes {
		out = append(out, map[string]any{
			"method":  r.Method,
			"pattern": r.Pattern,
		})
	}
	return map[string]any{
		"routes": out,
		"count":  len(out),
	}, nil
}

func (a *App) toolPlugins(_ context.Context, _ map[string]any) (any, error) {
	names := a.Plugins.Names()
	return map[string]any{
		"plugins": names,
		"count":   len(names),
	}, nil
}

func (a *App) toolBatteries(_ context.Context, _ map[string]any) (any, error) {
	names := a.Batteries.Names()
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		entry := a.Batteries.entries[n]
		if entry == nil {
			continue
		}
		out = append(out, map[string]any{
			"name":        n,
			"deps":        append([]string{}, entry.deps...),
			"initialized": entry.initialized,
		})
	}
	return map[string]any{
		"batteries": out,
		"count":     len(out),
	}, nil
}

func (a *App) toolConfig(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{
		"name":                    a.Config.Name,
		"json_case":               string(a.Config.JSONCase),
		"debug_endpoints":         a.Config.DebugEndpoints,
		"no_llmmd":                a.Config.NoLLMMD,
		"request_timeout_ms":      a.Config.RequestTimeout.Milliseconds(),
		"disable_request_timeout": a.Config.DisableRequestTimeout,
	}, nil
}

func (a *App) toolReadiness(ctx context.Context, _ map[string]any) (any, error) {
	checks := a.readinessChecks()
	// Force verbose=false regardless of App.readinessVerbose — /readyz
	// and /mcp may have very different trust boundaries (e.g. /readyz on
	// a private listener vs /mcp behind authenticated tunneling), and
	// the introspection tool must not leak raw error text (DSNs, IPs,
	// stack fragments) just because the operator enabled verbose for
	// the load balancer's probe.
	resp := runReadinessChecks(ctx, checks, false)
	out := make([]map[string]any, 0, len(resp.Checks))
	// An app with no checks registered is not "ready" — it's
	// unconfirmed. Surface that explicitly rather than silently
	// reporting ready=true (which would hide a wiring mistake).
	if len(checks) == 0 {
		return map[string]any{
			"ready":  false,
			"reason": "no readiness checks registered",
			"checks": out,
		}, nil
	}
	ready := true
	for _, c := range resp.Checks {
		if c.Status != "ok" {
			ready = false
		}
		entry := map[string]any{
			"name":        c.Name,
			"status":      c.Status,
			"duration_ms": c.DurMS,
		}
		if c.Error != "" {
			entry["error"] = c.Error
		}
		out = append(out, entry)
	}
	return map[string]any{
		"ready":  ready,
		"checks": out,
	}, nil
}
