# Agent Notes


## 2026-07-08 - scaffold subcommands (issue #20 stage B part 2b)

* **Scope:** `cmd/gofastr/generate.go` (runGenerate dispatch + extracted additive core), new `cmd/gofastr/generate_scaffold_test.go`, `framework/docs/content/{blueprints,entity-declarations,agent-notes}.md`.
* **Change:** `gofastr generate entity <name>` and `gofastr generate screen <name>` are back as quick-stub scaffolders (the old tombstone is gone). Each synthesizes a minimal one-piece `Blueprint` fragment in memory — an entity with one placeholder `name` field, or a screen at `/<kebab-name>` with a heading + stub paragraph — and routes it through the SAME additive path `--add` uses. To do that without duplicating logic, the additive core was extracted from `generateFromBlueprint` into `generateBlueprint(bp, options)` (the yml-loading path just loads then calls it); `--add` behavior is byte-identical (the additive test suite is unchanged and green).
* **Key decisions:** (1) NO blueprint-grammar extension — the stub is expressed with the existing `EntityDeclaration`/`BlueprintScreen`/`BlueprintBlock` shapes, so skip-existing, order continuity, union validation, the legacy-layout refusal, the client.go staleness warn, and the `--dry-run`/`--json` shapes (`skipped_existing`) all fall out of the additive machinery for free. (2) The scaffold forces `options.add = true` internally; `--force` and `--add` are REJECTED on the subcommand (scaffolding is additive by definition — say so in the error). (3) `parseScaffoldArgs` is its own flag parser (only `--out`/`--dry-run`/`--json` + exactly one positional), NOT `parseGenerateOptions`, so the scaffold never inherits the blueprint-only flags. (4) `generate theme <name>` is deliberately NOT provided — theme tokens live in owned `app.go`, there is no new-file representation.
* **Gotcha that bit the tests:** `addSetup` (generate_add_test.go) hardcodes the go.mod module as `example.com/addtest`; base blueprints passed to it MUST declare that same module or `resolveBlueprintModule` mismatches and `osExit(1)`s silently during base generation (the scaffold call itself is fine). The empty-dir scaffold tests use their own `writeAddGoMod(... "example.com/scaffold")` and synthesize a module-less fragment, so `resolveBlueprintModule` derives it — no mismatch there.
* **Next time:** when a subcommand wants to reuse `--add`'s machinery, extract/accept an already-loaded `Blueprint` rather than re-running the loader — the additive path's invariants then hold by construction. And remember the coverage-harness capture loss: `covT_capStdout` does not return its string when the inner fn `osExit`s (the sentinel panic unwinds through it first), so assert exit-via-`osExit` behavior with `covT_capExit` and usage TEXT by calling the non-exiting parser (`parseScaffoldArgs`) directly.

## 2026-07-08 - per-piece screen generation (screen layout split)

