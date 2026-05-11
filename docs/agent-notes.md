# Agent Notes

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

## 2026-05-09 - feature-batch-1

- Scope: `framework`, `core/middleware`, `examples/website`
- Symptom: large batch of feature gaps shipped together — slow-query log, OpenTelemetry tracing, composite cursors, scoped includes, nested filters, streaming JSON for huge lists, audit log, cron scheduler, DB-backed queue, generated Go client + typed lifecycle hooks. Each was a separate proposal item; merging them as one batch kept the dependency graph (typed hooks → audit; cursor + tracing → SSE-through-metrics fix) coherent.
- Evidence: commits `36a224f..9251db1` on `main`; the batch lands with full-stack E2E coverage in `framework/e2e_*_test.go` and `examples/website/*_test.go`.
- Next time: when a proposal item depends on instrumentation another item adds, batch them. Splitting into independent PRs forces the dependency back through review.

## 2026-05-10 - filter-island-pattern

- Scope: `core-ui/runtime`, `examples/website`
- Symptom: filter/search was the third in-page-state pattern needed alongside pagination and sort, and it had to land as an island RPC (per `core-ui/ARCHITECTURE.md` rule 1) rather than a URL-based reload. The customers CRUD demo was wired end-to-end against the same pattern to prove the model holds for write-side state.
- Evidence: commits `9a693a8` (customers CRUD demo) and `9251db1` (filter island); `examples/website/*_test.go` exercises both.
- Next time: every new in-page state pattern that lands should be added to the runtime drift tests at the same time so future contributors can't accidentally reintroduce the route-based version.

## 2026-05-11 - ui-runtime-drift-tests

- Scope: `core-ui`, `framework/uihost`, `examples/website`
- Symptom: the architecture doc captures the contract in prose but the codebase had no automated check that someone hadn't reintroduced a hard-refresh path, an SSE-for-user-action, or an in-page state route. Three previous regressions on this contract had each been caught only by manual review.
- Evidence: commit `b691506` adds drift checks that fail CI if any of the three failure modes from `core-ui/ARCHITECTURE.md` reappear (verified by `go test ./examples/website/ -run TestE2E`).
- Next time: every documented rule that's been broken before needs a test that fails when it's broken again. The architecture doc itself shouldn't be relied on as the enforcement mechanism.

## 2026-05-11 - docs-restructure

- Scope: `docs/`, `.claude/skills/`
- Symptom: README + architecture doc were solid, but `docs/*.md` had stub pages (~22 lines each) for security/migrations/search/query-dsl and was missing pages for half the surfaces the README advertised: batch, includes, events, cursor, multipart, hooks/tx, access control, multi-tenant, cron, audit, plugins, kiln. No mechanism existed to keep docs synced with API changes.
- Evidence: this commit expands the four stub pages with full surface tables and common-mistake callouts; adds 11 missing reference pages grounded in code reads; adds `.claude/skills/gofastr-docs/SKILL.md` that auto-loads on any code change so docs ship in the same commit as the API; adds `docs/README.md` index.
- Next time: a stub doc is a defect — either flesh it out or fold it into the README. Don't leave half-done reference pages that lie about the surface.
