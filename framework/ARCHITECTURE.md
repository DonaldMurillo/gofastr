# GoFastr framework package layout

> Read this before adding, moving, or extracting any package under
> `framework/`. The layout is intentional and non-obvious in places —
> the rules below explain *why* each subpackage exists, what's still in
> the root and why, and how to keep the layered shape intact when adding
> new code.

---

## The shape in one paragraph

`framework/` used to be a single 22k-LOC package. It now exposes the same
public API through a thin **facade** at `framework/`, with the actual
implementations split across ~25 runtime subpackages (plus a handful of
test-infra and `experimental/` packages that are out-of-contract — see
the map). The facade re-exports
types/funcs via type aliases and `var` bindings so external callers
(`framework.Entity`, `framework.NewApp`, etc.) keep working unchanged.
Subpackages communicate via **abstract interfaces** defined in low-level
packages (`entity.Registry`, `db.Executor`, `db.Beginner`) so no
subpackage needs to back-import the framework root.

---

## The package map

```
framework/
├── access/          Permission / Policy / RolePolicy / RBAC helpers
├── agentsinv/       Process-wide registry of agent-onboarding (agents.md) snippets
├── cron/            CronJob / Scheduler — minimal in-process tick loop
├── crud/            HTTP CRUD layer, eager loading, includes, nested
│                    filters, typed query, MCP tool generator,
│                    JSONCase config, owner/tenant scoping, in-tx helper.
│                    The biggest pkg.
├── db/              Executor + tx context primitives shared across
│                    crud, slowquery, and the framework root tx wrapper
├── dev/             Dev-mode-only helpers (livereload, debug surfaces)
├── docs/            Embedded doc content (framework/docs/content/*.md) + `gofastr docs`
├── dsl/             ?dsl=… query string parser/builder
├── entity/          Entity + EntityConfig + Define, all column types
│                    and operators (And/Or/Not/Condition/Order),
│                    relations, validators, EntityDeclaration JSON
│                    loader, and the shared Registry interface
├── event/           EventBus / Event / EventHandler
├── file/            FileField + Process/Delete/GeneratePath
├── filter/          Query-string filter & sort parsing
├── hook/            HookRegistry / HookType + lifecycle constants
│                    (BeforeCreate, AfterCreate, etc.)
├── i18nui/          Translated default strings for framework UI surfaces
├── image/           Image decode/encode + blurhash for the file/upload path
├── internal/casing/ snake↔camel helpers (private to the framework
│                    module — not part of the public API)
├── lifecycle/       Graceful shutdown contract — drain, flush, stop phases
├── migrate/         AutoMigrate / DiffSchema / Dialect / Bulk queries
├── openapi/         EntityOpenAPI spec generator + the entity-endpoint
│                    URL builders (EntityEndpointPath etc.)
├── owner/           "Who owns this row" seam — OwnerField scoping for CRUD
├── pagination/      Cursor + offset pagination
├── routegroup/      Route groups — prefix, middleware, access, OpenAPI tags
├── slowquery/       SlowQueryLogger — wraps any db.Executor
├── softdelete/      SoftDelete / Restore / ForceDelete + filter
├── tenant/          TenantConfig / Middleware / GetTenantID / etc.
├── static/          HTTP static-file serving
├── ui/              Server-rendered UI primitives (PageHeader, FormField,
│                    DataTable, …). The largest UI surface — composes
│                    core-ui/html + core-ui/patterns into intent-level
│                    components; see core-ui/ARCHITECTURE.md for the rule
│                    on when a primitive lives here vs in core-ui/.
└── uihost/          UI host + page renderer (SEO/Screen wiring)

Out-of-contract (NOT part of the layering rules below):
  experimental/apiversions   API versioning (URL prefix, deprecation
                             headers, projections) — experimental surface.
  testkit/, testdata/        Public test helpers + fixtures for host apps.
  factory/                   Rails-style fixture/factory helpers (tests).
  isolation/                 Per-worktree local runtime resource resolution.
  harness/                   The GoFastr agent harness.
```

