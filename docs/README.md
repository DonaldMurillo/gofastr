# GoFastr documentation index

The top-level `README.md` is the entry point. These pages are
per-feature references — each grounded in the actual code, each
ending with a "common mistakes" callout.

## Architecture & conventions

- [Project architecture review](project-architecture-review.md) —
  living risk + gap survey. Updated only on fresh review passes.
- [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) — UI/runtime
  contract. **Mandatory reading** before any UI or runtime change.
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
- [Plugins](plugins.md) — `Plugin` + optional capability interfaces.

## Operational surface

- [Migrations](migrations.md) — SQL files, CLI subcommands,
  auto-migrate, dialects.
- [Security defaults](security.md) — default middleware chain, CSP,
  CORS, CSRF, rate limiting.
- [Search](search.md) — `battery/search` backend interface, memory
  implementation.
- [Widgets](widgets.md) — `core-ui/widget` builder and presets.

## Build-time tooling

- [Blueprints](blueprints.md) — deterministic YAML-to-code input for
  `gofastr generate --from`, backed by the in-house `core/yaml` parser.
- [Kiln (agent-driven build mode)](kiln.md) — separate binary; build a
  GoFastr app live via an agent CLI, then freeze to canonical JSON.
- [Benchmarks](benchmarks.md) — tiered Go benchmarks covering claims-
  defending end-to-end paths, hot-path microbenchmarks, concurrency,
  and startup. `make bench`, output to `dist/bench/`.
- [Performance opportunities](performance-opportunities.md) —
  prioritized improvement list derived from the benchmark results.
  Each item names the benchmark that surfaced it for verification.

## Maintaining these docs

The `gofastr-docs` skill at
`.claude/skills/gofastr-docs/SKILL.md` auto-loads when adding,
changing, or removing any exported API. Docs ship in the same commit
as the code — not a follow-up.
