# GoFastr documentation index

The top-level `README.md` is the entry point. These pages are
per-feature references, each grounded in the actual code. Every
guide doc ends with a "common mistakes" callout; the handful of
data and index artifacts (this index, the overview map, benchmark
results, the harness contract) are exempt — the exemption list lives in
`framework/docs/docs_test.go` and is enforced by
`TestGuideDocsEndWithCommonMistakes`.

## Start here

- [Blueprint tutorial](tutorial-blueprint-app.md) — the thesis
  walkthrough: blueprint → generated UI + API → auth + owner scoping
  + RBAC → customize in plain Go → deploy.
- [Comparison](comparison.md) — vs PocketBase, Encore, Wasp, and
  hand-rolled Gin+sqlc; weaknesses stated honestly.

## Architecture & conventions

- [Runtime contract](runtime-contract.md) — the UI/runtime contract:
  SSR/hydration/island/SSE model + `data-fui-*` attribute reference.
  **Mandatory reading** before any UI or runtime change. (Embedded
  extract; the repo's source of truth is `core-ui/ARCHITECTURE.md`.)
- [`ROADMAP.md`](../ROADMAP.md) — forward-looking work (proposals,
  performance opportunities, in-flight plans).

## Entity surface

- [Entity declarations](entity-declarations.md) — JSON + Go field types
  and the runtime/code-gen loaders.
- [Batch endpoints](batch-endpoints.md) — `_batch` POST / PATCH / DELETE
  with one-transaction semantics.
- [Includes & eager loading](includes.md) — `?include=` with scoped
  filters and nested paths.
- [Cursor pagination](cursor-pagination.md) — opt-in keyset paging,
  composite cursors, when to pick offset vs cursor.
- [Entity events & SSE](events.md) — `_events` stream, push-only,
  backpressure rules.
- [File uploads](uploads.md) — multipart on `Image`/`File` fields via
  `WithFileStorage`.
- [Query DSL](query-dsl.md) — agent-friendly query parser →
  `core/query` builder.

## App surface

- [Hooks & transactions](hooks-and-transactions.md) — lifecycle hook
  points, `TxFromContext`, `App.InTx`, typed hooks.
- [Access control](access-control.md) — `RolePolicy`,
  `RequirePermission`, custom `Policy` implementations.
- [Authentication](auth.md) — `battery/auth`: AuthManager + plugins
  (login, OAuth, magic-link, 2FA, accounts, email verification,
  password reset), optional UserStore/SessionStore extensions,
  cookie + rate-limit posture, threat model.
- [Multi-tenant scoping](multi-tenant.md) — auto-inject, auto-scope,
  the tenant header middleware.
- [Cron / scheduled jobs](cron.md) — in-process `Scheduler`, spec
  syntax, single-replica constraint.
- [Audit log](audit-log.md) — `WithAuditLog`, transactional row
  writes, schema.
- [Server logs](log.md) — `battery/log`: structured JSON access logs,
  panic recovery, lifecycle events; fan-out to file (default or chosen
  path) and webhook sinks.
- [Plugins](plugins.md) — `Plugin` + optional capability interfaces.
- [Heavy-JS plugin platform](plugin-platform.md) — sandboxed-iframe
  isolation host for megabyte-class client plugins; capability grants
  over auth scopes; the trusted-mount opt-out; the plugins.json
  registry convention.
- [Dev-mode livereload](dev-livereload.md) — auto-wired SSE refresh
  under `gofastr dev`; env kill switches for prod.
- [Runtime JS minification](runtime-minification.md) — embedded
  `runtime.js` minifier, env gating, prod-wins defaults.

## Building UI

- [UI getting started](ui-getting-started.md) — the 15-minute path:
  scaffold → design direction → theme → framework-native composition.
- [UI composition recipes](ui-composition-recipes.md) — product-shaped
  desktop/mobile page structures composed entirely from `framework/ui`
  primitives.
- [UI wiring](ui-wiring.md) — adding the UI system to a plain
  `framework.App` by hand: site app, theme, layout, `uihost.New`,
  and the `WidgetMounter` shim, with a complete `main.go`.
- [Theming](theming.md) — the token catalog, dark mode via
  `DarkColors` + `data-color-scheme`, `ui.Themed` section overrides,
  `--ui-*` component knobs, and why component internals are never
  overridden from site CSS.
- [Runtime contract](runtime-contract.md) — the SSR / hydration /
  island / SSE model and the `data-fui-*` attribute reference
  (embedded extract of `core-ui/ARCHITECTURE.md`).
- [UI components index](ui-new-components.md) — one-page catalog of
  every component the framework ships, each with its package, `go doc`
  path, and live demo at `/components/<slug>` on the docs site.
- [Interactive patterns](interactive-patterns.md) — every `data-fui-*`
  behavior, client-only vs RPC-backed, plus writing a hand-written
  island end to end.
- [Form module](form-module.md) — HTML form primitives, `framework/ui`
  form components, validation, conditional sections, step wizards.
- [Widgets](widgets.md) — `core-ui/widget` builder and presets
  (modal, drawer, popover, toast).
- [Signal store](signal-store.md) — `core-ui/store` typed shared
  client state with SSR seeding.

## Operational surface

- [Worktree isolation](isolation.md) — automatic local port, DB, and
  service-env isolation for linked Git worktrees.
- [Migrations](migrations.md) — SQL files, CLI subcommands,
  auto-migrate, dialects.
- [Security defaults](security.md) — default middleware chain, CSP,
  CORS, CSRF, rate limiting.
- [Idempotency keys](idempotency.md) — `Idempotency-Key` header
  support for safe-retry of POST / PUT / PATCH / DELETE.
- [Health checks](health-checks.md) — `/healthz` + `/readyz`,
  custom readiness checks, plugin/battery integration.
- [Feature flags](feature-flags.md) — `core/featureflag` evaluator,
  rollout percentage, user/tenant/environment allow lists.
- [Outbound webhooks](webhooks.md) — `battery/webhook`: signed
  delivery, retry-with-backoff, dead-letter, glob event filters.
- [Internationalization](i18n.md) — `core/i18n` translator,
  JSON catalogs, plurals, `Accept-Language` negotiation.
- [Unified notifications](notifications.md) — `battery/notify`
  multi-channel fan-out with per-channel templates.
- [Factories / fixtures](factories.md) — `framework/factory`
  Rails-style test setup helpers.
- [Testkit](testkit.md) — `framework/testkit`: isolated per-test Postgres
  databases, migrate callback, auto-drop on cleanup.
- [Queue](queue.md) — `battery/queue`: background job processing, dead-letter
  replay, Redis and in-memory backends. *(page created by queue agent)*
- [Embed](embed.md) — local semantic search via brute-force cosine, no API
  key required. *(page created by embed agent)*
- [Admin UI](admin.md) — `battery/admin` stock screens for
  queue + audit log.
- [Printable documents](print.md) — `battery/print`: declare a
  print-friendly document route (invoice / receipt / report); optional
  headless-Chromium PDF via the `chromepdf` adapter.
- [Search](search.md) — `battery/search` backend interface, memory
  implementation.
- [Image pipeline](image.md) — `framework/image`: chainable
  Resize / Rotate / Flip / Modulate / Placeholder / BlurHash, pure-Go
  with no CGo or system codec dependencies.

## Build-time tooling

- [Static-site export](static-export.md) — `app.ExportStatic`: render
  every route in-process to a directory of query-free HTML + assets for
  any static host (GitHub Pages, S3). Replaces the broken `wget` crawl;
  stamps pages with `data-fui-static` so server-backed islands no-op
  instead of 404'ing.

- [Codegen](codegen.md) — YAML-configured generators, external extension
  protocol, safe output paths, and manifest-based cleaning.
- [Ship your API as a CLI](app-cli.md) — `gofastr generate cli`: a
  branded, stdlib-only terminal client for your customers, with scoped
  API-token auth, batch verbs, live `watch`, entity/verb selection, and
  a never-overwritten `custom.go` extension seam.
- [Ship your API as SDKs](sdk.md) — `gofastr generate sdk`: a
  downloadable Go SDK module and a zero-dependency JS/TS client, served
  by the app itself via `framework/sdkdocs` — live per-entity reference
  pages, tabbed install guides, and schema-hash drift detection.
- [Blueprints](blueprints.md) — deterministic YAML-to-code input for
  `gofastr generate --from`, backed by the in-house `core/yaml` parser.
- [Kiln (agent-driven build mode)](kiln.md) — separate binary; build a
  GoFastr app live via an agent CLI, then freeze to canonical JSON.
- [Benchmarks](benchmarks.md) — tiered Go benchmarks covering claims-
  defending end-to-end paths, hot-path microbenchmarks, concurrency,
  and startup. `make bench`, output to `dist/bench/`.

## Maintaining these docs

The `gofastr-docs` skill at
`.claude/skills/gofastr-docs/SKILL.md` auto-loads when adding,
changing, or removing any exported API. Docs ship in the same commit
as the code — not a follow-up.
