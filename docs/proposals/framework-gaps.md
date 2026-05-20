# Framework gaps

A living survey of where GoFastr already covers production needs and
where genuine holes remain. Use this to pick the next high-leverage
addition; update it whenever a gap closes or a new one surfaces.

This is paired with [`project-architecture-review.md`](../project-architecture-review.md),
which captures *risks in what we have*. This document captures
*things we don't have yet*.

---

## What's already in the tree

The framework is more mature than a first scan suggests. The
following are present and exercised by tests:

### HTTP middleware (`core/middleware/`)

- **Metrics** — Prometheus-compatible counters + latency histograms
  (`metrics.go`).
- **Tracing** — OpenTelemetry spans with W3C traceparent
  propagation (`tracing.go`).
- **Rate limiting** — token-bucket per pluggable key, 429 +
  Retry-After (`ratelimit.go`).
- **CSRF** — double-submit cookie, optional signed mode with
  `__Host-` prefix (`csrf.go`).
- **CORS** — `cors.go`.
- **Security headers** — CSP, HSTS, Referrer-Policy, frame
  options, permissions policy (`security.go`).
- **Recovery**, **request-id**, **timeout**, **logging** — round
  out the default chain.

### Framework spine (`framework/`)

- Entity declarations, CRUD (single + `_batch` + cursor +
  stream + upload + upsert), filter, pagination, DSL.
- Hooks (Before/After * Create/Read/Update/Delete) and typed hooks.
- Auto-migration, schema diffing, dialect detection.
- Multi-tenancy, soft delete, audit log, access (RBAC).
- Cron, events (internal pub/sub), file uploads, slow-query log.
- OpenAPI + MCP tool generation, TS SDK codegen
  (`cmd/gofastr/generate_ts.go`).

### Batteries (`battery/`)

- **auth** — login, OAuth2, magic-link, 2FA, password reset,
  account verification, rate limiting on auth endpoints.
- **cache** — memory + redis with HTTP middleware.
- **email** — SMTP + templates.
- **embed** — vector embeddings with language-aware chunking.
- **queue** — memory + db + redis + scheduler.
- **search** — in-memory full-text.
- **storage** — local + memory + S3.

### UI

- SSR-first + island hydration runtime, widget builder, pattern
  library, typed theme, form module, llm.md auto-gen, kiln live
  builder + YAML blueprint codegen.

---

## Tier 1 — genuine gaps that bite the first production deploy

Each of these is small, composes with existing pieces, and is
felt almost immediately when a real app ships.

### 1. Idempotency keys on unsafe writes — ✅ landed (hardened)

`core/middleware/idempotency.go` + [`docs/idempotency.md`](../idempotency.md).
Pluggable `IdempotencyStore` (memory + SQL), fingerprint-based
conflict detection (422), in-flight conflict (409), 2xx replay with
`Idempotent-Replay: true`, **principal namespacing** so two callers
sharing an `Idempotency-Key` never see each other's body, **strips
Set-Cookie / Authorization** from cached headers, **fail-closed (503)
by default** on store error (opt-in `FailOpen`), SQL store uses
`INSERT … ON CONFLICT DO NOTHING` for safe concurrent claims,
`Finish` runs on a fresh ctx so client disconnect doesn't strand
claims, memory store has `WithMemoryStoreMaxEntries` cap, SQL store
exposes `WithSQLIdempotencyDialect` / `WithSQLIdempotencyInFlightTTL`.
Redis backend remains TBD.

### 2. Built-in `/healthz` and `/readyz` — ✅ landed (hardened)

`framework/health.go` + [`docs/health-checks.md`](../health-checks.md).
`/healthz` is unconditional. `/readyz` runs registered checks in
parallel under a 5s deadline (override via `WithReadinessTimeout`),
**recovers from panicking checks**, **nil-guards check functions**,
races the wait against the deadline so a ctx-ignoring check is
reported as `timeout` instead of hanging, and **redacts error text
by default** so an unauthenticated probe cannot fingerprint internal
infrastructure (opt-in `WithVerboseReadiness`). Plugins and
batteries register via `ReadinessRegistrar.RegisterReadinessChecks`
— the framework now actually probes for and calls it during
`InitPlugins`. `db` is auto-registered when `WithDB` is set.

### 3. Feature flags / kill switches — ✅ landed (hardened)

`core/featureflag` + `framework/flags.go` + [`docs/feature-flags.md`](../feature-flags.md).
Enabled / Rollout / Users / Tenants / Envs rule model, stable FNV-1a
bucketing (with optional process-private salt via
`NewEvaluatorWithSalt` for adversary resistance), `EvalContext`
carried via context, `App.IsEnabled(ctx, key)` ergonomic call,
`Evaluator.BoolDefault(ctx, key, fallback)` for safe-default kill
switches, in-memory + SQL stores. Anonymous subjects (empty
UserID/TenantID) short-circuit to off below 100% rollout so a
deterministic-per-key bucket cannot silently flip an entire
anonymous segment. SQL store has `WithSQLDialect` and rejects
SQL-reserved table names. `SetFlagStore` panics if called after
the lazy default has already fired (no stale-evaluator race). Package
renamed to `featureflag` to avoid stdlib `flag` collision. Redis
store remains a follow-up.

