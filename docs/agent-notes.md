# Agent Notes

## 2026-05-20 - blueprint-theme-codegen

- Scope: `cmd/gofastr`, `docs/blueprints.md`, generated-app browser E2E
- Symptom: blueprint `app.theme` support needs both decode-time validation and generated `site.WithTheme(...)`; testing only emitted Go or raw CSS can miss whether the browser actually receives the generated app theme.
- Evidence: `cmd/gofastr/blueprint.go` decodes `app.theme`, rejects unknown color tokens, generates `BlueprintTheme()`, and `cmd/gofastr/blueprint_test.go` checks `getComputedStyle(document.documentElement)` for `--color-background`, `--color-primary`, and `--color-text` in the generated app.
- Next time: when adding blueprint app-level knobs, update decode, merge, validation, generated registration, docs, and the generated-app E2E path together.

## 2026-05-20 - wave-5-7-followups + adversarial review

- Scope: `framework/ui/{optimisticaction,networkretrybanner}`, `core-ui/patterns/scrollspy/`, `framework/uihost/` (SEO bundle), `core-ui/runtime/src/{optimisticaction,networkretrybanner,scrollspy}.js`, plus their demos/e2e in `examples/website/`
- Symptom: Wave 5 (OptimisticAction, NetworkRetryBanner) and Wave 7 (ScrollSpy, SEO head-wrapper) were the last unshipped items on the UI roadmap. Built them, then ran a 4-way parallel adversarial review of the diff; 10 concrete bugs surfaced (1 high-severity: OA's `Variant=ButtonPrimary` silently dropped the `ui-button--primary` class via a wrong `!= ButtonPrimary` guard; 1 security: OA fetch shipped without forwarding the page's `<meta name="csrf-token">`; the rest were a11y / multi-instance state / SPA-nav teardown / cssEscape leading-digit / bootstrap DOM-order / re-entrancy / doc/code mismatch).
- Evidence: TDD on every fix where deterministic reproduction was possible (failing test → fix → green). The harder-to-reproduce races (rollback timer clobber, IO disconnect) got fix-only with code-review verification. New e2e patterns established: (a) atomic high-water counter on a slow endpoint to assert "no concurrent fetches"; (b) recorder endpoint that captures incoming headers so a header-forwarding test can round-trip without inline `<script>`. ScrollSpy runtime now disconnects observers on `gofastr:navigate` and sorts targets by `compareDocumentPosition` before the bootstrap pick. NetworkRetryBanner state is per-banner via `WeakMap` + an iteration `Set`. OA runtime adds `aria-busy=true` + `disabled=true` during pending, clears them on commit/idle, and clears the rollback timer on a new click. ScreenSEO bundle struct landed in `framework/uihost`, threaded through `screenHeadHTML` with bundle-wins-over-per-concern + per-concern fall-through semantics for empty fields.
- Next time: when the public runtime API spans multiple instances (`__gofastr.networkStatus.reportFailure()` etc.), default to per-element state in a WeakMap on day one. The "single banner per page" assumption is almost always wrong by the time the third demo lands. Also: tests that overwrite `document.body.innerHTML` in chromedp don't refresh the runtime module's bound state — the module's IIFE already ran. Either re-navigate via `chromedp.Navigate` or expose a public `rescan(root)` API that re-binds without re-evaluating the module.

## 2026-05-19 - pattern-css-unification

