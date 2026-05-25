package framework

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/docs"
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
//   - framework_docs_list / framework_docs_get / framework_docs_search:
//                     expose the framework's markdown docs (embedded at
//                     build time, so they match the framework version
//                     this binary was built against — no GitHub fetch).
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
		{
			name:        "framework_docs_list",
			description: "List every framework documentation topic shipped with this binary. Returns name + title + summary for each topic. Pair with framework_docs_get to fetch the full markdown.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolDocsList,
		},
		{
			name:        "framework_docs_get",
			description: "Return the full markdown body of a framework doc topic by name. Pass the topic name without .md (e.g. \"entity-declarations\", \"hooks-and-transactions\"). Call framework_docs_list first to discover names.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{"type": "string", "description": "Topic name (no .md suffix)"},
				},
				"required": []string{"topic"},
			},
			handler: a.toolDocsGet,
		},
		{
			name:        "framework_docs_search",
			description: "Search across every framework doc topic for a substring (case-insensitive, min 3 chars). Returns matching lines with nearest-heading context, capped at `limit` hits (default 50). Use when you don't know which topic owns the answer.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term":  map[string]any{"type": "string", "description": "Search term (min 3 chars)"},
					"limit": map[string]any{"type": "integer", "description": "Max hits to return (default 50, hard cap to protect narrow-context clients)"},
				},
				"required": []string{"term"},
			},
			handler: a.toolDocsSearch,
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

// toolDocsList enumerates every embedded framework doc topic. Each
// entry has name (use with framework_docs_get), title (first H1 in the
// file), summary (first non-heading paragraph), and bytes (raw size).
func (a *App) toolDocsList(_ context.Context, _ map[string]any) (any, error) {
	topics, err := docs.List()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(topics))
	for _, t := range topics {
		out = append(out, map[string]any{
			"name":    t.Name,
			"title":   t.Title,
			"summary": t.Summary,
			"bytes":   t.Bytes,
		})
	}
	return map[string]any{
		"topics": out,
		"count":  len(out),
	}, nil
}

// toolDocsGet returns the full markdown body for a named topic.
func (a *App) toolDocsGet(_ context.Context, params map[string]any) (any, error) {
	topic, _ := params["topic"].(string)
	body, err := docs.Get(topic)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name":     topic,
		"markdown": string(body),
		"bytes":    len(body),
	}, nil
}

// toolDocsSearch greps every topic for a substring. The response shape
// mirrors the SearchHit type — topic, line, heading, excerpt — but
// keeps the payload size bounded by capping each excerpt at 240 chars.
func (a *App) toolDocsSearch(_ context.Context, params map[string]any) (any, error) {
	term, _ := params["term"].(string)
	limit := 0
	switch v := params["limit"].(type) {
	case int:
		limit = v
	case int64:
		limit = int(v)
	case float64:
		limit = int(v)
	}
	hits, err := docs.SearchWithLimit(term, limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"topic":   h.Topic,
			"line":    h.Line,
			"heading": h.Heading,
			"excerpt": h.Excerpt,
		})
	}
	return map[string]any{
		"term":  term,
		"hits":  out,
		"count": len(out),
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
