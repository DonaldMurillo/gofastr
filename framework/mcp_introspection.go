package framework

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"runtime/pprof"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// WithMCPIntrospection installs the read-only app_* MCP tools that report
// the running App's routes, plugins, batteries, modules, config,
// readiness, goroutine leaks, routines and unmounted copies.
func WithMCPIntrospection() AppOption {
	return func(a *App) {
		a.mcpIntrospection = true
	}
}

type mcpToolSpec struct {
	name        string
	description string
	schema      map[string]any
	handler     mcp.ToolHandler
}

func (a *App) registerBuiltinTools(kind string, tools []mcpToolSpec, opts ...mcp.ToolOption) error {
	for _, t := range tools {
		if a.skipDevEntityMCPCollision(t.name) {
			continue
		}
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler, opts...); err != nil {
			return fmt.Errorf("framework: register MCP %s %q: %w", kind, t.name, err)
		}
	}
	return nil
}

func (a *App) introspectionToolOptions() []mcp.ToolOption {
	if a.mcpIntrospectionDevImplied {
		return []mcp.ToolOption{mcp.WithDevImplied()}
	}
	return nil
}

func (a *App) registerIntrospectionTools() error {
	tools := []mcpToolSpec{
		{
			name:        "app_routes",
			description: "List every HTTP route registered on the App's router. Each entry has method (GET/POST/…) and pattern (Go 1.22 ServeMux syntax). Use to discover the surface area before issuing requests.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolRoutes,
		},
		{
			name:        "app_plugins",
			description: "List registered plugin names in registration order. Plugins are lightweight modules: see app_batteries for heavier modules with dependency declarations.",
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
			description: "Return the current AppConfig snapshot: Name, JSONCase, DebugEndpoints, NoLLMMD, RequestTimeout, DisableRequestTimeout, and the resolved http.Server connection timeouts (read_header/read/write/idle in ms). Read-only.",
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
			name:        "app_goroutine_leaks",
			description: "Report goroutines the Go runtime has proven leaked — blocked forever on channels or sync primitives nothing can reach (the goroutineleak profile). Returns count plus the leaked stacks; zero is healthy, a count that grows across calls means a worker or stream is being abandoned. Read-only.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolGoroutineLeaks,
		},
		{
			name:        "app_routines",
			description: "List every registered stored routine (function / procedure / trigger / view-as-routine) with its name, declared dialect (empty = all), sha256 checksum of the Up body, ledger state (present | drifted | missing | skipped_for_dialect | unknown; skipped_for_dialect means the routine's declared dialect doesn't match the active DB engine), and best-effort liveness (does the object exist in pg_proc / pg_views on Postgres, unknown on SQLite). Read-only. Use to verify a routine body change has propagated, or to spot a routine the code still registers but the boot no longer applies.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolRoutines,
		},
		{
			name:        "app_unmounted",
			description: "List the gofastr packages this binary carries as unmounted copies (made with gofastr unmount): each with source (the upstream import path), version (the gofastr release copied, or dev) and path (the copy's import path in the app). A copy no longer receives framework fixes on upgrade. Read-only.",
			schema:      map[string]any{"type": "object"},
			handler:     a.toolUnmounted,
		},
	}
	return a.registerBuiltinTools("tool", tools, a.introspectionToolOptions()...)
}

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
		"http_server_timeouts_ms": a.effectiveServerTimeoutsMs(),
	}, nil
}

func (a *App) effectiveServerTimeoutsMs() map[string]int64 {
	rh := defaultServerReadHeaderTimeout
	r := defaultServerReadTimeout
	w := defaultServerWriteTimeout
	idle := defaultServerIdleTimeout
	a.serverMu.Lock()
	if srv := a.server; srv != nil {
		rh, r, w, idle = srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout
	}
	a.serverMu.Unlock()
	return map[string]int64{
		"read_header_ms": rh.Milliseconds(),
		"read_ms":        r.Milliseconds(),
		"write_ms":       w.Milliseconds(),
		"idle_ms":        idle.Milliseconds(),
	}
}

func (a *App) toolGoroutineLeaks(_ context.Context, _ map[string]any) (any, error) {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return nil, fmt.Errorf("goroutineleak profile unavailable in this runtime")
	}
	runtime.GC()
	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return nil, err
	}
	const maxStackBytes = 64 << 10
	stacks := buf.String()
	truncated := false
	if len(stacks) > maxStackBytes {
		stacks = stacks[:maxStackBytes]
		truncated = true
	}
	return map[string]any{
		"count":     p.Count(),
		"stacks":    stacks,
		"truncated": truncated,
	}, nil
}

func (a *App) toolReadiness(ctx context.Context, _ map[string]any) (any, error) {
	if a.draining.Load() {
		return map[string]any{"ready": false, "reason": "draining", "checks": []map[string]any{}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, a.readinessTimeout())
	defer cancel()
	checks := a.readinessChecks()
	resp := runReadinessChecks(ctx, checks, false)
	out := make([]map[string]any, 0, len(resp.Checks))
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

func (a *App) toolRoutines(ctx context.Context, _ map[string]any) (any, error) {
	registered := a.migrationRoutines
	out := make([]map[string]any, 0, len(registered))

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

	dialect, dialectErr := a.txDialect()
	ledger, ledgerRead := map[string]string{}, false
	if dialectErr == nil {
		ledger, ledgerRead = a.readRoutineLedger(ctx, dialect)
	}

	for _, r := range registered {
		newSum := migrate.RoutineChecksum(r)
		entry := map[string]any{
			"name":     r.Name,
			"dialect":  dialectStringForTool(r.Dialect),
			"checksum": newSum,
		}
		if dialectErr == nil && r.Dialect != "" && r.Dialect != dialect {
			entry["ledger_state"] = "skipped_for_dialect"
		} else if !ledgerRead {
			entry["ledger_state"] = "unknown"
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

func (a *App) readRoutineLedger(ctx context.Context, dialect migrate.Dialect) (map[string]string, bool) {
	ledger := map[string]string{}
	rows, err := a.DB.QueryContext(ctx, `SELECT name, checksum FROM gofastr_routines`)
	if err != nil {
		exists, xerr := migrate.TableExistsBulk(ctx, a.DB, []string{"gofastr_routines"}, dialect)
		return ledger, xerr == nil && !exists["gofastr_routines"]
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, false
		}
		ledger[name] = checksum
	}
	if rows.Err() != nil {
		return nil, false
	}
	return ledger, true
}

func (a *App) probeRoutineLiveness(ctx context.Context, name string, dialect migrate.Dialect) string {
	switch dialect {
	case migrate.DialectPostgres:
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
		return "unknown"
	}
}

func dialectStringForTool(d migrate.Dialect) string {
	if d == "" {
		return "all"
	}
	return string(d)
}