- Scope: `core-ui/patterns/*`, `core-ui/check`, `examples/website/theme.go`, `core-ui/ARCHITECTURE.md`, `.claude/skills/component-build`
- Symptom: Two CSS contracts in the framework. `framework/ui/*` auto-wired via `registry.RegisterStyle` + `Style.WrapHTML` (CSS auto-loads on first appearance via the runtime's `data-fui-comp` scanner). `core-ui/patterns/*` exported `BaseCSS() string`, which every host app had to concatenate by hand into `WithCustomCSS`. A single missed concat shipped a component with no styling — the 2026-05-19 nestedlist incident.
- Evidence: All 6 patterns still on the legacy contract (accordion, breadcrumbs, nestedlist, pagination, progress, skeleton, tabs) migrated to `registry.RegisterStyle` + `Style.WrapHTML`. `BaseCSS()` exports removed; class selectors stay class-based. `examples/website/theme.go` lost 6 imports + 6 concatenations. `core-ui/check.LintNoPatternBaseCSS` + `TestNoPatternBaseCSS_RepoIsClean` enforce the new contract — any new pattern package that re-introduces `BaseCSS()` fails the build. Tabs's dynamic `:has()` rule generation became `buildCSS()` called from `styleFn`. Tests that asserted on exact `<nav aria-label="X">` strings relaxed to `aria-label="X"` since the wrapper now carries `data-fui-comp`. Skeleton-preset line widths moved from inline `Width:"50%"` (CSP-blocked) into the registered preset CSS. Architecture note added to `core-ui/ARCHITECTURE.md` ("Patterns use the same contract"); same rule added to the component-build skill as an anti-pattern.
- Next time: when introducing a CSS-bearing package, use the registry pattern from day one. The lint guard catches the regression at build time; don't disable it. If a pattern needs theme-aware CSS, `styleFn(t style.Theme) string` already receives the theme — use it. For dynamically generated CSS (like tabs's `:has()` rules), have `styleFn` call a builder function.

## 2026-05-19 - in-house-blueprint-codegen

- Scope: `core/yaml`, `cmd/gofastr`, `docs/`
- Symptom: YAML-to-code should be deterministic code generation, not runtime JSON declaration loading and not LLM-inferred behavior. The parser also belongs in `core` so framework users can reuse the in-house YAML subset without adding a production dependency.
- Evidence: `core/yaml` parses the supported subset; `gofastr generate --from=<file-or-dir>` decodes blueprints and writes `.gofastr/entities` plus `.gofastr/blueprint` code. Entities now carry properties/cursors/indices through codegen, screens can render property-based Kiln nodes, islands, widgets, and browser actions, and the generated-app E2E test drives HTTP CRUD, OpenAPI, MCP, and real browser UI behavior from the CLI output. Custom endpoints, middleware, plugins, and helpers generate Go stubs instead of invented handler bodies.
- Follow-up: code review found useful edge cases that now have regression coverage: split blueprint directories validate after merge, entity-owned and top-level endpoints append instead of replacing, endpoint method/path collisions fail before router registration, `--dry-run --json` emits JSON validation errors, unsupported YAML anchors/aliases/tags are rejected, and multiple UI actions can be reachable through event-specific `data-action-<event>` attributes.
- Second follow-up: empty blueprint directories now fail before generation, dry-run JSON validates unsafe output paths, CRUD collision checks use the framework's default table naming for dashed/spaced entity names, and duplicate per-block UI action events are rejected before rendering unreachable DOM attributes.
- Next time: keep blueprint expansion schema-first. Add new explicit keys and validation before rendering new artifacts, reject unsupported YAML syntax rather than silently treating full YAML as supported, and keep generated-app E2E coverage on the full surface instead of only unit-testing snippets.

## 2026-05-11 - framework-reorg

