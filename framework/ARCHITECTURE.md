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
implementations split across **17 subpackages**. The facade re-exports
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
├── cron/            CronJob / Scheduler — minimal in-process tick loop
├── crud/            HTTP CRUD layer, eager loading, includes, nested
│                    filters, typed query, MCP tool generator,
│                    JSONCase config, in-tx helper. The biggest pkg.
├── db/              Executor + tx context primitives shared across
│                    crud, slowquery, and the framework root tx wrapper
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
├── internal/casing/ snake↔camel helpers (private to the framework
│                    module — not part of the public API)
├── migrate/         AutoMigrate / DiffSchema / Dialect
├── openapi/         EntityOpenAPI spec generator + the entity-endpoint
│                    URL builders (EntityEndpointPath etc.)
├── pagination/      Cursor + offset pagination
├── slowquery/       SlowQueryLogger — wraps any db.Executor
├── softdelete/      SoftDelete / Restore / ForceDelete + filter
├── tenant/          TenantConfig / Middleware / GetTenantID / etc.
├── static/          (HTTP static-file serving, pre-existing)
├── ui/              (server-rendered UI primitives, pre-existing)
└── uihost/          (UI host + page renderer, pre-existing)
```

The root package `framework/` itself contains:

- **App spine**: `app.go`, `plugin.go`, `registry.go`, `typed_hooks.go`
- **App-method-tied helpers**: `audit.go` (App.WithAuditLog),
  `tx.go` (App.InTx)
- **Test harness**: `testharness.go` (TestApp wraps `*App`)
- **Facade re-exports**: `reexports_*.go` — one file per subpackage
  whose symbols are referenced as `framework.X` by external code
- **Doc**: `doc.go`

---

## Layering rules (top imports bottom — reverse is forbidden)

```
L1  internal/casing                         (no internal deps)
L2  entity                                   (uses casing)
L3  hook, event, file, cron, access, db,    (each uses entity, none cross)
    pagination, filter, dsl, slowquery,
    tenant, softdelete, migrate, openapi
L4  crud                                     (uses entity, hook, event, db,
                                              file, filter, pagination,
                                              tenant, softdelete)
L5  framework/  (facade)                     (re-exports everything for
                                              the public API surface)
```

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
| `plugin.go` | `Plugin` interface + `PluginManager` consumed by `App.RegisterPlugin`. |
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
   The `examples/website` chromedp suite is the slow one (~2.5 min)
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
