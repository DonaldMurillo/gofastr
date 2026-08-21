# Agent notes

## 2026-08-07 - Maturity audit → closed the 2026-07-26 eval's open items
- Scope: `cmd/gofastr` blueprint generator, `framework/openapi`, Postgres test infrastructure (`internal/pgtest`, `framework/internal/testdb`, CI, docker-compose), `framework/docs`, `framework/processmodule_migrate.go`
- Trigger: "Where is GoFastr, what are we missing, make the developer experience better." Audited first, then implemented all seven prioritised findings TDD-style.
- Approach: Measure before believing. The audit ran the cold-start path rather than reading status docs, which is how it found that **five of seven shipped blueprints emitted Go that did not compile** (`undefined: context`, `undefined: resource`), two import-set bugs whose common shape is a condition re-deriving what the emitter already decided. Only `examples/ecommerce` and `examples/meridian` ever had their generated Go compiled, and ecommerce is the single example declaring neither a home screen nor an `access:` policy, so it reached neither broken path. Also closed eval next-moves 1, 2 and 6 (OpenAPI prefix parity, repeated-start migration coverage, backend capability map), removed testcontainers from the consumer module graph (95→57 modules), and documented `sqlite/`.
- Evidence: PR #200, 12 commits, all 11 CI checks green on `2021cfc`. `TestExampleBlueprintsGenerateAndCompile` red on five blueprints before the fix, green on seven after. Every test that passed on first write was mutation-checked before being trusted.
- Gotcha: **A gate aimed at one fixture proves one fixture.** Both P0-class defects lived in paths the single gated example never touched. The same lesson repeated three times in one PR: the empty `<th>` was caught only by an 11-minute axe crawl, and `scripts/coverage-floors.sh` called `profile_for` inside a command substitution, so a *blocking* gate swallowed the failing package's test log and reported "tests failed (no coverage measurement)" with no test name for three CI runs.
- Next time: When a test suite moves from per-process ephemeral Postgres containers to one shared server, everything holding **cluster- or database-scoped** state breaks on the second run: fixed schema names, pid-less per-test schemas, and roles. Chasing that class is what surfaced a real production bug: `provisionModuleSchemaRole` creates the module role inside `DO $$ … CREATE ROLE … EXCEPTION WHEN duplicate_object THEN null $$`, which is idempotent for the role's *existence* and a silent no-op for its *password*, so process-module migrations failed with `28P01` on every deploy after the first. It was unreachable in CI while each test process got a throwaway container. Prefer a plain unit test over a live-infrastructure one wherever the invariant is expressible in a string.
- Status: active