The root package `framework/` itself contains:

- **App spine**: `app.go`, `plugin.go`, `battery.go`, `registry.go`, `typed_hooks.go`
- **App-method-tied helpers**: `audit.go` (App.WithAuditLog),
  `tx.go` (App.InTx), `flags.go` (App feature-flag store),
  `health.go` (App health/readiness probes), `i18n.go` (App.WithI18n / T),
  `mcp_introspection.go` (App.WithMCPIntrospection), `agents.go`
  (App agents-inventory wiring)
- **Test harness**: `testharness.go` (TestApp wraps `*App`)
- **Facade re-exports**: `reexports_*.go` — one file per subpackage
  whose symbols are referenced as `framework.X` by external code
- **Doc**: `doc.go`

---

## Layering rules (top imports bottom — reverse is forbidden)

```
L1  internal/casing                         (no internal deps)
L2  entity                                   (imports core/ only — no
                                              framework-internal deps)
L3  hook, event, file, cron, access, db,    (leaf packages — no framework-
    pagination, filter, owner                internal imports at all)
    dsl, tenant, softdelete, migrate         (each imports entity)
    slowquery                                (imports db — the one
                                              intra-L3 edge)
L4  crud                                     (uses entity, hook, event, db,
                                              file, filter, pagination,
                                              tenant, owner, access,
                                              internal/casing; softdelete
                                              is inlined, not imported)
    openapi                                  (uses crud, entity,
                                              internal/casing — sits above
                                              crud within L4)
L5  framework/  (facade)                     (re-exports everything for
                                              the public API surface)
```

(The map above is the real import graph as of v0.5.0 — verify with
`go list -f '{{join .Imports "\n"}}' ./framework/<pkg>` when in doubt.
The rule is direction: a package may import packages in lower layers,
never higher, and intra-layer edges should stay rare and deliberate —
today only `slowquery → db` and `openapi → crud` exist.)

**No subpackage may import `framework/`.** If a subpackage needs a type
defined in framework root (App, Registry, CrudHandler), one of three
patterns applies — see "Cycle-breaking interfaces" below.

---

## Why those things are still in framework root

The remaining root files share one property: their public type or method
set is bound to `*App` (the spine type). Moving any of them out would
either require an API redesign or create an unbreakable import cycle.

| File | Why it stays |
|---|---|
| `app.go` | Defines `App` itself plus all `(a *App)` configuration methods. The package whose name is on the import path is by definition where App lives. |
| `plugin.go` | `Plugin` interface + `PluginManager` consumed by `App.RegisterPlugin`. Both Plugin and Battery use a single `Init(*App)` integration point; the router late-binds middleware so anything Init does wraps existing routes. |
| `battery.go` | `Battery` interface + `BatteryManager` consumed by `App.RegisterBattery`. Same single-Init shape as plugin, plus dependency-resolved init order and an optional `BatteryLifecycle` for OnStart/OnStop. |
| `registry.go` | `Registry` is concrete state (`*sql.DB`, entity map). Subpackages reference it through the `entity.Registry` interface instead. |
| `typed_hooks.go` | Generic `OnBeforeCreate[T any](app *App, …)` etc. Take `*App` directly; their type signatures live with App. |
| `audit.go` | `(a *App) WithAuditLog(...)` is the public entry; the rest of the file (table creation, hook closures) is intrinsically tied to it. Could be split if the closures move out, but the win is small. |
| `tx.go` | `(a *App) InTx(...)` is the public entry. The lower-level context helpers already moved to `framework/db`; this file is just App's wrapper. |
| `testharness.go` | `TestApp.App *App` field is used by tests. Sixteen test files use `TestApp` / `TestHarness` unqualified inside `package framework`. Extracting requires either an interface refactor or qualifying every test. |

---

## Cycle-breaking interfaces

When a subpackage needed something from framework root, the answer was
**never** to back-import. Three patterns kept the graph acyclic:

### `entity.Registry` interface

