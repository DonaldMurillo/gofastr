# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

## [Unreleased]

## [Unreleased]

### Changed

- **`ui.ColorPicker` renders the swatch before its label** — control on
  the left, name on the right, matching Checkbox's reading order (it
  previously rendered label-first).

### Added

- **`seo.WebApplication`.** Typed JSON-LD for in-browser tools (schema.org
  SoftwareApplication subtype) — the right @type for SaaS products and
  online generators, previously missing from core-ui/seo's catalog.
  `NewWebApplication()` defaults `operatingSystem` to "Web"; pair with a
  free Offer for "free online tool" rich results.

## [0.29.0] - 2026-07-16

### Added

- **Configurable security headers.** `AppConfig.SecurityHeaders` (and the
  `framework.WithSecurityHeaders(cfg)` option) configure the defensive
  headers emitted by the default middleware chain, so an app can relax a
  single directive (e.g. `style-src 'unsafe-inline'`) without shadowing
  the whole chain with a hand-rolled `SecurityHeaders` middleware. Unset
  fields keep their strict built-in defaults; the zero value reproduces
  the previous behaviour exactly.

- Auto-CRUD now mounts `PATCH /<entity>/{id}` for sparse updates. PATCH shares
  PUT's access, owner/tenant scoping, hooks, audit, transaction, and validation
  path while validating and changing only fields present in the request body.
  OpenAPI, MCP update tools, generated typed clients, and entity `llm.md` expose
  the verb too.

### Changed

- **BREAKING:** successful single-record CRUD responses (create, get, PUT, and
  PATCH) now consistently use `{"data": {...}}`, matching list's
  `{"data": [...]}` envelope. Errors and DELETE responses are unchanged.