- Scope: `framework/` (no other modules touched)
- Symptom: One 22k-LOC `package framework` file dump (~86 .go files) made navigation, dependency reasoning, and per-concern testing painful. Aggressive bulk-AST splits (gofmt -r over the whole tree) were attempted first and produced uncompilable intermediate states because of (a) variable shadowing on common names like `entity`/`crud`, (b) struct field-key collisions in composite literals (`Foo{Index:…}` rewriting to `Foo{pkg.Index:…}`), and (c) unexported helpers crossing newly created package boundaries. Switched to per-package serial extraction with manual review of each callsite.
- Evidence: 8 commits on the `worktree-framework-reorg` branch extract 17 subpackages — `entity, crud, hook, event, file, cron, access, tenant, softdelete, migrate, dsl, filter, pagination, slowquery, db, openapi, internal/casing` — leaving only the App spine in `framework/` root. Cycle-breaking interfaces (`entity.Registry`, `db.Executor`, `db.Beginner`) let subpackages compose without back-importing framework root. Six `framework/reexports_*.go` files keep every external `framework.X` callsite (cmd/gofastr generators, kiln/render, every example) compiling unchanged. Full layout, layering rules, and a recipe for new extractions are in `framework/ARCHITECTURE.md`. Build + tests green: `go test ./framework/... ./cmd/... ./kiln/...` clean; `examples/core-ui-demo` chromedp test is environment-flaky and unrelated.
- Next time: pre-rename local vars that would shadow a target package name BEFORE running gofmt -r. After every gofmt -r pass on a package whose exports overlap with field names (`Entity`, `Index`, `Required`, `Unique`, `Relation`, `SoftDelete`), search for `pkg.Sym:` and undo struct-field-key rewrites — but leave `case pkg.Sym:` switch labels alone. Tests that compose the App spine (NewApp + WithDB + TestHarness) must stay at framework root and use the facade re-exports; trying to move them into subpackages either creates test-cycle errors or loses access to the unexported methods they verify.

## 2026-05-07 - architecture-review

- Scope: `testing`, `core-ui`, `framework`
- Symptom: `go test ./...` needs permission to bind local `httptest` ports, and the current real failure is `github.com/DonaldMurillo/gofastr/core-ui/app` overlay wrapper expectations.
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

## 2026-05-17 - ten-ui-primitives

- Scope: `framework/ui/`, `core-ui/widget/preset/`, `core-ui/runtime/runtime.js`, `examples/website`, `core-ui/ARCHITECTURE.md`, `docs/ui-getting-started.md`, `docs/widgets.md`
- Symptom: the `framework/ui` package shipped Avatar / Button / Callout / StatCard / DataTable / Form / Menu / Notification / PageHeader / Sidebar / Toast — solid as far as it went, but a real app reaching for "card", "stack/grid layout", "tag chip", "tooltip", "checkbox/switch", "spinner", "divider", "file upload", "popover", or "responsive lazy image" had to hand-roll the HTML+CSS each time. Three example screens already had bespoke `display:flex` divs with inconsistent spacing.
- Evidence: this commit adds ten primitives (`Stack`/`Cluster`/`Grid`/`Center`/`Spacer`/`Box`, `Card`, `OptimizedImage`, `Checkbox`/`Radio`/`Switch`, `Tooltip`, `Popover`, `Tag`, `Spinner`, `Divider`, `FileUpload`) plus dogfooded demo screens at `/components/{layout,card,image,toggle,tooltip,popover,tag,spinner,divider,fileupload}`. 95 new unit tests under `framework/ui/`, 14 new chromedp E2E tests under `examples/website/`. Runtime gains a `_popoverStack` so non-modal floating surfaces honour CloseOnEscape + CloseOnClickOutside (previously modal-only — see `core-ui/runtime/runtime.js` lines ~1559–1605, ~2087–2110). Cheat sheet rows added to `core-ui/ARCHITECTURE.md`; `framework/ui/doc.go` lists the full component inventory.
- Next time: when extending a runtime feature (Escape / outside-click) so a new primitive can deliver on its docstring promise, the docstring change ships in the same commit as the runtime change. Don't ship a preset whose comments lie about behaviour.

## 2026-05-11 - docs-restructure

- Scope: `docs/`, `.claude/skills/`
- Symptom: README + architecture doc were solid, but `docs/*.md` had stub pages (~22 lines each) for security/migrations/search/query-dsl and was missing pages for half the surfaces the README advertised: batch, includes, events, cursor, multipart, hooks/tx, access control, multi-tenant, cron, audit, plugins, kiln. No mechanism existed to keep docs synced with API changes.
- Evidence: this commit expands the four stub pages with full surface tables and common-mistake callouts; adds 11 missing reference pages grounded in code reads; adds `.claude/skills/gofastr-docs/SKILL.md` that auto-loads on any code change so docs ship in the same commit as the API; adds `docs/README.md` index.
- Next time: a stub doc is a defect — either flesh it out or fold it into the README. Don't leave half-done reference pages that lie about the surface.