```go
// framework/entity/registry.go
type Registry interface {
    All() map[string]*Entity
    Get(name string) (*Entity, error)
}
```

`*framework.Registry` satisfies this implicitly. Used by:
- `migrate.AutoMigrate(db, registry entity.Registry)`
- `migrate.DiffSchema(ctx, db, registry entity.Registry)`
- `dsl.BuildDSLQuery(registry entity.Registry, input string)`
- `crud.CrudHandler.Registry entity.Registry` field

### `db.Executor` interface

```go
// framework/db/db.go
type Executor interface {
    QueryContext(ctx, query, args...) (*sql.Rows, error)
    QueryRowContext(ctx, query, args...) *sql.Row
    ExecContext(ctx, query, args...) (sql.Result, error)
}
```

Both `*sql.DB` and `*sql.Tx` satisfy this. `framework.DBExecutor` is
kept as a type alias (`type DBExecutor = db.Executor`) for back-compat.
Used by `crud.CrudHandler`, `slowquery.SlowQueryLogger`, the eager
loaders.

### `db.Beginner` + `db.WithTx`/`db.TxFromContext`

```go
// framework/db/db.go
type Beginner interface {
    BeginTx(ctx, opts) (*sql.Tx, error)
}

func WithTx(ctx, tx) context.Context
func TxFromContext(ctx) (*sql.Tx, bool)
```

Both `App.InTx` (framework root) and `CrudHandler.inTx` (crud) call
into `db.WithTx` / `db.TxFromContext`. The previous private
`contextWithTx` helper would have created a cycle when the methods
ended up in different packages.

---

## Conventions established along the way

### Pre-rename to avoid package shadowing

Local variables named `entity` and `crud` were renamed to `ent` / `ch`
across the codebase **before** moving the matching packages. Inside a
function with `func f(entity *entity.Entity)`, Go resolves `entity.X`
in the body against the local var, not the package — making any
reference to a package-level symbol in the same function impossible.

If you add a local var that would shadow a subpackage import, rename it
first.

### Field-key collisions in struct literals

`gofmt -r 'X -> pkg.X'` rewrites struct field keys too, producing
invalid Go like `Foo{pkg.Index: 0}`. After every gofmt -r pass on a
package whose exports overlap with field names anywhere in the tree
(`Entity`, `Index`, `Required`, `Unique`, `Relation`, `SoftDelete`),
search for `pkg.Sym:` and undo the rewrite where it lands on a field
key. **Do not** undo `case pkg.Sym:` — those are switch labels and
need to stay qualified.

### Test placement

- **Per-feature internal tests** that read unexported helpers stay in
  the same package as the helper (`framework/cron/cron_test.go`,
  `framework/access/`).
- **Cross-feature tests** (e2e, integration, openapi conformance,
  typed-repo end-to-end) stay at the framework root and exercise the
  facade.
- **Tests that compose the App spine** (almost all of them — they call
  `NewApp`, `WithDB`, `TestHarness`) must stay at the framework root
  unless they're rewritten to import `framework` as an external test
  package, which only works for tests that don't need internal access
  to the package they're testing.

### Promotions across new boundaries

When a previously-unexported symbol crosses a new package boundary,
prefer **promotion** (rename to exported) over moving to `internal/`.
Promotion shows up in the public API but is documented in the comment;
`internal/` would hide it but break the test surface for free.

Promotions made during the reorg:
`cron.RunOnce`, `event.Snapshot`, `slowquery.TrimSQL`,
`migrate.SQLType/SQLDefault/ReadLiveColumns*/DetectDialect`,
`crud.NormalizePath`, `crud.BatchResponse`,
`crud.ApplyTenantScope/Count/Update/Delete`, `crud.InjectTenant`,
`crud.ApplySoftDeleteFilter/Count`,
`entity.Condition.SQL()`, `entity.Condition.Args()`.

### Agent-onboarding inventory (`agents.md` per package)

Each `battery/*` and every framework subpackage worth surfacing to an
agent ships an `agents.md` next to its source files plus an `agents.go`
that does:

