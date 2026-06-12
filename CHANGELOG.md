# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

## [Unreleased]

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