### 4. Outbound webhooks — ✅ landed (hardened)

`battery/webhook` + [`docs/webhooks.md`](../webhooks.md). HMAC-SHA256
signing with **timestamp bound into the signed material**
(`t=<unix>,v1=<hex>` Stripe-style — legacy `Sign`/`Verify`
deprecated), retry-with-backoff, dead-letter terminal state, glob
event filters with `**` recursive wildcard, in-memory + SQL stores,
`framework/event` bridge with optional `WithBridgeMarshalError` /
`WithBridgePublishError` callbacks. **SSRF guard** rejects
subscriber URLs targeting RFC1918, loopback, link-local, cloud
metadata, and non-http(s) schemes — opt-out via
`Options.AllowPrivateNetworks` for dev/tests. **Response body
capped** at `Options.MaxResponseBodyBytes` (default 64 KiB).
**Worker runs under a cancelable context** derived from `Start` and
cancelled by `Stop`, so `App.Shutdown` actually aborts in-flight
HTTP attempts. Subscribers can be registered paused via
`Paused: true`. `newID()` panics on `crypto/rand` failure rather
than minting all-zero IDs.

Multi-instance safety: the SQL store implements the optional
`LeasedStore` interface (Postgres `FOR UPDATE SKIP LOCKED`; SQLite
`BEGIN IMMEDIATE` + atomic update). The Manager auto-detects the
interface and uses the claim/lease path so concurrent workers don't
double-deliver. Tunable via `Options.LeasePeriod` (default 30s).

Secret at rest: `WithSQLSecretCodec(codec)` encrypts
`webhook_subscribers.secret` on write and decrypts on read. The
bundled `NewAESGCMSecretCodec(key)` covers the AES-128/192/256 cases;
unprefixed legacy rows pass through unchanged so deployments can roll
encryption without a migration step.

---

## Tier 2 — felt the first time a customer asks for it

### 5. Internationalization

No locale routing, no string catalog, no ICU number/date
formatting. Probably the biggest "is this a real framework" gap
once a non-English customer arrives.

### 6. Unified notifications

`battery/email` exists, but a single `notify.Send(user,
"order.shipped", data)` that fans out across channels (email +
in-app + webhook + push) is missing. Each channel today is its
own integration.

### 7. Factory / fixture / seeder ergonomics

`testharness` covers test DB lifecycle but the developer-facing
`factory.Make[User](opts)` and `db.Seed(...)` ergonomics aren't
there. Rails-style fixtures or `factory_bot` equivalents.

### 8. Admin UI for queue + audit log

The data already exists (`battery/queue`, `framework/audit`). What
is missing is a stock screen — jobs in flight, retries, DLQ, audit
search. A small UI battery on top of existing data.

---

## Tier 3 — quality of life that compounds

### 9. WebSocket primitive

SSE is excellent for push. Bidirectional surfaces (collab cursors,
presence, multiplayer islands) need a WebSocket equivalent in
`core/stream/` with the same backpressure rules.

### 10. CLI scaffolding beyond kiln

`kiln` is the high-level live builder. A lower-level
`gofastr new entity Post --fields …` for users who don't want the
visual flow.

### 11. Configuration management

No first-class config loader (env + file + secret-source). Apps
roll their own with `os.Getenv`. A `core/config` with typed binding
and validation removes a class of bugs.

### 12. Health-aware graceful shutdown contract

`App.Shutdown(ctx)` exists but there is no published contract for
"drain in-flight requests, stop accepting new ones, flush queues."
A documented lifecycle (and a `framework/lifecycle/` plugin hook)
would let batteries cooperate.

---

## What is *not* a gap (despite first impressions)

- **Observability** — Prometheus + OTel are already wired
  (`core/middleware/{metrics,tracing}.go`). What's missing is
  *opinionated defaults / dashboards*, not the primitive.
- **Rate limiting** — already present (`core/middleware/ratelimit.go`),
  plus auth-specific rate limiting in `battery/auth/ratelimit.go`.
- **CSRF / CORS / security headers** — all present in
  `core/middleware/`. The default `applyDefaultMiddleware` chain
  in `framework/app.go` should wire them by default if it doesn't
  already — that's a polish item, not a gap.
- **OAuth, magic link, 2FA, passkeys** — `battery/auth` has the
  first three; passkeys remain to be added (Tier 2 sub-item).
- **TypeScript SDK** — `cmd/gofastr/generate_ts.go` exists.

---

## Order of operations

Tier 1 — all landed:

1. ✅ Idempotency middleware (in-memory + SQL store, wired into the
   default chain via `WithIdempotency`).
2. ✅ Health/ready endpoints.
3. ✅ Feature flags (in-memory + SQL store).
4. ✅ Outbound webhooks (in-memory + SQL store, plus `event` bridge).

Next up: Tier 2, where **i18n** is the highest leverage.
