# Claude / agent instructions for the GoFastr repo

**Before writing any UI, runtime, or `framework/uihost` code, read
[`core-ui/ARCHITECTURE.md`](core-ui/ARCHITECTURE.md). This is mandatory.**

**Before adding, moving, or extracting anything under `framework/`, read
[`framework/ARCHITECTURE.md`](framework/ARCHITECTURE.md).** It captures the
package layout, the layering rules, the cycle-breaking interfaces
(`entity.Registry`, `db.Executor`), and the recipe for new extractions.

The architecture document is the contract. Three different attempts at
the UI navigation model were made before it was written, all wrong in
different ways. The doc captures the model and lists the failure modes
explicitly so they don't repeat.

**Before adding, renaming, or removing any exported API, route, CLI
subcommand, JSON field, or auto-generated artifact, the `gofastr-docs`
skill at `.claude/skills/gofastr-docs/SKILL.md` auto-loads. Docs ship
in the same commit as the code change — not a follow-up. The docs
live in `framework/docs/content/*.md` and are embedded into the
`gofastr` binary at build time — `gofastr docs` browses them; the
MCP tools `framework_docs_list` / `framework_docs_get` /
`framework_docs_search` expose them to agents connected to a live app.**

## TL;DR of the architecture (read the full doc anyway)

- **SSR-first**. Every page is fully server-rendered on initial load.
- **Hydration**, not re-render. `runtime.js` attaches handlers to the
  existing DOM after first paint.
- **Cross-page nav is client-side** with cache. No hard refreshes when
  going from `/a` to `/b`.
- **In-page state changes are islands**: a click fires an RPC, the
  server returns new island HTML, the runtime swaps just that island.
- **Server-pushed updates** flow through signals + SSE for genuine
  background events, not user actions.

## Hard rules

1. Never make in-page state changes (sort, paginate, expand) into routes.
   They are islands.
2. Never re-implement pagination/sort/filter math in JS. Server-side.
3. Never use SSE to deliver responses to user actions. SSE is push-only.
4. Never add `location.href = …` or full reloads as a "fix".
5. Never add new `data-fui-*` attributes without updating
   `core-ui/ARCHITECTURE.md` and the runtime test suite.
6. Never expose an entity holding per-user data via auto-CRUD without
   setting `EntityConfig.OwnerField`. See
   `framework/docs/content/entity-declarations.md` → "Per-user scoping".

## Common operations

- **Build / run the example website**: `./scripts/dev-watch.sh` (auto-rebuild + livereload, port `:8082`). Dev-watch writes to `/tmp/` because the watched tree must stay clean.
- **Build canonical binaries**: `make build` (→ `dist/gofastr`, `dist/kiln`) or `make build-all` (also builds every example into `dist/examples/`). The `dist/` directory is the **only** sanctioned build output location and is gitignored.
- **Test all packages**: `go test ./...`.
- **Run the FULL repo suite (build + vet + test, no cache, generous timeout)**: `./scripts/test-all.sh`. Use this before/after large refactors — it covers the slow chromedp suite (`examples/website`) and `kiln/integration`. `RACE=1`, `SHORT=1`, and a trailing package path are all supported.
- **Test the website end-to-end (chromedp)**: `go test ./examples/website/ -run TestE2E`.
- **Clean build artifacts**: `make clean` (wipes `dist/`, `bin/`, `.gofastr/`).
- **Audit no-binaries-committed**: `find . -maxdepth 3 -type f -size +500k ! -path "./.git/*" ! -path "./dist/*" ! -name "*.go" ! -name "*.md"` — anything in the result is a stray binary in the source tree; either move it to `dist/` or remove it before commit.

## Where to look first

- New UI component? Start in `framework/ui/` if it composes intent
  (PageHeader, FormField, DataTable). Use `core-ui/html` if it maps 1:1
  to an HTML tag, or add to `core-ui/patterns/` if it's a composed UI
  pattern (accordion, tabs, pagination, breadcrumbs…).
- New island? Use `core-ui/widget` builder.
- Theme tokens? `framework/ui/theme` for the canonical theme;
  `core-ui/style` for the underlying machinery.
- Entity model, columns, relations, validators, EntityDeclaration?
  `framework/entity/`. Most other framework subpackages depend on it.
- HTTP CRUD handler / batch / cursor / stream / upload / typed query /
  MCP tool generator / eager loading / includes? `framework/crud/`.
- Filtering, sorting, pagination, DSL parsing — `framework/filter/`,
  `framework/pagination/`, `framework/dsl/` (each is its own pkg).
- Lifecycle hooks (BeforeCreate/AfterUpdate/etc.)? `framework/hook/`.
- Auto-migration / schema diffing / dialect detection?
  `framework/migrate/`.
- Multi-tenancy, soft delete, RBAC? `framework/{tenant,softdelete,access}/`.
- Cron, events, file-field upload helper, slow-query logger?
  `framework/{cron,event,file,slowquery}/`.
- App lifecycle, plugins, registry, typed hooks? Stay in `framework/`
  root — these are the App spine. See `framework/ARCHITECTURE.md` for
  why each remaining root file is glued to App.
