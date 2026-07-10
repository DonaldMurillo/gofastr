# Modules

A **Module** is a Battery plus a manifest. Everything a module registers
during `Init` — routes, entities, cron jobs, queue consumers, MCP tools —
is **attributed** to the module, and a runtime **enable/disable** gate
enforces at dispatch time: a disabled module's routes 404, its cron jobs
and queue consumers skip, and its MCP tools refuse.

Toggling is live (no restart), persisted, and propagates across replicas
via the fanout seam (`WithFanout`).

## When to use module vs battery vs plugin

- **Plugin**: lightweight, no dependencies, no structured lifecycle.
  Use for small features that bundle a few routes or hooks.
- **Battery**: heavyweight, dependency-aware, structured OnStart/OnStop.
  Use when one extension must initialise before another or needs its own
  background workers.
- **Module**: a battery that also needs **runtime enable/disable**.
  Use when an operator should be able to turn a feature off without a
  redeploy — feature flagging at the module level, not the route level.

A module IS a battery. Everything batteries do (dependency ordering,
lifecycle hooks) applies unchanged. The manifest adds the metadata and
the enable/disable machinery adds the gates.

## Registration

```go
type FeatureModule struct{}

func (FeatureModule) Name() string { return "feature" }
func (FeatureModule) Init(app *framework.App) error {
    app.Router().Get("/feature/status", statusHandler)
    app.Entity("widgets", widgetConfig)
    return nil
}
func (FeatureModule) Manifest() framework.ModuleManifest {
    return framework.ModuleManifest{
        Version:        "1.0.0",
        Description:    "Widget management with CRUD + background jobs",
        DependsOn:      []string{"auth"},
        MigrationGroup: "feature",
    }
}

app.RegisterModule(FeatureModule{})
```

`RegisterModule` validates the name with the same rules as
`RegisterBattery` (non-empty, no control characters, ≤ 128 chars),
registers it as a battery with `deps = Manifest().DependsOn` so the
topo-sort orders module init, and records the manifest.

Registering two modules with the same name panics, exactly like a
duplicate battery.

## Attribution of surfaces

During a module's `Init`, the framework marks the module as "current" and
every registration funnel stamps ownership:

- **Routes**: every `app.Router().Handle` / `Get` / `Post` / … call and
  every Group sub-registration records `"METHOD /path"` → module. Two
  modules can own different HTTP methods on the same path independently;
  disabling one does not affect the other. Entity CRUD routes (mounted
  by `app.Entity`) are attributed the same way.
- **MCP tools**: every `app.MCP.RegisterTool` records tool → module.
- **Entities**: `app.Entity` records entity → module for introspection.
- **Cron**: `app.AddCron(scheduler)` called from a module's `Init`
  stamps the scheduler with a gate that skips jobs when the module is
  disabled.
- **Queues**: `app.AddQueue(queue)` called from a module's `Init`
  duck-types `SetGate` on the queue (both `DBQueue` and `MemoryQueue`
  implement it), deferring jobs when the module is disabled.

Routes or tools registered **outside** a module's `Init` (from `main`,
from a plugin, or from an `OnStart` hook) are **not** attributed and
pass through the gates unchecked. Attribution is an Init-time concept.

## Runtime enable/disable

```go
// Toggle at runtime — no restart:
err := app.Modules().Disable(ctx, "feature")
err = app.Modules().Enable(ctx, "feature")

// Query:
enabled := app.Modules().Enabled("feature")
list := app.Modules().List() // []ModuleInfo
```

- **Disabled → 404, not 403.** A disabled module's routes return a plain
  `http.NotFound`. The middleware chain does not run — auth, logging,
  and recovery never see the request. The module's existence does not
  leak. Method probing (e.g. `DELETE` against a path that only has a
  gated `GET`) also returns 404 — the `Allow` header in a 405 response
  lists only non-gated methods, so a disabled method is never advertised.
- **Disabled → jobs deferred, not dropped.** Cron jobs skip the tick.
  Queue jobs are released back to pending (without consuming a retry
  attempt) and run when the module re-enables.
- **Disabled → MCP tools refuse.** A `tools/call` for a disabled
  module's tool returns a generic `"tool unavailable"` JSON-RPC error
  (no module name or disabled state is leaked). The tool is also
  excluded from `tools/list` so it is not advertised while disabled.