## 2026-08-01 - Browser Reader Mode (`app.AsArticle()` + `ScreenArticle`)
- Scope: `core-ui/app`, `framework/uihost`, browser reader-mode readiness
- Trigger: User wanted pages marked up so Safari Reader / Firefox Reader View detect them and render them well (the browser's built-in feature, NOT a layout "reader mode" CSS shape. An earlier attempt built the wrong thing and was reverted). Then refined: it had to be SEAMLESS, "turn any normal screen into an article", not write a metadata method + duplicate the title.
- Approach: Two ways in, one seam. `app.AsArticle()` is a `ScreenOption` (`func(*Screen)`) that sets `Screen.Article`; the framework then wraps content in `<article>` (app: both SSR+SSG flow through `RenderPageResult`, no-layout/SSG through `renderComponentAs`) and synthesizes Article JSON-LD + `og:type=article` (uihost `resolveScreenSEOFor`), DERIVING headline/description from the screen's own `ScreenTitle`/`ScreenDescription`, with zero article-specific data. The optional `ScreenArticle()` interface enriches with author/date/image and also marks the screen an article. `wrapArticle`/synthesis trigger on flag OR interface; only gaps are filled so explicit `ScreenSchema`/`ScreenSEO` win.
- Evidence: `go test ./core-ui/app/ -run 'TestScreenArticle|TestAsArticle'` and `./framework/uihost/ -run 'TestScreenArticle|TestAsArticle'` green (wrap, synthesis, schema de-dup, OG preservation, title-derivation). Live `/reader` (a plain screen + `app.AsArticle()`) emits `<article>` + Article JSON-LD with headline/description derived from the screen title + og:type=article; non-article pages stay og:type=website with no `<article>`.
- Gotcha: value (non-pointer) struct screen components 404, because the host only serves pointer-to-struct screens (`newInstance` assumes pointers). Always register `&Screen{}`.
- Next time: The dominant reader-mode signal is the `<article>` element + real paragraph density. No config fakes article content, so `Render()` must be actual prose. `RenderRaw` (SSG/internal) has a pre-existing double-`<main>` when combined with a layout; SSG ships via `host.RenderStaticPage`→`RenderPageResult` (clean), so it doesn't surface.
- Status: active

## 2026-07-20 - Framework maturity review
- Scope: architecture, release readiness, documentation
- Trigger: Assess where GoFastr stands after v0.38.0.
- Approach: Cross-check roadmap claims against implementation/tests before treating them as open work.
- Evidence: PR #122 removed the stale `ROADMAP.md` 7.1-7.6 findings and corrected the obsolete `AuthorizeTopic` example and Go-version reference after cross-checking them against implementation and tests.
- Next time: Cross-check status docs against implementation first, then prioritize external adoption and the v1 public-API freeze over adding surface area.
- Status: active

## 2026-07-26 - Codex backend vertical-slice benchmark
- Scope: backend framework behavior and AI-assisted workflow
- Trigger: Compare real non-deterministic agent outcomes, token use, and maintenance behavior across GoFastr, Gin, and `net/http`.
- Approach: Treat the result as a backend-heavy vertical-slice study; do not infer full-stack adoptability without equivalent UI/product requirements and scoring.
- Evidence: `evals/backend-adoption/results/2026-07-26-codex.md`; GoFastr averaged 95 cold-start points and 313,579 tokens versus Gin's 100 and 72,172.
- Next time: Fix API-prefix/OpenAPI parity, then run separate backend-only and full-stack product lanes with equivalent guidance and 10+ repetitions.
- Status: active

## 2026-07-22 - Audit skips must distinguish lane boundaries from drift
- Scope: `gofastr audit lint`, browser E2E
- Trigger: Missing gallery fixtures were silently hidden behind `t.Skip`, while legitimate `testing.Short()` guards produced noise.
- Approach: Allow canonical short-lane guards, fail hard for missing required fixtures, and keep SQL detection anchored to actual statement forms.
- Evidence: `go run ./cmd/gofastr audit lint ./examples/site` reports zero findings; focused banner/tree E2E tests pass.
- Next time: Use skips only for declared environment or lane constraints; treat missing in-repo UI contracts as failures.
- Status: active

## 2026-07-23 - Component galleries still need a real document outline
- Scope: site component demos and example screens
- Trigger: The full axe crawl found nested main landmarks, heading skips, and repeated default nav labels that isolated component tests allowed.
- Approach: Let the site layout own the sole main landmark, give demo stages an h2 before component-owned headings, and configure unique landmark labels when examples repeat a navigation primitive.
- Evidence: Targeted runtime axe scans report zero violations on all ten affected routes in both schemes; the all-pages axe test passes.
- Next time: Test reusable primitives in a composed page shell as well as in isolation.
- Status: active

## 2026-07-22 - Runtime budgets should drive module boundaries
- Scope: `core-ui/runtime`
- Trigger: Core and widget gzip budgets required overrides after optional behavior accumulated in large modules.
- Approach: Split optional widget helpers, focus handling, and deep-link behavior into demand-loaded modules; keep strict budgets override-free.
- Evidence: `go test -run 'TestRuntimeModuleSizeBudgets|TestTypicalPagePayloadBudget|TestRuntimeJSSyntax' ./core-ui/runtime` passes with no overrides.
- Next time: Add optional behavior behind a marker-driven or parent-module-driven split before raising a payload budget.
- Status: active

## 2026-07-22 - Route-aware mounts preserve framework diagnostics
- Scope: framework routing and UI host integration
- Trigger: Entity/page collisions were actionable only when entities were registered second.
- Approach: Let mountables optionally expose concrete `RoutePatterns` and check them against registered entity CRUD paths before mounting.
- Evidence: `TestEntityScreenCollision` covers both registration orders in `framework/collision_test.go`.
- Next time: Give framework-owned mount adapters route introspection so domain-specific diagnostics run before generic router panics.
- Status: active

## 2026-07-22 - Auth-owned presets avoid framework import cycles
- Scope: browser-backend authentication posture
- Trigger: A proposed `framework.WithBFFPosture` needed `AuthManager`, but `battery/auth` already imports `framework`.
- Approach: Expose `auth.WithBFFPosture` as a `framework.AppOption`; derive its API boundary from `AppConfig`, and exempt only the auth-owned logout path from generic CSRF because that handler enforces same-origin submission itself.
- Evidence: BFF tests cover untrusted origins, cookie-only login, both API-prefix option orders, and the real `ui.SignOut` flow.
- Next time: Keep one owner for shared security boundaries and test public components through the complete middleware stack.
- Status: active

## 2026-07-22 - Group public config additively before removal
- Scope: `entity.EntityConfig` API evolution
- Trigger: Related scope, pagination, and exposure fields were flattened across a large public struct.
- Approach: Add pointer sub-configs as authoritative groups, normalize them into the existing runtime fields, and retain deprecated flat fields through the documented compatibility window.
- Evidence: Entity normalization and grouped blueprint generation tests cover both Go and declaration paths.
- Next time: Introduce grouped public shapes additively and keep one normalization boundary instead of rewriting every downstream consumer at once.
- Status: active

## 2026-07-23 - Public variants and security posture need hostile contract tests
- Scope: sidebar variants and BFF authentication posture
- Trigger: Exported sidebar variants were cosmetic, while auth tests missed configured cookies, whitespace bearer forms, exact logout paths, and ordinary OPTIONS.
- Approach: Test public configuration through rendered markup, demand-loaded runtime behavior, a real browser, and the complete middleware stack.
- Evidence: Focused auth, runtime, component, and sidebar browser tests cover every corrected boundary.
- Next time: Attack the configured surface rather than a convenient default, and require behavioral E2E coverage before calling an exported variant implemented.
- Status: active

## 2026-07-26 - Make alternative security controls visible to audit lint
- Scope: `gofastr audit lint`, auth form security
- Trigger: Whole-repo lint flagged the magic-link confirmation form even though `rejectCrossSiteForm` and hostile tests protect its POST.
- Approach: Keep the behavioral security test and place the supported `csrf-exempt:` explanation beside forms protected by an equivalent control.
- Evidence: `go run ./cmd/gofastr audit lint .` reports `battery/auth/magiclink.go:415`; `TestMagicLinkConfirmRejectsCrossSite` pins the guard.
- Next time: Run audit lint at repository scope and document intentional alternative controls where the heuristic can verify them.
- Status: active

## 2026-08-16 - Measure the discovery-tax fix and close the WithConfig footgun
- Scope: `evals/backend-adoption` rerun, `framework.WithConfig`, `gofastr init` scaffold
- Trigger: PR #200 left two reviewer items open. The capability map's effect was unmeasured, and an option placed before `WithConfig` was silently discarded.
- Approach: Rerun the eval's GoFastr lane with Gin as a drift control on the same codex-cli 0.145.0; keep WithConfig's replace semantics but scaffold it first and warn at boot naming discarded fields.
- Evidence: `evals/backend-adoption/results/2026-08-16-codex.md`. Cold-start 313,579 → 233,716 tokens against a control that rose 34%, ratio 4.35× → 2.42×; `framework/withconfig_order_test.go` and `TestInitScaffoldsWithConfigFirst` pin the fix.
- Next time: When a run fails a probe, read the candidate's code before blaming the framework: run 1's isolation miss was a hand-rolled handler validating before the owner-scoped lookup, with the framework's contract warning suppressed.
- Status: active

## 2026-08-17 - Seed error hygiene: values never reach logs; refuted bigint alias claim
- Scope: `cmd/gofastr` blueprint emitter/validator, `examples/ecommerce/app` regeneration
- Trigger: CodeRabbit round. Generated apps labeled failed seed rows by VALUE (title/email/body, falling back to the whole row map) in a log.Fatal path; boot-gate probe used unbounded http.Get; seed type validation returned on first map-iteration-ordered rejection.
- Approach: `seedCreateError(entity, index, err)` now labels rows by one-based position only (seeds carry admin emails/passwords); `bootProbeClient = &http.Client{Timeout: 5s}` bounds the readiness probe; `validateBlueprintSeedTypes` accumulates every finding via schemaErrors over sortedMapKeys, rows reported one-based to match boot.
- Evidence: `TestSeedErrorOmitsRowValues`, `TestBootProbeClientTimesOut` (fires at 5.00s against a hung server), `TestSeedErrorsReportEveryField`; `TestExampleBlueprintsBoot` and ecommerce byte-parity pass after regeneration.
- Next time: The canonical field-type alias table is `parseFieldType` (framework/entity/declaration.go), where int is `int|integer`, float is `float|number`, nothing else. A claim that `bigint`/`money`/`double`/`numeric` "fall through seed validation and die at boot" is wrong: `decl.Config()` rejects those types at validate time (blueprint.go entity loop), so no generated app can carry them. `blueprintGenerateSeedRows`' extra case labels are dead aliases. Align the VALIDATOR to parseFieldType, never to the generator's list.
- Status: active