- **BREAKING: auto-CRUD requires an authenticated session by default.**
  An entity declaring none of `OwnerField`, `Access`, or the new
  `Public` had ZERO enforcement — every operation (List/Get/Create/
  Update/Delete) was reachable by an anonymous caller; an unauthenticated
  `POST /api/<entity>` returned 201 and persisted the row (#65). Entity
  MCP tools inherited the same gap since they dispatch through the same
  router. `framework/crud`'s `requireScope` chokepoint now requires an
  authenticated session (`core/handler.GetUser`) for every operation
  unless an explicit mechanism already governs the entity: `OwnerField`
  or a declared `Access` block (unchanged, "as today"), or the new
  `EntityConfig.Public` / blueprint `public: true` — a deliberate, full
  opt-out for genuinely public entities (a contact form, a blog's
  comments). No `mcp.Gated` wiring was needed for entity MCP tools: they
  re-dispatch through the router and inherit the REST fix for free.
  `gofastr generate` now prints a warning listing every entity left
  publicly readable/writable (`public: true`), and the existing unscoped-
  entity lint's message was corrected — it no longer claims anonymous
  exposure (that gap is now closed); it flags the narrower cross-user
  ("every authenticated user can read every row") exposure instead.
  Existing apps with entities that declare neither `OwnerField` nor
  `Access` will see those entities start 401ing anonymous requests;
  add `public: true` for entities that are genuinely meant to be open,
  or a real `access:`/`OwnerField` for the ones that aren't.
  `framework.TestApp` (the in-memory test harness) gained
  `AsUser(user any)` to authenticate test requests under the new
  default. See [entity-declarations](framework/docs/content/entity-declarations.md)
  → "Default CRUD authentication" and
  [security](framework/docs/content/security.md) → "Default CRUD
  authentication".

### Fixed

- **Eager loading / `?include=` no longer fails on nullable foreign keys.**
  `BelongsTo`/`HasOne` relations over a nullable FK column (e.g.
  `work_items.milestone_id`, `assignee_id`) returned
  `sql: Scan error … converting NULL to string is unsupported` and failed
  the whole eager load (and the request). The BelongsTo loaders in both
  the `EagerLoad` helper and the live include path now scan the FK into
  `sql.NullString`, so a `NULL` FK yields the parent row with the relation
  absent/`null` instead of erroring.
- **Generated `e2e_test.go` is Windows- and Postgres-portable** (issue #68).
  The blueprint generator's end-to-end test template had two portability
  defects. (1) It built the binary to a bare `app` and exec'd it — on Windows
  that name has no `.exe` suffix, so the child can't start. The template now
  appends `.exe` when `runtime.GOOS == "windows"`. (2) It always booted the
  child with `DATABASE_URL=file:e2e.db` (a SQLite DSN), but a
  `db.driver: postgres` blueprint links only `lib/pq`, which cannot open a
  SQLite file — the server never became ready and the test timed out with a
  misleading message. The template now bootstraps from the blueprint's
  declared driver: SQLite/empty drivers still use a throwaway file DB; a
  postgres blueprint carves a disposable database from the env-provided
  `TEST_POSTGRES_DSN` admin DSN and `t.Skip`s when Postgres is unreachable, so
  driverless CI stays green-by-skip.
- **Pre-image casing contract documented; typed/snake accessors added
  (#69).** `crud.AuditPreImageFromContext(ctx)` keys the pre-update row by
  the handler's `JSONCase` (camelCase by default, e.g. `"statusId"`) — not
  the snake_case DB column name every other hook-adjacent surface speaks.
  A hook doing `pre["status_id"]` silently got `nil` back; casing-identical
  keys (`"version"`, `"key"`) happened to work either way, masking the
  miss. Added `crud.AuditPreImageAs[T](ctx) (T, bool)`, which decodes
  through the same casing translation typed hooks already use, and
  `crud.AuditPreImageSnakeFromContext(ctx) map[string]any` for plain
  snake_case map access. The casing contract is now documented on
  `AuditPreImageFromContext`/`WithAuditPreImage` and in
  `framework/docs/content/hooks-and-transactions.md`.

- **Screen router accepts both `:param` and `{param}`.** A UI screen
  registered with the `{param}` brace syntax (the form used by the
  blueprint, REST/entity routers, and the docs) silently never matched —
  no error, just a 404. The core-ui router now normalizes `{param}` to
  `:param` at registration, so both syntaxes work identically. The HTTP
  router's `{param}`-only syntax is unchanged.
- **DevMode no longer locks local tooling out of `/auth/login`.** The
  per-IP login limiter tripped after a handful of rapid logins even with
  `DevMode: true`, blocking screenshot/verification tooling. DevMode now
  relaxes the per-IP login limiter (`RateLimiterConfig.DevMode`);
  production is unchanged and fail-closed. The per-account brute-force
  limiter is deliberately NOT relaxed in dev.

- PostgreSQL auth stores now create native `BOOLEAN` columns for password
  and 2FA flags, convert legacy auth `INTEGER` booleans during schema
  initialization, and accept native Go `bool` writes on a fresh database.
- Generated bootstrap-admin accounts now seed through `App.WithSeed`
  after auto-migration; missing-password, lookup, hash, and insert errors
  fail startup instead of being swallowed.

- **Authenticated accessibility audits report real coverage.** `gofastr audit
  a11y --url` accepts `--email` / `--password`, clicks the app's `/login` form,
  discovers and scans pages in that browser session, and reports `Audited N of
  M discovered pages`. Login redirects and a login-only run fail as incomplete
  instead of producing a misleading clean verdict.
- **Admin CRUD uses the app's fully wired handler.** Admin create/update/delete
  now runs the app hook registry, so `WithAuditLog` records transactional rows
  (including CRUD pre-images) instead of silently seeing `Hooks == nil`. The
  Queue overview/navigation is hidden when no `queue.Browsable` backend exists;
  backed queue browsing and replay remain available.

## [0.28.0] - 2026-07-16

### Added

- **Configurable API-token prefix.** Hosts brand their credentials:
  `TokenSpec.Prefix` at issue time, `TokensPlugin.WithPrefix` for the
  self-service route, `TokenMiddleware`'s `WithTokenPrefix` for recognition.
  A leaked token's prefix now identifies WHICH product leaked it. Default
  stays `gfsk_`; prefixes are validated (lowercase alnum, trailing `_`).
- **`auth.TokenID(ctx)`.** TokenMiddleware now stashes the authenticating
  token's own ID in the request context alongside owner and scopes — one
  owner can hold many tokens, and per-token metering/quotas/audit need to
  attribute the request to the specific credential.
- **Admin token operations.** `SQLAPITokenStore.ListAll` (every token across
  owners) and `RevokeAny` (revoke ignoring owner scoping) for host-built
  admin surfaces. Deliberately NOT on the `APITokenStore` interface, so the
  plugin's self-service routes can never reach them.

### Fixed

- **`ui.Gallery` grids are responsive by default.** `Columns` was a hard
  `repeat(N, 1fr)` — four columns stayed four columns on a phone, crushing
  every tile, and each consumer had to hand-write media queries. `Columns`
  is now a MAXIMUM: tracks are sized for exactly that many columns but
  never shrink below `--ui-gallery-min` (default `9.5rem`, override via a
  class), so `auto-fill` collapses to fewer columns as the container
  narrows. Masonry gets the same contract via `column-width` +
  `column-count`.
- **Session reads try every cookie candidate.** `SessionMiddleware`, the
  `/auth/me` handler, and logout read only the FIRST cookie with the session
  name (`r.Cookie`). A jar can hold several — a stale cookie from an old
  deployment at a more specific `Path`, or another localhost port's cookie —
  and browsers send the most path-specific first, so a dead cookie shadowed a
  live session: login silently failed while a valid cookie sat one position
  later. All session reads now try every candidate, and logout revokes ALL
  of them so a shadowed-but-valid session cannot survive.

## [0.27.1] - 2026-07-16

### Fixed

- **Phantom color tokens resolved theme-side.** framework/ui components
  referenced ten `--color-*` custom properties that no theme ever emitted
  (`--color-muted`, `--color-warn`, `--color-surface-hover`,
  `--color-primary-hover`, `--color-ring`, …). CSS custom properties fail
  soft: each reference silently rendered its hardcoded fallback — constants
  tuned for light themes — so dark themes hit contrast failures like
  `ui-copy-btn:hover` turning near-white under light-gray text. Themes now
  emit a derived-alias block mapping every legacy name onto canonical
  ColorSet tokens (via `var()`/`color-mix()`, so dark-scheme re-declarations
  flow through), CopyButton's hover/copied states use real tokens directly,
  and a framework/ui test fails the build if any component references a
  token that no theme defines.

## [0.27.0] - 2026-07-16

### Added

- **`gofastr dev --pkg` for `cmd/`-layout apps.** The build target is now
  independent of the watch root. Previously `dev` ran `go build .` at `--dir`,
  so an app whose main lives under `cmd/<name>/` had no working invocation:
  from the project root the build failed with `no Go files in <root>`, while
  `--dir ./cmd/<name>` moved the watch root and the server's cwd along with it
  — silently missing edits under `internal/` and resolving relative paths
  (sqlite `db_url`, static dirs) against the command directory, so the app came
  up against the wrong database. Use `gofastr dev --dir . --pkg ./cmd/<name>`:
  the watcher and cwd stay at the project root while only the build target
  moves. `--pkg` defaults to `.`, so the scaffold layout is unaffected.

- **Kiln is current again.** OMP with GLM-5.2 is the turnkey and first-choice
  live driver; the world/tool schemas now cover the current app, entity,
  screen, navigation, and owned-Go scaffold surfaces; pages render through the
  framework UI host and component registry; and `kiln freeze` deterministically
  emits generator-ready `gofastr.yml` plus lossless `world.json`. The removed
  `entities/*.json` graduation path is gone end to end.
- **Blueprint layout blocks.** Screens now preserve and generate the current
  `framework/ui` `stack`, `cluster`, `grid`, and `stat_grid` primitives, with
  semantic spacing/alignment validation and generate→pack recovery. This keeps
  Kiln's typed live composition intact across the owned-Go freeze boundary.
- **Windows embed WAL snapshots.** Snapshot completion now resets the
  append-mode WAL by closing and reopening it with truncation, avoiding the
  Windows `Access is denied` failure from truncating the live append handle.
- **Windows generator and dev-loop parity.** `gofastr dev` now builds a
  per-process `.exe` on Windows, its end-to-end harness kills the watcher tree
  before canceling the parent, codegen's symlink guard handles drive-qualified
  paths, additive generation normalizes platform separators, and generated
  app/extension tests execute platform-native binaries. Fresh-port allocation
  prevents stale dev servers from contaminating later browser tests.
- **Deterministic Meridian scheme captures.** The flagship visual gate now
  waits for Chromium to commit the scheme repaint before taking a from-surface
  screenshot and asserts that the dark marketing band keeps visible heading
  and paragraph contrast in both schemes.
- **`widget.Builder.SSERefresh`.** SSE-triggered screen changes now force the
  normal SPA navigation pipeline for the current URL. The old `SSEReload` name
  remains as a source-compatible alias but no longer performs a hard reload.

### Fixed

- **Freeze fails loudly on YAML-unrepresentable worlds.** The blueprint
  emitter quotes commas, quotes, and brackets wherever they appear (a seed
  value like `"a, b"` no longer re-parses as two items), leads list-item maps
  with an inline scalar list when no scalar key exists, and
  `BlueprintYAML` now errors — naming the offending key — on seed rows or
  props that `core/yaml` cannot round-trip (map-only rows, keys containing
  colons) instead of writing a silently corrupt `gofastr.yml`.
- **Typed kinds render at any depth.** A design-system kind (`card`,
  `stack`, `stat_card`, …) nested inside a semantic leaf container (`div`,
  `form`, table cells) now dispatches through its component instead of
  falling through to core noderender's unknown-kind comment; the `class`
  strip for legacy journals now applies at every depth.
  (`core-ui/noderender` exports the shallow `RenderKind` seam this uses.)
- **Deleting the page being viewed shows the Kiln fallback.** The host
  fallback carries a `<main>`, so the SSE-triggered SPA refresh swaps in its
  content instead of emptying (and caching) a blank content area.
- **`gofastr dev` removes its temp server binary on shutdown**, instead of
  accumulating one pid-suffixed binary per run in the temp dir; the e2e
  harnesses remove it for the processes they SIGKILL.
- **The Kiln landing follows the theme.** The host fallback page now styles
  itself entirely from the `/__gofastr/app.css` tokens (`--color-*`,
  `--font-*`, `--radius-*`) instead of a hardcoded always-dark palette, so it
  honors `set_theme` overrides and the light/dark scheme like every other
  surface; its styles ride inside `<main>` so an SPA-swapped fallback keeps
  its layout. The landing visual gate now captures both schemes.
- The kiln skill's `add_page` example is valid JSON again (gated by a test),
  and the hooks/routes/seeds tools, action kinds, and expression language it
  references are documented in the skill once more.

### Changed

- **`gofastr dev` runs the server in the project directory.** The rebuilt
  binary's working directory is now `--dir` — the same cwd it gets when run
  by hand — so relative paths (sqlite `db_url`, static dir) resolve against
  the project, and the app's worktree-isolation lookup keys off the
  project's location instead of wherever `gofastr dev` was launched. If you
  relied on launch-cwd-relative paths, your sqlite file now lives in the
  project dir.
- **Kiln defaults its live REST surface to `/api`.** Entity CRUD and HTML
  screens can share a name (`/api/posts` and `/posts`), matching current
  blueprints. Agent-authored page trees reject `class`, `style`, and `on*`
  props and compose the shared design system instead. Previously advertised
  native-agent meta tools (`set_theme`, reject, reset) are now actually
  dispatchable, and the new `set_scaffold` tool authors nav/stubs.

## [0.26.1] - 2026-07-15

Repo-hygiene patch: process ledgers become enforced gates, and the docs
that remain are current. No framework API changes.

### Added

- **`repolint` bans the process-artifact genre.** Two new rules:
  `root-markdown` (root-level `.md` must be one of the GitHub
  community-health files + `ROADMAP.md` + `CLAUDE.md`/`AGENTS.md`) and
  `process-artifact-markdown` (SCREAMING_SNAKE ledger names — AUDIT,
  FINDINGS, NOTES, JOURNAL, HANDOFF, LEDGER — rejected anywhere in the
  tree). Rationale for judgment calls lives in commit messages and
  comments beside the tests that enforce them, not in ledger files.
- **The scaffolded host skill covers the v0.26 surface** —
  `uihost.WithAppIcon`, the SEO options + `ScreenSEO`/`ScreenSchema`,
  `gofastr audit a11y` + the enforced build lint, and the
  `gofastr upgrade` flow, with matching trigger phrases.
- The `gofastr-docs` skill's change→doc checklist gained a
  release/BREAKING section (CHANGELOG + `upgrades.yml` `through` bump +
  SECURITY.md supported-versions + host-skill sync).

### Changed

- **`ROADMAP.md` trimmed 57KB → 11KB** — shipped sections deleted per
  the file's own rule; only genuinely-unbuilt work remains. Inbound
  section references across the architecture docs and tests were
  rewired.
- **`perf-results.md` re-measured against v0.26.0** — all 12 hot-path
  benchmarks re-run (Postgres via testcontainers); rewritten as a
  self-contained results doc with a "reading these numbers" section.
- The embedded kiln skill no longer triggers on "GoFastr" alone — it
  requires explicit Kiln signals, so framework-direct users aren't
  mis-routed into HTTP IR mutations.

### Removed

- **~600KB of point-in-time process markdown**, all preserved in git
  history: `references/` research dumps, `docs/` (audit handoff +
  website brief), `SECURITY_FINDINGS.md` + its ledger gate test (every
  row was re-verified fixed 2026-06-10; `SECURITY.md` now points at git
  history), `COVERAGE_NOTES.md` (floors + rationale live in
  `scripts/coverage-floors.sh`), `AI_TEST_AUDIT.md` (the pinning
  `*_security_test.go` files are the record), the embedded
  `agent-notes.md` dev diary and `project-architecture-review.md` risk
  register (its enforceable content already exists as CI gates), and
  `examples/ecommerce/BUILD_JOURNAL.md`.
- Two permanently-skipped contradiction tests in `battery/embed` —
  replaced by a comment beside the tests that actually carry the auth
  contract.

## [0.26.0] - 2026-07-15

Technical SEO and ADA compliance become first-class: static export now
ships the full crawler contract, one image becomes the whole favicon/app
icon surface, and accessibility moves from "tests some examples run" to a
guided `gofastr audit a11y` command with an enforced (escape-hatched)
`gofastr build` gate. Upgrades get the same funnel treatment (#62): a
documented workflow plus `gofastr upgrade`, which reads a migration
registry embedded in the CLI and points at the exact lines in your app a
breaking release touches. The whole batch was hardened by a dual external
review (nine findings, all fixed pre-release — headline: the a11y lint
honors the ARIA escape hatch, so documented icon-only buttons pass).

### Added

- `gofastr upgrade` — guided release upgrades (#62). The CLI embeds a
  migration registry (`cmd/gofastr/upgrades.yml`, one entry per release
  with migration-relevant changes, complete through a `through` marker
  a tripwire test pins to the CHANGELOG's latest release). The command
  reads the project's `go.mod`, resolves the target (`--to vX.Y.Z` or
  the newest tag via the module proxy), prints every note the project
  crosses — with per-note regex detectors pointing at the affected
  `file:line` in the app — and `--apply` runs the mechanical
  `go get` / `go mod tidy` / build / test steps, stopping at the first
  failure. Warns when the target is newer than the binary's registry.
- Upgrade documentation (#62): a version-independent **Updating
  GoFastr** section in the README and a full `upgrading` docs topic
  (`gofastr docs upgrading`) covering the module + CLI split, release
  notes first, MVS version confirmation, and go.mod/go.sum review.
- `gofastr <cmd> --help` now reaches subcommands that implement their
  own help (`audit`, `upgrade`, `docs`); other commands keep the global
  interception so a side-effectful `dev --help` can't start a server.
- `uihost.WithAppIcon(source)` — derives the entire icon surface from one
  image: 32/180/192/512px PNGs under `/__gofastr/icons/`, `/favicon.ico`,
  the `rel="icon"` + `apple-touch-icon` head links, the PWA manifest
  192/512 icons when `PWAConfig.Icons` is empty, and the same files in
  static export. Non-square sources are center-cropped; undecodable
  sources warn and skip.
- `image.NewGradient(w, h, from, to)` — generated placeholder imagery
  (diagonal #RRGGBB gradient) so apps ship an icon without committing
  binary assets; blueprint-generated apps use it for their default icon.
- `uihost.ScreenRobots` per-screen interface and `uihost.WithRobotsMeta`
  sitewide option — `<meta name="robots">` parity with the other
  per-concern SEO interfaces (previously bundle-only).
- Static export writes `sitemap.xml` and `robots.txt` when
  `WithSitemap`/`WithRobots` are configured — same bytes as the live
  handlers (new `UIHost.SitemapXML`/`UIHost.RobotsTXT` single source),
  with `--export-base` folded into `<loc>` entries and the derived
  `Sitemap:` line; user-supplied files in the static dir win.
- `gofastr audit a11y` — guided static accessibility lint: flags missing
  required a11y fields on core-ui/html element configs (Alt, Label,
  Legend, For, …) with a teach-the-rule fix hint per finding; exits 1 on
  findings. `--url <base>` mode runs the vendored axe-core engine via
  headless Chrome against a running app under BOTH color schemes, with
  pages discovered from `/sitemap.xml` (or `--pages`).
- `gofastr build` now enforces the static accessibility lint between
  `go vet` and compilation (guided failure output; `--no-a11y` skips).
  The lint honors the ARIA escape hatch — an `ExtraAttrs` literal with
  `aria-label`/`aria-labelledby`/`role` satisfies the matching typed
  field, and non-literal `ExtraAttrs` fails open (runtime validation
  still backstops it) — so the documented icon-only button form passes.
- `check.LintA11yFile` — a11y-only linter entry point that works on any
  .go file (import-alias aware, no false positives on non-core-ui/html
  calls), backing both the audit command and the build gate.
- Blueprint-generated apps ship `uihost.WithAppIcon` (gradient derived
  from the theme's primary color) and a protective default `robots.txt`
  (`Disallow: /__gofastr/`).
- Docs: new `seo` and `accessibility` topics; static-export and PWA docs
  updated for the new surfaces. Meridian and examples/site showcase the
  SEO stack (sitewide OG/description, per-screen `ScreenSEO` bundle with
  Product JSON-LD, Organization/WebSite schema, sitemap + robots, icons).

## [0.25.0] - 2026-07-15

The MCP surface gets the funnel treatment (#61): the dev loop implies the
full agent toolkit ("livereload for agents"), generated apps ship the
complete MCP contract, mutating control and log debug tools stay
fail-closed outside dev, custom tools gain first-class auth gating, and
the guidance — skills, agents.md, embedded docs — is pinned to the code
by tripwire tests.

### Added

- **`gofastr dev` is livereload for agents: the MCP surface auto-enables
  in the dev loop.** Under `GOFASTR_DEV` (set by `gofastr dev`),
  `framework.NewApp` auto-mounts `/mcp` and enables the read-only
  introspection tools AND the new mutating control tools with zero
  options; battery/log auto-enables its `log_recent` / `log_filter` /
  `log_metrics` / `log_set_level` debug tools the same way; and every
  CRUD-enabled entity serves its MCP data tools without per-entity
  `mcp: true` (entities with `crud: false`, like the auth battery's
  user/session configs, are never implied — no routes, no tools). Opt
  out with `GOFASTR_DEV_MCP=0` (mirrors `GOFASTR_DEV_LIVERELOAD=0`); a
  production `GOFASTR_ENV` always wins, and production processes never
  see `GOFASTR_DEV`. A dev-implied mount yields (warn, not panic) to a
  hand-wired `/mcp` route and dev-implied tool registration tolerates
  name collisions, so existing apps can't be broken by running under
  `gofastr dev`.
- **`framework.WithMCPControl()` — runtime control over MCP.** The
  mutating counterpart to `WithMCPIntrospection`: `app_module_enable` /
  `app_module_disable` toggle registered modules on the running app
  through the module store (dependency-checked, fail-closed), for
  `/mcp` endpoints reachable only by trusted callers. Code-level change
  stays the `gofastr dev` rebuild loop's job; MCP control mutates
  runtime state the app already models.
- **Blueprint-generated apps ship the debug loop.** The generated
  `main.go` registers battery/log (canonical zero config: per-app file
  sink, access log, panic recovery, dev console) — so under `gofastr
  dev` a generated app answers "recent requests / current errors /
  trace this request_id" and accepts module toggles over `/mcp` out of
  the box, while a production boot exposes none of it. The MCP e2e gate
  now pins both halves (dev boot has entity + introspection + log +
  control tools; prod boot refuses the mutating/debug set).
- **Auth gating for custom MCP tools.** `mcp.Gated(gate, handler)`
  wraps any directly registered tool handler with a per-caller
  precondition, and battery/auth ships the gates: `auth.MCPUser()`
  (any signed-in caller) and `auth.MCPRole("admin", …)`. The `/mcp`
  route runs under the app's global middleware chain, so the session /
  JWT middleware resolves the caller before the tool executes — the
  gate reads the same identity `RequireRole` does. Entity CRUD tools
  never needed this (they re-dispatch through the router and inherit
  HTTP auth + owner scoping + RBAC); `Gated` covers the direct
  registrations that bypass route middleware: `app.MCP.RegisterTool`
  handlers and `Endpoint.MCPHandler` twins.
- **UI-quality eval: MCP funnel signals.** Each candidate now records
  whether the builder touched `/mcp` during the build
  (`builder_used_mcp`), and the served candidate is probed for its MCP
  surface (`candidate_mcp_tools`, `candidate_mcp_introspection`) plus a
  fail-closed check that dev-only log tools did not leak into the prod
  boot (`candidate_mcp_log_tools_prod`).

- **Blueprint-generated apps are MCP-complete out of the box.** The
  generated `main.go` now wires `framework.WithMCP()` +
  `framework.WithMCPIntrospection()` instead of hand-mounting a
  POST-only `/mcp`: generated apps gain the GET SSE half of the
  Streamable HTTP transport, the MCP discovery endpoints
  (`/mcp/server-card`, `/.well-known/mcp/server-card.json`,
  `/.well-known/mcp/catalog.json`), and the nine read-only
  introspection tools (`app_routes`, `app_readiness`,
  `framework_docs_search`, …) alongside the per-entity CRUD tools. A
  new e2e gate (`TestE2E_MCP_BlueprintApp`) generates, builds, boots,
  and drives the whole contract over real JSON-RPC.
- **Introspection guidance is pinned to the live tool set.**
  `TestIntrospectionGuidanceNamesEveryTool` fails whenever a tool
  registered by `WithMCPIntrospection` is missing from
  `framework/agents.md`, the `agent-ready` doc, or the app-introspect /
  mcp-debug skills — the "five tools" drift (four tools had shipped
  undocumented) is fixed and can't silently recur.

### Fixed

- MCP guidance accuracy sweep: the app-introspect skill no longer
  claims `app_readiness` returns per-check `error` text under
  `WithVerboseReadiness()` (the tool always redacts it — `/readyz` and
  `/mcp` can sit on different trust boundaries), documents the
  zero-checks `reason` field, and the skills' `go run ./examples/site`
  instructions now point at the right port (:8083; :8082 is
  dev-watch). The mcp-debug skill's wiring snippet uses `WithMCP()`
  (matching `examples/site`) instead of a manual mount that would panic
  alongside it.
- `examples/site` registers a real readiness check (`docs-embed`), so
  `app_readiness` on the flagship reports `ready:true` instead of the
  unconfirmed `"no readiness checks registered"` state.
- Shipped-guidance relevance sweep: five battery `agents.md` snippets
  had drifted into wouldn't-compile territory —
  `email.SMTPConfig{TLS:…}` (field is `UseTLS`), `cache.Set/Get`
  missing the `ttl`/`dest` arguments, `webhook.Manager.Stop()` without
  its context, `setup.AdminStep` used as a one-return value, and
  `admin` guidance calling a nonexistent `User.HasRole` — plus the
  host skill's two-arg `testkit.NewIsolatedDB` and stale file paths in
  the gofastr-docs skill. The agents.md snippet gate now also
  validates struct-literal field names against the real structs, so
  the `TLS:`-class drift fails CI.

## [0.24.0] - 2026-07-15

Dev-experience overhaul + static site as an app (#59): hot reload reaches
every HTML surface and the guidance funnels to `gofastr dev`; HMR-readiness
is gated deterministically and measured non-deterministically in the
UI-quality eval; a 77-surface documentation-accuracy sweep (including
`gofastr migrate` honoring an exported `DATABASE_URL`); and static exports
with `WithPWA` become fully offline-capable installable apps.

### Added

- **Hot reload now reaches every HTML surface.** `framework.NewApp` mounts
  dev-only middleware (active only under `gofastr dev`'s `GOFASTR_DEV=1`)
  that splices the livereload client into full HTML documents — responses
  declaring `Content-Type: text/html` that contain `</body>` and don't
  already carry the tag. `static.Handler` file serving (the SPA and
  static-site shapes), widget-server pages, and hand-rolled handlers that
  set the type now auto-refresh like uihost screens. Fragments (island
  swaps, SPA-nav partials), compressed bodies, HEAD/Range requests,
  WebSocket upgrades, JSON/SSE/streams, and flushed progressive HTML all
  pass through untouched.
- **HMR-readiness evals.** An examples sweep boots every runnable example
  under `GOFASTR_DEV=1` and asserts the livereload endpoint plus client
  injection; a blueprint dev-loop e2e generates an app, runs it under
  `gofastr dev`, edits a screen, and asserts the rebuilt content serves; a
  docs tripwire pins that the dev-loop docs lead with `gofastr dev`.
- **Static site as an app: full-site offline PWA for static exports.**
  A static export with `uihost.WithPWA` now emits a full-site service
  worker (`PWAStaticServiceWorkerJS`): the exported page set is closed and
  immutable, so the whole site — pages, widget chrome, `llm.md`, component
  stylesheets under their versioned `?v=` URLs — is precached at install
  and navigations are cache-first (trailing-slash tolerant) — land once,
  install the app, and the whole site works offline, including
  never-visited pages. User static-dir files precache best-effort so one
  un-servable file cannot brick the install; the cache version
  fingerprints the exported tree (unchanged rebuilds stay byte-identical);
  static caches use their own `gofastr-pwa-static-…` prefix so live and
  static deployments on one origin never delete each other's caches; and
  a user-supplied manifest or worker in the static dir wins over the
  generated one. The live app's conservative network-first worker is
  unchanged and deny rules apply to both. Proven by a Chrome e2e:
  install, kill the server, navigate to a never-visited page, get its
  real content.
- **Non-deterministic dev-loop funnel signal in the UI-quality eval.**
  Builders now get the snapshot's own `gofastr` CLI first on PATH via a
  logging shim (previously `gofastr docs`/`gofastr dev` depended on a
  possibly version-mismatched global install), the builder prompt stays
  neutral about dev commands, and each candidate records whether the blind
  builder discovered `gofastr dev` from the generated guidance alone —
  surfaced in `result.json` and the leaderboard as funnel telemetry.

### Changed

- **`gofastr migrate` honors an exported `DATABASE_URL`.** The docs always
  promised the 12-factor pattern, but only `--db-url=` and a `.env` file
  worked; the process environment now sits between them (flag > env > .env,
  matching the framework's dotenv precedence).
- **Documentation-accuracy sweep** (77 guidance surfaces audited against the
  code): the audit-log example now shows the real chained
  `app.WithAuditLog(cfg)` wiring instead of a NewApp option that never
  compiled (framework agents file + host skill); the ecommerce example's
  README pointed at a `gen/` directory that hasn't existed since it moved to
  `output_dir: app`; the notify battery no longer advertises a nonexistent
  `WebhookChannel` (signed outbound webhooks are `battery/webhook`);
  `pack` is no longer marked *(future)* in the architecture doc; the
  migrations quick-reference shows the required `--from=`; and the harness
  docs (and one error string) now say plainly that `verify-ack`,
  `conformance`, `ack`, `token`, and `--auth-token-file` are specified but
  not yet wired into the CLI.
- **The development loop funnels to `gofastr dev`.** `go run .` never
  hot-reloads (only `gofastr dev` sets `GOFASTR_DEV=1`), yet the
  highest-traffic guidance led with it. `ui-getting-started`,
  `tutorial-blueprint-app`, `project-structure`, the README quickstart, the
  post-`generate` next-steps output, the generated AGENTS.md trigger row,
  and the UI-eval builder prompt now lead with `gofastr dev` for iteration,
  keeping `go run .`/`go build` for one-shot runs and production.

## [0.23.0] - 2026-07-14

AI-native UI composition (#57): framework-owned operational components,
generated apps that ship zero bespoke CSS with an app-owned `DESIGN.md`, an
adaptive-by-default canonical theme (**BREAKING** — see below), and a
framework-snapshot UI-quality evaluation harness.

### Added

- **Framework-owned operational composition.** `ui.RecordSummary` provides one
  compact dominant record/event summary with bounded status, next-decision,
  metrics, support, ownership, and natural-width action slots. Its optional
  `Aside` fills a purposeful wide-screen rail, while `Actions` stays in the
  lead region and ahead of support context on phones; `ui.MetricBand` renders
  one to six related signals as a flat semantic row that becomes two columns
  on phones. `SiteHeaderConfig.MobileBrand` now swaps long desktop identities
  for a concise phone mark/name without app CSS.
- **Natural UI composition guidance.** `gofastr init` now creates an app-owned
  `DESIGN.md`, points generated agent onboarding at it, and embeds a
  `ui-composition-recipes` reference for product-specific desktop/mobile page
  structures composed from `framework/ui`.
- **Framework-snapshot UI evaluation.** The UI-quality harness can use Codex,
  OMP / GLM-5.2, or Claude Code / Opus builders and Codex or Claude visual
  judges, with role-specific provenance retained in run manifests.

### Changed

- **BREAKING: `theme.Default()` is now adaptive.** The canonical framework
  theme carries a complete contrast-safe `DarkColors` palette, so every app
  mounting `theme.Default()` — new or existing — follows the OS dark
  preference and `ThemeToggle` works without host setup. An existing app whose
  own CSS assumes light tokens should either audit its surfaces in dark mode
  or opt out explicitly (`t := theme.Default(); t.DarkColors = nil`).
  `gofastr theme init` emits the same palette (generated from the canonical
  map, not a copy), and `theme.Overrides.DarkColors` lets a small brand reskin
  provide explicit dark values instead of silently reverting to the canonical
  palette. Forced-theme browser gates synchronize both the HTML attribute and
  the native color-scheme meta.
- **BREAKING: `SiteHeader` wraps its Brand slot.** Brand (and the new
  `MobileBrand`) render inside a `.ui-site-header__brand` wrapper div, so host
  CSS or tests selecting the brand as a direct child of the header must adjust
  one level. The framework now ships typography defaults for a linked brand
  (replacing browser-default blue underlines) at **zero specificity** via
  `:where()` — any consumer rule still wins, preserving the "consumer owns
  visual identity" contract.
- **Responsive and touch-target hardening.** `ui.DocLayout` now shrinks inside
  flex/grid parents instead of preserving a desktop min-content width on
  phones, and `ui.ValidationSummary` field links meet the WCAG 2.2 24px target
  floor. Button wrapping is container-driven: `flex: 0 0 auto` sizes each
  control to its unwrapped label so action rows wrap whole controls first,
  while a label wider than its own container — a sidebar rail or card cell at
  any viewport, not just a phone — wraps inside the bounded button instead of
  clipping. `ui.AvatarGroup` now uses a 10% overlap that keeps initials
  readable, an adaptive overflow chip, and compact corner presence dots.
- **The interactive set reads both theming surfaces.** Counter, Tabs, Toggle,
  Dropdown, and Collapsible chain every legacy `--fui-*` bridge read to its
  canonical token — `var(--fui-border, var(--color-border, …))` — so the
  adaptive theme reaches them with no host aliases while existing `--fui-*`
  host overrides keep winning. Collapsible also gains an opaque
  `--color-surface` background to hold contrast on tinted panels.
- **Balanced phone signal bands.** Odd three- and five-item `ui.MetricBand`
  sets now make their final signal span the phone row instead of rendering an
  accidental empty quadrant, and a single-item band no longer paints a stray
  column divider.
- **BREAKING: `ui.Cluster` zero value now wraps.** Clusters wrap whole
  controls by default; `ClusterConfig.NoWrap` is the explicit opt-out for
  compact chrome. The old boolean `Wrap` field remains source-compatible but
  is now ignored — it is deprecated because its zero value could not represent
  the documented wrapping default. A caller that relied on `ClusterConfig{}`
  or `Wrap: false` rendering nowrap must set `NoWrap: true`.
- **The init scaffold emits zero app-owned CSS.** The generated
  `screens/styles.go` and `WithCustomCSS` wiring are removed; the starter page
  now composes framework UI primitives. Reinit preserves `DESIGN.md` even with
  `--force`.
- **UI evaluation variants now represent framework roots, not injected design
  prompts.** Generated onboarding is fingerprinted and must remain untouched;
  historical prompt-treatment scores are explicitly invalid for framework
  quality claims.
- **UI evaluation runs fail closed on contamination.** Exclusive run creation
  and locks prevent mixed concurrent artifacts; reuse rejects linked run
  directories; candidate and framework fingerprints are rechecked around
  mutating gates; result JSON is atomically replaced; agent and candidate
  environments omit unrelated credentials; Windows jobs and Unix process
  groups own descendants; capture blocks non-candidate network requests and
  broken images; and visual judges treat screenshot text as untrusted output.

### Fixed

- **Eval-runner hardening (post-review).** The OMP stream is drained through a
  line-buffered `Stdout` writer instead of a `StdoutPipe` raced against
  `Wait`, so a successful build can no longer lose its final `message_end`
  event (and an oversized final message no longer fails extraction); switching
  an agent to the codex backend clears the previous backend's model and
  demands an explicit `--*-model` instead of launching `codex --model opus`;
  candidate gates pin the runner's resolved `GOMODCACHE` so the isolated home
  no longer forces a full re-download of the dependency graph `go mod tidy`
  just warmed; judge evidence is always copied rather than hard-linked so one
  judge process cannot corrupt the panel's shared pixels; and a
  machine-specific `NODE_EXTRA_CA_CERTS` fallback path was removed (export the
  variable instead). The five capture tests that drive a real headless Chrome
  are now gated behind `-short` like the other browser suites, and
  `test-all.sh` retries their contention deadline signature serially.
- **Generated home screen keeps prose out of the shell block.** The sample
  entity hint is part of the Section description; the `CodeBlock` contains
  only runnable commands.

## [0.22.0] - 2026-07-14

First-class installable-PWA support and blueprint-generated LLM page
documentation (#54).

### Added

- **Installable PWA (`uihost.WithPWA`)** — one opt-in option turns a UIHost
  app into an installable Progressive Web App: a typed web app manifest at
  `/manifest.webmanifest` (emitted via `encoding/json`, defaults derived at
  serve time — name from the app title, `/` start URL/scope, standalone
  display), a generated service worker at `/service-worker.js`, a CSP-safe
  external registration script, and an offline fallback screen at
  `/__gofastr/pwa/offline` (framework default or `PWAConfig.OfflineScreen`;
  deliberately not wrapped in the app layout — it is precached, so nothing
  personalized may render into it). The worker is conservative by
  construction: document navigations are **network-first and never cached**
  (rendered HTML can be personalized), falling back to the precached offline
  screen; Cache Storage only ever holds the versioned app shell (runtime,
  split modules under their content-addressed `?v=` URLs, `app.css`, the
  offline page + its per-component stylesheets, icons, declared `Precache`
  extras), matched **exactly** — content-addressed URLs are cache-first
  (immutable), everything else is network-first so post-deploy HTML never
  pairs with the previous deployment's runtime/CSS. Sensitive endpoints
  (SSE, session, signal, action, widgets, `/api`, `/auth`, plus
  `PWAConfig.DenyPaths` for custom mounts) can never be precached and are
  never intercepted. Cache names version deterministically — the fingerprint
  includes the bytes of static-served precache entries, so swapping an icon
  in place rotates the cache — and activation deletes only obsolete caches
  owned by the app. Updates never force a reload: no `skipWaiting`; a
  waiting worker dispatches `gofastr:pwa-update` on `window`. Precached
  responses are re-wrapped at install time so a static host's redirects
  can't poison the offline fallback. Verified end-to-end by a serialized
  Chrome test (registration, installability metadata, offline fallback
  against a dead server, v1→v2 cache-version cleanup). See `gofastr docs
  pwa`.
- **Static export emits the PWA surface** — `ExportStatic` writes the
  manifest, service worker, registration script, and offline page; under a
  non-root `BasePath` the manifest URLs, the worker's precache/deny lists,
  and the registration target are all prefixed, so the exported app installs
  and works offline from a subpath (GitHub Pages project sites included).
- **Blueprint `app.pwa`** — `gofastr generate` scaffolds the full surface
  from one block, including replaceable 192px/512px/maskable placeholder
  icons (deterministic in-process PNGs colored from `theme_color`, falling
  back to the theme's `primary` token) so a generated app is installable
  immediately. A custom `api_prefix` or auth `base_path` flows into
  `DenyPaths` automatically. Round-trips through `pack`.
- **Blueprint `app.llm_md`** — emits `uihost.WithPublicLLMMD()` so every
  registered screen serves its `/llm.md` document plus the `/llm-pages.md`
  index; app-level and per-screen `NoLLMMD` opt-outs keep working.
  Independent of `app.pwa` by design; both default off, and existing
  blueprints generate byte-identical output.

## [0.21.0] - 2026-07-13

Multi-replica auth durability + a hardened third-party plugin isolation
boundary. **BREAKING:** production mode refuses the in-memory 2FA store
(see below).

### Added

- **Heavy-JS plugin platform (`framework/pluginhost`)** — the client mirror
  of the process-isolation track (#37). Hosts third-party-grade JS plugins
  in a sandboxed **opaque-origin iframe** (`sandbox="allow-scripts"`, never
  `allow-same-origin`) with a versioned postMessage protocol (ready→init
  handshake, capability grants, save/upload/theme/resize events; source
  check by `event.source`, never origin strings). Isolation is enforced by
  the runtime, not by convention: the sandbox derivation
  (`Manifest.SandboxString` + the broker's `sandboxFor`) is **authoritative**
  — it strips `allow-same-origin` and forces `allow-scripts` regardless of
  manifest input — and the framed-asset CSP carries `sandbox allow-scripts`
  so a **top-level** load of the frame document is opaque too, not just an
  embed. The capability gate is **default-deny**: `pluginhost.Allow(ctx,
  granted, required)` is `grant-set ∩ caller-authority` (reusing the
  battery/auth `resource:verb` matcher via the new `auth.ScopeMatch`), so a
  plugin can't exceed its grants even under a session cookie;
  `pluginhost.Guard` is the fail-closed route chokepoint (`403
  E_CAPABILITY_DENIED`). Framed assets validate the request origin before
  interpolating it into the CSP (no header-injected directives) and carry
  `nosniff`; the mount marker drops unsafe attribute names. Ships plugin
  `Manifest` + validating `NewClientModule`, `MountMarker`
  (`data-fui-plugin*` markers — see core-ui/ARCHITECTURE.md attribute
  table), the same-origin `AssetServer`, and the host broker
  (`host/pluginhost.js`, served at its own route — core `runtime.js`
  budgets untouched). Distilled from the proven `gofastr-plugins` wysiwyg
  build; that repo now aliases this package.

- **Shared login rate limiting (`auth.SQLRateLimitStore`)**. Every auth
  limiter (login per-IP / per-account, register, 2FA challenge,
  magic-link, password-reset, email-verification) accepts
  `RateLimiterConfig.Store` — a database-backed attempt ledger so the
  brute-force budget stays `MaxAttempts` total across N replicas (not
  `× N`) and a block on one replica holds on all. One store instance
  backs every limiter (keys namespaced per `Scope`, defaulted by each
  surface); schema self-creates; a store error fails **closed**. The
  in-process limiter remains the zero-config default.
- **Durable 2FA store (`auth.EntityTwoFAStore`)**. The missing sibling of
  `EntitySessionStore`: TOTP secrets, enrollment state, and bcrypt-hashed
  backup codes persist in a database table (SQLite or PostgreSQL) instead
  of process memory, so a restart or a second replica no longer wipes
  enrollment. `TwoFAPlugin.Init` self-migrates the table (no host DDL);
  backup-code consumption is replica-safe via an optimistic
  compare-and-swap (two replicas racing to burn the same code — exactly
  one wins). `TwoFAEntityFields()` exposes the table to the entity system
  with the secret and code hashes `Hidden`.

### Changed

- **BREAKING: production mode refuses the in-memory 2FA store.**
  Previously `DevMode: false` + `TwoFAPlugin` without a configured store
  logged a WARN and booted — a restart then silently reverted every
  enrolled account to password-only auth. A security control that
  quietly stops applying is not warning-grade: Init now fails closed.
  Fixes: set `TwoFAConfig.Store: auth.NewEntityTwoFAStore(db, "auth_twofa")`,
  or acknowledge a deliberate single-node deployment with
  `AuthConfig.AllowInMemoryStores: true` (downgrades the refusal to a
  WARN). DevMode is unaffected.

## [0.20.0] - 2026-07-12

Issues #39, #40, #45, #47, #52.

### Added

- **Nested predicate filters (`?where=<json>`)** (#52). The list endpoint
  accepts a boolean predicate tree — leaves (`{field, op, value}`) and
  AND/OR groups — for filters the flat `?field_op=` params can't express
  (`status = A OR (priority = high AND assignee = me)`). Compiled to one
  parenthesized WHERE clause: every field is schema-validated (Hidden
  rejected), every value bound as a placeholder, depth/node bounds
  fail-closed, and — the invariant — a user OR-group can never widen past
  the owner/tenant/soft-delete scopes (they stay outer ANDs).
- **Data export / import registry (`App.ExportData` / `App.ImportData`)**
  (#39). Anti-lock-in: dump every entity's rows plus registered battery
  tables (auth, queue) to a portable NDJSON archive + a checksummed
  manifest, and restore it. Reads and writes RAW (preserving original
  ids, timestamps, owner/tenant, hidden columns, soft-deleted rows) so
  referential integrity survives; identifiers are `SafeIdent`-whitelisted,
  values bound, import staged (validate-then-write in one transaction).
  No new dependency.
- **Cross-container (kanban) sortable + version-aware conflict recovery**
  (#45). `core-ui/patterns/sortablelist` gains `data-fui-sortable-group`
  (lists sharing a group accept cross-container drag) +
  `data-fui-sortable-container` (destination column in the commit
  payload), an optional `data-fui-sortable-version` token with a distinct
  409 conflict callback (refetch/reconcile instead of blanket rollback),
  cross-column keyboard moves, and an aria-live announcer. Single-list
  behavior is unchanged.
- **Runtime-editable RBAC (`access.GrantStore`) + admin management
  screens** (#40). A DB-backed grant store that loads role→permission
  grants into the live `RolePolicy` at boot and persists edits, plus
  `battery/admin` screens (behind the admin default-deny gate, audited)
  to manage grants and user→role assignments at runtime. Adds
  `AuthManager.SetUserRoles` / `UserStore.UpdateRoles`. Role and
  permission strings are always bound parameters.
- **Presence foundation (single-replica)** (#47). Live "who's viewing
  this" rosters: an SSE connection joins `?presence=<topic>`, the island
  manager tracks a per-topic roster with **server-derived** identity
  (never a client param; anonymous synthesized from the session), and
  roster changes push live over the existing SSE lane. In-process
  `Manager.PresenceRoster` + a demo composing `ui.AvatarGroup`. There is
  deliberately no ungated HTTP roster endpoint (it would leak identities);
  cross-replica aggregation is future work (#47 tracks it).

## [0.19.0] - 2026-07-12

Issues #41, #42, #46, #47, #48, #49, #50.

### Added

- **Durable pgvector embedding store (`embed.NewPgVector`)** (#42). A
  Postgres+pgvector-backed `embed.Store` so multiple app replicas share
  one vector index instead of each holding an in-process FlatStore.
  Ranking is server-side cosine distance (`<=>`) and matches FlatStore's
  top-K order for the same vectors; it implements `chunkLister` so hybrid
  search composes, and deliberately omits snapshotting (a Postgres table
  IS the durable copy, so pairing it with `Options.Path` fails closed).
  `EnsureSchema` creates the `vector` extension + table, with an
  actionable error when the DB role can't `CREATE EXTENSION`. No new
  dependency — vectors are encoded in pgvector's text format over the
  existing `lib/pq`.
- **Pane-host / split-pane layout primitive (`ui.PaneHost`)** (#50). A
  master-detail shell: an always-visible primary pane plus one or two
  openable side panes (`Secondary` / `Tertiary`) with a declarative
  open/close/swap lifecycle, focus handoff on open and restore on close,
  and a responsive collapse where — below 768px — an open side pane
  becomes a fixed overlay drawer (backdrop, focus trap, scroll lock,
  ESC-to-close). Driven by the `panehost` runtime module +
  `data-fui-pane-*` attributes; `window.__gofastr.openPane` /
  `closePane` / `swapPane` expose programmatic control. Pane content
  loads via the existing RPC→signal(html) rail — pane state is never a
  route.
- **Avatar presence dot (`AvatarConfig.Status`)** (#47). `ui.Avatar` /
  `ui.AvatarGroup` render an optional presence dot (online / away / busy
  / offline) sized as a fraction of the avatar and colored from the
  status tokens, with a ring in the surface color. This is the roster
  *visual*; presence *transport* (binding a user to their live
  connection and aggregating it across replicas) remains app-owned and
  is tracked in #47.
- **Queue lane reservations (`Job.Lane` + `WithDBLaneWorkers` /
  `WithLaneWorkers`)** (#41). A lane is a capacity-reservation tag on a
  job: dedicated workers claim only their lane, shared workers keep
  claiming any lane by priority, so a bulk backfill can no longer starve
  urgent jobs by saturating every worker. DBQueue adds a `lane` column
  (auto-migrated onto pre-existing tables, both dialects) plus a
  `(lane, status, scheduled_at, priority)` index. `RedisQueue` stays
  instance-per-lane via its `queueName`.
- **MemoryQueue honours `Job.Priority`** (#41). Dispatch moved from a
  FIFO channel to a priority heap (`Priority DESC`, enqueue-order
  tiebreak) for dev parity with DBQueue. The pending store is now
  unbounded (`Enqueue` no longer blocks at 1024 queued jobs); the
  dead-letter set stays bounded.
- **SSE connection state (`window.__gofastr.sseStatus`)** (#46). The SSE
  runtime module now maintains `{connected, lastEventAt, retryCount}`
  (one live object, mutated in place) and dispatches a
  `gofastr:sse-status` event on connect/disconnect.
  `NetworkRetryBanner`'s `SSESilenceMs` trigger — previously dead
  because nothing wrote `lastEventAt` — now works, and the banner
  re-probes its health endpoint on SSE reconnect so it can dismiss.
- **Per-user locale switching (`framework.WithLocaleResolver` +
  `i18n.CookieLocale`)** (#48). Locale negotiation accepts resolvers
  consulted before the `X-Locale`/`Accept-Language` headers, so a
  stored preference (cookie/session) wins. Resolver values are
  length/charset-bounded and only accepted when they match a catalog
  locale — a garbage cookie cannot force an unsupported locale.
- `i18nui.TVars(ctx, key, vars)` — translate + interpolate `{name}`
  placeholders on both the catalog and English-default paths (#48).

### Fixed

- **framework/ui components now actually translate** (#48). The i18n
  middleware attached only the locale — never the translator — so every
  component rendered English even with a catalog wired. `WithI18n` now
  bridges the translator onto the request context, and a translator
  miss on a `ui.*` key falls back to the English default instead of
  rendering the raw key. On top of the four previously-wired
  components, all ~30 framework/ui components with user-facing copy
  (DataTable, FilterToolbar, forms, uploads, navigation, a11y labels,
  …) now resolve their default labels through `i18nui`; explicit config
  values always win, and default English output is byte-identical.
- **Dead `--radius-*` token references** (#49). 18 references across 12
  component files used `var(--radius-*)` while the token pipeline emits
  `--radii-*` — those styles silently used hardcoded fallbacks (and
  `Repeater`, with no fallback, lost its border radius entirely). All
  renamed to the emitted `--radii-*` tokens, so theme radius overrides
  now reach every component.

## [0.18.0] - 2026-07-10

Backlog from the 2026-07-10 dual blind cold-start eval (two agents each
built a multi-surface app from the repo alone; every item below was hit
independently or verified against the running builds).

### Added

- **Role-based cross-owner read (`EntityConfig.CrossOwnerRead`).** An
  owner-scoped entity can name an RBAC permission (e.g.
  `"tickets:read:all"`) that lifts owner scoping for READ operations
  only — list/get/count/cursor/stream/includes, HTTP and in-process.
  Checked via `access.Can` at the single owner-scope chokepoint, so it
  is fail-closed (no policy in context ⇒ scoping stays on) and
  spoof-proof (roles enter context only via server-side middleware).
  Writes never widen: update/delete stay owner-scoped and creates still
  stamp the caller. The admin battery's wildcard grant passes any
  permission, so opted-in entities are fully visible in the back office
  — per-entity opt-in, decided by the entity author.
  `owner.AllowCrossOwner` remains the in-process escape hatch.
  Blueprint key: `cross_owner_read`.
- **Free-text search (`EntityConfig.SearchFields` + `?q=`).** The list
  endpoint's `?q=` parameter now searches the declared columns
  server-side: whitespace-tokenized (deduped, capped at
  `filter.MaxSearchTerms`), each token an OR-group of
  `LOWER(col) LIKE` with metacharacters escaped, tokens AND-composed
  with owner/tenant/soft-delete scoping on every path (count, buffered,
  cursor, streaming). ASCII-case-insensitive on every dialect. Hidden
  or non-text columns are rejected at `Define` (value-disclosure
  oracle). In-process parity via `ListOptions.Search`; OpenAPI and the
  MCP list tool document/forward `q`; blueprint key: `search_fields`.
- **SQLite FTS5 search backend (`search.NewSQLiteFTS`).** Durable
  ranked full-text search without Postgres: FTS5 virtual table (porter
  tokenizer), bm25 ranking, prefix matching, FTS5 operators neutralized
  by quoting, `FieldEquals` via allow-listed `json_extract`. Requires
  building with `-tags sqlite_fts5` (schema creation says so when the
  module is missing).
- **`upload.ServeHandler(storage)`.** The download half that uploads.md
  always claimed existed: GET/HEAD, sniffed content type, `nosniff`
  always, HTML/SVG neutralized to `application/octet-stream` +
  attachment. Traversal stays enforced in the storage backends, now
  classified via `upload.ErrInvalidKey` (400) vs `ErrNotFound` (404).
- **`Router.MethodNotAllowed` + uihost fall-through.** Registering a
  non-GET route at a screen path no longer shadows the screen with a
  bare text 405: the uihost delegates GET/HEAD to the screen render
  when one resolves, and renders a styled 405 page (Allow header
  preserved, gated-method 404 semantics unchanged) otherwise.
- **`crud.ValidationError` (+ `framework.ValidationError`).** Exported
  with `Fields() map[string][]string` and `NewValidationError`, so
  in-process callers can branch on per-field messages with `errors.As`.
  HTTP wire shape unchanged.
- **`crud.WithServerWrites(ctx)`.** Opt-in for trusted server code to
  persist ReadOnly/Hidden fields through
  `CreateOne`/`UpdateOne`/batch/upsert — previously such writes were
  silently dropped with no error. HTTP handlers never set the flag
  (mass-assignment protection unchanged); owner and tenant columns stay
  context-stamped and body-immutable regardless.
- **`AuthConfig.DefaultRoles` + `AuthManager.ListUsers`.** New-account
  roles are configurable (register, magic-link, and OAuth auto-create;
  still strictly server-assigned), and back offices can enumerate users
  through the optional `UserLister` store interface (implemented on
  `EntityUserStore`, paginated, never selects the password hash)
  instead of raw-SQLing `auth_users`.
- **Queue failure logging.** `battery/queue` DBQueue and MemoryQueue
  take a logger (`WithDBLogger` / `WithLogger`, default
  `slog.Default()`): handler failures log at WARN, terminal
  dead-letters at ERROR, swallowed Ack/Nack errors at WARN. A failing
  job is no longer silent.

### Fixed

- **BarChart no longer renders black bars** for unrecognized `Color`
  values: registered status variants resolve to their accent color,
  syntax-valid CSS colors pass through, anything else falls back to the
  theme primary.
- **Docs drift**: `uploads.md` documented a nonexistent
  `upload.NewLocal(dir, urlPrefix)` API; `entity-declarations.md`
  claimed hidden fields are "still stored and API-readable" (they are
  excluded from responses and skipped on client writes); the stale
  "in-memory user store" comment on `AuthConfig.UserStore`.
- **Docs discoverability**: the island cookbook
  (`interactive-patterns.md`) is now cross-linked from the entity,
  admin, UI, and widget docs; `theming.md` gains a self-hosted web
  fonts recipe with the explicit CDN-fonts-are-CSP-blocked callout.

## [0.17.0] - 2026-07-10

### Added

- **Module manifests + runtime enable/disable (#35).** A Module is a
  Battery plus a manifest (`ModuleManifest` with `DependsOn`,
  `MigrationGroup`, `Version`, `Description`). Everything a module
  registers during `Init` — routes, entities, cron jobs, queue
  consumers, MCP tools — is attributed to the module, and a live
  enable/disable gate is enforced at dispatch time: disabled → routes
  404 (before auth, so existence doesn't leak; the gate keys on
  `METHOD + " " + path` so two modules owning different methods on the
  same path are gated independently, and 405 `Allow` headers list only
  non-gated methods), cron jobs skip, queue jobs defer (released to
  pending without consuming retries — they run on re-enable; gated job
  types are filtered before claim so the DB queue never churns
  claim/release cycles), MCP tools refuse with a generic
  `"tool unavailable"` error and are excluded from `tools/list`.
  Toggling is persisted (`gofastr_modules` table when a DB is set,
  in-memory otherwise; if the table cannot be created, `Start` fails
  closed rather than silently re-enabling every disabled module),
  fail-closed on dependencies (disable refuses if an enabled module
  depends on it; enable refuses if a dependency is disabled; both
  check-then-act sequences are serialized by a toggle mutex), and
  propagates across replicas via `WithFanout` on topic
  `gofastr.modules` with node-ID self-dedup. The fanout message is a
  refresh signal only — the receiving replica re-reads authoritative
  state from its store rather than trusting the payload. Attribution
  hooks are string callbacks on `core/router` (`SetRegisterHook` /
  `SetRouteGate`), `core/mcp` (`SetRegisterHook` / `SetCallGate`),
  `framework/cron` (`Scheduler.SetGate`), and `battery/queue`
  (`DBQueue.SetGate` / `MemoryQueue.SetGate`); neither core package
  imports framework. New `app_modules` introspection tool under
  `WithMCPIntrospection`. (#35)

- **First-run setup (`battery/setup` + `framework.WithSetup`).** A
  deployed binary against an empty database now has a guided first
  boot: while the `Complete` predicate reports false, `Start` serves
  an SSR setup
  wizard (composed from the design system) instead of the app router —
  everything else 503s, `/healthz`+`/readyz` stay up, and background
  consumers (cron/queue/outbox relay) wait until bootstrap finishes,
  then the handler swaps atomically with no restart. Access requires a
  **single-use setup token** printed to the boot banner (first visit
  exchanges it for an HttpOnly cookie and invalidates the URL form;
  restart mints a fresh one); wizard POSTs are origin-guarded
  regardless. The same steps run **headless** for IaC installs when
  their env vars (`GOFASTR_ADMIN_EMAIL`/`GOFASTR_ADMIN_PASSWORD`, or
  per-field `EnvVar`) fully resolve — before the port binds, failing
  loud. Steps are pluggable (`setup.Step` with fields, validation, and
  a `Run`); shipped: `setup.AdminStep(auth, db, usersTable)` (initial
  admin via the auth battery's hasher + password policy) and
  `setup.HealthStep(app)` (readiness checks with actionable errors).
  Completion is derived state, never a marker file — a crash mid-setup
  re-enters setup. `GOFASTR_SETUP=off|force` overrides; worker-role
  processes refuse to start while setup is incomplete. See the
  first-run doc. (#34)

- **Migration groups (`Migration.Group` / `-- +migrate Group <name>`).**
  Migrations can now be scoped to the feature or module that owns them:
  versions are unique per group, `Up`/`Down`/`Status` take an optional
  group selection (`m.Up(ctx, "knowledge")`, CLI `--group=<name>`,
  repeatable), and enabling a feature later applies only its pending
  group under the same advisory lock. Within a group ordering is
  strictly by version; across groups a run interleaves in
  `(version, group)` order — a deterministic tiebreak, not a dependency
  mechanism (groups must be self-contained). Group-less usage is
  untouched: the runner emits byte-identical SQL and never alters the
  tracking table until a non-default group is actually in play, at
  which point `group_name` is added and the primary key upgrades in
  place to `(group_name, version)` (atomic ALTER on Postgres, a
  transactional rebuild on SQLite). Checksums, dirty state, and
  `force` key on `(group, version)`; `migrate generate --group=<name>`
  stamps the directive. A named group with no registered migrations is
  a *disabled module*: its applied rows are shown by `status` but never
  compared, blocked on, rolled back, or dropped (`force --group` is the
  reconciliation escape hatch) — the default group is never treated as
  a module, so drift there still errors. `--group=default` (reserved
  name) addresses the default group in selections. `Migrator.Register`
  now returns an error (duplicate `(group, version)` or invalid group
  name). (#33)

## [0.16.0] - 2026-07-09

### Added

- **Cross-replica real-time push (`framework.WithFanout`).** The real-time
  lane — entity `_events` SSE streams, `On`/`Subscribe` handlers, and UI-host
  island push — previously stopped at the process boundary: an event emitted
  on replica A never reached a browser connected to replica B (the docs'
  answer was sticky sessions). A new `core/fanout.Fanout` seam bridges it:
  `framework.WithFanout(f)` mirrors every bus emit to the other replicas and
  re-emits it there, and wires any mounted UI host's island manager so island
  updates reach sessions connected elsewhere (`SSEBrokerConfig.Fanout` covers
  hand-built brokers). Backends: `framework/fanout.NewPostgres(dsn, db)`
  (LISTEN/NOTIFY on the database you already run; payloads past the NOTIFY
  size limit spill to a self-purging fallback table) and
  `core/fanout.NewRedis` (bring-your-own client, mirroring
  `cache.RedisClient`); `core/fanout.NewInProcess` simulates replicas in
  tests. **Semantics under fanout: the bus becomes a broadcast** — every
  handler fires on every replica, so side-effect work belongs on outbox
  consumers, and handlers that derive new events must gate on the new
  `event.IsRemote(ctx)`. Delivery stays lossy best-effort (the durable lane
  is the outbox's); publishes never block emitters or request handlers (per
  publish/subscriber bounded drop-oldest queues). Closes the sticky-session
  requirement in the scaling doc. (#28)

- **Worker-process mode (`framework.WithRole` / `GOFASTR_ROLE`).** The same
  binary can now run as a dedicated web or worker process instead of always
  doing both: `RoleServe` serves the full router but never starts
  `AddCron`/`AddQueue` workers or the outbox relay; `RoleWorker` runs those
  consumers and serves only `/healthz` + `/readyz` (same handlers as the full
  router, so orchestrator probes work unchanged); `RoleAll` (default) is
  today's combined behavior. Explicit `WithRole` beats the `GOFASTR_ROLE`
  env var; invalid values fail loudly at construction. Worker-scoped
  drainers are only registered when their workers actually start, so a
  serve-only shutdown never drains a scheduler that never ran. Plain
  `OnStart` hooks stay role-agnostic — gate custom background work on
  `App.Role()`. (#32)

### Changed

- **BREAKING: transactional outbox now delivers per-consumer, not
  whole-row.** The `framework/outbox` relay previously published each row
  to the event bus all-or-nothing across co-subscribers — one failing
  subscriber failed the whole row and blocked its siblings until `Replay`.
  Delivery is now split into two disjoint lanes: a best-effort **real-time
  lane** (the live bus / SSE `EventStream` / ephemeral `On`/`Subscribe`,
  fed post-commit by `EmitEvent`) and a durable **per-consumer lane**.
  Durable consumers are now declared and named via
  `framework.WithOutboxConsumer(name, eventType, handler)` (or
  `Outbox.Consume`); each `(row, consumer)` pair is tracked in a new
  `event_outbox_delivery` child table and retried / backed-off /
  dead-lettered **independently** (sibling isolation). Consumer-set changes
  are handled by **time**, not by a per-replica snapshot, so rolling
  deploys can't lose events: a delivery whose consumer has no handler
  anywhere is `abandoned`, and a parent is completed or an orphan type
  dropped, only once older than the handler grace (`WithHandlerGrace`,
  default 15m) — so a lagging replica never destroys, or prematurely
  completes, a freshly-added consumer's work. (Consequence: a
  fully-delivered parent's `dispatched` bookkeeping lags by the grace;
  delivery to consumers stays prompt.) Adds `Outbox.ListDeliveries`,
  `Outbox.ReplayConsumer`
  (resurrects dead *or* abandoned deliveries), `WithHandlerGrace`, and
  `WithRetention` (optional purge of settled rows); `StartRelay` drops its
  `bus` argument and no longer publishes to the event bus. **Migration:**
  drain in-flight outbox rows before upgrading — the new relay ignores the
  old single-delivery row state and there is no automatic backfill.
  Declaring `WithOutboxConsumer` without `WithOutbox` now panics at
  construction rather than silently dropping the consumer.

### Removed

- **OIDC PKCE `code_challenge`.** The confidential OIDC provider no longer
  sends a PKCE `S256` `code_challenge`/`code_verifier`. It was only an
  IdP-compatibility shim: the verifier was derived from the same client
  secret (and public state) that already protects the server-to-server
  code→token exchange, so it added no defense a secret-holder didn't
  already have. Genuine PKCE — a random per-request verifier bound via a
  cookie or store — remains the path for *public* (SPA/mobile) clients and
  is out of scope for the confidential provider. No API change: `AuthURL`
  and `ExchangeCode` are unchanged; the internal `ExchangeCodeWithState`
  seam and the `stateExchanger` interface are gone.

## [0.15.0] - 2026-07-08

Nexus-gap wiring round two — six issues surfaced by building on GoFastr
(tracking #35), each built at the existing seam and then hardened by two
adversarial review rounds. No breaking changes.

### Added

- **Inbound webhook battery** (#26). `battery/webhook` gains an HTTP
  ingestion endpoint beside the outbound one: constant-time, fail-closed,
  body-size-capped signature verification; envelope persistence (memory +
  SQL stores); dedupe by provider delivery id; and enqueue for async
  processing via `battery/queue`. Delivery is durable-before-ack — with a
  queue wired, the dedupe key is registered only *after* a successful
  enqueue, so an envelope that never reached the queue can never
  dedupe-ack the sender's retry (no lost events); without a queue,
  persistence itself is durable acceptance. Best-effort store-update
  failures surface through `IngestConfig.Logger`.
- **Generic OIDC login provider** (#29). `battery/auth` gains an
  authorization-code OIDC provider that verifies the id_token locally:
  RS256/ES256 alg allowlist enforced before key lookup, kty/crv-vs-alg
  cross-check, `iss`/`aud`/`azp`/`exp`/`nbf`/`iat` validation, JWKS fetch
  with per-kid rotation-refetch rate-limiting, and RSA/EC key sanity
  (≥2048-bit moduli, non-degenerate exponent, P-256). A PKCE `S256`
  `code_challenge` is sent for compatibility with IdPs that mandate it
  (the confidential client secret remains the actual exchange protection).
- **Scoped API tokens and service accounts** (#30). `gfsk_`-prefixed
  personal-access tokens and non-human service-account credentials,
  sha256-hashed at rest, scoped `resource:verb` with wildcards.
  `TokenMiddleware` fails closed through a single funnel that clears any
  outer identity on a bad token; `RequireScope` gates machine routes while
  sessions/JWTs pass unscoped. The token-management endpoints are
  session-only, so a leaked scoped token can't mint an unscoped one for
  its owner.
- **Transactional event outbox** (#25). `framework/outbox` delivers events
  at-least-once: `Append` stages a row inside the caller's transaction and
  a leased relay (Postgres `FOR UPDATE SKIP LOCKED` / SQLite tx) delivers
  to the event bus with exponential backoff and a dead-letter state. A
  panicking consumer is retried and eventually dead-lettered, never
  silently marked dispatched (new `EventBus.EmitStrict`). Enable per-App
  with `framework.WithOutbox(...)`; CRUD mutations stage their lifecycle
  events into the caller's transaction automatically. `WithoutEnsureTable`
  opts out of the boot-time table create.
- **`framework.WithoutAutoMigrate()`** (#24). Suppresses the boot-time
  entity DDL for deployments that require every schema change to come from
  a reviewed migration; documented alongside the two-layer migration model.
- **Per-file and additive blueprint generation + scaffolds** (#20).
  `gofastr generate` now emits one file per entity and per screen behind a
  fixed, name-free registration seam. `gofastr generate --from=<partial>
  --add` additively emits only the new pieces into an existing project
  (never overwriting, continuing declaration order, refusing colliding
  routes and pre-0.15 layouts); `gofastr generate entity|screen <name>`
  scaffolds a stub through the same path. `gofastr pack` reverses the new
  layout, so generate/pack still round-trips.

## [0.14.0] - 2026-07-07

Nexus-gap wiring round one — three small framework gaps surfaced by
building Nexus on GoFastr (tracking issue #35), each closed at the
existing seam rather than with new machinery.

### Added

- **OpenAPI specs now say how to authenticate** (#21). When any entity is
  auth-gated (owner-scoped, multi-tenant, or RBAC), the generated spec
  declares `components.securitySchemes` — `bearerAuth` (HTTP bearer, JWT)
  and `cookieAuth` (the auth battery's session cookie, production default
  `__Host-session`) — and every gated operation carries a per-operation
  `security` block accepting either. Ungated entities stay unmarked, and
  there is no global `security` requirement. Deployments overriding
  `AuthConfig.SessionCookie` can replace the scheme via
  `Spec.SetSecurityScheme("cookieAuth", …)`. `core/openapi.Operation`
  gained the underlying `Security` field + `AddSecurity`.
- **Rate-limit budget headers** (#22). `core/middleware.RateLimit` emits
  the IETF-draft `RateLimit-Limit` / `RateLimit-Remaining` /
  `RateLimit-Reset` headers on every response (allowed and 429) so API
  clients can self-pace; `RateLimitConfig.OmitBudgetHeaders` suppresses
  them. `Retry-After` on 429 is unchanged. The auth battery's limiter
  deliberately emits only `Retry-After` — a live remaining-attempt count
  on login/reset endpoints would hand attackers brute-force pacing.
- **Storage content checksums** (#23). `battery/storage.SaveWithChecksum`
  tees any `Storage` save through SHA-256 in a single pass and returns
  `SaveResult{Size, SHA256}`; `VerifyChecksum` re-reads and compares,
  wrapping `ErrChecksumMismatch` on mismatch. The `Storage` interface is
  unchanged — the helpers wrap any backend, including user-implemented
  ones.

- **Auth security events reach the audit log** (#31). `battery/auth` now
  emits a fixed-vocabulary `SecurityEvent` at every security decision point
  — login succeeded/pending-2FA/failed, register, logout, the full 2FA
  lifecycle (enroll, challenge pass/fail, disable, backup-code regen),
  password reset requested (known *and* unknown emails, so probing is
  visible) and completed, session revocation with counts, OAuth
  linked/login/refused, and magic-link request/consume. Wire it with one
  line: `AuthConfig.AuditSink = sink` where
  `sink, _ := auth.NewSQLAuditSink(db, "")` writes into the same
  `audit_log` table as the CRUD hooks (entity `"auth"`). Events never
  carry credentials — the only user-controlled string is the email, and a
  leak-guard test greps every event for planted secrets. A panicking or
  failing sink never breaks the auth flow it was recording.
  `framework.AppendAuditEvent` is the new exported append primitive for
  custom sinks, sharing the CRUD trail's sanitization.
- **Postgres full-text search backend** (#27). `search.NewPostgres(db, cfg)`
  implements the existing `Backend` interface over a single table with an
  in-SQL `tsvector` (GIN-indexed, idempotent `EnsureSchema`), ranked
  `ts_rank` results, weighted fields (`Document.Fields` keys promoted to
  weights `'B'..'D'`; `Text` is always `'A'`), configurable language, and
  built-in prefix matching on the final query term (search-as-you-type).
  Query text is sanitized through a single unit-tested chokepoint and always
  parameterized. `Query` gained `FieldEquals` — an exact-match filter on
  `Document.Fields` implemented identically in both backends (JSONB
  containment in Postgres) so tenant/owner scoping happens in-query instead
  of post-filtering. pg_trgm is deliberately omitted (needs
  `CREATE EXTENSION`/superuser).

### Fixed

- Docs drift: `uploads.md` showed a two-method `Storage` interface that
  no longer exists (real interface has `Save`/`Delete`/`Get`/`Exists`,
  with `Save` returning `error`), and `security.md`'s rate-limit example
  used `Requests`/`Window` fields that were renamed to
  `Capacity`/`RefillEvery`/`RefillBy` long ago.

## [0.13.0] - 2026-07-07

The UI-library hardening release: a five-dimension evaluation of
`framework/ui` + `core-ui` (API, correctness, extensibility, docs,
discoverability) followed by fixes for everything it found, an adversarial
review pass over those fixes, and fixes for what *that* found.

### BREAKING

- **Unknown component variants panic at build time.** `ui.Card` now rejects
  an unregistered `CardVariant` the way `ui.Button` always rejected bad
  `ButtonVariant`s, and `ui.ToggleAction` validates `Variant`/`Size` the same
  way. A typo like `Variant: "primry"` that used to silently render unstyled
  markup now fails loudly at screen construction. Register custom variants
  first (see below).
- **Plain modals paint a real panel.** A bare `preset.Modal` used to render
  its slot content floating invisibly on the dimmed backdrop; the centered
  widget skeleton now wraps its slots in one `.fui-panel` that paints surface,
  border, radius, padding, and shadow from theme tokens. Bodies that own their
  chrome opt out with `.fui-slot-bare` on the body's root element (Lightbox
  and CommandPalette are excluded automatically). Anything targeting the old
  `.fui-pos-center > .fui-slot` selector should target `.fui-panel`. Bottom
  sheets similarly gained a default surface (75vh cap, scroll inside).
- **`html.Button` requires a label.** An empty `Label` with no
  `ExtraAttrs["aria-label"]` now panics at build time instead of rendering an
  unlabeled `<button>`. Icon-only buttons pass an `aria-label`. (Data-driven
  renderers degrade instead: a labelless Kiln button node renders with
  `aria-label="button"` rather than panicking at request time.)
- **`ui.Sticky` stacks at the theme's `--z-sticky` (200).** The old CSS read
  a `--z-index-sticky` token that never existed, so sticky elements silently
  stacked at the 100 fallback. Anything relying on that broken value moves up
  to the designed `ZIndexSet` layer. `StickyConfig.ZIndexTier` now actually
  works (`dropdown`/`modal`/`popover`/`toast`) and panics on unknown tiers.
- **`patterns/tabs` panics past 16 tabs** instead of silently breaking (the
  registered stylesheet generates 16 nth-child slots; the panic names the
  ceiling and the escape hatch).

### Added

- **Accessibility enforcement grew three gates.** A shared axe-core harness
  (`internal/axetest`) now drives two app gates: the existing site gate
  (every component demo page, both color schemes) and a new Meridian gate —
  marketing, auth, app, and admin pages scanned logged-in with an **empty
  allowlist**, plus the first open-widget scan (the quick-add modal's open
  DOM state). A keyboard-only traversal gate walks Tab through key pages of
  both apps asserting no focus traps, visible focus indication on every
  stop, complete reachability of interactive elements, and the modal's
  trap-then-release cycle. What the gates flushed out was fixed at the
  source: `battery/admin` pages now render a `<main>` landmark; DataTable's
  empty actions header is hidden from the a11y tree; `PricingCard`, `Card`,
  and `PageHeader` gained a `HeadingLevel` config so composed pages keep a
  sane heading outline; the pricing badge's default text color adapts
  per-scheme via `color-mix` toward the text token; the sidebar's active
  nav link has a visible keyboard focus ring (it previously vanished under
  the `aria-current` background); Sidebar renders a `<div>` shell instead
  of nesting an `<aside>` landmark inside the layout's `<nav>`; and
  Meridian's status/text-subtle palette was retoned to clear WCAG AA on
  the components' tinted chips in both schemes. The site gate also
  enables axe's WCAG 2.2 `target-size` rule (24px minimum) and scans a
  curated page subset at a 390px viewport — carousel dots grew invisible
  24px hit areas (`::after` pip), tree row links meet the floor, and
  horizontally-scrolling command lines are keyboard-focusable.

  An adversarial review of the gates then hardened them further: the
  harness rejects blank pages instead of scanning them as vacuously
  clean; the site's three allowlisted rules apply only to `/components/*`
  demos (content pages scan with an empty allowlist — which surfaced and
  fixed nine real heading/landmark defects across the home, get-started,
  philosophy, and docs pages); every `/docs/<slug>` page, `/kiln`, and
  every Meridian admin CRUD screen (list + create per entity) is now
  scanned. New knobs from those fixes: `CalloutConfig.Landmark` opts an
  in-flow tip out of the complementary-landmark role, and
  `EmptyStateConfig.HeadingLevel` keeps empty states in the page outline
  (AnchoredRail's rail label is no longer a stray `<h6>`).
- **Variant registration.** `ui.RegisterButtonVariant`, `RegisterButtonSize`,
  `RegisterCardVariant`, and `RegisterStatusVariant` open the variant system
  at init time: pass `VariantCSS{Props, Hover, Focus}` (or
  `StatusVariantCSS{Color, Icon}`) with `{colors.x}` token references and get
  a typed variant value back. Status variants fan out to StatusBadge, Tag,
  Callout, and Notification. Registration is init-only (sheets seal on first
  materialization; late registration panics). Custom button variants style
  `ui.ToggleAction` markup too.
- **`ui.ToggleAction`** — an optimistic press-and-commit button
  (follow/subscribe/pin): idle/committed labels + icons, endpoint POST with
  rollback on failure, optional untoggle endpoint, and `data-fui-toggle-group`
  mutex semantics. Gallery demo + e2e.
- **`__gofastr.doc`** — the runtime's single owner of global document state.
  A frozen manifest of every `<html>` attribute, `<body>` class, and DOM
  singleton the runtime may touch (guard warns on undeclared writes),
  refcounted scroll-lock (two closing widgets can't unlock each other's
  scroll), SSR-adopting `singleton()`, and a `reattach()` hook for cross-layout
  shell swaps. Documented in `core-ui/ARCHITECTURE.md`; a test parses the doc
  table against the manifest.
- **Theme slots for the code palette.** `Theme.Code` / `Theme.DarkCode` emit
  the `--tk-*` syntax-highlighting tokens (optional group — zero slots emit
  nothing), so dark mode can restyle code blocks through the theme instead of
  raw CSS.
- **New embedded docs**: `ui-wiring.md` (annotated main.go wiring
  `framework.App` + core-ui app + uihost, compile-verified), `theming.md`
  (token catalog, `DarkColors`, `ui.Themed`, `--ui-*` knobs, and why theming
  never relies on CSS source order), and `runtime-contract.md` (the full
  `data-fui-*` attribute reference + SSR/island/SSE model, sync-tested against
  `core-ui/ARCHITECTURE.md` — fixes five dead links in the embedded docs).
  Plus a form-in-a-modal recipe in `widgets.md` documenting
  `data-fui-rpc-close` / `data-fui-rpc-reset`, and pagination-package
  disambiguation on `DataTableConfig`.
- **Routing users to the UI system.** `framework/ui` registers with
  `agentsinv` (generated apps' AGENTS.md now lists the `ui` package), the
  gofastr-host skill's don't-reinvent table gained a UI row, the docs index
  gained a "Building UI" group, and the README points at the catalog.
- **Meridian exercises the full surface** (design-system canary): a plain
  quick-add modal on the default panel surface, an ink-band
  `ui.Themed`/`RegisterThemeOverride` CTA, an island-mode DataTable (RPC
  sort/pagination sharing one config between screen and endpoint), and
  `--ui-layout-container-width`. Its `app.css` now contains only published
  `--ui-*` variable declarations — the page-header/section internals
  overrides became upstream knobs (`--ui-page-header-title-*`,
  `--ui-section-eyebrow-*`).
- **`gofastr pack` follows extracted helpers.** A resource chain moved into a
  package-local zero-arg helper (so a screen and its island endpoint share one
  config) still reverses to the blueprint block.

### Fixed

- **Security.** Escaping/sanitization holes closed across the component
  catalog: `SignalToggle` label/name/class, chart `Class` (SVG context),
  href sinks in Card, Tag, NotificationBell, ProgressSteps, Sidebar, and
  DocLayout (safeURL with `#` fallback), the CommandPalette→combobox
  `data-fui-push-state` href (scheme allow-list; unsafe values omit the
  attribute), and Menu `ExtraAttrs` keys (routed through the `render.Attr`
  allow-list so a smuggled key can't become a live event handler).
- **Correctness.** Multiselect chips show the option Label (the `label[for]`
  never matched) and option IDs no longer collide ("C++"/"C#", or two
  instances on a page); static-options Combobox SSRs `aria-expanded="true"`
  so Escape/outside-click dismissal works before any keystroke; `ui.Tabs`
  mirrors `aria-selected` on click; SiteHeader styles the active link on
  `aria-current="page"` (the dead `data-fui-active` attribute is gone);
  infinitescroll's noscript fallback stays GET (a noscript form cannot carry
  the CSRF token a POST would need); tree items only show a focus ring when
  focused; heading auto-IDs are documented as deterministic with `ID` as the
  collision escape hatch.
- **Runtime.** `data-fui-rpc-navigate` to the current page re-renders instead
  of silently no-opping, and post-mutation navigation bypasses the stale
  screen cache (including across `X-Gofastr-Location` redirects); demand-loaded
  modules now load when the marker sits on a lazily-mounted root node (drag-to-
  dismiss on late-mounted sheets was silently dead); the minifier never drops a
  semicolon after `)` (an empty `if(x);` body was corrupted into a
  SyntaxError). Core runtime stays within its 12KB gzip budget.
- **Theming/tokens.** Component CSS reads the theme's typography and spacing
  scales — 194 literal `font-size` declarations became `var(--text-*, …)` and
  76 spacing literals became `var(--spacing-*, …)` (a budget test blocks new
  literals); large buttons keep their own step (`--text-lg`); the danger
  button and notification badge read `--color-danger`/`--color-primary-fg`
  instead of hardcoded hex; the pricing-card badge exposes
  `--ui-pricing-card-badge-fg` for themes whose primary isn't text-safe on
  tinted chips.
- **`style.Contribute` works as documented**: the uihost's `app.css` now fans
  in contributed styles automatically as the last layer — no `style.Apply`
  hand-wiring in the host.

### Changed

- The drag-dismiss handler moved out of the core runtime into a demand-loaded
  module (`src/dragdismiss.js`) — pages without bottom sheets don't ship it.

## [0.12.1] - 2026-07-06

### Fixed

- **Opening a modal no longer dislodges sticky elements.** The overlay
  scroll-lock set `overflow: hidden` on `<body>`, which turns the body into a
  clipped scroll container and breaks any `position: sticky` descendant — on a
  scrolled docs page, opening the ⌘K command palette (or any modal/drawer) sent
  the sticky nav rail off-screen. The lock now applies to `<html>`, which locks
  the viewport just as effectively while leaving sticky elements pinned and
  preserving scroll position.

### Added

- **The components gallery now covers the full `framework/ui` catalog.** Added
  showcase pages for `Hero`, `HeroSplit`, `PricingCard`, `AuthCard`,
  `FilterToolbar`, `DetailList`, `FactBox`, `TerminalBlock`, `StepRail`, and
  `StatusPill`, which shipped in the design system but had no `/components/<slug>`
  demo. A new coverage test (`TestComponentGalleryCoversUI`) parses
  `framework/ui` and fails when a component constructor has neither a gallery
  entry nor an explicit allow-list line, so the gallery and
  `docs/content/ui-new-components.md` can't silently fall behind again.

### Changed

- **Blueprint tutorial teaches "generate once, then own the Go."** The
  getting-started tutorial no longer edits `gofastr.yml` and re-runs
  `gofastr generate` (which is one-shot and refuses to overwrite without
  `--force`) to add security — it generates once with auth enabled, then adds
  `owner_field` + `access` + the RBAC policy by editing the owned
  `entities/register.go` and `app.go` directly. Also corrects the REST paths to
  the `/api` prefix. The dev-mode→production note in `blueprints.md` likewise
  points at the owned `app.go` rather than "regenerate."

## [0.12.0] - 2026-07-04

### Added

- **Generated `entity_list` screens get facet filters (`filters:`).** A
  top-level `entity_list` block can now declare `filters: [status, assignee_id,
  …]` naming enum, bool, and relation columns. The generated list screen renders
  a responsive `ui.FilterToolbar` above the table — enums as pills (≤4 short
  values) or a `<select>`, bools as Yes/No pills, relations as a `<select>` of
  the related records' display names — folded into the **same** URL-driven GET
  form as the existing search box (never two competing forms). The owned
  `resource.go` engine applies each active facet as a server-side equality
  filter that composes with search, sort, and pagination (sort-header and page
  links preserve the active facets; applying a facet resets to page 1).
  Filtering is **explicit**: omit `filters:` and the list renders exactly as
  before. `validate` rejects a filter column that is unknown or of an
  unsupported type. `pack` round-trips `filters:` back to YAML. The Meridian
  flagship's customers list (enum pills) and invoices list (enum + relation
  selects) now exercise it. This closes the last gap that made both cold-start
  evaluations hand-write their main list screen.
- **Blueprint fonts are self-hosted and actually work.** A theme naming
  `font_heading` / `font_body` used to emit `@font-face` rules pointing at
  `/fonts/<slug>.woff2` while shipping no font files — every fresh app 404'd
  and silently fell back to system fonts (the strict CSP blocks the Google CDN,
  so a `<link>` never worked either). `gofastr generate` now **fetches** each
  family's latin `woff2` subset at generate time and writes it to
  `static/fonts/<slug>.woff2` (defaulting `static_dir` to `static/` when only a
  font is declared), so a named font renders with zero manual steps. Offline
  generation still emits the app but prints a loud warning naming the exact
  files to supply, and the generated `main.go` boot-checks for them — no silent
  404 path remains.
- **Blueprint seed `count:` and `weights:` for realistic demo data.** A seed
  entry can now declare `count: N` to auto-generate N demo rows (filling scalar
  + enum columns with deterministic, reproducible values) and an optional
  per-column `weights:` map to skew enum distributions. The unweighted default
  is a deterministic, *non-uniform* skew seeded from the entity name, replacing
  the flat `open/in_progress/resolved/closed` round-robin that read as obviously
  fake.

- **`owner.AllowCrossOwner(ctx)` — sanctioned cross-owner read escape.**
  Entities with `OwnerField` auto-scope every read to the signed-in user,
  with no way to express an app-legitimate cross-owner aggregate (e.g.
  "spots remaining = capacity − COUNT(bookings across ALL members)" or
  reading a whole waitlist to promote the oldest entry) short of dropping
  to raw SQL against framework-managed tables. `owner.AllowCrossOwner`
  returns a context that lifts owner scoping for the **in-process Go**
  `CrudHandler` methods (`ListAll`, `CountAll`, `GetOne`, and the
  mutate-by-id methods, which share the scope helpers) — the owner-side
  twin of `tenant.AllowCrossTenant`. Secure by default is untouched: the
  context key is unexported, so the auto-generated **HTTP CRUD endpoints
  have no path to it** and stay owner-scoped, always (regression-tested).
  It lifts the owner *requirement* only — it authorizes nothing; gate the
  caller yourself. See `docs → entity-declarations` → "Reading across
  owners".

- **`interactive.Action.WithConfirm(message)` — pre-flight confirm as a
  first-class builder method.** The only way to gate a destructive RPC behind
  a confirmation was `interactive.Confirm(msg)` passed to `OnSuccess(...)` —
  but the gate fires *before* the request, not on success, so its placement
  actively misled readers. `WithConfirm` reads in the order it executes and
  emits the identical `data-fui-confirm` attribute. `interactive.Confirm` is
  now deprecated (still works). See `docs → interactive-patterns` → "Confirm".

- **`ui.FilterToolbar` — the filter/sort control strip for list screens.**
  The framework had every filter *primitive* (`Select`, `SegmentedControl`,
  `SearchInput`, `FilterChipBar`) but no composed strip for the ubiquitous
  "row of facet controls + search + sort + Apply/Reset above a table"
  surface, so callers hand-rolled it — and repeatedly shipped the same
  mobile defect: the row overflowed a narrow container and pushed the sort
  control and Apply button off-screen (unreachable at 375px), while pill
  labels wrapped mid-label to three lines. `FilterToolbar` renders one
  URL-driven `<form method="GET">` (facets as `<select>` or `Kind:
  FacetPills` radio-pill groups, optional search + sort, Apply submit +
  Reset link) whose submitted params are the source of truth for the
  screen's `Load(ctx)`. It is responsive by construction — declares itself a
  container and degrades row → wrapped rows → single-column stack as its own
  width shrinks, keeping every control (Apply/Reset included) on-screen and
  tappable, with pills that wrap between themselves but never mid-label.
  Zero new `data-fui-*` attributes (native GET form + radio semantics);
  styling is theme-token CSS, light and dark. See `docs → ui-new-components`
  → "Filter toolbars — the URL-driven pattern".

### Fixed

- **`interactive-patterns` docs are now a complete hand-written-island
  cookbook.** Added an end-to-end recipe covering the four traps cold-start
  authors hit: a raw `data-fui-rpc` route needs its own `app.Router().Post`
  (only `widget.Mount` auto-wires); the RPC JSON key is the input's `name`,
  not its `id` (curl hides the mismatch); a `<select>` rides the existing
  `data-fui-rpc-trigger="input"` (no `change` trigger exists or is needed);
  and the two placeholder syntaxes — `{id}` on the HTTP router vs `:id` on the
  screen router — are a silent 404 when crossed. Also corrected stale
  `interactive.Action{…}` struct-literal examples (fields are unexported; use
  `interactive.Post(...)`) and documented `ui.ConfirmAction` as the themed,
  test-drivable alternative to native `window.confirm`. Behind the select
  recipe: new `TestInputTrigger_SelectFiresRPC` e2e in `core-ui/runtime`.
  README and `blueprints.md` now cross-link the cookbook so blueprint users
  find it before reverse-engineering `runtime.js`.

- **Chart / stat `source:` no longer silently renders "—".** A `stat_card` or
  `*_chart` bound to `source: {entity: X}` rendered an empty dash whenever `X`
  had no generated list/detail screen, because `RegisterGenerated` only
  populated `appResources` for entities with screens. Every entity referenced by
  a data source is now registered (pure lookup-map population — no extra routes),
  so charts sourced from screen-less entities show real numbers. `gofastr
  validate` / `generate` additionally reject a chart/stat source pointing at an
  unknown or crud-disabled entity up front.

### Changed

- **BREAKING (generator output): `gofastr generate` emits a flat `package
  main`, and is now a one-shot generator.** The blueprint used to scaffold a
  `blueprint/` subpackage (`package blueprint`: `app.go`, `screens.go`,
  `resource.go`, `stubs.go`, `resource_test.go`) alongside `main.go`. That
  folder read as "the generator's", forced every custom screen into it, and
  the unexported `blueprintAuthPolicy` etc. leaked generator branding into
  your code. Those files now land at the project root as ordinary
  `package main` (`entities/` is unchanged), and `main.go` calls
  `RegisterGenerated(...)` directly instead of importing a `blueprint`
  package. Emitted identifiers dropped the `Blueprint`/`blueprint` prefix
  (`BlueprintAppName`→`appName`, `blueprintAuthPolicy`→`authPolicy`,
  `BlueprintBaseCSS`→`appBaseCSS`, `BlueprintFontCSS`→`fontFaceCSS`,
  `blueprintResources`→`appResources`, `BlueprintSeedData`→`seedData`, …).
  Generation now **refuses** to overwrite an existing project — if any target
  file is present it lists the conflicts and stops; pass `--force` to
  overwrite. There is no merge/regen workflow: the emitted code is yours to
  own and edit. **Existing generated apps are unaffected at runtime** — only
  the output of a fresh `generate` changes; re-run `generate --force` into a
  scratch dir if you want the new layout. `gofastr pack` reads the new flat
  layout.

- **`ui.BarChart` is legible by default.** Dashboard bar charts previously
  rendered as flat, unlabeled slabs: no way to read magnitudes, near-equal
  or uniform data (8/8/8/8) all looked like identical full-height
  rectangles, and 4+ category labels collided/truncated mid-word. The chart
  now (1) prints each bar's value above its cap by default (opt out with
  `BarChartConfig.HideValues`); (2) rounds the y-scale up to a clean maximum
  so the tallest bar keeps ~15% headroom — uniform and near-equal data read
  as intentionally-equal or clearly-ranked bars, never a wall of slabs;
  (3) always draws a hairline baseline; (4) wraps long `ShowLabels` category
  labels onto two lines (a single over-long word ellipsizes; the full text
  stays in the bar's `<title>`); and (5) caps bar thickness so 1–2 category
  charts don't render giant blocks. `ShowAxis` now renders proper gridlines
  at clean tick values with left-gutter numeric labels. All styling stays in
  `registry.RegisterStyle` + theme tokens (verified light + dark + 375px).
  Default `Height` is now 200 (was 180) to fit the value + label bands.
- **`ui.LineChart` edge x-axis labels no longer clip.** The first and last
  tick labels sit exactly on the SVG's left/right boundary; they now anchor
  inward (`start` / `end`) instead of centering and clipping.

## [0.11.0] - 2026-07-03

### Documentation

- **Doc code samples that cited non-existent APIs are corrected** —
  `app.Router` as a field → `app.Router()`, `Router.With(...)` → wrap
  the handler with `RequirePermission(...)`, `u.HasRole` →
  `slices.Contains(u.GetRoles(), …)`, `Registry.Names()` →
  `range Registry.All()`, `cron.Scheduler.Every(string, fn)` →
  `Register(CronJob{…})`, and the phantom webhook `Sign`/`Verify`/
  `sha256=` paragraph removed. A new `framework/docs` regression test
  (`TestDocsAvoidKnownWrongAPIs`) fails if any of these forms — or a
  `migrate diff` command reference — reappears in an embedded doc.
- **Doc drift cleaned up** — README `migrate diff` → `migrate generate`
  and `force`; CLI `--help` lists `audit lint` and the full `migrate`
  subcommand set; blueprint field keys (`auto_generate`, `read_only`,
  `hidden`, `pattern`) and `app.theme` font/dark tokens documented;
  flagship renamed ecommerce→meridian in `comparison.md`; the "~10 UI
  primitives" count corrected to 90+; stale "@main install" tutorial
  note removed; new `scaling.md`/`deploy.md`/`observability.md`/
  `agent-ready.md` added to the README index.

### Fixed

- **Reliability footguns** (Tier 3):
  - `battery/queue` — the DBQueue-cron `Scheduler` re-reads its schedule
    set each tick and re-arms on `Register`, so jobs registered after
    `Start` fire (it used to snapshot once and drop late registrations).
    New `WithDBHandlerTimeout` cancels a stuck handler's context so a
    black-holed dependency can't wedge the (single default) worker. The
    in-memory queue re-enqueues timed-out retries on a fresh context
    instead of the already-cancelled one (retries were silently lost).
  - `battery/email` — the SMTP sender bounds its dial (`SMTPConfig.
    DialTimeout`, default 10s) and sets the same budget as the
    connection's I/O deadline (the caller's ctx deadline wins when
    sooner, including on the implicit-TLS path), so neither a
    black-holed host nor one that accepts and then stalls mid-exchange
    can hang the caller forever.
  - `battery/cache` — `NewMemoryCache` warns when built both unbounded
    and never-expiring (the OOM shape).
  - `framework/event` — a panicking subscriber is still recovered (no
    write rollback) but now logged at Error with its stack, instead of
    silently no-op'ing.
- **Generator design-system regression (`.mrd-*`) removed** (`cmd/gofastr`).
  The blueprint generator emitted dead `mrd-chart`/`mrd-chart__title`/
  `mrd-muted` classes into every generated app — the exact one-styling-
  surface tripwire documented as fixed in June. Titled charts now compose
  `ui.Card(Heading: …)`, and empty values render the new `ui.EmptyValue()`
  (a `ui.Muted` em dash, colored by `--color-text-muted`).
  `TestGeneratorEmitsNoBespokeClasses` pins the contract; meridian and
  ecommerce were regenerated.
- **`line_chart` blocks render** (`cmd/gofastr`). `line_chart` validated
  and compiled but fell through to an HTML comment. It now renders a
  `ui.LineChart` over the grouped counts, and all three chart kinds
  **require** a valid `source: {entity, group_by}` at validation time
  instead of silently disappearing.
- **`examples/backoffice` ships zero bespoke CSS.** The `.bo-*`
  stylesheet and hand-rolled form markup are gone; the public pages
  compose `ui.Hero`, `ui.AuthCard`, `ui.Form`/`ui.FormField`, and the
  centered-container layout shell.

### Added (design system)

- **`ui.Muted` / `ui.EmptyValue`** (`framework/ui`) — subdued inline
  text and the canonical muted em-dash "no value" placeholder, colored
  via `--color-text-muted`.

- **`WithConfig` merges instead of clobbering** (`framework`). Granular
  options (`WithAPIPrefix`, `WithPublicOpenAPI`, …) set before or after
  `WithConfig` now survive: each non-zero `AppConfig` field wins, zero
  fields preserve what's already set — the same contract the
  `WithAgentReady` fix established. A reflection test pins the field
  list so new `AppConfig` fields can't silently drop out of the merge.
- **Graceful shutdown is now the default** (`framework`). `App.Start`
  installs a SIGINT/SIGTERM handler that runs the full `App.Shutdown`
  drain — HTTP server, batteries, OnStop hooks — before the process
  exits, matching what `deploy.md` always claimed. The drain is bounded
  by the new `AppConfig.ShutdownTimeout` (default 15s): connections
  still open at the deadline (e.g. SSE streams, which never go idle)
  are force-closed instead of hanging the drain. In-flight cron job
  goroutines are joined under the same deadline via the new
  `cron.Scheduler.StopContext`; `AppConfig.DisableSignalHandling` opts
  out for hosts that own signal handling themselves.

### Added

- **Horizontal-scaling doc + multi-replica boot warnings.** New
  `scaling.md` doc page consolidates every process-local default
  (sessions, 2FA state, rate limits, cron, in-memory queue, SSE push,
  cache) with its replica-safe alternative. `battery/auth` production
  mode now logs a WARN at Init when running on the default in-memory
  session or 2FA store; `AuthConfig.AllowInMemoryStores: true` is the
  explicit single-node opt-in that silences it.

### Security

- **HSTS emitted by default** (`core/middleware`). The default
  `SecurityHeadersConfig` now sends `Strict-Transport-Security:
  max-age=31536000` on HTTPS responses — direct TLS or an
  `X-Forwarded-Proto: https` proxy. Previously the zero-value config
  meant no HSTS at all. `HSTSMaxAge: -1` opts out.
- **CSRF cookie is `Secure`/`__Host-` behind a TLS proxy**
  (`core/middleware`). The CSRF middleware now treats
  `X-Forwarded-Proto: https` as HTTPS, so a proxy-terminated deployment
  gets the secure cookie instead of a plain one. Both this check and
  the HSTS one compare the header case-insensitively (matching uihost),
  so proxies that send `HTTPS` count too.
- **Login/register/logout reject cross-site form posts** (`battery/auth`).
  These cookieless-CSRF-prone endpoints now refuse a form POST whose
  `Origin` (or `Sec-Fetch-Site: cross-site`) is another site — closing
  the login-CSRF vector SameSite cookies don't cover. JSON posts and
  no-Origin clients (curl, native apps) are unaffected.
- **`/auth/register` is rate-limited by default** (`battery/auth`). A new
  `AuthConfig.RegisterRateLimit` defaults to 10/min/IP (15-min block),
  matching login's always-on throttle, to blunt account-table flooding
  and email bombing. Form submissions that hit the limit get the
  form-aware 303 error redirect (not a raw JSON 429 page), and
  cross-site posts are rejected before they count against the budget —
  an attacker page can't burn a victim's own login/register allowance.
- **Blueprint refuses `multi_tenant` with no resolver** (`cmd/gofastr`).
  A generated multi-tenant app has no tenant resolver (the strategy is
  host-specific) and `ApplyTenantScope` is fail-closed, so it read empty
  and stamped empty tenants — broken while looking secure. `validate`/
  `generate` now reject it with the manual-wiring remedy.
- **`golang.org/x/image` bumped to v0.43.0** — clears the four
  GO-2026-506x/4961 decode-DoS advisories reachable from
  `framework/image`.
- **BREAKING: `battery/admin` exposure is opt-in.** An empty
  `admin.Config.Entities` now exposes **nothing** instead of every
  CRUD-enabled table as an editable back-office. Set the new
  `AllEntities: true` for the previous whole-back-office behavior
  (still skips `CRUD=false` credential tables), or name entities
  explicitly. The blueprint generator and examples set `AllEntities`.
- **Generator warns on ANY unscoped auto-exposed entity**
  (`cmd/gofastr`). The PII lint only matched a fixed token list, so
  `notes`/`journal_entries`/`balances` generated fully public with no
  signal. `gofastr generate` now warns for every auto-exposed entity
  with no `owner_field`/`access`/`multi_tenant`, spelling out the
  anonymous read/write exposure. `examples/api-tour`'s `profiles`
  (per-user bio) now gates its write operations via `Access` — reads
  stay public for the `?include=` tour flows, anonymous writes 403.
- **Webhook SSRF guard survives a custom `HTTPClient`**
  (`battery/webhook`). Supplying `Options.HTTPClient` (proxy, tracing,
  timeout) previously dropped the dial-time SSRF guard entirely,
  reopening loopback/RFC1918/169.254.169.254 via DNS rebinding. `New`
  now wraps the caller's client with a per-request check that resolves
  the delivery target and refuses internal IPs before the caller's
  transport runs — the transport itself (proxy, tunnel, custom dialer)
  is used verbatim. `AllowPrivateNetworks: true` remains the only
  opt-out.
- **Blueprint-generated apps no longer commit secrets** (`cmd/gofastr`).
  Generated Go reads `JWT_SECRET`, `DATABASE_URL`, and
  `ADMIN_SEED_PASSWORD` from the environment instead of inlining the
  blueprint's values as string literals (the emitted `BlueprintDBURL`
  constant is password-stripped, and the generated e2e test reads the
  admin password from env too). When the blueprint holds secrets the
  generator emits a `.env` (so the app still runs out of the box) plus
  a `.gitignore` excluding it, and `main.go` loads `.env` before the DB
  opens. A deploy without `ADMIN_SEED_PASSWORD` logs a WARN instead of
  silently skipping the admin seed; the generated e2e test falls back
  to a test-local password so a fresh checkout stays green; DSN
  redaction fails closed on unparseable URLs and handles libpq-quoted
  passwords. `.env` values are quoted when their shape requires it, and
  `gofastr pack` reads the file back with the same `core/dotenv` parser
  the generated app boots with, so awkward secrets round-trip exactly.
  `TestBlueprintNeverInlinesSecrets` pins the contract.
- **2FA now fails closed at login** (`battery/auth`). If a registered
  `TwoFactorChecker` reports a user enrolled but the pending-2FA state
  can't be established — the session store doesn't implement
  `SessionPendingMarker`, the mark call errors, or the 2FA state lookup
  itself fails — login is rejected (500), the just-minted session is
  destroyed, and a WARN is logged. Previously a custom session store
  silently downgraded every 2FA-enrolled account to password-only auth.
- **Pending-2FA logins no longer receive a JWT.** The JSON login
  response for a 2FA-enrolled user omits `token` until the challenge
  succeeds (a stateless JWT issued at password time bypassed the second
  factor on JWT-authenticated routes) and now carries
  `"two_factor_required": true` so clients can branch without probing.

## [0.10.0] - 2026-06-27

### Added — agent-readiness surface (isitagentready.com)

GoFastr apps can now advertise the discovery artifacts AI web agents
(and scanners like isitagentready.com) look for, in one opt-in call.
The framework already shipped the plumbing — MCP tools, an OpenAPI spec,
per-screen markdown, sitemap, robots — so this is the *discovery* layer
that makes those capabilities findable. Everything is additive and opt-in;
existing robots/sitemap/openapi/llm.md behavior is unchanged.

- **`uihost.WithAgentReady`** (`framework/uihost`) — one-call bundle: serves
  `/llms.txt` + the A2A agent card + AI-bot-aware robots rules + `Link`
  response headers on every HTML page. Granular options
  (`WithLLMsTxt`, `WithAgentCard`, `WithAgentLinkHeaders`,
  `WithMarkdownNegotiation`) expose each piece.
- **`/llms.txt`** (llmstxt.org) — curated markdown index (H1 title,
  blockquote summary, `## Section` file-lists); a default Docs section
  links the app's `/llm-pages.md` index when `WithPublicLLMMD` is on.
- **A2A agent card** — `/.well-known/agent-card.json` (+ legacy
  `/.well-known/agent.json`) describing identity, service URL,
  capabilities, and skills (Agent2Agent v1.0, camelCase keys; `supportedInterfaces`
  + `skills` always present). The service endpoint lives in
  `supportedInterfaces[].url` — when `MCPEndpoint` is set, `/mcp` is
  advertised as the JSON-RPC interface (it genuinely speaks JSON-RPC)
  and a derived `mcp` skill points agents at it.
- **AI-bot-aware robots** — `AllowAIBots` augments `/robots.txt` with
  explicit per-crawler rules (GPTBot, ClaudeBot, Google-Extended, …),
  merged into the existing `WithRobots` config regardless of option order.
- **`Link:` response headers** — every HTML page advertises
  `rel="sitemap"`, `rel="llms-txt"`, `rel="agent-card"`,
  `rel="service"` (the MCP endpoint), `rel="service-desc"` (OpenAPI, when
  `OpenAPIEndpoint` is set), and `rel="alternate"` markdown.
  Absolute URLs resolve one canonical origin (`WithAgentReady`/`WithSitemap`
  `BaseURL`, else the forwarded request scheme+host).
- **Markdown content negotiation** — `WithMarkdownNegotiation()` serves a
  page's markdown when the request `Accept`s `text/markdown`.
- **`framework.WithMCP`** — auto-mounts `/mcp` (Streamable HTTP: POST
  JSON-RPC + GET SSE), replacing the hand-wired
  `Router().Handle("POST","/mcp", MCP)`.
- **`framework.WithOAuthProtectedResource`** — serves
  `/.well-known/oauth-protected-resource` (RFC 9728) for OAuth-token-
  protected APIs.
- **MCP handshake** — `core/mcp` now handles `initialize` (returns
  protocolVersion + capabilities + serverInfo, name wired from the app's
  `Config.Name`) and `ping`, so the advertised `/mcp` is functional for
  spec-compliant MCP clients (Claude, Cursor, …), not just `tools/list`.
- **Scanner well-known endpoints** — the isitagentready.com checks the
  framework now auto-serves: `/.well-known/api-catalog` (RFC 9727
  linkset+json, when the app has an API), the MCP Server Card at both
  `/.well-known/mcp/server-card.json` (scanner path) and the spec-reserved
  `/mcp/server-card` + `/.well-known/mcp/catalog.json` (SEP-2127 shape:
  $schema/name/version/description/remotes), when `WithMCP` is on,
  `/.well-known/agent-skills/index.json` (always; opt-in entries via
  `WithAgentSkills`), and opt-in `/.well-known/oauth-authorization-server`
  (RFC 8414, via `WithOAuthAuthorizationServer`).
- **Content Signals** — `AgentReadyConfig.ContentSignals` emits a
  `Content-Signal:` directive in robots.txt (contentsignals.org), e.g.
  `ai-train=no, search=yes, ai-input=yes`.
- **Auth.md** (WorkOS agentic-registration profile) — `WithAuthMD` serves
  `/auth.md` (a markdown manifest) and merges an `agent_auth` block
  (skill + identity/claim/events endpoints) into the
  `/.well-known/oauth-authorization-server` metadata.
- **Web Bot Auth / UCP / ACP** (remaining production-scanner checks) —
  `WithWebBotAuth` serves `/.well-known/http-message-signatures-directory`
  (the site's signing JWKS), `WithUCP` serves `/.well-known/ucp`, `WithACP`
  serves `/.well-known/acp.json`. DNS-AID / x402 / MPP / WebMCP / ap2 remain
  documented-only (DNS / payment-middleware / client-side / server-only).
- **Docs** — new `framework/docs/content/agent-ready.md` reference;
  `examples/site` dogfoods the full bundle (`WithAgentReady` +
  `WithMCP` + `WithMCPIntrospection`) so gofastr.dev is agent-ready, and
  now also serves `Accept: text/markdown` content negotiation
  (`WithPublicLLMMD` + `ContentNegotiation`).

### Fixed

- **`uihost.WithAgentReady` merges instead of clobbering.** It replaced the
  agent-ready config wholesale, silently dropping any granular option
  (`WithMarkdownNegotiation`, `WithLLMsTxt`, `WithAgentCard`,
  `WithAgentLinkHeaders`) set before it. It now merges field-by-field, so the
  bundle and granular options compose regardless of option order.
- **`examples/site` docs catalogue** — the `/docs/` "Every doc · A–Z" index
  linked 10 embedded docs (incl. the new agent-ready reference) that had no
  registered route, rendering live links to 404s. All are now catalogued
  (51 → 61 doc pages), guarded by a new embedded-doc → catalogue parity test.

What this deliberately does not do: no A2A task server (the A2A card is
discovery-only; A2A/Auth.md/WebMCP/commerce aren't among the scanner's
scored checks); DNS-AID (infra/DNS, documented); Web Bot Auth (client-side
RFC 9421, documented); commerce (x402/MPP/UCP/ACP — no core primitives).
The 11 scored isitagentready checks are all covered (6 always-on, the rest
opt-in/conditional).

## [0.9.0] - 2026-06-25

### Added — `log.ConsoleSink` (zero-config colorized dev feed)

The log battery now ships a human-readable console sink alongside the
JSON file sink, so `log.New(Config{})` gives every local developer a
colorized stderr feed with no configuration — without leaking ANSI into
prod where stderr is captured (journald, containers) rather than shown.

- **`log.ConsoleSink(ConsoleOpts)`** (`battery/log`) — a Sink that renders
  each JSON entry as a single human-readable, optionally colorized line
  (`14:32:07.412 INFO  app.start app="myapp" go="go1.24.1"`). Level
  colors, a bolded message, and a dimmed timestamp; attr order preserved
  via token decoding so operators see fields in emit order, not
  `json.Unmarshal`'s randomized map order. Multi-line values (e.g. panic
  stacks) keep newlines escaped so each entry stays one line; non-object
  bytes are written verbatim so a malformed entry is visible, not
  silently dropped. Honors the `NO_COLOR` convention; serializes on a
  mutex so concurrent entries don't interleave.
- **`log.Config.Console` (`ConsoleMode`)** — `ConsoleAuto` (the zero
  value) attaches the sink only when stderr is a terminal and `NO_COLOR`
  is unset; `ConsoleOn` forces it on regardless of TTY (coloring still
  follows TTY + `NO_COLOR`, so piped output drops ANSI and stays
  greppable); `ConsoleOff` disables it. The console sink is appended last
  so it closes last on shutdown.
- **Purely additive** — the file and webhook sinks run unchanged
  alongside it; nothing about the existing JSON log surface moves.

## [0.8.1] - 2026-06-17

### Changed — README rework

- **Added a "Why this exists" section** stating plainly that GoFastr is a
  personal project first: solidifying web-tech foundations, attacking UI
  generation from a compiled-language angle (the author's background is
  Node), working in a compiled language, skipping the convention-vs-
  configuration choice, building something large with AI, and making a
  framework that's AI-first on both the authoring and the consuming side.
- **Removed the framework comparison** from the README — the
  PocketBase / Encore / Wasp / Supabase / FastAPI name-drops and the
  `comparison.md` link. (The `comparison.md` file itself is left on disk
  for now.)
- **Demoted Kiln to a brief experimental mention.** The README no longer
  leads with it: the ~60-line Kiln section became one paragraph linking
  to `kiln.md`, the "Built with GoFastr" Kiln bullet and the `cmd/kiln`
  install line were dropped, and the repo-layout line is marked
  `(experimental)`.
- **Migrated the README-only Kiln detail into `kiln.md`** so nothing was
  lost: plan-gated destructive ops, the full tool surface, the Claude Code
  MCP wiring, and a concrete HTTP tool-call example.
- **Rephrased "dogfooded" → "runs on itself";** fixed the opaque
  "Walkthrough: the v2 read/write surface" heading → "Walkthrough: the
  read/write API".

## [0.8.0] - 2026-06-16

### Added — `gofastr export` (native static-site generation)

The framework now exports a deploy-ready static site itself, replacing the
broken `wget --mirror` crawl used for the GitHub Pages deploy. The crawl baked
cache-bust `?v=<hash>` queries into on-disk module filenames; the static host
strips the query, every split runtime module 404'd, and all client
interactivity (theme toggle, command palette, copy, widgets) silently died.

- **`App.ExportStatic(ctx, dir, basePath)`** (`framework`) — drives the app
  in-process (no port, no crawl), enumerates every declared route, renders each
  through the SSG-aware path, and dumps all `/__gofastr` assets (split runtime
  modules, `color-scheme.js`, `app.css`, per-component CSS) with **query-free
  filenames**. Finds the `*uihost.UIHost` via `Mountables()`.
- **`static.Builder`** (`framework/static`) — the already-tested builder, now
  wired as the export engine. Emits query-free assets, the `color-scheme.js`
  bootstrap, and split runtime modules via `runtime.ModuleNames()`/`Module()`.
- **Runtime static-mode** (`core-ui/runtime`) — a `data-fui-static` marker on
  `<html>` (stamped only at export time) no-ops every server-backed dispatch
  (RPC, widget-catalog fetch, `data-fui-open`) so disabled actions read as
  intentionally inactive, not broken. Client-only features (theme, copy,
  signals) are unaffected; live pages carry no marker so the guards are no-ops
  in the normal server-backed app. Detected via `hasAttribute` to stay within
  the runtime's 12 KB gzip budget.
- **Subpath base path** (`--export-base /<repo>`) — a GitHub Pages *project*
  site serves the artifact under a subpath, so the builder prefixes every
  root-absolute `src`/`href`, the inline component-catalog `stylePath` JSON
  values, and bakes the prefix into the emitted `runtime.js` (it constructs
  split-module URLs in JS). External links, fragments, and code samples
  (`core/markdown` escapes quotes in `<code>`) are left untouched. Omit for an
  apex/custom-domain deploy.
- **`ui.Banner` "static preview" notice** — injected at export time only,
  dismissible (`DismissID: gofastr-static-preview`, persists in `localStorage`),
  explaining that server-backed actions are disabled and how to run locally.
- **`examples/site` `--export` / `--export-base` flags** — the same binary
  serves live *or* exports; the live `Start` path is byte-identical when the
  flag is absent.
- **Docs** — new `framework/docs/content/static-export.md` guide (what it is,
  how to export, what gets emitted, static-mode behavior, subpath vs apex,
  GitHub Pages workflow, common mistakes). `.github/workflows/pages.yml` now
  runs `site --export _site --export-base /gofastr`.

### Added — `ui.CodeBlock.Scroll` + `ui.HighlightLines`

- **`CodeBlockConfig.Scroll`** (`framework/ui`) — caps the body height
  (`var(--ui-code-block-scroll-max, 26rem)`) and makes it scroll vertically, for
  showing a long file in full without it dominating the page. Forces the framed
  container; horizontal panning still works.
- **`ui.HighlightLines`** (`framework/ui`) — the fenced-block tokenizer is now
  exported, so callers can render raw source through `CodeBlockConfig.Lines`
  with the same comment/string/number highlighting the markdown renderer uses.
- **`/examples` Meridian row shows the real blueprint** — the synthetic,
  malformed pseudo-YAML snippet is replaced with the **exact, full
  `examples/meridian/gofastr.yml`** (embedded at build time, drift-guarded by
  `TestEmbeddedBlueprintsMatchSource`), shown in a scrolling, copyable block.
  The copy button copies valid YAML with newlines (via `innerText`).

### Fixed

- **Sticky sidebars were silently broken site-wide.** `body { overflow-x:
  hidden }` (latent since the site-chrome commit) forced `overflow-y` to
  compute as `auto`, turning `<body>` into a scroll container; every
  `position: sticky` descendant (docs + components sidebars, in-page TOC,
  get-started step-rail) then anchored to the non-scrolling `body` and
  scrolled away with the page. The horizontal-scroll guard now lives on
  `html` (the real viewport scroller); `body` is `overflow: visible`.
- **Components-page sidebar wouldn't pin even after the above.** The
  framework's sidebar `<nav>` wrapper is the grid column but didn't pass its
  height down to the `SectionMenu` widget inside it, leaving the sticky rail's
  containing block too short to travel. The wrapper is now a flex column so
  the widget fills the column (no-op on short pages; pins on tall ones).
- **Header brand read as a doubled version** (`λ gofastr v0.x dev`). Dropped
  the static `v0.x` stability tag sitting beside the version; the badge now
  shows one version (`dev` locally, `v0.8.0` tagged), with the "v0.x — pin a
  version" warning kept as the status tooltip.

### Changed

- **Homepage + getting-started repositioned to lead with screens + blueprints.**
  A new "One file, a real app" section pairs a generated screen mock with the
  `gofastr.yml` blueprint + `gofastr generate` output, and the hero lede
  foregrounds screens/endpoints/MCP/migrations instead of centering the data
  layer.
- **Kiln marked experimental** across the site + `framework/docs/content/kiln.md`
  (hero pill, get-started card, footer, palette, docs catalog) so its maturity
  is honest at first glance.

## [0.7.0] - 2026-06-15

### Added — marketing pricing + long-form content blocks

- **`ui.PricingCard`** (`framework/ui`) — a real marketing pricing card (plan
  name, headline price + period, checked feature list, CTA, featured variant) so
  pricing pages read like marketing instead of a CRUD table. Composed via a new
  `pricing` blueprint block (`props.plans[]`).
- **`markdown` blueprint block** renders rich long-form prose via `ui.Markdown`
  from a `text:` string; plain `heading`/`paragraph` content is now typeset to a
  readable measure on marketing pages instead of running full-bleed. The Meridian
  flagship's `/pricing` is now pricing cards and `/terms` + `/privacy` are
  markdown, demonstrating all three content treatments.

### Added — `gofastr pack` (the inverse of generate)

- **`gofastr pack [app-dir]`** reconstructs a `gofastr.yml` from a generated app's
  Go source — the inverse of `gofastr generate`. It reads the real artifacts
  (`entities/register.go`, `blueprint/app.go`, `blueprint/stubs.go`,
  `blueprint/screens.go`) via the Go AST and re-serializes the authored blueprint:
  app config + theme/dark + auth + admin, every entity (fields, types, access,
  indices, relations), the screens (reversing the emitted `framework/ui` grammar —
  hero, sections, cards, charts, stat cards, entity list/detail, auth forms,
  headings), nav, and seed. Synthesized `/new` + `/{id}/edit` form screens are
  dropped (they weren't authored). A round-trip test gates the invariant
  `parse(meridian.yml)` ≡ `parse(pack(generate(meridian.yml)))`, so generator↔pack
  divergence is caught as features are added.
- Two supporting fixes the round-trip surfaced: generated entity order now follows
  the blueprint's authored order (was alphabetised); and `entity_list`'s `text:` /
  `empty_text:` are now wired (custom list heading + empty-state copy) instead of
  silently ignored.

### Added — blueprint generates real, full web apps

The blueprint generator now emits **owned Go that composes the full `framework/ui`
catalog** instead of raw HTML, turning a blueprint into a credible product (see
the new `examples/meridian` flagship — a SaaS billing console + marketing site).

- **Server-rendered entity screens.** `entity_list` / `entity_detail` are emitted
  as request-time SSR screens (`RenderCtx`) backed by the entity's `CrudHandler`,
  composing `ui.DataTable` / `ui.PageHeader` with an owned `blueprint/resource.go`
  engine: humanized headers, formatted cells (bool→Yes/No + status badges, enums,
  `$` money, dates), foreign keys resolved to the related record's name, and
  server-side search / sort / pagination + empty states. Replaces the old
  client-fetch raw-`<table>` islands.
- **Full UI catalog as blueprint blocks** — `page_header`, `hero`, `section`,
  `card`, `stat_row`, `stat_card`, `bar_chart`/`pie_chart`, `link_button`,
  `callout`, plus data-bound dashboard widgets: `stat_card`/charts with
  `source: {entity, agg: count|sum, field, group_by, filter}` compute live
  metrics server-side.
- **Marketing + app layouts** — `screen.layout: marketing` uses
  `ui.SiteHeader`/`ui.SiteFooter`; `layout: app` uses the sidebar shell.
- **Auth screens + RBAC gating** — `signup_form` block; `screen.access:
  {auth: true, role: …}` emits an `appui.Policy` that redirects anonymous GETs to
  the login page (with `?next=`) and 403s a signed-in user missing the role.
- **Writable app screens (create / edit / delete).** `entity_list` gains a
  `create: true` flag → a "New <Singular>" button + a synthesized `<route>/new`
  create form; every `entity_detail` gets **Edit** + **Delete** header actions and
  a synthesized `<detail>/edit` form prefilled from the record (enum/relation
  `<select>`s render their options + selection server-side). Forms submit as
  `data-fui-rpc` islands and SPA-navigate back on success. The resource engine
  gained a `Form(ctx, id)` method; the generator installs an `access.RolePolicy`
  (admin role → wildcard) + `access.Middleware` so the gated CRUD API actually
  accepts the signed-in operator's writes instead of 403ing.
- **The generator emits a test for every generated surface.** Each app gets an
  owned `blueprint/resource_test.go` (formatting + input-type helpers) and a
  comprehensive `e2e_test.go` that builds + boots the binary and asserts: the
  home brand; **every** static public screen renders; **every** gated screen
  redirects anonymous callers and renders once signed in; a full
  **create → read (detail + edit) → update → delete** lifecycle against a
  writable entity through its CRUD API + form screens; and that an anonymous
  write to an access-gated entity is refused (RBAC). The create payload is
  synthesized from the entity's required fields, so the suite stays valid across
  schemas.
- A standard `box-sizing: border-box` reset + body theme surface ship in the
  generated base CSS so padded full-width bars don't overflow on mobile.

### Added

- **`core-ui/node` + `core-ui/noderender`** — the JSON-clean UI node tree
  (`Node`, `Action`, tree helpers) and its HTML renderer, extracted from
  `kiln/world` + `kiln/noderender` into first-party `core-ui` packages. The
  blueprint codegen no longer emits any import of the `kiln/*` namespace;
  Kiln consumes the node packages like any other caller (`kiln/world`
  type-aliases `node.Node`/`node.Action`, so kiln-internal code is unchanged).
- **`data-action-mount` runtime primitive** — fires a compiled component
  action once on hydration (and after each SPA nav), so a server-rendered
  island can populate itself on load without a user event.
- **`ScreenGroup.Standalone()`** (`core-ui/app`) — marks a screen group as a
  self-contained shell so the host App's default layout does NOT also wrap it.
  `battery/admin` uses it: the back-office mounts on the host's App but renders
  its own sidebar, which previously nested inside the app's sidebar (a
  double-sidebar). Now the admin renders a single, correct shell.
- **Blueprint `app.api_prefix`** (default `"api"`) — entity JSON CRUD mounts
  under `/api/<table>`, freeing the bare `/<table>` path for HTML screens.
- **Blueprint login + admin back-office** — a `login_form` screen block renders
  a no-JS HTML sign-in form (posts to the auth battery, redirects on success),
  and `app.admin` wires the admin battery: an editable CRUD back-office over
  every entity at `/admin`, gated by a role, with `seed_email`/`seed_password`
  bootstrapping an admin account on first boot. `battery/admin` gained a
  `LoginPath` config — an unauthenticated GET redirects to the login page
  instead of returning a bare 401.

### Changed

- **Meridian is the flagship example** across the README and the website
  (`/examples`, the home grid). It supersedes ecommerce as the headline blueprint
  demo (ecommerce stays as a second, owner-scoped pipeline example). Acronym field
  labels now read correctly (MRR, ID, URL, …) instead of "Mrr"/"Id".
- **Seed rows generate with sorted keys**, so re-running `gofastr generate` no
  longer churns `blueprint/stubs.go` with random map-iteration order.
- **Blueprint-generated apps are now usable websites end-to-end.** Screens
  routed at an entity's path are no longer shadowed by the CRUD JSON handler
  (the API moved under `/api`); `entity_list` / `entity_form` / `entity_detail`
  blocks render and auto-populate on load (including when nested inside a
  `section`/`div`, which previously degraded to an HTML comment); `enum` fields
  render populated `<select>`s and `relation` fields render selects populated
  from the related entity; forms submit via `data-fui-rpc`; dynamic detail
  routes (`/x/{id}`) resolve; and declared `seed:` data is applied on first
  boot (ordered, idempotent, decimal-coerced, non-fatal on a bad row).
  Generated apps ship a responsive base stylesheet (`BlueprintBaseCSS`) and a
  body font floor so they render in a system/Inter stack instead of the
  browser serif default.

  **BREAKING (generated apps):** regenerated blueprint apps serve entity JSON
  at `/api/<table>` instead of `/<table>`. Set `app.api_prefix: ""` to keep
  the old bare paths. MCP tools and the OpenAPI spec follow the prefix
  automatically.

### Added — per-user data isolation + complete auth UX

- **`owner_field` auto-creates the owner column.** Declaring `owner_field` now
  synthesizes a hidden owner column (AutoMigrate creates it; it never appears in
  generated forms/tables) — you no longer hand-declare the field. The generated
  seed runs *as* the bootstrap admin, so demo data is owned and a freshly
  registered user starts with an empty, owner-scoped workspace. Meridian's
  customers/subscriptions/invoices/payments are now per-user (`owner_field:
  user_id`); plans stays a shared catalog.
- **Auth is a full UX, not just a gate.** `ui.SignOut` (a POST logout control)
  in the app sidebar footer and the marketing header; an **auth-aware marketing
  header** (`app.NewContextComponent`) that shows a Dashboard link + Sign out
  when signed in, Sign in when not; a **guest policy** that redirects
  already-signed-in visitors off the login/signup screens.
- **Inline auth errors.** `ui.AuthCard` gained an `Alert` slot, and a failed
  form login now redirects back to the login page with `?error=` (via
  `auth.SetDefaultLoginErrorPath`) and renders "Invalid email or password."
  inline — instead of dumping a raw JSON error body.
- **Role-aware navigation.** A nav item `role:` (→ `ui.SidebarItem.Roles` +
  `ui.SetRolesExtractor`) filters role-gated entries by the signed-in user's
  roles, on both the desktop sidebar and the mobile drawer — a link a user can't
  use never appears (and is never a dead end into a 403).
- **The auth battery self-migrates** its `auth_users` / `auth_sessions` tables
  (`EntityUserStore`/`EntitySessionStore.EnsureSchema`, dialect-aware), so the
  generated app ships **zero** hand-rolled DDL.

### Added — SPA cross-layout navigation + resilient chrome

- **Cross-layout SPA navigation.** The outermost shell carries
  `data-fui-layout`; the route manifest carries each route's layout; the runtime
  detects a layout change (e.g. marketing → app) and swaps the *whole* shell
  instead of just the content, so the new screen renders in the right chrome —
  no hard reload.
- **Charts render an empty state instead of panicking.**
  `ui.BarChart`/`PieChart`/`LineChart`/`Sparkline` no longer crash the page on
  empty data (a zero-data user's dashboard previously 404'd behind the SSR host's
  panic recovery) — they show a calm "No data yet" placeholder.
- **`ui.SiteHeader` collapses its actions into the mobile drawer**, so an auth
  header no longer overflows a phone bar; the bar becomes brand + hamburger.
- **`app.NewContextComponent`** + ctx-aware widget chrome
  (`component.RenderComponentCtx`, `serveChrome` renders with the request
  context), so per-request chrome (role-aware nav drawer, auth-aware header) sees
  the signed-in user.
- The app shell drops its empty top bar — the theme toggle lives in the sidebar
  footer (visible on desktop, in the drawer on mobile).

### Fixed

- **The framework-managed owner column is persisted on create/upsert.**
  `doCreate`/`doUpsert` skipped Hidden/ReadOnly fields when building the INSERT,
  silently dropping the owner id `InjectOwner` stamps — so owner-scoping matched
  nothing and a seeded admin's rows were invisible. The owner column is now
  exempt from the skip.

## [0.6.1] - 2026-06-12

Patch release fixing two docs-site (`examples/site`) bugs. No breaking changes,
no framework API changes.

### Fixed

- **The site version is stamped from the deployment's git tag instead of a
  hand-bumped constant.** `examples/site` displayed a hardcoded `siteVersion`
  that had drifted to `0.4.0` while releases moved on. It is now injected at
  build time via `-ldflags "-X main.siteVersion=$(git describe --tags
  --abbrev=0)"` (wired into `scripts/dev-watch.sh`, `make build-examples`, and
  the Pages workflow, which now checks out with tags), so the deployed site
  always matches the tag it was built from. An un-stamped local `go build`
  shows `dev`.
- **The Sidebar showcase's mobile nav drawer now opens.** At < 900px the
  `/components/sidebar` demo rendered a hamburger wired to `ui-sidebar-drawer`,
  but that drawer widget was never mounted, so the button silently did nothing.
  The drawer is now mounted (page-scoped) sharing the showcase's sidebar config,
  and a contract test (`TestE2E_Sidebar_HamburgerOpensDrawer`) covers it.

## [0.6.0] - 2026-06-12

Reframes the blueprint as a **generator, not a source of truth**. `gofastr
generate` now scaffolds owned Go in an idiomatic, module-root layout you read,
edit, and commit — there is no quarantined `gen/` directory and no `// Code
generated … DO NOT EDIT.` header on the scaffold. Re-running the generator is
add-only and never clobbers your edits. Contains two **BREAKING** changes (see
below); pin a version (`go get …@v0.6.0`).

### Changed

- **BREAKING: `gofastr generate` scaffolds into the module root, not `gen/`.**
  A blueprint now scaffolds `main.go` plus `entities/` and `blueprint/`
  subpackages at the module root (imports rooted at your module), as owned Go
  with no `// Code generated … DO NOT EDIT.` header. Writes are **conflict-skip**:
  a re-run writes new files but never overwrites a file you have hand-edited —
  pass `--force` to overwrite. The blueprint is an on-ramp; once scaffolded the
  generated Go is the source of truth and the running app does not need the
  `gofastr.yml`. **Migration:** to keep the old quarantined layout, pass
  `--out=gen` (or set `app.output_dir: gen` in the blueprint) — output is still
  owned Go, just in a subpackage. Build/run with `go run .` (or `go run ./<dir>`
  when `--out` is used) instead of `go run ./gen`. Monorepo examples that host a
  test package alongside the app use `output_dir` for a subpackage —
  `examples/ecommerce` now scaffolds into an owned `app/`.

- **BREAKING: `gofastr migrate diff` has been removed.** It applied a blueprint
  directly onto a live database, reconciling the running schema to the
  blueprint — i.e. treating the blueprint as authoritative over the world. Code
  generation and schema migration are separate concerns. **Migration:** use
  `gofastr migrate generate <name>` to emit a reviewable, versioned migration,
  then `gofastr migrate up` to apply it; additive column changes also converge
  on boot via `AutoMigrate`. `migrate generate` is unchanged and still accepts
  `--from=<blueprint.yml>` as an opt-in schema source.

## [0.5.1] - 2026-06-11

Patch release correcting startup readiness reporting and repository release
metadata. No breaking changes.

### Fixed

- **Startup readiness output now follows the listener bind.** `App.Start`
  prints its framework banner only after `net.Listen` succeeds, uses the
  resolved address for a new `Listening:` line, and prints API-prefixed entity
  URLs correctly. Bind failures no longer claim the server is ready.
- **Repository status surfaces match `v0.5.0`.** The security support line,
  codegen Make target, roadmap implementation statuses, architecture version,
  coverage example path, and API-versioning package paths were corrected.
  Obsolete roadmap-worktree scripts were removed, and `repolint` now guards
  supported-version drift plus retired build-script paths.

## [0.5.0] - 2026-06-10

The first-contact release: an adversarially-verified 10-dimension audit
(2026-06-09) found the engine strong but the first-touch surface broken —
the README quickstart failed verbatim, the flagship example shipped
insecure, RBAC was unreachable from the blueprint, and CI was red on every
release tag. Everything below closes those findings. Contains a **BREAKING**
auth change (see below); pin a version (`go get …@v0.5.0`).

### Changed

- **BREAKING: `battery/auth` fails closed on an empty `JWTSecret` in
  production.** An `AuthManager` with `DevMode=false` and no `JWTSecret`
  now makes `Init` return an error (the app refuses to boot) instead of
  warning and continuing — an empty HMAC key yields forgeable,
  restart-unstable JWTs. The error names the remedy (set
  `AuthConfig.JWTSecret`, or `DevMode: true` for local HTTP). The
  blueprint path rejects `app.auth` with `dev_mode: false` and no
  `jwt_secret` at `gofastr validate`/`generate` time, so a generated app
  can't be built into the broken state. **Migration:** set
  `AuthConfig.JWTSecret` from your secret store (most prod apps already
  do); `DevMode` is unchanged and still mints a per-process secret.

### Added

- **Blueprint `access:` key — per-operation RBAC from `gofastr.yml`.** An
  optional `access:` map on an entity (`read` / `create` / `update` /
  `delete`, each a permission string) threads through
  `EntityDeclaration.Access` into the generated `register.go` as
  `framework.AccessControl{…}`, where the existing CRUD chokepoint enforces
  it fail-closed (403). Fully additive: blueprints without the key produce
  byte-identical output. Closes the audit's "one leg of the secure-by-default
  triad is unreachable from the primary declaration format". Also re-exports
  `framework.AccessDeclaration` for symmetry with `EntityDeclaration`.
- **`gofastr validate <blueprint.yml>`.** Parse + full blueprint validation
  (including the module/go.mod coherence check, a render pass, and an
  entity-name check that rejects names whose generated Go identifier would
  not compile, e.g. `2fa_tokens`) without generating; exit 0/1 with
  agent-friendly file:line + remedy diagnostics. `--from`, `--config`, and
  `--out` on `gofastr generate` now also accept the space form
  (`--from x.yml`), which previously silently did nothing.
- **`unscoped-pii` lint — CLAUDE.md hard rule #6 in the toolchain.** Flags
  any auto-exposed entity (CRUD default-on or `mcp: true`) with PII-shaped
  fields and no `owner_field` / `access` / `multi_tenant`. Enabling
  `app.auth` alone does **not** suppress it — the session middleware is
  pass-through for anonymous requests (an adversarial review proved the
  original auth-suppression wrong by anonymously reading user emails from
  a generated example app). Error from `gofastr validate`, prominent
  warning from `gofastr generate`, finding in `gofastr audit lint`.
  Running it against the repo's own examples found and fixed shipped
  exposures in `blog`, `lms`, `real-estate`, `portfolio`, and
  `project-manager` — each now demonstrates a scoping pattern (RBAC-gated
  staff rosters, public-catalog reads with gated writes, lead-capture
  forms with open create and gated reads).
- **Executable-README CI gate.** `cmd/gofastr/readme_quickstart_test.go`
  extracts the README's quickstart blocks and runs them for real: blueprint
  → generate → build → boot → `GET /posts` 200; plus drift gates (no
  "unpublished"/replace-directive guidance anywhere in the embedded docs,
  README blueprint relations must resolve). The audit found this single
  gate would have caught five of its eight confirmed findings.
- **`App.OnReady(func(addr string))`** — lifecycle hook that fires after
  the listener has actually bound (and is skipped on start failure).
  Generated apps now print their startup banner from it, so a migrate
  failure can no longer print "Server starting" and then exit 1.
- **Trust surface.** `SECURITY.md` (private vulnerability reporting,
  honest v0.x support policy), `CONTRIBUTING.md` (truthful prereqs: Docker,
  Chrome, the test-isolation rules), and low-ceremony issue templates.
- **Docs: `comparison.md` and `tutorial-blueprint-app.md`.** An honest
  head-to-head (PocketBase / Encore.go / Wasp / hand-rolled Gin+sqlc,
  weaknesses included) and the missing thesis tutorial (blueprint →
  generate → secure with auth + `owner_field` + `access:` → customize in
  plain Go → deploy), every step executed end-to-end before shipping.
  Plus a README for `examples/ecommerce`.

- **In-package Postgres coverage for `framework/crud`.** The SQL-generation
  core had 66 sqlite-only test files; a focused testcontainers suite now
  exercises the representative paths (filters, sort, offset+COUNT, cursor
  keyset walk, batch rollback, upsert, eager-load include, soft-delete,
  owner+tenant fail-closed) against real Postgres.
- **First tests for `framework/owner`** (14): fail-closed verified at the
  HTTP gate (anonymous → 401 with no extractor), last-call-wins override
  warns, OwnerField-unset is inert, forged client `user_id` overridden,
  race-checked. Two layer-level fail-open contracts (`ApplyOwnerScope`,
  `InjectOwner` without an extractor) are pinned with tests and comments
  documenting that `requireScope` upstream is the actual boundary.
- **Common-mistakes callouts completed and gated.** The docs claimed every
  topic ends with one; 21 of 60 didn't. Real, code-verified callouts added
  to the 15 guide docs (including entity-declarations, "the heart of the
  model"); 6 data/index docs exempted with reasons; the claim text now
  matches reality and `TestGuideDocsEndWithCommonMistakes` enforces it.
- **Security ledger fully re-verified.** All 103 SECURITY_FINDINGS.md rows
  re-checked against current code: 102 `fixed` (each with the mitigation
  cited AND a named pinning test run and observed passing), 1 `accepted`
  (#58, an intentional accepted-risk documented in code), 0 unverified or
  open. A guard test pins header-count == row-count and valid status
  tokens (`fixed`/`open`/`needs-verification`/`accepted`).
- **Coverage floors are a CI gate.** `scripts/coverage-floors.sh` fails the
  blocking job if any claimed package drops ~2 points below its measured
  coverage. COVERAGE_NOTES.md now separates own-package numbers from the
  full-suite-overlay numbers the old 100% claims were quoting.

### Fixed

- **SECURITY: `EntityConfig.MaxListLimit` could be bypassed on two list
  paths.** The cursor path never consulted the entity cap (asked for ≤3,
  served up to 100), and an oversized `?limit` on the offset/stream path
  silently fell back to the default page size (20) — exceeding any cap
  below 20. Both were hidden behind the security tests' auto-skip-on-500
  heuristic; converting those skips to hard failures (an audit
  recommendation) exposed them immediately. Oversized `?limit` now clamps
  to the effective cap on every path (`listLimitCap` shared by offset,
  stream, and cursor), with regression tests un-skipped and green.
- **crud security tests fail instead of skip on server errors.** The
  skip-on-redacted-500 heuristic (and its "SQLite can't run $N
  placeholders" rationale, which was false — the 500s were fixtures
  missing `Table`) is gone across the suite.
- **`battery/queue` tests are deterministic.** Thirteen sleep-based
  assertions replaced with Close-drain semantics, bounded `waitFor`
  polling, and an unexported clock seam for lease/visibility expiry —
  stable under `-race -count=3`; no production behavior change.
- **Doc staleness found while verifying the new callouts:** `widgets.md`
  described a `widget.Mount` return value and bootstrap route that don't
  exist (rewritten around `MountRuntime`/`RuntimeTag`);
  `ui-new-components.md` cited three nonexistent drift gates (corrected to
  the real ones); `ui-getting-started.md` had a non-compiling
  `DBFromContext` snippet and listed `APIPrefix` as roadmap (it shipped).
  `cursor-pagination.md` now documents the per-entity cap.

- **Boot-time auto-migrate now adds missing columns.** `AutoMigrate` did
  `CREATE TABLE IF NOT EXISTS` only, while deploy.md claimed
  "create tables, add columns" — adding a field to an existing entity
  broke the next boot. It now reuses the existing schema-diff machinery
  and applies the **additive** changes only (drops/renames/retypes stay
  behind `migrate diff`'s destructive gate); required new columns are
  added nullable, column adds run before index DDL, and a racing replica
  re-reads live columns on the lock-holding transaction and no-ops.
  Also fixes Postgres live-schema readers to case-fold unquoted table
  names (mixed-case entities were mis-reported as missing every boot).
- **Light color scheme now passes WCAG AA — and the axe gate can no
  longer be platform-blind.** The "Linux-only" CI axe failures were real:
  Linux Chrome defaults to `prefers-color-scheme: light`, so CI audited
  the light palette that Dark-mode dev Macs never did. Light primary/
  accent/code-comment tokens retoned (worst offender was 1.71:1, now all
  ≥4.6:1), the framework's `DefaultTheme` status tones darkened so any
  light-theme Badge/Tag chip passes AA, the `gofastr theme init` scaffold
  updated to match, and `TestAxe_AllPagesAreClean` now scans every page
  under BOTH forced schemes. The browser-e2e CI job is **blocking** again.
- **Generated auth honors `dev_mode` and validates it strictly.** The
  generator hardcoded `DevMode: true`; the blueprint key now works, with
  a deliberate default of `true` (production cookie posture —
  `__Host-session` + `Secure` — never round-trips on the plain HTTP a
  fresh app serves) announced in the generated code, the `gofastr
  generate` output, and the docs. `dev_mode: yes` is a hard error, not a
  silent coercion to prod mode. `auth.CSRF` is deliberately not mounted
  (it would 403 the JSON/MCP surface); the gap and the
  `SameSite=Strict` mitigation are documented.
- **Docs-site copy drift caught by visual review.** The site header
  advertised "pre-alpha 0.0.4" and the examples pages described blog as
  "JSON-declared" — a format removed in v0.4.0. Version now lives in one
  `siteVersion` constant; blog is correctly described as Go-declared
  (users/posts/comments).
- **The README quickstart now runs verbatim — and CI enforces it.** The
  blueprint example was rewritten in block style (the in-house YAML parser
  deliberately rejects flow mappings) with every referenced entity
  declared; a long-unclosed code fence that inverted every subsequent
  block is closed; stale "unpublished / add a replace directive" guidance
  is gone (the module resolves on the proxy at v0.4.0); the
  `go test ./...` claim now states its real prerequisites.
- **Flow mappings fail with an honest error.** `core/yaml` now says
  `flow mapping "{ ... }" is not supported; use block style …` instead of
  the misleading "nested mapping must be on an indented line"; flow-list
  items that previously silently misparsed now error.
- **Relation-typed fields are validated at generate time.** A field like
  `author_id: {type: relation, to: users}` pointing at an undeclared
  entity now fails `gofastr generate`/`validate` with a remedy, instead of
  generating an app that exits 1 at startup. Blueprint `module:` that
  contradicts the enclosing `go.mod` errors with the exact fix.
- **Generated apps with auth actually authenticate.** The generator
  enabled the auth battery but never mounted `auth.SessionMiddleware`, so
  authorized requests got 401 like anonymous ones (found by dogfooding the
  secured flagship). Generated `app.go` now mounts it after
  `authMgr.Init`; the flagship test asserts the full register → login →
  create → owner-isolated list flow across two users.
- **`examples/ecommerce` no longer ships insecure.** Auth is enabled and
  `orders` / `order_items` are owner-scoped (`owner_field: user_id`) —
  previously any anonymous caller or MCP agent could read every customer's
  PII and mutate orders, violating the repo's own hard rule #6.
  `BUILD_JOURNAL.md` keeps the honest history.
- **Deterministic CI.** `core/static` MIME detection now consults its
  canonical extension table before the host mime database, so Content-Type
  is identical on macOS and Linux (the 6/6-failing `TestDetectFromName` is
  fixed at the detection layer, not the test); `.js`/`.mjs` serve the
  RFC 9239 canonical `text/javascript`. `ci.yml` is restructured into a
  blocking deterministic job and an isolated, serialized browser-e2e job
  (non-blocking until the known Linux-Chrome axe contrast discrepancy in
  `examples/site` is resolved — condition documented in the workflow).
- **Docs drift purged.** `overview.md` core/ package count corrected
  (twelve → eighteen); `framework/ARCHITECTURE.md`'s layering map redrawn
  to match the real import graph (`openapi` above `crud`, the
  `slowquery → db` edge, `crud → access`); `core-ui/ARCHITECTURE.md` now
  states the real runtime size (~7,400 lines across budget-enforced split
  modules, ≤12KB-gz core) instead of "a few hundred lines";
  `examples/README.md` gained the flagship row.

### Changed

- **Example blueprints declare their real module paths.**
  `blog`/`lms`/`portfolio`/`project-manager`/`real-estate` now declare
  `module: github.com/DonaldMurillo/gofastr/examples/<name>` (matching the
  flagship), so `gofastr validate` passes in-place inside the repo.
- **Repo-wide gofmt sweep + CI gate.** 299 tracked files reformatted in one
  mechanical pass; the blocking CI job now fails on any gofmt drift in
  tracked Go files.
- **README repositioned around the wedge.** Leads with "one blueprint
  becomes a server-rendered UI and an API with secure scopes, in plain Go
  you own"; MCP/OpenAPI demoted to supporting evidence (schema-derived MCP
  became table stakes); validation-status block updated for the secured,
  CI-gated flagship.

## [0.4.0] - 2026-06-08

The blueprint becomes GoFastr's single declaration format: the legacy
`entities/*.json` path is removed (**BREAKING**), and a declaration-driven
flagship (`examples/ecommerce`) proves one `gofastr.yml` → SQL + REST + OpenAPI
+ MCP + UI end to end.

### Added

- **Declaration-driven flagship example — `examples/ecommerce`.** A complete
  storefront (five related entities, screens, nav, custom endpoints, seed data,
  and a theme) declared once in `gofastr.yml` and emitted as runnable Go by
  `gofastr generate --from=gofastr.yml` (the generated `gen/` is gitignored).
  `flagship_test.go` regenerates, builds, and runs it to prove every surface —
  SQL schema, REST CRUD, OpenAPI, the 25-tool MCP surface, and the
  server-rendered UI — is live with zero hand-written application code. See
  `examples/ecommerce/BUILD_JOURNAL.md`.

### Fixed

- **`gofastr generate` now gofmt's its generated Go.** Blueprint output is run
  through `go/format` before being written, so the emitted package is clean and
  stable across regenerations (no more spurious diffs on re-`generate`).
- **`gofastr generate --from` re-run no longer refuses to clean `main.go`.** The
  output-dir cleaner now owns `main.go` (the blueprint emits `gen/main.go`), so
  regenerating over an existing `gen/` succeeds instead of erroring with
  "refusing to clean — contains unknown entry".

### Removed

- **BREAKING: the legacy `entities/*.json` declaration format is gone.** The
  `gofastr.yml` blueprint is now the single declaration format — it decodes into
  the same `EntityDeclaration` shape and additionally emits `main.go`, screens,
  and stubs, so the JSON-file path was a strict subset. Removed:
  - Framework API: `App.EntityFromFile`, `App.EntitiesFromDir`,
    `App.GroupEntitiesFromDir`, `framework.LoadEntityDeclaration`,
    `framework.LoadEntityDeclarations`. (The `EntityDeclaration` /
    `FieldDeclaration` types and `.Config()` remain — they are the in-memory
    shape the blueprint loader decodes entities into.)
  - CLI: `gofastr generate entity <name>` and `gofastr new entity <name>` (both
    now print a removal notice and exit non-zero); the `--entities=<dir>` flag
    on `gofastr generate`, `gofastr migrate generate`, and `gofastr migrate diff`.
  - `gofastr generate` no longer defaults to "scan `entities/` and generate." It
    requires `--from=<blueprint.yml>` (or a `gofastr.codegen.yml` extension
    config). Auto-discovery of `gofastr.yml` is intentionally not done — that
    filename is also the `gofastr init` isolation config.

  **Migration:** declare entities in a `gofastr.yml` blueprint and run
  `gofastr generate --from=gofastr.yml`, or declare them in Go via
  `app.Entity(name, framework.EntityConfig{…})` (unchanged). `gofastr migrate
  generate <name> --from=<blueprint.yml>` and `gofastr migrate diff
  --from=<blueprint.yml>` replace the old `--entities=<dir>` form. The
  `gofastr.codegen.yml` extension protocol and `codegen` package are unchanged.

  _Follow-up (kiln is experimental): `kiln freeze` still writes
  `entities/*.json` as its own snapshot artifact; emitting a `gofastr.yml`
  blueprint directly is tracked for a later pass._

## [0.3.3] - 2026-06-08

The four larger features held back from v0.3.2, each additive and
backward-compatible. The OAuth token store passed the mandatory dual-model
security audit (see `AI_TEST_AUDIT.md`).

### Added

- **Typed schemas for custom `entity.Endpoint`.** New optional
  `InputSchema`/`OutputSchema` (`[]schema.Field`) fields. When set, the OpenAPI
  spec emits a typed `requestBody`/`200` response and the generated MCP tool
  advertises a typed input schema, instead of a shapeless `{type:object}`. A
  single helper (`openapi.EndpointInputSchema`) feeds both the OpenAPI and MCP
  paths. Endpoints with no schema render exactly as before.
- **OAuth2 token store + transparent refresh** (`battery/auth`). A new
  `OAuthTokenStore` interface + AES-GCM-sealed `SQLOAuthTokenStore` persists
  `{access, refresh, expiry}` per `(user, provider)`; `RefreshOAuthToken` /
  `ValidOAuthToken` refresh transparently on/near expiry via the provider's
  refresh grant (Google + GitHub). OAuth login now persists the refresh token
  (previously discarded) when a store is wired. **Opt-in** — login is unchanged
  with no store configured. `EncryptionKey` is **required** (fails closed); the
  `userID` passed to refresh/valid must be the authenticated principal.
- **Cron-expression scheduling in the queue Scheduler.** `Scheduler.Cron(spec)`
  fires on a standard 5-field cron expression (plus `@daily`/`@hourly`/… shortcuts),
  alongside the existing `Every(interval)`. Reuses `framework/cron` (now exposing
  `Parse`/`Schedule.Next`) — no second cron parser. Interval schedules are unchanged.
- **Request context in i18n-rendering `framework/ui` components.** `RepeaterConfig`,
  `LightboxConfig`, `StepWizardConfig`, `PasswordInputConfig` gain an optional
  `Ctx` field so their localizable strings resolve the request's locale instead
  of always rendering the default. Nil `Ctx` preserves today's behavior.

## [0.3.2] - 2026-06-08

A developer-experience patch from the same whole-framework assessment that
drove v0.3.1 — twenty DX improvements and small features, all with tests and
 shipped docs. No BREAKING changes; everything is additive.

### Added

- **`App.WithSeed(func(ctx) error)`** — register seed funcs that run AFTER
  auto-migration (tables exist) and before the listener binds, fixing the
  first-run "no such table" footgun.
- **`framework.DBFromContext(ctx)` / `WithDBContext`** + an auto-wired
  `DBContextMiddleware` — screens reach the app's `*sql.DB` from the request
  context instead of a package-level global handle.
- **`access.GetRoles(ctx)`** (and the `framework.GetRoles` facade) — the reader
  half of the role-context seam, for role-based UI branching.
- **`PluginGetAs[T]`** — typed plugin lookup mirroring the existing `GetAs[T]`.
- **Typed interactive effects** — `Confirm`/`AfterText`/`AfterDisable`/`ScrollTo`/
  `PushState` builders in `core-ui/interactive`, replacing hand-written
  `data-fui-*` attribute strings.
- **`ListOptions.NestedFilters`** — in-process `ListAll`/`CountAll` now apply the
  same `?author.name=alice` EXISTS-subquery nested filters the HTTP path does.
- **`RedisQueue.Start(ctx, interval)`** — background reclaim ticker recovers jobs
  stranded by a crashed worker, matching `DBQueue`.
- **`Battery` wrappers for cache/search/storage** (`NewBattery`) with clean
  lifecycle shutdown of background goroutines.
- **`gofastr harness creds add|list|delete`** — store credentials in the
  encrypted credstore; `gofastr --help` now lists the `harness`/`agents` subcommands.
- **`audit_log.tenant_id`** — a nullable column (idempotent `ADD COLUMN`) stamped
  from the request tenant, so multi-tenant audit trails are scopeable.

### Changed

- **The OpenAPI spec advertises `?fields=` (projection) and `?trashed=`** query
  parameters so SDK generators and agents can see them.
- **Auto-CRUD registration pre-flights entity/screen path collisions** with an
  actionable diagnostic (names the entity, the colliding path, and the fixes)
  instead of the opaque ServeMux `/foods/llm.md conflicts` panic.
- Queue `Queue`/`Browsable`/`Replayable` interface assertions moved into source
  files (fail at build, not test-link).
- The `agents.md` snippet validator now understands interface methods and
  non-`New` constructors (e.g. `embed.Open` → `Index`), so it stops
  false-flagging correct interface APIs while still catching fictional methods.

### Documentation

- New **`queue.md`** and **`testkit.md`** reference pages; **`battery/embed`** now
  ships `agents.go`/`agents.md` so semantic search is discoverable to agents.
- Documented the typed list/get hooks (`OnBeforeList`/`OnAfterList`/`OnBeforeGet`/
  `OnAfterGet`) and a consolidated hook-skip matrix.
- Security docstrings on the unscoped `softdelete.Restore`/`ForceDelete`/`WithTrashed`
  helpers; "Common mistakes" sections (form-module, api-versioning); deeper
  observability docs; `GetRoles`/`PluginGetAs` docs; and stale-claim fixes.

## [0.3.1] - 2026-06-08

A correctness and developer-experience patch from a whole-framework
assessment. No BREAKING changes. Twenty fixes, all with regression tests;
the recurring theme was converting silent wrong answers into correct
behavior or loud errors.

### Security

- **Codegen and blueprints no longer drop `OwnerField`.** `renderEntityRegistration`
  emitted every scope flag except `OwnerField`, and the blueprint YAML allow-list
  rejected `owner_field` outright — so generated/blueprinted apps silently lost the
  per-user row scoping the docs hard-warn about. Both paths now preserve it.
- **Streaming list can no longer bypass `AfterList` redaction.** `?stream=true`
  skipped include resolution and the `AfterList` hook; an `AfterList` redactor would
  have been silently bypassed, leaking the fields it exists to hide. An explicit
  stream with `?include=` or a registered `AfterList` hook is now refused with `400`;
  an auto-stream (very large `limit`) falls back to the buffered path so redaction
  always runs.
- **`GOFASTR_HARNESS_MACHINE_KEY` no longer silently downgrades.** Only a raw 32-byte
  value was accepted; a hex or base64 key failed the length check and fell through to
  the default passphrase with no warning. The env var now decodes raw-32/hex-64/base64
  and errors loudly on an unparseable or wrong-length value.
- **The OpenAPI spec advertises `401`/`403` on RBAC-gated, batch, and SSE operations.**
  `EntityConfig.Access` is folded into the gated flag and `403` is added alongside
  `401`, so generated SDKs/agents see the real auth contract instead of treating
  RBAC-gated routes (and `_batch`/`_events`) as public.
- The `UpsertOne` DO-NOTHING fallback `SELECT` now applies tenant/owner/soft-delete
  scope (defense-in-depth; `upsertPreflight` already guarded the row).

### Fixed

- **`updated_at` is restamped on every UPDATE and bulk update.** It previously froze
  at its creation value because the field loop skips all auto-generate columns; cache
  invalidation and change detection silently saw stale timestamps. Clients still
  cannot forge it.
- **`ADD COLUMN` for a required field with no default no longer emits `NOT NULL`.**
  That DDL fails on a populated table (Postgres and old SQLite); the column is now
  added nullable with the deferral noted in the change summary (matches the kiln path).
- **`App.InTx` joins an ambient transaction** already in the context (e.g. when called
  from a CRUD hook) instead of silently opening a second independent transaction and
  breaking atomicity.
- **DSL `after(cursor)` is wired into `BuildDSLQuery`.** It was parsed and discarded,
  so DSL pagination always returned page 1. Composite-cursor/unknown-field entities now
  return a clear error instead of no-oping.
- **LiveSearch debounce works.** The emitted attribute (`data-fui-rpc-debounce`) did not
  match what the runtime reads (`…-ms`); debounce was silently ignored.
- **Widget dismiss closes its `EventSource`s** instead of leaking a live server SSE
  connection on every modal open/close.
- **Signal ARIA is text-mode only.** `role=status`/`aria-live` is no longer applied to
  attribute- or html-mode signal nodes (invalid ARIA + live-region spam on island swaps).
- **Carousel timers and the toc `IntersectionObserver` are torn down on SPA navigation**
  instead of leaking for the session.
- **`RedisQueue` implements `Browsable`** (`ListJobs`/`Stats` over the dead-letter list),
  so the admin queue page works on the most common non-DB production backend.
- **Scheduler enqueue failures log via `slog`** instead of `fmt.Printf`, surfacing
  otherwise-invisible job loss to the log battery/observability.
- **`MemoryQueue` handler timeout is configurable** via `WithHandlerTimeout` (default
  unchanged at 30s) so long jobs aren't silently cancelled and dead-lettered.
- **Per-page Open Graph/meta beats the global default.** Per-screen SEO is emitted before
  the sitewide `WithOpenGraph` tags, so first-match crawlers honour the page override.
- **`gofastr new entity` and `generate entity` agree on table naming** (singular
  snake_case, matching the framework default) so migrations target one table.
- **Built-in harness profiles are embedded** (`go:embed`, on-disk-wins fallback), so
  `gofastr harness --framework` works for an installed binary outside the source tree.

### Documentation

- Corrected the `access.Policy` interface in the docs from a non-existent 3-arg
  `Can(ctx, permission, resource)` to the real 2-arg `Can(ctx, permission)` (custom
  policies following the docs failed to compile), and documented that per-record
  decisions are made via `OwnerField` scoping or `Before*` hooks. A compile-time
  assertion now pins the doc to the interface. The Go interface is unchanged.
- Documented the streaming/`AfterList` exclusivity, `App.InTx` ambient-tx joining,
  the `ADD COLUMN` `NOT NULL` deferral, and corrected the stale `updated_at` hook
  comment in `migrate.go`.

## [0.3.0] - 2026-06-07

First release after the assessment-driven remediation. Highlights: MIT
LICENSE; secure-by-default authorization (multi-tenant fail-closed,
per-operation RBAC on auto-CRUD, admin default-deny); kiln free-order
authoring + same-origin guard; observability + deployment story; durable
auth token store; dead-letter replay across all queue backends; and a
broad sweep of build-quality fixes. **Contains BREAKING changes — read the
entries marked BREAKING below before upgrading from v0.2.x.**

### Security

- **BREAKING — typed-repo queries are now tenant-fail-closed.** A re-audit found
  `Repo.Query().Find/First/Count/Exists/UpdateAll/DeleteAll` (the generated
  typed-query builder) only applied `ApplyTenantScope` — which no-ops on an empty
  tenant — so on a `MultiTenant` entity a tenant-less context read/mutated across
  every tenant. The in-process `crud_api.go` already gated this; the typed-query
  path slipped. Now gated via `requireTenantContext` (honors
  `tenant.AllowCrossTenant`). Owner scope stays permissive for typed repos by
  design (trusted in-process; admin reads across owners).
- **The SSE `_events` live feed now enforces `Access.Read`.** The real-time feed
  is a read surface but skipped the per-op RBAC gate, so an authenticated user
  without the read permission could subscribe for a live stream of all writes
  despite `403` on the static read endpoints. `EventStream` now runs
  `requirePermission(opRead)` alongside the owner/tenant gates.
- **kiln: same-origin guard on the unauthenticated tool API.** `POST
  /kiln/tool/{name}`, `/kiln/agent`, and `/mcp` mutate the in-memory world with
  no auth (loopback bind is the primary control). A new origin guard refuses
  cross-origin browser POSTs (DNS-rebinding / CSRF from a page in the user's
  browser), while non-browser clients (agent, curl, MCP/ACP — no `Origin`) are
  unaffected. Docs: `kiln.md`.
- **`battery/auth` warns on a missing production JWT secret.** With
  `DevMode=false` and an empty `JWTSecret`, the auth battery now logs a loud
  startup warning (an empty HMAC key means forgeable, restart-unstable
  sessions). DevMode still auto-mints a per-process secret, also warned. New
  secrets guidance in `deploy.md` (env injection, Vault/SSM/K8s).
- **`migrate.View` name is validated as a SQL identifier.** `View.Name` was
  interpolated into `CREATE/DROP VIEW` DDL verbatim; it's now checked with
  `query.SafeIdent` and panics on an unsafe name (developer misconfig, fail-fast).
  `View.Select` remains intentionally free-form developer SQL.
- **BREAKING — admin battery is default-deny for non-admins.** With no custom
  `Config.Authorize`, the admin now requires an authenticated user holding the
  admin role (`Config.AdminRole`, default `"admin"`) — detected via the
  structural `GetRoles() []string` interface (`battery/auth.User` satisfies it).
  Previously any authenticated, non-nil user reached full admin CRUD over every
  exposed entity, so a freshly-registered reader was effectively an admin.
  Authenticated-but-unauthorized now returns `403` (vs `401` for anonymous).
  Docs: `framework/docs/content/admin.md`.
- **Per-operation RBAC on auto-CRUD — `EntityConfig.Access`.** Declare the
  permission required for each operation (`Read` covers List+Get, plus
  `Create`/`Update`/`Delete`) and auto-CRUD refuses requests lacking it with
  `403` — across List/Get/Create/Update/Delete and the batch/stream variants.
  Previously auto-CRUD had **no permission check at all**: exposing an entity
  granted every authenticated user full CRUD unless the host hand-composed
  route-group middleware. New seams: package-level **`access.Can(ctx, perm)`**
  and **`access.Middleware(policy, roles)`** (re-exported as `framework.Can` /
  `framework.AccessMiddleware`) to install policy+roles in one line. **BREAKING:**
  `access.Policy.Can` / `RolePolicy.Can` drop the unused `resource any`
  parameter. Docs: `framework/docs/content/access-control.md`.
- **BREAKING — multi-tenant CRUD is now fail-CLOSED over HTTP.** A
  `MultiTenant` entity served with no tenant id in the request context is
  refused with `401` on every operation (list/get/create/update/delete, batch,
  stream, SSE), matching the in-process CRUD API which already failed closed.
  Previously the HTTP path failed *open* — an empty tenant id disabled filtering
  and returned/mutated every tenant's rows, a silent cross-tenant data leak.
  Deliberate cross-tenant access (admin tooling) must now opt in explicitly and
  server-side via the new **`tenant.AllowCrossTenant(ctx)`** marker (never from
  a client header). New seam: **`CrudHandler.RequireTenant(w, r)`**, the HTTP
  mirror of `RequireOwner`, run alongside the owner gate through a single
  `requireScope` chokepoint. Docs: `framework/docs/content/multi-tenant.md`.

### Fixed

- **`battery/embed`: custom `Store` now fails closed instead of silently
  corrupting.** A custom `Store` (anything but the built-in `FlatStore`) was
  type-asserted to `*FlatStore` in four places, so with one it would silently:
  skip persistence even with `Options.Path` set; **never purge keyword entries
  on delete** (stale hits leak forever); and **drop every keyword hit** so
  hybrid search degraded to vector-only. Replaced the assertions with optional
  capability interfaces (`Snapshot`/`LoadSnapshot`; `ChunkIDsForDoc`/`ChunkByID`/
  `AllChunks`) and made `Open()` **return an error** when `Path`/`Keyword` is set
  but the store lacks the capability. `FlatStore` implements all of them, so no
  in-tree caller changes.
- **Blueprint codegen produces compilable Go in two edge cases.** (1) An
  endpoint with no `handler` emitted `func (w http.ResponseWriter, r *http.Request) {`
  — read by Go as a method with two receivers; the handler name now falls back
  to the endpoint `name`, and a fully-anonymous endpoint is skipped. (2) A screen
  whose body was only freeform node blocks imported `core-ui/html` without using
  it (a build error) — import accounting now only flags `html` when a top-level
  block actually emits an `html.*` call. Both are pinned by tests that parse /
  build the generated output.
- **Generated apps no longer ship Kiln's authoring engine.** `gofastr generate`
  emitted `import "…/kiln/render"` into blueprint apps that use freeform node
  blocks, which transitively pulled `kiln/expr`, `kiln/effect`, and `framework`
  — Kiln's whole build-mode evaluator — into a shipped binary. `RenderNode` is
  now a leaf package **`kiln/noderender`** (imports only `core-ui/html`,
  `core/render`, `kiln/world`); codegen targets it and `kiln/render` keeps a
  thin re-export for the live path. A new codegen build test compiles a
  generated node app and asserts its dependency graph excludes the engine.
- **UI host warns when chrome can't be injected.** The host injects the
  runtime, color-scheme bootstrap, SEO head, and widget chrome via
  `strings.Replace` on `<head>`/`</head>`/`</body>`. A custom layout missing one
  of those markers made the replace a silent no-op, shipping a subtly broken
  page. Injection now routes through a guard that logs a warning naming the
  missing marker. Unit-tested.
- **Island SSE drops are now observable.** When a client's island-update
  channel is full the update is dropped (slow consumer); this was silent. The
  manager now counts drops, exposed via `island.Manager.DroppedUpdates()` —
  wire it to a metric/health check to detect stalled streams.
- **`battery/cache`: bounded cache buffering.** The middleware buffered the
  entire response in memory before deciding cacheability, with no size cap — a
  pathological large response could pin unbounded memory. It now streams a
  response past `DefaultMaxCacheableBytes` (8 MiB) straight to the client and
  skips caching it. New `CacheMiddlewareWithLimit(cache, ttl, maxBodyBytes)`.
- **`battery/embed`: data race on the Ollama embedder's lazy dimension.**
  `OllamaEmbedder.dim` was a plain int written by `Embed` and read by `Dim` from
  another goroutine. It's now an `atomic.Int64` set via CompareAndSwap.
- **Nested `_in` filter on a BelongsTo relation now matches.** `?author.name_in=a,b`
  split into separate AND-ed `EXISTS(... = a) AND EXISTS(... = b)` subqueries, so
  a to-one relation could never satisfy both and silently returned nothing.
  Nested `_in` now coalesces into a single `EXISTS(... col IN (a,b))`, matching
  the top-level `_in` semantics, for BelongsTo/HasOne/HasMany/ManyToMany.
- **`App.Start` no longer leaks workers on bind failure.** A non-graceful
  `ListenAndServe` error (port already in use being the common case) returned
  immediately without draining, leaking every battery/cron/queue and OnStart
  worker an earlier start phase had spawned. The bind-failure path now runs the
  same `abort()`→`Shutdown` drain as every other start phase.
- **Scaffolded apps accept a bare `$PORT`.** `isolation.Runtime.Addr` now
  normalizes a bare numeric port (e.g. `PORT=8088`, as Heroku/Render/Railway/
  Cloud Run inject) to `":8088"`. Previously the generated `main.go` printed
  `http://8088` and then died with `missing port in address` on every such PaaS.
- **`examples/blog` runs again.** It loaded entities from a nonexistent
  `entities/` directory (`go run ./examples/blog` failed immediately, despite
  being the README's first step). Entities are now declared in Go (self-
  contained, runs from any cwd; `gofastr.yml` still mirrors them for the
  codegen path), and seeding runs after AutoMigrate so the demo data actually
  lands. Added a boot+HTTP-200 test (`examples/blog`) — the missing test layer
  the assessment flagged.

- **kiln: free-order authoring no longer bricks the rebuild.** Adding an entity
  with a `BelongsTo` to a not-yet-created entity (e.g. `posts`→`users` before
  `users` exists) failed the live auto-migrate and left the session unable to
  rebuild. The live migrator now defers a dangling `BelongsTo` and re-derives it
  once the target is added; the durable world and `kiln freeze` keep the full
  relation. Fixes the deterministically-red `TestFreezeRoundTripWithRichWorld`.
- **kiln: poison journal entries can no longer persist.** `live.Apply` now
  validates an entry with a trial rebuild **before** the durable journal append,
  so an entry that fails to rebuild is rejected and never written (previously it
  was fsynced first, then re-failed on every restart). On any failure the
  in-memory session is restored by replaying the journal.

### Added

- **Dead-letter inspect + replay for queue and webhook.** Terminally-failed work
  could be listed but never re-run. Add optional capabilities —
  `queue.Replayable{Replay}` (implemented by `DBQueue`) and
  `webhook.ReplayableStore{ListDeadDeliveries, ResetDelivery}` (implemented by
  `SQLStore` + `MemoryStore`, surfaced via `Manager.DeadDeliveries`/`Manager.Replay`).
  Replay is idempotent and only touches terminal rows (`status='failed'` for
  queue, `'dead'` for webhook), so it can't double-run an in-flight job. **All
  three queue backends now support replay**: `RedisQueue.Replay` moves a job off
  the dead-letter list back onto the main queue (new `LRange`/`LRem` on the
  host-provided `RedisClient` interface — no new dependency), and `MemoryQueue`
  now **retains** dead jobs in a bounded list (was silently dropping them),
  implements `Browsable`/`Replayable`, and replays them. The
  admin battery surfaces a **Replay** button on the failed-jobs view behind the
  admin gate + CSRF (`POST /admin/queue/_replay/{id}`), and its queue filter
  chips no longer advertise a `dead` status `DBQueue` never writes.
- **`auth.SQLMagicLinkTokenStore` — durable token store for passwordless flows.**
  Magic-link, password-reset, and email-verification tokens were in-memory only,
  so those flows broke on restart and couldn't scale across replicas. Add a
  DB-backed `MagicLinkTokenStore` (single-use via `DELETE … RETURNING`, TTL,
  cleanup) and a `TokenStore` config field on all three plugins
  (`MagicLinkConfig`, `PasswordResetConfig`, `EmailVerificationConfig`) — pass
  `NewSQLMagicLinkTokenStore(db)` in production. In-memory stays the default.
- **Observability is discoverable — `WithMetrics()` / `WithTracing()`.** The
  production-grade Prometheus metrics and OpenTelemetry tracing middleware
  existed in `core/middleware` but were never wired into `App`, re-exported, or
  documented. `WithMetrics()` adds the metrics middleware to the default chain
  and mounts a Prometheus `/metrics` endpoint; `WithTracing()` adds the otel
  span middleware (no-ops until a TracerProvider is installed). Both panic if
  combined with `WithoutDefaultMiddleware` (wire them yourself then). Re-exported
  `framework.{NewMetrics,MetricsMiddleware,MetricsHandler,Tracing,Metrics}`.
  New docs: `observability.md`, `deploy.md` (single-binary model, production
  Dockerfile, env config, migrations-as-a-step, TLS/graceful shutdown,
  health/metrics wiring).
- **`App.TryEntity(name, config) error`** — the error-returning variant of
  `App.Entity`. `Entity` panics on misconfiguration (fail-fast for hand-written
  declarations); `TryEntity` returns the error instead and recovers panics from
  deeper validation, so a single bad config (e.g. an AI-authored field, a
  dynamic schema) can't crash the process. `Entity` is now a thin panicking
  wrapper over `TryEntity`. Docs: `framework/docs/content/entity-declarations.md`.
- **`framework.WithPublicOpenAPI()` / `AppConfig.PublicOpenAPI`.** Serves
  `/openapi.json` without the auth gate. The spec is auth-gated by default (it
  enumerates every route), so a minimal app returned `401` there — surprising
  anyone following the quickstart `curl`. Swagger UI at `/api/docs/` is
  unaffected. README quickstart updated to call this out.
- **LICENSE — GoFastr is now MIT licensed.** A top-level `LICENSE` file (MIT)
  replaces the previous "all rights reserved / no license chosen" note. The code
  is now free to use, modify, and redistribute (including commercially) with the
  copyright notice preserved. This unblocks adoption, vendoring, and deployment.
- **Framework DX round-4.** Closes a focused batch from the V4 host-app feedback:
  - **`render.If(cond, html) HTML` / `render.When(cond, fn) HTML`** — inline conditional fragments. `When` is the lazy form for expensive truthy branches.
  - **`render.Classes(parts ...string) string`** — joins non-empty class strings with spaces. Pair with **`render.ClassIf(cond, name) string`** for sparse conditionals: `render.Classes("base", render.ClassIf(isActive, "active"))`. Coexists with `html.Classes(map[string]bool)` for predicate-dense cases.
  - **`html.InputConfig.Value` / `.Placeholder`** and **`html.TextAreaConfig.Content` / `.Placeholder` / `.Rows` / `.Cols`** — typed fields for the common attributes; killed the V4 papercut of falling back to `render.Tag("textarea", attrs, render.Text(content))` for prefilled edit sheets. `Attrs` remains as the escape hatch.
  - **`EntityConfig.Seed func(ctx, *sql.DB) error`** — runs once per entity after `AutoMigrate`. Completion is recorded in a new `_gofastr_seeded` ledger table; subsequent restarts skip seeded entities. Errors abort `App.Start`.
  - **`EntityConfig.SeedFS fs.FS` + `EntityConfig.SeedPath string`** — bind embedded seed data to an entity; reachable inside `Seed` via **`entity.SeedDataFromContext(ctx) ([]byte, error)`**. Removes loose JSON files from tarball-style single-binary deploys.
  - **`App.RegisterEntities(map[string]entity.EntityConfig) *App`** — sugar over multiple `Entity(...)` calls. Iterates the map in alphabetical-by-name order so route registration, OpenAPI tag emission, and MCP tool list order are deterministic across restarts. FK ordering stays correct because AutoMigrate also topologically sorts.
  - **`style.Contribute(func(*StyleSheet)) / style.Apply(*StyleSheet)`** — co-located scoped styles. Declare CSS next to the Go render code via `var _ = style.Contribute(...)` at package scope; the host calls `style.Apply(ss)` inside `createStyleSheet`. Final CSS is identical between dev and prod — no nonces, no inline `<style>`, no CSP relaxation. Distinct from `registry.RegisterStyle` (named, lazy-loaded per-component sheet); `Contribute` adds fragments to the host's global theme stylesheet. Kills the 3-file (screen + theme + reload) iteration cycle.
  - `App.Router()` doc comment now points application-level code at `App.Use` / `App.Group` and documents `Router()` as the plugin/internal surface.
  - **`App.Entity` panics at registration** when `SeedFS` is set but `SeedPath` is empty — a misconfiguration that would otherwise silently mark the entity as seeded with empty data on first run.
  - **`App.Start` failure paths drain via `Shutdown`** — AutoMigrate / RunSeeds / InitPlugins / runStartHooks errors no longer leak goroutines past Start returning. The app lifecycle context is created before AutoMigrate so RunSeeds and individual Seed functions can observe cancellation.
  - **`migrate.RunSeeds` reads the ledger in one round-trip** (was N+1 per entity) and emits per-seed lifecycle slog events (`seed start`, `seed done`, `seed skip`, `seed ledger read`) when a logger is attached via `migrate.WithSeedLogger(ctx, l)`.
  - **`webhook.VerifyTimestamped` rejects non-positive tolerance** (was: silently skipped the replay check) and out-of-range timestamps. Added **`webhook.DefaultTimestampTolerance = 5 * time.Minute`** as the suggested default.
  - **`entity.Registry.AllSorted() []*Entity`** — returns entities in alphabetical-by-name order so order-sensitive consumers (`OpenAPI` tag emission, `crud.RegistryLLMMD`) produce byte-stable output across restarts. Existing `All()` keeps the map shape but its godoc now spells out that map iteration is randomised. Fixes a pre-existing non-determinism that broke ETag caching of `/openapi.json` and `/api/llm.md`.
  - **`gofastr audit deps`** CLI command — scans the project for packages whose `init()` mutates framework-wide state (`style.Contribute`, `registry.RegisterStyle`, `render.RegisterComponent` / `RegisterLayout` / `RegisterFunc`). Output is grouped by Go import path; pairs with the documented supply-chain trust model on `style.Contribute`. Docs: `framework/docs/content/audit-deps.md`.
- **`core/dotenv` package + auto-load in `framework.NewApp()`.** Probes `.env.local`, `.env.<APP_ENV>` (when `APP_ENV` set), and `.env` from CWD before option processing. Existing `os.Environ` always wins. Parser handles double/single-quoted values, escapes, optional `export` prefix, comments; rejects malformed input loudly. Bracket-form `${VAR}` expansion with cycle detection, depth cap, undefined-as-empty, and `\${literal}` escape. Disable via `GOFASTR_DOTENV=off` in the process env. `cmd/gofastr migrate` now routes through this instead of its ad-hoc 1-key scanner. Docs: `framework/docs/content/dotenv.md`.
- **SSR auth policies.** `core-ui/app` exposes a `Policy { Decide(ctx) Decision }` machinery with four decision kinds (Allow / Redirect / RenderAlt / Block). Attach via `Screen.WithPolicy(p)` or `NewScreenGroup(prefix, layout, policies...)`. Construct decisions through the new `core-ui/app/decide` subpackage so call sites don't shadow common variable names: `decide.Allow()`, `decide.Redirect(url)`, `decide.RenderAlt(factory)`, `decide.Block(status, msg)`.
- **`battery/auth.SessionPolicy(opts...)` and `RolePolicy(roles, opts...)`** are the SSR counterparts to the existing `RequireSession` / `RequireRole` middleware. Options: `WithRedirect(url, ...RedirectOpt)`, `WithRenderAlt(factory)`, `WithBlock(status, msg)`. `RedirectOpt`: `NoNext()` to suppress the auto-appended `?next=<request-path>`.
- **`auth.SessionFrom(ctx) (User, bool)`** — cheap in-component getter for ctx-aware chrome (sibling nav, conditional CTAs). Pair with `RenderCtx` for in-page gating without policy machinery.
- **`auth.Roles(roles ...string) []string`** — ergonomic literal-list helper so `auth.RolePolicy(auth.Roles("admin", "owner"), ...)` reads cleanly. Documents the asymmetry with the variadic `auth.RequireRole`.
- **`component.ContextComponent { RenderCtx(ctx) HTML }`** — the optional ctx-aware render interface. Does NOT embed `Component` (so a type can satisfy it via just one method). Embed `component.ContextOnly{}` to also satisfy `Component` with a stub `Render` that the framework never calls.
- **`framework.entity.EntityDeclaration.OwnerField` JSON key (`owner_field`).** Mirrors `EntityConfig.OwnerField` so per-user CRUD scoping works for entities declared in JSON, not just Go.
- **DevMode auto-mints a random JWT secret** when `AuthConfig.JWTSecret == ""`. 32 cryptographically-random bytes, base64-encoded, logged as WARN. Sessions invalidate on restart — set `JWTSecret` for stable dev tokens.
- **`X-Gofastr-Location` partial-redirect signal.** Policy-Redirect outcomes on a partial fetch return 200 + that header + empty body (NOT 303 — the runtime fetcher uses `redirect:'follow'` and would auto-chase a 303, losing the header). The runtime's `loadPage` calls itself with the redirected URL and updates `pushState`.

### Removed (greenfield cleanup)

- **BREAKING — escape-hatch field `Attrs` renamed to `ExtraAttrs`** across `core-ui/html/*.Config`, `core-ui/patterns/{disclosure,sortablelist,multiselect}.Config`, and every `framework/ui/*.Config` that exposes a passthrough HTML attribute bag. The new name signals "extra attributes beyond the typed surface" so callers reach for typed fields first. `core/featureflag.Flag.Attrs` stays — it's primary data, not an escape hatch. `html.Attrs` *type* alias is unchanged.
- **BREAKING — 410 GONE compat endpoints removed**. `/__gofastr/theme.css`, `/__gofastr/styles.css`, `/__gofastr/routes.js`, `/__gofastr/catalog.js`, `/__gofastr/css/<path>` now 404 instead of serving a 410 with a migration hint. Use `/__gofastr/app.css` for CSS; routes + catalog ship inline as `<script type="application/json">` in the SSR'd page; per-component CSS comes from `/__gofastr/comp/<name>.css` via `registry.RegisterStyle`.
- **Dead code removed**: `migrate.alreadySeeded` (replaced by batch `readSeededSet`), `i18nui.replaceAll` (inlined to `strings.ReplaceAll`).
- **Doc framing cleanup**: removed "legacy", "back-compat", "kept for", "transitionally" language from comments that describe current first-class APIs (cursor pagination, runtime.js, framework facade, decodeCursorAny, App.Shutdown).

### Changed

- **BREAKING — form intercept is opt-in.** `<form>` elements with the default `application/x-www-form-urlencoded` or `multipart/form-data` enctype are NOT intercepted by `runtime.js`. The browser submits them natively (cookies set, `Location:` followed, file uploads, password-manager UX all work without any framework involvement). Forms posting to a JSON endpoint must opt INTO interception with `enctype="application/json"` OR `data-fui-spa`. `data-fui-rpc` still triggers RPC dispatch as before. **Migration:** `grep -rn '<form' .` — forms that POST to a JSON CRUD/island handler need `enctype="application/json"` added; forms that POST to a redirect-returning handler (auth, settings) need no change.
- **BREAKING — `core-ui/app.App.RenderPage` / `RenderPartial` now wrap richer `*Result` variants.** Returns an error for `Redirect` and `Block` decisions (the legacy shape can't express them). Use `App.RenderPageResult` / `RenderPartialResult` for the policy-aware shape.
- **BREAKING — `core-ui/app.Router.Render` → `Router.RenderRaw`** and **`App.RenderScreen` → `App.RenderScreenRaw`**. Renamed to call out that they bypass the Policy chain. HTTP-serving code must use `App.RenderPageResult`; `RenderRaw` is for SSG/internal callers.
- **BREAKING (effectively no-op) — `core/router.Middleware` is now a type ALIAS for `core/middleware.Middleware`.** Anonymous-func cast no longer needed when feeding `battery/auth.SessionMiddleware(mgr)` (or any battery middleware) into `Router.Use(...)`. Existing `router.Middleware(x)` conversions still compile. NOTE: `core/middleware/tracing_test.go` moved to `package middleware_test` because the alias introduces a test-only cycle.
- **BREAKING — `Screen.Policies` field unexported.** Use `Screen.WithPolicy(p)` to add, `Screen.PolicyChain()` to read a copy. Matches `ScreenGroup.policies` (already unexported).
- **Kiln-rendered `form` nodes default `enctype="application/json"`** because they target CRUD endpoints. The world API accepts an explicit `enctype` prop to override.

### Fixed

- **SECURITY (P0) — `/auth/register` no longer honors client-supplied `roles`.** Was an anonymous privilege escalation: any visitor POSTing `roles=admin` (form or JSON) was created with admin role. Form-encoded requests were CSRF-reachable from any origin. Now roles are server-assigned to `["user"]` by default; role elevation must happen via a separate admin-gated flow. Regression tests in `battery/auth/register_roles_security_test.go`.
- **SECURITY (P0) — `X-Gofastr-Location` open-redirect sealed.** A policy returning `decide.Redirect("//evil.com")` (or any non-relative URL) was emitted into the header raw, which the runtime feeds to `loadPage()` — a cross-origin fetch with credentials. Sealed via `isSafePartialRedirect` in uihost: only same-origin relative paths flow through the header path; absolute / protocol-relative / scheme-bearing / backslash-bypass URLs fall through to a hard 303 (which the browser handles safely). 8-case regression table in `framework/uihost/partial_redirect_test.go`.
- **(P0) Mutex copy in `renderComponentInScreen`.** The previous `tmp := *screen` copied a `sync.Mutex` while the caller held the lock; `go vet` flags it as a contract violation and it was a real concurrent-render corruption risk. Replaced with a free `wrapByScreenType(t, title, content)` helper reused from `Screen.RenderCtx`.
- **(P0) `RenderAlt` cross-user data leak via shared instance.** `WithRenderAlt(alt component.Component)` captured `alt` by pointer; concurrent anonymous requests racing through different screens with the same `landing` instance would clobber its `SetParams`/`Inject`/`Load` mutations across users. Changed to `WithRenderAlt(factory func() component.Component)` — framework calls the factory once per request. Race-tested under `-race` with 32 parallel requests across 8 distinct gated screens.
- **(P0) Partial-redirect `X-Gofastr-Location` was dead-lettered.** `handlePartialPage` previously set the header AND `http.Redirect(303)`. The runtime fetch silently chased the 303 server-side and the header never reached client JS. Now: 200 + header + empty body; runtime detects, replaces `pushState`, loads the redirect target. Chromedp e2e in `framework/uihost/partial_redirect_e2e_test.go`.
- **(P0) TagInput Enter swallow ate legitimate submits.** Chromium dispatches the implicit form submit despite a bubble-phase `preventDefault` on single-input forms. The prior defensive one-shot listener on the form ate the NEXT submit (the user's actual Save click). Replaced with a same-tick timestamp guard: a document-level capture-phase submit listener swallows submits within 50ms of the last tag-input Enter; legitimate submits a few hundred ms later proceed.

### Tests

New coverage added during the adversarial review + tightening pass:

- `framework/uihost/partial_redirect_e2e_test.go` — full chromedp chain for SPA-nav into a Redirect-policy screen.
- `framework/uihost/partial_redirect_test.go` — httptest for the 200+header contract, full-page 303 non-regression, `X-Gofastr-Location` open-redirect rejection (8-case table), ContextOnly screens through full uihost dispatch.
- `framework/uihost/native_form_e2e_test.go` — chromedp confirming an unadorned `<form action="/x" method="POST">` (no enctype, no opts) submits browser-native, Set-Cookie sticks, 303 followed.
- `framework/uihost/render_alt_visual_test.go` — RenderAlt anon→landing screenshot.
- `framework/uihost/safe_path.go` — `isSafePartialRedirect` helper.
- `core-ui/app/policy_test.go` — RenderAlt factory-per-request (concurrent across 8 screens), policy resolver edge cases.
- `battery/auth/policy_test.go` — `SessionPolicy` / `RolePolicy` matrix incl. `?next=` table (6 cases), `WithRenderAlt`, anon→403 default, anon→redirect override, `NoNext()`.
- `battery/auth/register_roles_security_test.go` — privilege-escalation regression (JSON + form).
- `battery/auth/manager_dev_secret_test.go` — random JWT secret minting / explicit-secret preservation / prod-mode opt-out.
- `core/router/middleware_alias_test.go` — alias compile-time + Router.Use acceptance.
- `core-ui/component/context_component_test.go` — ContextOnly satisfies Component, ContextComponent preferred over Render.
- `framework/entity/declaration_owner_field_test.go` — JSON round-trip + omitempty.

## 2026-05-23 — round-1 DX feedback + 6 rounds of adversarial review

Commit `2044154`. Addressed FRAMEWORK-FEEDBACK.md from a third-party
app (`wtf-do-i-eat`). Highlights:

### Added

- **`EntityConfig.OwnerField`** — declarative per-user CRUD scoping. Auto-CRUD now injects `WHERE owner_field = <ctx user>` for List/Get/Update/Delete and auto-stamps Create.
- **`battery/auth.SessionMiddleware(mgr)`** — cookie → ctx user loader (the missing counterpart to JWT-only `RequireAuth`).
- **`battery/auth.RequireSession(opts...)` + `WithRedirectOnFail(path)`** — HTTP middleware to gate JSON/API routes (or, with redirect option, browser flows).
- **`battery/auth.VerifyAuthEntitiesPrivate()`** — startup audit that fails fast if `users`/`sessions` entities are exposed via REST or MCP.
- **CSRF helpers + form-encoded auth endpoint negotiation.**

### Fixed (security)

- Open-redirect via `next=/\evil.example` and percent-encoded backslash variants in `successRedirect`.
- Anonymous SSE event leak.
- Anonymous batch endpoints mutating others' rows.
- Hook OR-clause precedence bypass.

## 2026-05-22 — worktree isolation mode

Commit `118605c`. First-class runtime resolver for git-worktree
collisions on `PORT`, SQLite files, Postgres database names, and
service env values. See `framework/docs/content/isolation.md`.
