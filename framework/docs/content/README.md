# GoFastr documentation index

The top-level `README.md` is the entry point. These pages are
per-feature references — each grounded in the actual code, each
ending with a "common mistakes" callout.

## Architecture & conventions

- [Project architecture review](project-architecture-review.md) —
  living risk + gap survey. Updated only on fresh review passes.
- [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) — UI/runtime
  contract. **Mandatory reading** before any UI or runtime change.
- [`ROADMAP.md`](../ROADMAP.md) — forward-looking work (proposals,
  performance opportunities, in-flight plans).
- [Agent notes](agent-notes.md) — append-only review log.

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
- [Dev-mode livereload](dev-livereload.md) — auto-wired SSE refresh
  under `gofastr dev`; env kill switches for prod.

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
- [Admin UI](admin.md) — `battery/admin` stock screens for
  queue + audit log.
- [Search](search.md) — `battery/search` backend interface, memory
  implementation.
- [Widgets](widgets.md) — `core-ui/widget` builder and presets.

## Build-time tooling

- [Codegen](codegen.md) — YAML-configured generators, external extension
  protocol, safe output paths, and manifest-based cleaning.
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
