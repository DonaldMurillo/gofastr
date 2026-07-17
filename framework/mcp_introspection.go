package framework

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
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
//     expose the framework's markdown docs (embedded at
//     build time, so they match the framework version
//     this binary was built against — no GitHub fetch).
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
			name:        "app_modules",
			description: "List registered modules with their manifest metadata (version, description, dependencies, migration group), enabled state, and owned surface counts (routes, entities, MCP tools). Modules are batteries that carry a manifest and support runtime enable/disable.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolModules,
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
			name:        "app_routines",
			description: "List every registered stored routine (function / procedure / trigger / view-as-routine) with its name, declared dialect (empty = all), sha256 checksum of the Up body, ledger state (present | drifted | missing | unknown), and best-effort liveness (does the object exist in pg_proc / pg_views on Postgres, unknown on SQLite). Read-only. Use to verify a routine body change has propagated, or to spot a routine the code still registers but the boot no longer applies.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolRoutines,
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

func (a *App) toolModules(_ context.Context, _ map[string]any) (any, error) {
	mods := a.Modules().List()
	out := make([]map[string]any, 0, len(mods))
	for _, m := range mods {
		out = append(out, map[string]any{
			"name":            m.Name,
			"version":         m.Version,
			"description":     m.Description,
			"depends_on":      m.DependsOn,
			"migration_group": m.MigrationGroup,
			"enabled":         m.Enabled,
			"entity_count":    m.EntityCount,
			"route_count":     m.RouteCount,
			"tool_count":      m.ToolCount,
		})
	}
	return map[string]any{
		"modules": out,
		"count":   len(out),
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

// toolRoutines lists every routine the App registered (via App.Routine or
// App.RoutinesFS) alongside the live ledger/liveness state. The intent is
// agent self-orientation: "did the routine body change I just deployed
// actually land?", "is there a routine the code still registers but the boot
// skipped?", "is there an object missing in pg_proc?".
//
// Each entry carries: name, declared Dialect ("" = all engines), the sha256
// checksum of the current registered Up body, ledger_state ∈
// {present,drifted,missing,unknown}, and liveness ∈ {present,absent,unknown}.
// ledger_state is "unknown" when no DB is wired; liveness is "unknown" on
// SQLite (no pg_proc/pg_views) or when the catalog query errors — the tool
// never fails, it just degrades gracefully so an agent always gets the
// static fields.
func (a *App) toolRoutines(_ context.Context, _ map[string]any) (any, error) {
	registered := a.migrationRoutines
	out := make([]map[string]any, 0, len(registered))

	// No DB → return static fields + unknown ledger/liveness. The tool never
	// fails; a degraded answer is more useful to an agent than an error.
	if a.DB == nil {
		for _, r := range registered {
			out = append(out, map[string]any{
				"name":         r.Name,
				"dialect":      dialectStringForTool(r.Dialect),
				"checksum":     migrate.RoutineChecksum(r),
				"ledger_state": "unknown",
				"liveness":     "unknown",
			})
		}
		return map[string]any{
			"routines": out,
			"count":    len(out),
			"db_wired": false,
		}, nil
	}

	ctx := context.Background()
	dialect := migrate.DetectDialect(a.DB)

	// Read the ledger in one shot. On error, every entry gets ledger_state
	// "unknown" — we still surface the registered set + checksums.
	ledger := map[string]string{}
	if rows, err := a.DB.QueryContext(ctx,
		`SELECT name, checksum FROM gofastr_routines`); err == nil {
		for rows.Next() {
			var name, checksum string
			if err := rows.Scan(&name, &checksum); err == nil {
				ledger[name] = checksum
			}
		}
		_ = rows.Close()
	}

	for _, r := range registered {
		newSum := migrate.RoutineChecksum(r)
		entry := map[string]any{
			"name":     r.Name,
			"dialect":  dialectStringForTool(r.Dialect),
			"checksum": newSum,
		}
		// Ledger state: only meaningful when the dialect matches. A routine
		// declared PG-only running against SQLite will never have a ledger row
		// for this engine — report "skipped_for_dialect" so an agent doesn't
		// read it as drift.
		if r.Dialect != "" && r.Dialect != dialect {
			entry["ledger_state"] = "skipped_for_dialect"
		} else if oldSum, ok := ledger[r.Name]; ok {
			if oldSum == newSum {
				entry["ledger_state"] = "present"
			} else {
				entry["ledger_state"] = "drifted"
			}
		} else {
			entry["ledger_state"] = "missing"
		}
		entry["liveness"] = a.probeRoutineLiveness(ctx, r.Name, dialect)
		out = append(out, entry)
	}

	return map[string]any{
		"routines": out,
		"count":    len(out),
		"db_wired": true,
	}, nil
}

// probeRoutineLiveness asks the DB whether a routine object with the given
// name currently exists. Postgres: pg_proc (functions/procedures) UNIONed
// with pg_views (views). SQLite: there is no portable catalog query that
// covers all routine kinds cheaply, so we report "unknown" rather than lie.
// Any query error returns "unknown" — the introspection tool degrades to the
// static fields rather than failing.
func (a *App) probeRoutineLiveness(ctx context.Context, name string, dialect migrate.Dialect) string {
	switch dialect {
	case migrate.DialectPostgres:
		// One round-trip: pg_proc (functions + procedures) and pg_views.
		const q = `SELECT EXISTS (
				SELECT 1 FROM pg_proc WHERE proname = $1
				UNION ALL
				SELECT 1 FROM pg_views WHERE viewname = $1
			)`
		var exists bool
		err := a.DB.QueryRowContext(ctx, q, name).Scan(&exists)
		if err != nil {
			return "unknown"
		}
		if exists {
			return "present"
		}
		return "absent"
	default:
		// SQLite: sqlite_master covers views, triggers, tables, but a function
		// or procedure body has no catalog entry (SQLite has no stored
		// functions). Reporting "present" or "absent" off sqlite_master would
		// be misleading for the function/procedure case, so we surface
		// "unknown" and let ledger_state carry the water.
		return "unknown"
	}
}

// dialectStringForTool renders a Dialect for the MCP response. Re-implements
// the migrate package's dialectString helper because this file lives in
// package framework and the helper is unexported there. The two must stay in
// sync — see TestRoutineChecksum_StableAndDifferent's sibling test
// (framework-level) for the parity assertion.
func dialectStringForTool(d migrate.Dialect) string {
	if d == "" {
		return "all"
	}
	return string(d)
}
