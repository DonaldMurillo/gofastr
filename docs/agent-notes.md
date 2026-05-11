# Agent Notes

## 2026-05-11 - framework-reorg

- Scope: `framework/` (no other modules touched)
- Symptom: One 22k-LOC `package framework` file dump (~86 .go files) made navigation, dependency reasoning, and per-concern testing painful. Aggressive bulk-AST splits (gofmt -r over the whole tree) were attempted first and produced uncompilable intermediate states because of (a) variable shadowing on common names like `entity`/`crud`, (b) struct field-key collisions in composite literals (`Foo{Index:…}` rewriting to `Foo{pkg.Index:…}`), and (c) unexported helpers crossing newly created package boundaries. Switched to per-package serial extraction with manual review of each callsite.
- Evidence: 8 commits on the `worktree-framework-reorg` branch extract 17 subpackages — `entity, crud, hook, event, file, cron, access, tenant, softdelete, migrate, dsl, filter, pagination, slowquery, db, openapi, internal/casing` — leaving only the App spine in `framework/` root. Cycle-breaking interfaces (`entity.Registry`, `db.Executor`, `db.Beginner`) let subpackages compose without back-importing framework root. Six `framework/reexports_*.go` files keep every external `framework.X` callsite (cmd/gofastr generators, kiln/render, every example) compiling unchanged. Full layout, layering rules, and a recipe for new extractions are in `framework/ARCHITECTURE.md`. Build + tests green: `go test ./framework/... ./cmd/... ./kiln/...` clean; `examples/core-ui-demo` chromedp test is environment-flaky and unrelated.
- Next time: pre-rename local vars that would shadow a target package name BEFORE running gofmt -r. After every gofmt -r pass on a package whose exports overlap with field names (`Entity`, `Index`, `Required`, `Unique`, `Relation`, `SoftDelete`), search for `pkg.Sym:` and undo struct-field-key rewrites — but leave `case pkg.Sym:` switch labels alone. Tests that compose the App spine (NewApp + WithDB + TestHarness) must stay at framework root and use the facade re-exports; trying to move them into subpackages either creates test-cycle errors or loses access to the unexported methods they verify.

## 2026-05-07 - architecture-review

- Scope: `testing`, `core-ui`, `framework`
- Symptom: `go test ./...` needs permission to bind local `httptest` ports, and the current real failure is `github.com/gofastr/gofastr/core-ui/app` overlay wrapper expectations.
- Evidence: `go test ./core-ui/app` fails `TestNewDrawer`, `TestNewSheet`, and `TestNewDialog`; `go test ./core/query ./framework ./core/middleware ./cmd/gofastr` passes.
- Next time: run focused package tests first, then escalate the full suite only when browser/httptest packages are required.

## 2026-05-07 - api-ui-review

- Scope: `api`, `core-ui`, `architecture`
- Symptom: Architecture reviews should separate declarative feature flags from enforcement paths; current risks cluster around CRUD scoping, DB dialect assumptions, and runtime/server UI contracts.
- Evidence: `docs/project-architecture-review.md` tracks round 2 API findings and round 3 core-ui findings with file references.
- Next time: check generated docs/specs against actual parser behavior, then check shared UI instances for request-scoped mutable state.

## 2026-05-08 - continuation-review

- Scope: `core/middleware`, `framework`, `core/router`, `core-ui/devserver`, `core-ui/island`
- Symptom: Normal `go test ./...` can pass while concurrency bugs and unconnected public APIs remain. Race-enabled checks found the timeout middleware writes to the same response recorder from two goroutines.
- Evidence: `docs/project-architecture-review.md` round 4 records findings 17-23. `go test -race ./core/middleware ./core-ui/island ./core-ui/devserver` fails on `core/middleware/timeout.go`.
- Next time: include `go test -race` for middleware and SSE/streaming packages during reviews, and verify public registries/hooks are invoked by the runtime path, not only unit-tested as standalone helpers.

## 2026-05-08 - fresh-architecture-review

- Scope: `framework`, `core`, `core-ui`, `battery`, `cmd`
- Symptom: Round-based review notes became hard to use after multiple fix passes. A clean consolidated review makes current risks easier to triage and avoids carrying fixed findings forward.
- Evidence: `docs/project-architecture-review.md` now starts from scratch with architecture summary, prioritized findings, test gaps, and verification. File-field context handling was rechecked and left out because it now uses caller-supplied context.
- Next time: when asked to "start from scratch," rewrite the review artifact instead of appending rounds, and revalidate each old finding against the current tree before preserving it.

## 2026-05-08 - proposal-gap-scan

- Scope: `proposal`, `plan/tasks`, `cmd`, `framework`
- Symptom: `plan/tasks.md` and task checkboxes are stale and still mark broad areas as not started, even though core primitives, batteries, CRUD, OpenAPI, hooks, events, plugins, and tests exist in code.
- Evidence: compare `proposal.md` and `plan/tasks/*.md` with implemented packages under `core/`, `battery/`, `framework/`, and `cmd/gofastr/`; remaining large gaps are codegen-to-`.gofastr`, JSON entity loading, entity MCP auto-tools, DSL query parser, custom endpoint config, and production-grade CLI subcommands.
- Next time: assess roadmap status from source and tests first, then update the tracker separately instead of trusting unchecked boxes.

## 2026-05-08 - declaration-codegen-mcp

- Scope: `framework`, `cmd/gofastr`, `examples/blog`, `docs`
- Symptom: Proposal-level JSON declarations, `.gofastr` generation, and entity MCP tools now share a single `framework.EntityDeclaration` contract.
- Evidence: `framework/declaration.go` loads `entities/*.json`; `cmd/gofastr/generate.go` emits `.gofastr/entities/register.go` and `models.go`; `framework/entity_mcp.go` registers `{entity}_list/get/create/update/delete`; `examples/blog/entities/*.json` exercises runtime loading.
- Next time: extend this path by adding richer generated query builders and wiring `.gofastr` output into scaffolded apps before adding another declaration format.

## 2026-05-08 - remaining-proposal-gaps

- Scope: `framework/dsl`, `battery/search`, `cmd/gofastr/migrate`, `examples/core-ui-demo`
- Symptom: Full-suite verification is viable but slow because `examples/core-ui-demo` browser tests take about 5.5 minutes; earlier apparent hangs were premature stops.
- Evidence: `go test ./examples/core-ui-demo -count=1 -timeout=10m` passed in 326s; `go test ./... -timeout=12m` passed. DSL parser, search battery, and SQL-file migrate CLI now have focused tests.
- Next time: run browser-heavy packages with explicit long timeouts, and use focused package tests while iterating to avoid mislabeling slow chromedp runs as hangs.