- **Scope:** `cmd/gofastr/{generate.go,generate_typed.go,pack.go}` + layout tests; `examples/meridian/screen_*.go` + `screens_register.go` regenerated; `framework/docs/content/{blueprints,entity-declarations,tutorial-blueprint-app,agent-notes}.md`.
- **Change:** generated screens are now ONE FILE PER SCREEN instead of one aggregated `screens.go`. A fixed seam `screens_register.go` defines `screenRegistrar{order int, fn func(fwApp *framework.App, site *app.App, db *sql.DB)}`, a `screenRegistrars` slice, and `mountGenerated(...)` which sorts registrars by `order` and runs them — byte-identical for any screen/entity count, naming no screen/entity/app. Each `screen_<snake>.go` (authored non-CRUD screen) or `screen_<entity>_crud.go` (an entity's list/detail/new/edit screens) appends one `screenRegistrar{order: N, fn: mount…}` in `init()`. Add a screen = a new file; the seam is never edited.
- **Key invariant:** an entity's `appResources["<entity>"] = ResourceConfig{...}` wiring moved OUT of `app.go`'s `RegisterGenerated` and INTO the entity's `screen_<entity>_crud.go` mount func (it needs `fwApp`). `RegisterGenerated` now calls `mountGenerated(fwApp, site, db)` and names no screen type or `appResources` entry; `appLayout`/`marketingLayout` are package-level vars it assigns. `app.go` still owns layouts, nav, theme, `authPolicy`/`guestPolicy`, auth, toasts, endpoints.
- **Key decisions:** (1) authored screen order is observable — `gofastr pack` recovers it from each file's `screenRegistrar{order: …}` — so init()-based self-registration keyed by an explicit `order` is behavior-preserving even though init() runs lexical-by-filename, not declaration order. (2) pack is dual-path: reads the per-screen `screen_*.go` files + their `screenRegistrar` orders when present, else falls back to the legacy aggregated `screens.go` so old apps still pack. (3) `--add` now covers screens too (not just entities): new screen files continue after the project's existing max screen order, an entity fragment also emits its `screen_<entity>_crud.go`, and `--add` refuses the old aggregated `screens.go` layout with the same recover-with-`gofastr pack` message shape as the entities guard. `parse(yml) ≡ parse(pack(generate(yml)))` still holds.
- **Next time:** when adding a generated screen construct, emit it into its own `screen_<name>.go` and append one `screenRegistrar` in `init()` — never aggregate into the seam. Teach pack's per-screen reader the new reverse grammar in the same change, or the round-trip test breaks.

## 2026-07-08 - per-piece entity generation (issue #20 stage A)

- **Scope:** `cmd/gofastr/{generate.go,generate_typed.go,pack.go}` + layout tests; `examples/meridian/entities/*` regenerated; `framework/docs/content/{entity-declarations,tutorial-blueprint-app,blueprints}.md`.
- **Change:** generated entities are now ONE FILE PER ENTITY — `entities/<snake>.go` (model, columns, repo, events, and its own `register<Camel>` self-registration) plus a thin `entities/register.go` seam that is byte-identical regardless of entity count. Adding an entity = a new file; existing files are never rewritten. The client package stays aggregated; `gofastr generate entity <name>` subcommands (stage B) are out of scope.
- **Key decisions:** (1) registration order is runtime-unobservable (Registry is map-only; migrate topologically sorts; routes keyed by name/table), so init()-based self-registration is behavior-preserving even though init() order is lexical-by-filename, not declaration order. (2) To keep `gofastr pack`'s round-trip exact on non-lexical entity names, each entity file emits `registrar{order: N, fn: registerX}` and RegisterAll sorts by `order`; pack reads the order literal back. (3) pack is dual-path: reads per-entity files when present, else falls back to the legacy inline `register.go` RegisterAll so old apps still pack. (4) collision guard: an entity named `register`/`shared`/`doc` lands as `entity_<name>.go`.
- **Fence note:** pack.go and examples/meridian are outside the brief's literal file list but coupled to the generator — pack is "the inverse of generate" and pack_test asserts the layout (in-fence); the flagship is committed generator output the pack round-trip reads. Updated both for coherence; neither touches the battery/auth worker's subtree.
- **Next time:** regenerating `examples/meridian/entities/` must go through the gofmt step — a raw `renderBlueprintFiles` write skips struct-tag alignment and shows spurious client.go churn. Stage B will need to compute the next `order` index by scanning existing entity files (each is self-contained).
## 2026-06-25 - adversarial review pass on agent-ready (3 reviewers)

- **Scope:** the agent-readiness feature (`framework/uihost/agentready.go`, `seo.go`, `oauth_resource.go`, `core/mcp`).
- **Trigger:** 3 parallel adversarial reviews (security, spec-compliance, correctness) after implementation-first authoring.
- **Findings:** AdvSec — 0 (CRLF-in-`Link:`-header verified non-exploitable: Go's request parser rejects CR/NUL values with 400, bare-LF terminates into a separate header). AdvSpec — A2A card non-conformant: snake_case keys (spec mandates camelCase), `skills` deleted instead of `[]`, REQUIRED `supportedInterfaces` omitted, non-v1.0 top-level `url`. AdvLogic — AI-bot `Allow:/` groups shadowed the host's path-specific `Disallow` (RFC 9309 most-specific-group); GPTBot would crawl `/__gofastr/` on gofastr.dev.
- **Fixes:** camelCase keys; `skills` always `[]`; `supportedInterfaces` now advertises the `/mcp` JSON-RPC endpoint (REQUIRED field present, honest — initialize/tools-list work) and the top-level `url` dropped; allowed AI bots moved into the main `User-agent` group as consecutive lines so they inherit host rules. Added a regression test (`TestRobots_AIBotAllow_InheritsHostDisallow`) — the original `TestRobots_AIBotBlock` used an empty `RobotsConfig` and so never exercised the conflict.
- **Next time:** when a review's finding conflicts with an earlier advisory, the conformance evidence wins — the earlier "omit supported_interfaces" call was wrong because the field is REQUIRED. And: tests that pair a feature with an *empty* config don't catch interaction bugs — always add a case combining the feature with a populated sibling config.

## 2026-06-25 - agent-readiness discovery surface (isitagentready.com)

- **Scope:** `framework/uihost/{agentready.go,seo.go,uihost.go}`, `framework/{app.go,oauth_resource.go}`, `core/mcp/{protocol.go,server.go}`, `framework/docs/content/agent-ready.md`, `examples/site/main.go`.
- **Trigger:** make GoFastr apps score on isitagentready.com — the framework already had the *plumbing* (MCP, OpenAPI, `/llm.md`, sitemap, robots) but not the agent-*discovery* surface.
- **Change:** opt-in `uihost.WithAgentReady` bundle serves `/llms.txt` (llmstxt.org), the A2A `/.well-known/agent-card.json` (+ legacy `agent.json`), AI-bot-aware robots rules, `Link:` response headers on every HTML page, and markdown `Accept` negotiation. `framework.WithMCP` auto-mounts `/mcp` (was host-hand-wired); `framework.WithOAuthProtectedResource` adds RFC 9728. Absolute discovery URLs resolve one canonical origin (`WithAgentReady`/`WithSitemap` BaseURL → forwarded request).
- **Key decisions:** the A2A card conforms to v1.0 — camelCase JSON keys (ADR-001), `supportedInterfaces` (REQUIRED) advertises the `/mcp` JSON-RPC endpoint (it genuinely speaks JSON-RPC — `initialize`/`tools/list` work), `skills` always emitted as `[]` when empty, and no top-level `url` (the endpoint lives in `supportedInterfaces[].url`). AI-bot robots rules merge into the main `User-agent` group (consecutive UA lines, RFC 9309) so allowed bots inherit the host's `Allow`/`Disallow` — a standalone `Allow:/` group would shadow path-specific exclusions. Default `/llms.txt` links only the `/llm-pages.md` index, never per-route `.md` (non-screen routes like `/api/*`, `/healthz`, `/.well-known/*` have no markdown).
- **Next time:** advertising an MCP endpoint obligates a working handshake — `core/mcp` only dispatched `tools/list` + `tools/call`, so the advertised `/mcp` returned "method initialize not found" for spec-compliant clients (Claude/Cursor call `initialize` first). Added `initialize` + `ping` to the dispatch; when exposing any RPC endpoint via a discovery artifact, verify the client's first call succeeds, not just the one you test. Smoke-test binaries go stale silently — force-rebuild (`rm` the binary) or use a fresh port per run; a held port makes a new server fail to bind and curls hit the old process.
- **Status:** active

## 2026-06-11 - current-state review verification

- **Scope:** framework architecture, release docs, CI/test reliability.
- **Trigger:** a deep review of `v0.5.0` found the code healthier than several repository status surfaces, while the short full gate failed a headline browser security test that passed repeatedly in isolation.
- **Approach:** run `SHORT=1 ./scripts/test-all.sh`, rerun browser failures isolated with `go test -count=5 -run <test>`, then compare `ROADMAP.md`, `SECURITY.md`, Make targets, and verification scripts against current package paths and source.
- **Evidence:** `TestUIE2E_OwnerScope_CrossUserIsolation` relies on a correctness-bearing `200ms` sleep and failed under suite load; the review also found stale Roadmap §9 statuses and worktree scripts referencing the retired `framework/apiversions` path, corrected in the follow-up.
- **Next time:** classify full-suite browser failures before trusting a green isolated rerun, and derive roadmap/release status from executable witnesses before documentation labels.
- **Status:** active

## 2026-06-10 - boot auto-migrate adds missing columns (additive convergence)

- **Scope:** `framework/migrate/{migrate,schema_diff,bulk}.go`, `framework/migrate_addcolumn_test.go`, `framework/docs/content/{migrations,deploy,tutorial-blueprint-app,perf-results,benchmarks}.md`, `kiln/db/migrate.go` (comment only).
- **Symptom:** docs (deploy.md "create tables, add columns"; migrations.md's will-not list) promised column convergence, but boot `AutoMigrate` only did `CREATE TABLE/INDEX IF NOT EXISTS` — adding a field to an existing entity and rebooting hit "table notes has no column named user_id". Two dogfood workarounds existed for the gap: kiln's `alignColumns` and the tutorial's declarative-diff-apply detour (that CLI path has since been removed in favour of `migrate generate`).
- **Change:** `AutoMigratePlanContext` pre-reads live columns in one bulk query (both dialects; replaces the PG-only `TableExistsBulk` call — emptiness doubles as the existence check), and `migrateEntity` converges existing tables additively via the shared `diffEntityFromLive` path with `Destructive` changes filtered out. On drift, columns are re-read on the advisory-lock-holding tx before ALTERing, so racing PG replicas no-op instead of dying on a duplicate column. Column adds run before index DDL so a field+index arrive in one boot. PG live-schema readers now match table names case-insensitively (unquoted DDL folds to lowercase — previously mixed-case tables read as "missing" in `DiffSchema` too). Drops/renames/retypes stay behind an explicit, reviewed `migrate generate` migration (the auto-migrate path never emits destructive DDL). Idempotent re-run cost ~2× (PG N=50: 1.6 ms → 3.4 ms same-machine); perf-results.md §7f carries the honest update.
- **Next time:** `MigrateEntity`/`MigrateEntityDialect` (single-entity, registryless) intentionally stay create-only; if a NOT NULL tightening story is wanted after backfill, that's a versioned-migration concern, not boot's.

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

## 2026-06-08 - remove-entities-json-format

- Scope: `framework` (`app.go`, `entity/declaration.go`, `reexports_entity.go`), `cmd/gofastr` (`generate.go`, `generate_watch.go`, `new.go`, `build.go`, `migrate_generate.go`, `migrate_diff.go`, `main.go`), `cmd/kiln/freeze.go`, `kiln/*` + `framework/*` tests, `framework/docs/content/*`
- Symptom: the project carried TWO declaration formats — the standalone `entities/*.json` files (loaded via `App.EntitiesFromDir`/`LoadEntityDeclarations`, scaffolded by `gofastr new entity` / `gofastr generate entity`, the default `gofastr generate` json_dir path) AND the `gofastr.yml` blueprint. The blueprint is a strict superset: it decodes into the same `framework.EntityDeclaration` and additionally emits `main.go`, screens, and stubs. The JSON-file format was dead weight that duplicated surface and contradicted the "one declaration → many surfaces" thesis. This reverses the `go/entities`/`go/client` "no-config entity generation" built-ins introduced in the 2026-05-21 `yaml-codegen-extensions` entry, while keeping the general `codegen` extension engine intact.
- Evidence: this change deletes the JSON disk loaders + the three `App.*FromDir`/`EntityFromFile` runtime loaders + the two re-exports + the `generate entity`/`new entity` scaffolders + the json_dir default-generate path + the `--entities` flag; `gofastr generate` now requires `--from=<blueprint.yml>` (or a `gofastr.codegen.yml`); `gofastr migrate generate|diff` take `--from=<blueprint.yml>`. The shared `EntityDeclaration`/`.Config()` + render helpers stay (the blueprint path reuses `renderGeneratedProject`). `go build ./...` and `go vet ./...` clean; cmd/gofastr + framework (`-short`) + framework/entity + kiln/freeze + kiln/integration roundtrip tests green. `gofastr init`'s Go `entities/entities.go` scaffold was deliberately NOT touched — it emits Go `app.Entity(...)` code, not the JSON format.
- Next time: `gofastr.yml` is overloaded — it is both the `gofastr init` isolation config and the blueprint. That ambiguity is why `gofastr generate` does NOT auto-discover `gofastr.yml` (an isolation config has a `version:` key the blueprint parser rejects); generation must be explicit via `--from`. A future cleanup should split these into distinct filenames (e.g. `gofastr.blueprint.yml`) so discovery can be safe. Also: removing a public loader ripples into experimental `kiln freeze`, whose output format (`entities/*.json`) now has no framework loader — graduating that to emit a blueprint is a tracked follow-up.

## 2026-07-04 - blueprint-flat-package-main

- Scope: `cmd/gofastr` (`blueprint.go`, `generate.go`, `pack.go` + blueprint tests), `examples/meridian/*`, `README.md`, `CHANGELOG.md`, `framework/ARCHITECTURE.md`, `framework/docs/content/{blueprints,tutorial-blueprint-app,project-structure,overview,entity-declarations,comparison}.md`
- Symptom: `gofastr generate` scaffolded a `blueprint/` subpackage (`package blueprint`: app.go/screens.go/resource.go/stubs.go/resource_test.go) beside `main.go`. Two cold-start evals showed the folder read as "the generator's" — users treated it as off-limits, custom screens were forced into it (the unexported `blueprintAuthPolicy` etc. lived there), and the input `.yml` and output package confusingly shared the name "blueprint".
- Evidence: those files now emit as a flat `package main` at the project root (`entities/` unchanged); `main.go` calls `RegisterGenerated(...)` directly instead of importing a `blueprint` package. Every emitted `Blueprint*`/`blueprint*` identifier dropped the branding to a neutral unexported name (`BlueprintAppName`→`appName`, `blueprintAuthPolicy`→`authPolicy`, `BlueprintBaseCSS`→`appBaseCSS`, `BlueprintFontCSS`→`fontFaceCSS`, `blueprintResources`→`appResources`, `BlueprintSeedData`→`seedData`, `openBlueprintDB`→`openDB`, `TestBlueprintE2E`→`TestE2E`, the emitted chart/format helpers, etc.). `generate` is now one-shot: it refuses to write into a directory that already holds any target file (listing conflicts) unless `--force` — no merge/regen workflow. `pack` reads the flat layout. `examples/meridian` regenerated to the new layout (entities/ byte-identical, static/app.css + fonts preserved). BREAKING for generator *output* only; existing generated apps are unaffected at runtime. `go build`/`go vet ./...` clean; cmd/gofastr + examples/meridian + kiln/freeze green; a fresh `/tmp` scaffold builds, boots, serves, and the second `generate` refuses without `--force` and succeeds with it.
- Next time: emitted-identifier renames are best verified by *generating* a sample app and grepping the output for stray `[Bb]lueprint` tokens — several helpers (`blueprintStatValue`, `blueprintAuthError`, `blueprintKV`, …) lived inside the `resource.go` embed const and were invisible to a grep of the generator's own symbol table. Also: `BlueprintSeedEntity` is BOTH a generator-side Go type (the `Blueprint.Seed` field) and an emitted type — a blanket rename would have broken the generator, so the emitted one needed a targeted edit.