```go
package mybattery

import (
    _ "embed"
    "github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

//go:embed agents.md
var agentsMarkdown string

func init() {
    agentsinv.Register(agentsinv.Entry{
        Name:       "mybattery",                     // dir name
        Kind:       agentsinv.KindBattery,           // or KindFramework
        ImportPath: "github.com/.../battery/mybattery",
        Markdown:   agentsMarkdown,
    })
}
```

Why a registry instead of file globs:

- Importing the battery = its snippet is in scope. No central allow-list
  to update on every new battery.
- `cmd/gofastr` blank-imports the batteries it inventories, then walks
  `agentsinv.All()` to write `AGENTS.md` (see `framework/agentsinv/inventory.go`).
- An empty `agents.md` panics at init — the file is missing or the
  `//go:embed` directive is stale.

Per-battery `agents.md` content lives next to the code it documents.
Treat it as a short "use this when / shape / don't reinvent" reference
for AI agents, not as a user manual.

### Naming collisions with `core/`

Several `core/` packages share names with what subpackages would want:

| Subpackage rename | Why |
|---|---|
| `framework/entity` (not `framework/schema`) | `core/schema` is imported as `schema` in 46 framework files |
| `framework/migrate` aliases `core/migrate` as `coremig` inside its own files | self-name collision |
| `framework/openapi` forces `app.go` to alias `core/openapi` as `coreoa` | same as above |

If you add a subpackage, grep `core/` for name conflicts first.

---

## How to extract a new subpackage (recipe)

1. **Confirm it's actually a leaf.** Grep the file for references to
   `*App`, `(a *App)`, or App methods. If any of those exist, the file
   stays in framework root or needs an API redesign first.

2. **Pre-rename shadowing locals.** If the future package name
   (e.g. `tenant`) appears as a local variable name anywhere in the
   tree, rename to a non-conflicting form before moving anything:
   ```bash
   for f in framework/*.go; do gofmt -r 'tenant -> tenantID' -w "$f"; done
   ```

3. **Move with `git mv` and rewrite the package declaration:**
   ```bash
   mkdir -p framework/<pkg> && \
   git mv framework/<file>.go framework/<pkg>/<file>.go && \
   sed -i '' '1s/^package framework$/package <pkg>/' framework/<pkg>/<file>.go
   ```

4. **Qualify every caller** with `gofmt -r` per exported symbol:
   ```bash
   for sym in Sym1 Sym2 …; do
     for f in framework/*.go; do
       gofmt -r "${sym} -> <pkg>.${sym}" -w "$f"
     done
   done
   goimports -w framework/*.go
   ```

5. **Undo struct-field-key collisions** (see convention above).

6. **Build and fix.** Common categories of failure:
   - Test access to unexported helpers → promote in the new package.
   - Methods on a moved type referenced from framework root → the
     method must travel with the type (Go's package-locality rule);
     either add a wrapper at the call site or move the calling file
     too.
   - External callers using `framework.Sym` → add a re-export in
     `framework/reexports_<pkg>.go`.

7. **Keep tests that compose `App` at framework root.** They use the
   facade re-exports; that's the whole point of the facade.

8. **Run the full suite, including external consumers**:
   `go test ./framework/... ./cmd/... ./kiln/... ./examples/...`.
   The `examples/site` chromedp suite is the slow one (~2.5 min)
   and the only thing that exercises the full SSR + island + widget
   stack end-to-end.

---

## What I recommend NOT doing

- **Do not** add a new subpackage just to make `framework/` smaller.
  Each subpackage justified its own move — pure data/helpers, or a
  cluster of related types. Single-file extractions of methods on
  `*App` are not worth the API churn.

- **Do not** delete the facade re-export files. They are the seam
  that lets external code keep using `framework.X`. Removing them
  is a breaking change for every consumer.

- **Do not** bypass the `entity.Registry` / `db.Executor` interfaces
  by adding a back-import to framework root. If a subpackage needs
  more than the interface offers, extend the interface — don't reach
  for the concrete type.
