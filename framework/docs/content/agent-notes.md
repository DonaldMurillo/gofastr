# Agent Notes

## 2026-05-31 - framework visibility: git init, CLAUDE.md, docs surfacing, live evals

- **Scope:** `cmd/gofastr/init.go`, `cmd/gofastr/embedded/gofastr-host-skill.md`, `.claude/skills/gofastr-host/SKILL.md`, `evals/`, `README.md`, `framework/docs/content/ui-getting-started.md`.
- **Symptom:** New users running `gofastr init` got scaffolded Go files but no git repo, no CLAUDE.md (so Claude Code had no entry point), no mention of `gofastr docs` (so agents couldn't discover features), and the host skill referenced a non-existent `gofastr docs search` command instead of `gofastr docs --grep`.
- **Change:** `gofastr init` now runs `git init` and writes a thin `CLAUDE.md` that points to `AGENTS.md`, the gofastr-host skill, and the embedded docs (`gofastr docs`, `gofastr docs <topic>`, `gofastr docs --grep`). The host skill's wrong command was fixed. Init "Next steps" output mentions `gofastr docs`. Added 13 live-agent evals using OMP's `agent()` that verify generic AI agents (not gofastr-aware) can discover every framework feature from the scaffolded onboarding files alone — batteries, cross-cutting concerns, UI components, charts, islands, widgets, theming — with zero reinventions across all 74 assertions.
- **Next time:** when a new battery or feature is added, add it to the eval suite to prevent the "agent doesn't know about it" regression; when the embedded doc topic count changes, update the README CLI section that mentions the count.


## 2026-05-31 - reliability follow-up: perf, repolint, Postgres evidence, SSE block

- **Scope:** `framework/crud`, `framework/uihost`, `core/stream`, `cmd/repolint`, benchmark docs, `scripts/perf-postgres-evidence.sh`.
- **Symptom:** The next reliability pass needed four concrete follow-ups: filtered-list/UI-host perf witnesses, repo-owned enforcement of the no-external-lint-tools policy, a CI-friendly Postgres benchmark evidence command, and an explicit stronger-delivery SSE mode for clients that do not want oldest-drop behavior.
- **Change:** CRUD list paths now reuse cached visible-field/JSON-key slices internally and pool row maps on the no-include/no-hook path while preserving `VisibleFields()` as a copy-returning public API. UI host chrome injection batches head/body insertions to avoid repeated whole-page replacements. `repolint` now flags external lint dependencies in `go.mod`, `make bench-pg-evidence` records Postgres-tagged benchmark output, and `core/stream.SSEBroker` supports `?slow=block` / `X-SSE-Slow: block` publisher backpressure.
- **Next time:** filtered-list allocation work helped but did not meet the latency target. Keep it marked `NEEDS-WORK` until parser short-circuiting or generated typed rows prove the time gap has closed; do not turn partial benchmark wins into roadmap closure just because the diff looks tidy.

## 2026-05-31 - perf and scaffold witnesses

- **Scope:** `core/render`, `framework/migrate`, `framework/bench_tier9_test.go`, `core/stream/sse_broker_test.go`, `cmd/gofastr` scaffold tests.
- **Symptom:** Island RPC p99 was reported from `testing.B.SetParallelism(64)`, which overstates "64 concurrent users"; Postgres bulk helpers existed but were not wired into `DiffSchema` or idempotent `AutoMigrate`; several scaffold commands still had helper-level coverage instead of CLI build-through coverage.
- **Change:** Island RPC benchmark now uses fixed worker counts, `render.Tag`/`Join` pre-size builders and skip attr sorting for single attrs, `DiffSchema` uses `ReadLiveColumnsBulk`, Postgres `AutoMigrate` reruns use `TableExistsBulk`, SSE slow-subscriber semantics are pinned as oldest-drop/latest-retained, and `generate --config`, `theme init`, and `new` have CLI E2E build guards.
- **Next time:** when a perf helper exists, confirm the public path actually calls it before marking the roadmap item verified; when benchmark names imply concurrency, make the worker count literal.

## 2026-05-31 - repo-owned lint policy

- **Scope:** `Makefile`, `cmd/repolint`.
- **Symptom:** `make lint` depended on `golangci-lint`, which violates the no-external-lint-tools policy and made the lint target fail when that binary was not installed.
- **Change:** lint is now built from Go-team tools plus a repo-owned `cmd/repolint` checker. Keep new lint rules low-noise and first-party; do not add external lint binaries unless the dependency policy changes.
- **Next time:** add narrow repolint rules with tests when a recurring repo hygiene issue appears, then wire them through `make lint` instead of adopting a broad third-party lint bundle.

## 2026-05-31 - reliability cleanup rules

- **Scope:** `cmd/gofastr` generation commands, `framework/docs/content/project-architecture-review.md`, performance benchmark witnesses, battery agent inventory.
- **Symptom:** old review docs preserved fixed findings, `battery/print` had `agents.go`/`agents.md` but was missing from the CLI blank-import inventory, and the SSE backpressure benchmark still measured a legacy raw channel instead of `core/stream.SSEBroker`.
- **Change:** architecture review is now a current risk register, inventory tests remain directory-driven, perf docs must say when a witness is stale/Postgres-needed/needs-work, and generated/scaffolded CLI paths should compile or run generated output through temp-module E2E tests.
- **Next time:** when a generated, documented, or benchmarked surface changes, update the executable witness and remove stale review findings in the same commit.

## 2026-05-26 - runtime JS minifier + copy module carve

- **Scope:** new `core-ui/runtime/minify` package (token-aware JS minifier, pure Go, zero deps), `core-ui/runtime/runtime.go` wires it into `RuntimeJS()` + `Module()` via `sync.Once`, new `core-ui/runtime/src/copy.js` (carved out of `runtime.js`), `core-ui/runtime/preload.go` adds copy marker, `framework/docs/content/runtime-minification.md`, ROADMAP §8 status update.

- **Minifier is env-gated, prod-wins.** Default polarity: minify unless `GOFASTR_DEV=1` (and `GOFASTR_ENV` is not a non-dev env). `RUNTIME_NOMINIFY=1` / `RUNTIME_MINIFY=1` are manual overrides that trump the env detection. An end-user who just `go build`s their app and runs in production with no env vars gets the minified runtime automatically. Dev workflow (`gofastr dev` → `GOFASTR_DEV=1`) keeps raw output so browser stack traces stay readable.

- **Tier-2 scope, intentionally narrow.** Strip comments + whitespace, distinguish regex from division (via prev-token class), preserve string + template-literal payloads byte-for-byte (including `${…}` interpolation re-tokenized), preserve ASI hazards (`return\nfoo`), keep `++`/`--` un-split, handle control bytes in regex char classes (runtime.js has `/^[\s\x00-\x1f]+/`). No identifier renaming, no DCE — output stays parseable + debuggable without source maps.

- **The `i++` bug.** First implementation inserted a fusion-guard space whenever two `+` would be adjacent. That broke `i++` into `i+ +`, which Acorn rejected as a parse error. Fix: only emit the fusion-guard space when the original source had whitespace between the two tokens (`m.sawSpace`). Adjacent chars in the source can't fuse into a different token by definition. Same logic applies to `--`, `//`, `/*`.

- **Copy carve is the pattern.** `[data-fui-copy-text-from]` global click delegator moved from runtime.js → `src/copy.js`. Lazy-loaded via the existing `_moduleMarkers` scanner. Pages without copy buttons no longer parse the clipboard logic. Net runtime.js shrink was small (~340B raw) but architecturally it validates the carve-on-demand pattern for future growth.

- **Sizes after.** Bundled `runtime.js`: 92 KB raw / 28 KB gz → 38 KB raw / **10.4 KB gz** (beats the ROADMAP §8 12 KB gz target). Total embedded JS corpus: 262 KB raw / 88 KB gz → 132 KB raw / 44 KB gz. `budget_test.go` overrides tightened accordingly.

- **Stale-anchor cleanup.** Several tests grepped for substrings (`EventSource`, `data-island`, `redirect: 'follow'`, `(() =>`) that either lived only in comments (the minifier correctly strips them) or used pre-minify spacing. Replaced with code-only anchors or relaxed to accept both spacings.

- **Tests:** new `core-ui/runtime/minify` package suite (table-driven unit + corpus idempotency + size report); `TestNominifyEnvGating` pins the env contract; `TestRuntimeModule_Copy` covers the new module. Full website chromedp e2e (285 tests) green against the minified runtime. Per-module budget for `copy`: 1.8 KB raw.

- **Next time:** when carving more code out of `runtime.js`, check `core-ui/runtime/preload.go`'s `demandLoadMarkers` table — the marker must be added there too or pages won't get the `<link rel="modulepreload">` tag. Drift test (`TestDemandLoadMarkersMatchRuntimeJS`) enforces alignment.

## 2026-05-24 - core/dotenv + auto-load in NewApp

- **Scope:** new `core/dotenv` package (parser, expander, loader), `framework.NewApp` auto-load wiring, `cmd/gofastr` migrate-command swap, `framework/docs/content/dotenv.md`.

- **NewApp auto-loads `.env` files BEFORE options run.** Probe order (earlier wins): `.env.local`, `.env.<APP_ENV>` (only if APP_ENV is set), `.env`. Missing files silent; malformed files fail fast. Existing `os.Environ` always wins over file values — operator-set vars are never clobbered. Kill switch: `GOFASTR_DOTENV=off` in the process env.

- **Parser is a strict subset of the de facto dotenv spec.** Keys: `^[A-Za-z_][A-Za-z0-9_]*$` or parse error. Double-quoted values interpret `\n \t \r \" \\`. Single-quoted values are verbatim (no escapes, no expansion). `export` prefix tolerated. Inline-comment-after-unquoted-value is preserved as part of the value (write a quoted value if you need `#` inside). Multi-line values NOT supported.

- **Variable expansion is bracket-form only (`${VAR}`), double-quoted only.** Bare `$VAR` is left verbatim. Lookup order: local (earlier keys / earlier files), then `os.Environ`, then empty. Hardening: cycle detection via visited-set, depth cap 16, undefined → empty, `\${...}` escape blocks expansion at that position, malformed `${...` (no closing brace) left verbatim.

- **Migrate cmd cleanup.** `cmd/gofastr/migrate_cmd.go` was rolling its own 1-key prefix scanner for `.env`; now routes through `dotenv.Load` so it picks up quoted DATABASE_URL values, `export DATABASE_URL=...`, etc.

- **Tests:** parser cases (basic, quoted, escapes, comments, export, whitespace, empty, malformed-rejection, dup-key-last-wins); expander cases (basic, bare-dollar-not-expanded, undefined-empty, local-vs-env precedence, nested, self-reference, mutual cycle, deep-chain-bounded, malformed-unclosed-verbatim, empty-brace-verbatim, escape-blocks-expansion, single-quoted-no-expand); loader (existing-env-wins, sets-missing, idempotent, missing-file-silent, earlier-file-wins, malformed-error). NewApp wiring: 4 tests (auto-load, existing-wins, OFF kill switch, APP_ENV overlay).

- **Next time:** when adding env-touching framework features, remember the framework now sets env BEFORE options run. Pre-NewApp `os.Getenv` calls in caller code may see different values than they did before (if a `.env.local` is present). Document the precedence chain in any new feature that reads env.

## 2026-05-24 - framework DX round-2 + adversarial review fixes

- **Scope:** `core-ui/component/component.go`, `core-ui/app/{app,policy,screen,screen_group,router}.go`, new `core-ui/app/decide` subpkg, `core-ui/runtime/runtime.js`, `core-ui/runtime/src/taginput.js`, `core/router/router.go`, `framework/{entity/declaration,uihost/uihost}.go`, `battery/auth/{manager,policy,form_decode,core}.go`, `kiln/render/node.go`, framework docs.

- **Form intercept is opt-in.** Default-enctype and `multipart/form-data` forms submit browser-native (no fetch wrapping, no SPA nav). Only `enctype="application/json"` or `data-fui-spa` opts INTO the runtime interceptor. Auth flows (`<form action="/auth/login" method=POST>`) are not intercepted. CRUD POSTs that expect JSON must add `enctype="application/json"` explicitly. **Kiln-rendered `form` nodes default `enctype=application/json` so kiln+CRUD keeps working.**

- **SSR auth via policy chain.** `core-ui/app` adds `Policy { Decide(ctx) Decision }`, `Decision` (Allow/Redirect/RenderAlt/Block), `RenderResult`, `Screen.WithPolicy`, `NewScreenGroup(prefix, layout, policies...)`, `SubGroup(prefix, layout, policies...)`. Construct decisions via the `decide` subpackage: `decide.Allow()`, `decide.Redirect(url)`, `decide.RenderAlt(factory)`, `decide.Block(status, msg)` — subpkg exists so call sites don't shadow common variable names. `battery/auth` adds `SessionPolicy(opts...)`, `RolePolicy(roles []string, opts...)`, `SessionFrom(ctx) (User, bool)`, `Roles(...)` ergonomic-list helper. `RenderAlt(factory)` MUST take a factory closure that returns a fresh component per request — singleton would race across users.

- **ContextComponent + ContextOnly.** New `component.ContextComponent { RenderCtx(ctx) HTML }` interface (does NOT embed Component). For ctx-only screens, embed `component.ContextOnly{}` to satisfy `Component` with a no-op `Render` — the framework prefers `RenderCtx` and never calls the stub. Doc example in `framework/docs/content/ui-getting-started.md`.

- **`owner_field` in entity JSON declarations.** Mirrors `EntityConfig.OwnerField`. Per-user CRUD scoping works in JSON-declared entities too.

- **DevMode mints a random JWT secret** when `JWTSecret == ""` (32 cryptographically-random bytes via `crypto/rand`, base64-encoded, logged WARN). Sessions invalidate on restart; set `JWTSecret` for stability.

- **Middleware type unified.** `core/router.Middleware` is now a type alias for `core/middleware.Middleware` — no more anonymous-func cast when feeding `battery/auth.SessionMiddleware(mgr)` into `Router.Use(...)`. Note: the `core/middleware/tracing_test.go` test moved to `package middleware_test` to break a test-only cycle the alias introduces.

- **Partial-redirect dispatch via header, not 303.** `handlePartialPage` on `DecisionRedirect` returns 200 + `X-Gofastr-Location` + empty body. The runtime fetcher in `core-ui/runtime/runtime.js` checks for that header on the partial response and `loadPage(redirectTo)` itself — replacing `pushState` with the redirect destination. A 303 here would be silently auto-followed by `redirect:'follow'` and the header would never reach JS.

- **SECURITY: `/auth/register` no longer honors client-supplied `roles`.** Was an anonymous privilege-escalation — anyone POSTing `roles=admin` (form OR JSON) became admin. Now roles are server-assigned (`[]string{"user"}` default). Tests in `battery/auth/register_roles_security_test.go` pin this.

- **TagInput Enter race.** Chromium dispatches the implicit form submit despite a bubble-phase `preventDefault` on a single-input form. Fix is a same-tick guard: keydown handler stamps `performance.now()` into `__fuiTagInputLastEnter`; a document-level capture-phase submit listener swallows submits within 50ms of that stamp. Outside the window, legit submits (Save button click) proceed normally.

- **Router.RenderRaw + App.RenderScreenRaw.** Renamed from `Router.Render` / `App.RenderScreen` to call out that they bypass the Policy chain. Use `App.RenderPageResult` for HTTP-serving paths; `RenderRaw` is for SSG/internal.

- **Mutex copy fixed.** `core-ui/app.Screen` contains a `sync.Mutex`; the prior `tmp := *screen` in `renderComponentInScreen` triggered `go vet` and was a real corruption risk. Replaced with the free function `wrapByScreenType(t, title, content)` reused from `Screen.RenderCtx`.

- **Next time:** when designing a Decision-shaped option API, factory closures (not singleton instances) are the safe default — anything the framework will Inject/Load/SetParams on must be per-request. When building a runtime opt-in mechanism that affects browser-native behavior, ship the inverse migration audit checklist alongside (grep targets, common breakage shapes, expected error symptoms) — for round 2 those are documented in this file and in `core-ui/ARCHITECTURE.md`'s Forms section.

## 2026-05-22 - worktree-isolation-mode

- Scope: `framework/isolation`, `framework.App.Start`, `cmd/gofastr dev`, generated app entrypoints, `docs/isolation.md`
- Symptom: linked Git worktrees could collide with the main checkout on `PORT`, SQLite files, Postgres database names, and service env values. Port isolation can be applied at `App.Start`, but DB/cache isolation must happen before app code opens clients.
- Change: isolation is a first-class runtime resolver. `App.Start` remaps listen ports, `gofastr dev` passes isolated child env, and generated apps call `Runtime.Database` before `sql.Open`. Config lives under `isolation:` in `gofastr.yml`, with worktree-only activation by default and `GOFASTR_ISOLATION=off` as the process escape hatch.
- Next time: if a new resource is opened before `App.Start`, wire it through `framework/isolation` or an env template. Do not assume the framework can rewrite an already-open connection.

## 2026-05-20 - blueprint-entity-list-e2e

- Scope: `cmd/gofastr/blueprint.go`, `cmd/gofastr/blueprint_test.go`, `docs/blueprints.md`
- Gap: generated screens could render static YAML UI and generated CRUD existed separately, but the blueprint shape had no data-aware UI block proving the generated browser could read generated CRUD data.
- Change: `kind: entity_list` now validates that it targets a CRUD entity and known fields, renders a refreshable table shell, and registers generated client JS that fetches the entity list endpoint through the generated app.
- Test rule: keep this covered in `TestBlueprintCLIGeneratesEntireWorkingAppE2E`; the browser should click the generated refresh action after creating real CRUD data and assert the DOM includes that data.

## 2026-05-20 - blueprint-real-app-e2e

- Scope: `cmd/gofastr/blueprint.go`, `cmd/gofastr/blueprint_test.go`, `docs/blueprints.md`
- Symptom: the generated-app E2E was not actually proving the YAML-to-app boundary; it wrote a hand-built temp `main.go` that opened DB, registered entities, mounted UI, and called `blueprint.RegisterGenerated` itself.
- Evidence: blueprint generation now emits `.gofastr/main.go` when `app.module` is set, and `TestBlueprintCLIGeneratesEntireWorkingAppE2E` runs the real CLI, builds `./.gofastr` into a binary, starts that binary, then drives HTTP CRUD, OpenAPI, `/mcp`, static assets, and browser UI/actions against the generated process.
- Next time: generated-app E2E must start the generated binary. Package-level harnesses are useful as build smoke tests only; they cannot be the acceptance test for app generation.

## 2026-05-20 - blueprint-theme-codegen

- Scope: `cmd/gofastr`, `docs/blueprints.md`, generated-app browser E2E
- Symptom: blueprint `app.theme` support needs both decode-time validation and generated `site.WithTheme(...)`; testing only emitted Go or raw CSS can miss whether the browser actually receives the generated app theme.
- Evidence: `cmd/gofastr/blueprint.go` decodes `app.theme`, rejects unknown color tokens, generates `BlueprintTheme()`, and `cmd/gofastr/blueprint_test.go` checks `getComputedStyle(document.documentElement)` for `--color-background`, `--color-primary`, and `--color-text` in the generated app.
- Next time: when adding blueprint app-level knobs, update decode, merge, validation, generated registration, docs, and the generated-app E2E path together.

## 2026-05-20 - wave-5-7-followups + adversarial review

- Scope: `framework/ui/{optimisticaction,networkretrybanner}`, `core-ui/patterns/scrollspy/`, `framework/uihost/` (SEO bundle), `core-ui/runtime/src/{optimisticaction,networkretrybanner,scrollspy}.js`, plus their demos/e2e in `examples/site/`
- Symptom: Wave 5 (OptimisticAction, NetworkRetryBanner) and Wave 7 (ScrollSpy, SEO head-wrapper) were the last unshipped items on the UI roadmap. Built them, then ran a 4-way parallel adversarial review of the diff; 10 concrete bugs surfaced (1 high-severity: OA's `Variant=ButtonPrimary` silently dropped the `ui-button--primary` class via a wrong `!= ButtonPrimary` guard; 1 security: OA fetch shipped without forwarding the page's `<meta name="csrf-token">`; the rest were a11y / multi-instance state / SPA-nav teardown / cssEscape leading-digit / bootstrap DOM-order / re-entrancy / doc/code mismatch).
- Evidence: TDD on every fix where deterministic reproduction was possible (failing test → fix → green). The harder-to-reproduce races (rollback timer clobber, IO disconnect) got fix-only with code-review verification. New e2e patterns established: (a) atomic high-water counter on a slow endpoint to assert "no concurrent fetches"; (b) recorder endpoint that captures incoming headers so a header-forwarding test can round-trip without inline `<script>`. ScrollSpy runtime now disconnects observers on `gofastr:navigate` and sorts targets by `compareDocumentPosition` before the bootstrap pick. NetworkRetryBanner state is per-banner via `WeakMap` + an iteration `Set`. OA runtime adds `aria-busy=true` + `disabled=true` during pending, clears them on commit/idle, and clears the rollback timer on a new click. ScreenSEO bundle struct landed in `framework/uihost`, threaded through `screenHeadHTML` with bundle-wins-over-per-concern + per-concern fall-through semantics for empty fields.
- Next time: when the public runtime API spans multiple instances (`__gofastr.networkStatus.reportFailure()` etc.), default to per-element state in a WeakMap on day one. The "single banner per page" assumption is almost always wrong by the time the third demo lands. Also: tests that overwrite `document.body.innerHTML` in chromedp don't refresh the runtime module's bound state — the module's IIFE already ran. Either re-navigate via `chromedp.Navigate` or expose a public `rescan(root)` API that re-binds without re-evaluating the module.

## 2026-05-19 - pattern-css-unification

- Scope: `core-ui/patterns/*`, `core-ui/check`, `examples/site/styles.go`, `core-ui/ARCHITECTURE.md`, `.claude/skills/component-build`
- Symptom: Two CSS contracts in the framework. `framework/ui/*` auto-wired via `registry.RegisterStyle` + `Style.WrapHTML` (CSS auto-loads on first appearance via the runtime's `data-fui-comp` scanner). `core-ui/patterns/*` exported `BaseCSS() string`, which every host app had to concatenate by hand into `WithCustomCSS`. A single missed concat shipped a component with no styling — the 2026-05-19 nestedlist incident.
- Evidence: All 6 patterns still on the legacy contract (accordion, breadcrumbs, nestedlist, pagination, progress, skeleton, tabs) migrated to `registry.RegisterStyle` + `Style.WrapHTML`. `BaseCSS()` exports removed; class selectors stay class-based. `examples/site/styles.go` lost 6 imports + 6 concatenations. `core-ui/check.LintNoPatternBaseCSS` + `TestNoPatternBaseCSS_RepoIsClean` enforce the new contract — any new pattern package that re-introduces `BaseCSS()` fails the build. Tabs's dynamic `:has()` rule generation became `buildCSS()` called from `styleFn`. Tests that asserted on exact `<nav aria-label="X">` strings relaxed to `aria-label="X"` since the wrapper now carries `data-fui-comp`. Skeleton-preset line widths moved from inline `Width:"50%"` (CSP-blocked) into the registered preset CSS. Architecture note added to `core-ui/ARCHITECTURE.md` ("Patterns use the same contract"); same rule added to the component-build skill as an anti-pattern.
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
- Symptom: planning tracker and task checkboxes were stale and still marked broad areas as not started, even though core primitives, batteries, CRUD, OpenAPI, hooks, events, plugins, and tests exist in code.
- Evidence: compared planning files with implemented packages under `core/`, `battery/`, `framework/`, and `cmd/gofastr/`; remaining gaps at the time were codegen-to-`.gofastr`, JSON entity loading, entity MCP auto-tools, DSL query parser, custom endpoint config, and production-grade CLI subcommands.
- Next time: assess roadmap status from source and tests first, then update the tracker separately instead of trusting unchecked boxes.
- 2026-05-21 follow-up: the entire planning tree (`plan/`, `draft.md`, `proposal.md`, `research-ui-approaches.md`) was removed. Per-feature truth lives in `docs/*.md` and the two `ARCHITECTURE.md` files; forward-looking work lives in `ROADMAP.md`. Git history is the only reference for the old planning shape.

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

- Scope: `framework`, `core/middleware`, `examples/site`
- Symptom: large batch of feature gaps shipped together — slow-query log, OpenTelemetry tracing, composite cursors, scoped includes, nested filters, streaming JSON for huge lists, audit log, cron scheduler, DB-backed queue, generated Go client + typed lifecycle hooks. Each was a separate proposal item; merging them as one batch kept the dependency graph (typed hooks → audit; cursor + tracing → SSE-through-metrics fix) coherent.
- Evidence: commits `36a224f..9251db1` on `main`; the batch lands with full-stack E2E coverage in `framework/e2e_*_test.go` and `examples/site/*_test.go`.
- Next time: when a proposal item depends on instrumentation another item adds, batch them. Splitting into independent PRs forces the dependency back through review.

## 2026-05-10 - filter-island-pattern

- Scope: `core-ui/runtime`, `examples/site`
- Symptom: filter/search was the third in-page-state pattern needed alongside pagination and sort, and it had to land as an island RPC (per `core-ui/ARCHITECTURE.md` rule 1) rather than a URL-based reload. The customers CRUD demo was wired end-to-end against the same pattern to prove the model holds for write-side state.
- Evidence: commits `9a693a8` (customers CRUD demo) and `9251db1` (filter island); `examples/site/*_test.go` exercises both.
- Next time: every new in-page state pattern that lands should be added to the runtime drift tests at the same time so future contributors can't accidentally reintroduce the route-based version.

## 2026-05-11 - ui-runtime-drift-tests

- Scope: `core-ui`, `framework/uihost`, `examples/site`
- Symptom: the architecture doc captures the contract in prose but the codebase had no automated check that someone hadn't reintroduced a hard-refresh path, an SSE-for-user-action, or an in-page state route. Three previous regressions on this contract had each been caught only by manual review.
- Evidence: commit `b691506` adds drift checks that fail CI if any of the three failure modes from `core-ui/ARCHITECTURE.md` reappear (verified by `go test ./examples/site/ -run TestE2E`).
- Next time: every documented rule that's been broken before needs a test that fails when it's broken again. The architecture doc itself shouldn't be relied on as the enforcement mechanism.

## 2026-05-17 - ten-ui-primitives

- Scope: `framework/ui/`, `core-ui/widget/preset/`, `core-ui/runtime/runtime.js`, `examples/site`, `core-ui/ARCHITECTURE.md`, `docs/ui-getting-started.md`, `docs/widgets.md`
- Symptom: the `framework/ui` package shipped Avatar / Button / Callout / StatCard / DataTable / Form / Menu / Notification / PageHeader / Sidebar / Toast — solid as far as it went, but a real app reaching for "card", "stack/grid layout", "tag chip", "tooltip", "checkbox/switch", "spinner", "divider", "file upload", "popover", or "responsive lazy image" had to hand-roll the HTML+CSS each time. Three example screens already had bespoke `display:flex` divs with inconsistent spacing.
- Evidence: this commit adds ten primitives (`Stack`/`Cluster`/`Grid`/`Center`/`Spacer`/`Box`, `Card`, `OptimizedImage`, `Checkbox`/`Radio`/`Switch`, `Tooltip`, `Popover`, `Tag`, `Spinner`, `Divider`, `FileUpload`) plus dogfooded demo screens at `/components/{layout,card,image,toggle,tooltip,popover,tag,spinner,divider,fileupload}`. 95 new unit tests under `framework/ui/`, 14 new chromedp E2E tests under `examples/site/`. Runtime gains a `_popoverStack` so non-modal floating surfaces honour CloseOnEscape + CloseOnClickOutside (previously modal-only — see `core-ui/runtime/runtime.js` lines ~1559–1605, ~2087–2110). Cheat sheet rows added to `core-ui/ARCHITECTURE.md`; `framework/ui/doc.go` lists the full component inventory.
- Next time: when extending a runtime feature (Escape / outside-click) so a new primitive can deliver on its docstring promise, the docstring change ships in the same commit as the runtime change. Don't ship a preset whose comments lie about behaviour.

## 2026-05-11 - docs-restructure

- Scope: `docs/`, `.claude/skills/`
- Symptom: README + architecture doc were solid, but `docs/*.md` had stub pages (~22 lines each) for security/migrations/search/query-dsl and was missing pages for half the surfaces the README advertised: batch, includes, events, cursor, multipart, hooks/tx, access control, multi-tenant, cron, audit, plugins, kiln. No mechanism existed to keep docs synced with API changes.
- Evidence: this commit expands the four stub pages with full surface tables and common-mistake callouts; adds 11 missing reference pages grounded in code reads; adds `.claude/skills/gofastr-docs/SKILL.md` that auto-loads on any code change so docs ship in the same commit as the API; adds `docs/README.md` index.
- Next time: a stub doc is a defect — either flesh it out or fold it into the README. Don't leave half-done reference pages that lie about the surface.

## 2026-05-21 - yaml-codegen-extensions

- Scope: `codegen`, `cmd/gofastr`, `docs`
- Symptom: `gofastr generate` used to be an entity-specific CLI path. The extensible surface is now a general codegen engine: YAML config selects generators, sources are structured inputs, and external commands speak a JSON protocol.
- Evidence: `codegen/` owns config discovery, source loading, safe file paths, manifest cleaning, in-process generators, and command extensions. `cmd/gofastr` registers `go/entities` and `go/client` built-ins while preserving no-config entity generation.
- Next time: do not add another special-purpose generator command first. Add a generator or extension path through `codegen`, then expose any CLI sugar as a thin wrapper.

## 2026-05-25 - framework-image-pipeline

- Scope: `framework/image/`, `framework/docs/content/image.md`
- Symptom: the framework shipped routing, persistence, UI primitives, auth, audit, and codegen — but no first-class image story. Anyone needing to take an uploaded photo and produce a thumbnail had to reach for CGo-heavy bindings (libvips, govips, bimg) or pull in a third-party pure-Go lib outside the Go-team libraries the project sticks to. Bun's recent `Bun.Image` released a chainable pipeline that the user wanted mirrored here under the stricter "stdlib + golang.org/x/image only" constraint.
- Evidence: this commit adds `framework/image` (chain API: Resize / Rotate / Flip / Flop / Modulate / AutoOrient + JPEG/PNG/GIF/BMP/TIFF encoders + Placeholder + BlurHash) — pure Go, zero CGo, only `golang.org/x/image` as a dependency beyond stdlib. Minimal EXIF orientation parser inline. Decompression-bomb guard defaults to 268 MP (Bun parity). `/framework-ui/image-pipeline` demo screen renders every operation against a synthetic gradient with `data-test` selectors. Lossless WebP / HEIC / AVIF return `ErrFormatUnsupported`; VP8L lossless encoder is planned as a follow-up under `framework/image/webp/`.
- Next time: when adding a feature with multiple potential codec backends, decide upfront which formats are in scope and which return a sentinel error — and surface that in the docs' format table. Pretending HEIC/AVIF "might come later" without writing the codec is worse than saying explicitly "not without CGo, not in scope."

## 2026-05-25 - framework-image-pipeline-vp8l

- Scope: `framework/image/internal/vp8l/`, `framework/image/`, `framework/ui/`, `framework/docs/content/image.md`
- Symptom: shipping the image pipeline with WebP-encode as `ErrFormatUnsupported` would leave a real capability gap — `cwebp` is the canonical "smaller than PNG" path for modern delivery, and without a Go-team encoder available we needed to write one. Adding it touched four moving parts: a pure-Go VP8L encoder, a typed VariantSet + PipelineImage layer so apps can plug image pipeline output into the rest of the framework without ceremony, and a demo + e2e proving the contract.
- Evidence: this batch adds `framework/image/internal/vp8l` (bit-writer, length-limited canonical Huffman via package-merge, RIFF framing, subtract-green + predictor transforms, LZ77 hash-chain match finder, 8-bit color cache); `framework/image.VariantSet` + `framework/ui.PipelineImage` (headless variant generator + multi-MIME `<picture>` component); `/framework-ui/image-pipeline` demo + e2e (chromedp asserts every transform produces a data URL, WebP `<source>` precedes JPEG, BlurHash is base83). Five test inputs round-trip pixel-equal through `golang.org/x/image/webp`. On size: 128×128 gradient is 0.86× PNG, 256×256 patches is 0.40× PNG, noise ties with PNG. Synthetic only — natural photos still trail `cwebp` because we ship a single global predictor mode rather than per-block selection.
- Next time: when implementing a complex codec, ship the smallest-correct version (Phase A: literal-only Huffman) FIRST and validate round-trip end-to-end before adding any size-optimisation. The bugs that surfaced (simple-vs-normal Huffman path mismatches, secondary-Huffman length cap, single-symbol decoder shortcut, Kraft-tight vs Kraft-slack via package-merge) would have been ten times harder to find with all four phases landed together. Each phase landed in its own commit so a regression bisect is a one-line `git bisect`.