- **Fail-closed dependency rules.** `Disable` refuses if any
  **enabled** module lists it in `DependsOn` — no cascade, no orphaning.
  `Enable` refuses if any of the module's `DependsOn` is disabled.
  The error names the blocking dependents or dependencies.
- **Persist first, then flip.** On a successful toggle the store is
  written first; only on store success does the in-memory cache flip and
  the fanout publish. A store failure leaves state unchanged.

### Persistence

When the app has a DB (`WithDB`), the module state persists in a
`gofastr_modules` table:

| column | type |
|---|---|
| `name` | TEXT PRIMARY KEY |
| `enabled` | BOOLEAN NOT NULL |
| `updated_at` | TIMESTAMP NOT NULL |

The table is self-migrating (`CREATE TABLE IF NOT EXISTS`) — it is NOT
a migrate group. Modules absent from the table are **enabled by default**;
store rows for unknown module names (a removed module) are kept but
ignored. Without a DB the state is in-memory and resets on every boot.
If the table cannot be created (e.g. a read-only or corrupt DB), `Start`
**fails closed** rather than silently falling back to in-memory — a
deliberately disabled module must not come back enabled on a broken store.

### Multi-replica propagation

With `WithFanout` attached, the module manager subscribes to topic
`gofastr.modules` at boot. Every successful toggle publishes
`{"name":…,"enabled":…}` JSON via the fanout, wrapped in the standard
node-ID envelope so a replica ignores its own publishes. The message is
treated as a **refresh signal only**: the receiving replica re-reads the
authoritative state from its own store and sets its cache to whatever the
store says — never to what the payload says. This makes message ordering
irrelevant (the store is the source of truth) and neuters crafted payloads.

Because the signal carries no state, **cross-replica propagation requires
a shared DB-backed store**: every replica must read the same
`gofastr_modules` table (i.e. `WithDB` against the shared database). With
a fanout but no shared store, each replica re-reads its own in-memory
store and toggles never propagate. Without a fanout, other replicas don't
learn about a toggle until they restart and re-read the store;
persistence is orthogonal (the store is always written).

## Migration-group tie-in

`Manifest.MigrationGroup` defaults to the module name and is an
informational pointer to the `-- +migrate Group <name>` directive from
the migrations system. The framework does not enforce that it matches a
registered migration group, but the convention keeps schema migrations
and module enable/disable in sync.

**Disabled-module migration policy**: a named migration group with no
registered migrations is treated as a *disabled module* by the migration
runner — its applied rows are shown by `status` but never compared,
blocked on, rolled back, or dropped. See the migrations doc for details.
`force --group=<name>` is the reconciliation escape hatch when a module
is permanently removed.

## Introspection

The `app_modules` MCP tool (available via `WithMCPIntrospection`) lists
every module's name, version, description, dependencies, migration
group, enabled state, and owned surface counts (routes, entities, tools).
Enable/disable is Go-API-only for now — no mutating MCP tools.

## Common mistakes

- **Disabling a shared dependency.** If module B depends on module A and
  both are enabled, disabling A fails with an error naming B. Disable B
  first, then A. The framework refuses to cascade — a silent breakage
  is worse than a clear error.
- **Expecting queued jobs to drop.** When a module is disabled, its
  queue jobs are deferred (released to pending), not dropped. They run
  when the module re-enables. If you need to purge them, drain the queue
  explicitly.
- **Registering routes outside Init and expecting attribution.** Routes
  added from `OnStart`, from `main`, or from a plugin are not attributed
  to any module. The gate cannot block what it cannot see. Keep route
  registration inside the module's `Init`.
- **Expecting process isolation.** A disabled module's code is still
  loaded in the process — its types, closures, and goroutines spawned
  outside the gate are still live. The gates cover dispatch surfaces
  (routes, cron, queue, MCP), not arbitrary Go code. Process isolation
  is explicitly out of scope.
- **Existence probing via trailing-slash redirects.** A `GET /modsub`
  for a module that registered `/modsub/` triggers Go ServeMux's
  automatic 307 redirect to `/modsub/` — before any gate fires.
  Similarly, method probing (trying methods to see which return 405
  vs 404) can reveal a disabled module's registered methods. These
  are inherent to the mux-layer gate; they are only fully closed by
  process-level isolation.
