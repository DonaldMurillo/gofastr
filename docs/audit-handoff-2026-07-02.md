# GoFastr audit handoff — v0.10.0

**Produced:** 2026-07-02 · **Against:** `v0.10.0` (tag == HEAD `4934e75c`) · **Method:** six parallel read-only investigators (2 Opus, 3 Sonnet, 1 Haiku) across design-system consistency, security-defaults regression, API footguns, ops readiness, cross-surface consistency, repo hygiene. `go vet ./...` and `go build ./...` clean at time of audit.

This is an **assessment handoff**, not a fix log — nothing below was changed. Every finding is traced to source with `file:line`. Pick up any tier and act without re-running the audit.

## How to read this

- **Confidence:** `CONFIRMED` = a code path was traced end-to-end. `SUSPECTED` = strong signal, not fully traced.
- **Corroboration:** findings flagged by ≥2 independent agents are marked **[×2]** — highest confidence, act first within their tier.
- **Severity** is by production consequence, not exploit elegance.
- File:line citations are point-in-time; verify against current code before editing (some may have shifted).

---

## Baseline — verified still solid (do NOT re-audit)

The engine hardening from the June 2026 passes is intact and, in places, extended. Confirmed by direct code trace this round:

- `framework/crud/owner.go` `requireScope` is the single fail-closed chokepoint for owner + tenant + per-op RBAC, applied across **every** verb: List/Get/Create/Update/Delete (`crud.go:324,533,621,666,712`), all three batch verbs (`crud_batch.go:124,194,273`), streaming list (`crud_stream.go:40`), cursor, and upsert.
- Upsert preflight fails closed on foreign-owner/tenant and soft-delete resurrection (`crud_upsert.go:235-298`).
- Typed/in-process query gates Find/Count/UpdateAll/DeleteAll (`typed_query.go:107,163,247,295`).
- MCP entity tools re-dispatch through the full router so middleware + identity re-resolve (`crud/mcp.go:25-33,140-169`); Hidden fields excluded from tool schemas.
- SSE `_events` feed gated by owner + tenant + `requirePermission(opRead)` (`crud_events.go:90-112`).
- Tenant filter is `WHERE 1=0` on empty tenant (`framework/tenant/tenant.go:88-98`); `TenantMiddleware` does not trust the client header.
- auth Init fails closed on empty `JWTSecret` + `DevMode=false` (`battery/auth/manager.go:358`); `/auth/register` roles are server-assigned `["user"]` (`core.go:297`).
- Panic recovery universal-by-default (`core/middleware/recovery.go`); CRUD never leaks driver errors to clients; admin form errors re-render as HTML, never JSON.
- Security headers on by default (CSP `default-src 'self'`, X-Frame-Options DENY, nosniff); 1 MiB JSON body cap + content-type gate blocks simple-request CSRF on writes.
- Migrations: PG advisory lock + single-transaction additive-only auto-migrate + destructive-DROP gate; versioned CLI runner adds SHA-256 checksum drift + dirty-state blocking. Close to golang-migrate/Atlas parity.
- Health/readiness always mounted (`/healthz`, `/readyz` with DB ping); `X-Request-ID` middleware always in the default chain.
- Version bookkeeping clean: `v0.10.0` annotated tag == HEAD; CHANGELOG `[Unreleased]` empty; no version-const drift.

---

## Tier 1 — Production blockers

These are the "someone gets burned when they deploy" set. Shared root cause: **defaults are tuned for the single-node demo, not the deployed service.**

### T1.1 — Graceful shutdown is wired nowhere shipped · CONFIRMED · Blocker
Every example (`examples/blog/main.go:54`, `examples/site/main.go:60`, `examples/meridian/main.go:85`) and the blueprint generator (`cmd/gofastr/blueprint.go:2958`) call `app.Start(addr)` synchronously. `App.Start` installs **no** signal handling; that only exists in the separate opt-in `App.RunWithSignals` (`app.go:1186` → `lifecycle.go:211`), which nobody calls. `SIGTERM` (docker stop, k8s rollout) kills the process immediately — no drain, no `Batteries.StopAll`, no `db.Close()`. And `deploy.md:107-109` **falsely** claims `App.Start` drains in-flight requests.
**Fix direction:** make `Start` (or the generated `main.go`) wire signal handling by default, or rename so the contract is honest; add a bounded `ShutdownTimeout` (an open SSE stream can otherwise hang the drain — `sse_broker.go:166-190`); wait on in-flight cron jobs (`cron.go:105-155` only joins the ticking loop). Correct the doc either way.

### T1.2 — Silent multi-replica failure · CONFIRMED · Blocker
- Auth defaults to an in-memory `SessionStore` (`battery/auth/session.go:50,90-96`, self-documented "single-instance") with no warning at `auth.New()`. Second replica → sessions don't resolve.
- Login rate-limit (`ratelimit.go:49-54`) and 2FA state (`twofa.go:101-108`) are process-local with **no** shipped DB/Redis alternative.
- `framework/cron.Scheduler` and `battery/queue.Scheduler` fire on every replica with no default distributed lock (each doc mentions it in isolation; no unified "scaling" page).
- `core-ui/island.Manager` SSE state is in-process only — cross-replica pushes never arrive.
- Replica-safe counter-examples that already exist: `queue.DBQueue` (`FOR UPDATE SKIP LOCKED`) and webhook `LeasedStore`.
**Fix direction:** warn or fail-loud when an in-memory session store is used without an explicit single-node opt-in; ship DB-backed rate-limit/2FA stores or document the gap prominently; provide a default distributed-lock seam for schedulers; write one consolidated horizontal-scaling doc.

### T1.3 — Generated apps commit secrets to source control · CONFIRMED · Blocker
The blueprint generator inlines the JWT secret and DB DSN as plaintext Go string literals in committed `blueprint/app.go` (`cmd/gofastr/blueprint.go:5176-5177` for the secret with no `getEnv` indirection; `:4969` for the raw DSN). It never writes `.env`/`.gitignore` (unlike `gofastr new`, which does — `init.go:119-149`). Contradicts `deploy.md:81-82`. No test guards this.
**Fix direction:** emit `os.Getenv` reads + a `.env`/`.gitignore` scaffold from the generator; add a test asserting no secret literal lands in generated source.

### T1.4 — 2FA fails OPEN at login · CONFIRMED · High (Tier-1 by blast radius)
`battery/auth/core.go:210-225`: the "pending second factor" mark is only set inside `if marker, ok := store.(SessionPendingMarker); ok` — **no else, no log**. A host plugging in a custom `SessionStore` that doesn't implement the optional interface gets 2FA users fully authenticated on password alone. Same outcome if `HasTwoFactorEnabled` errors (`err != nil → continue`).
**Fix direction:** fail closed (reject login / destroy session) and WARN when a checker reports enabled/errored but the store can't mark pending.

---

## Tier 2 — High-severity footguns (host-developer facing)

All CONFIRMED by code trace unless noted.

### T2.1 — `WithConfig` wholesale-clobbers sibling options
`framework/app.go:220-224` (`a.Config = config`). Same class as commit 468d688a. `NewApp(WithAPIPrefix("/api"), WithPublicOpenAPI(), WithConfig(AppConfig{Name:"x"}))` silently discards the prefix, public-spec flag, `DebugEndpoints`, `RequestTimeout`. No warning.
**Fix:** merge field-by-field (as the agent-ready fix did), or panic when `WithConfig` follows a granular setter.

### T2.2 — admin exposes every entity as editable CRUD by default
`battery/admin/entity_admin.go:166-183`. `admin.New(admin.Config{})` with `Entities` zero-value = expose every CRUD-enabled table with full create/update/delete.
**Fix:** empty list → expose nothing; add explicit `AllEntities: true` for the whole-back-office behavior.

### T2.3 — webhook SSRF guard silently dropped with a custom HTTPClient
`battery/webhook/webhook.go:240-248`. `ssrfGuardedTransport` attaches only when `opts.HTTPClient == nil`. Supply your own client (proxy/tracing/timeout) and the guard is gone even with `AllowPrivateNetworks=false` — reopens `169.254.169.254`, RFC1918, loopback.
**Fix:** always wrap the caller's `Transport` with the guard unless `AllowPrivateNetworks` is true.

### T2.4 — Blueprint emits fully-public CRUD for unscoped entities; PII lint is name-heuristic · **[×2]**
`cmd/gofastr/generate.go:487,536-538` + `lint_pii.go:33-39,61`. Omitting `access`/`owner_field`/`multi_tenant` → anonymous world read/write/delete. The sole guard `lintUnscopedPII` matches a fixed token list (`email`, `ssn`, …), so `notes`/`journal_entry`/`balance`/`document` generate public with zero warning. **Live instance:** `examples/api-tour/main.go:71-81` `profiles` (holds per-user `bio`, required `user_id`, no `OwnerField`) → `GET /profiles` returns every user's bio anonymously.
**Fix:** warn on any auto-exposed entity lacking owner/access/tenant scoping, not just PII-named columns; and scope the api-tour example.

### T2.5 — `kind: line_chart` silently renders nothing · CONFIRMED · High (silent)
`cmd/gofastr/blueprint.go:3750`. `line_chart` passes `validateBlueprintBlock` + compiles, but `renderBlueprintCatalogBlock` only implements `bar_chart`/`pie_chart`; `line_chart` falls to the node renderer `default:` → emits `<!-- noderender: unknown kind "line_chart" -->`. Block disappears with no error at any stage; not in the documented kind catalog.
**Fix:** implement `line_chart`, or reject it at validation time.

---

## Tier 3 — Reliability footguns (medium-high)

- **Cache defaults never-expire AND unbounded** — `battery/cache/cache.go:138-147` + `memory.go:166-176`: `defaultTTL=0` + `maxEntries=0`. Request-derived keys → OOM; otherwise stale-forever. Redis variant passes `ttl=0` straight through. **Fix:** require an explicit TTL or default finite; bound `maxEntries` or fail-loud when both zero.
- **Queue Scheduler snapshots once + early-returns on empty** — `battery/queue/scheduler.go:151-159`: `Start` copies `s.schedules` once and returns if empty. Natural wiring (start subsystems, then register jobs) makes the goroutine exit; jobs registered after `Start` never run, no log. **Fix:** re-read schedules under lock each tick.
- **DBQueue: no handler timeout + 1 worker default** — `battery/queue/db.go:610-617` (no timeout) + `:68-70` (default 1). Compounds with `battery/email/smtp.go:62` ignoring its `ctx` and dialing with no timeout: one black-holed SMTP host wedges the only worker forever. **Fix:** add `WithHandlerTimeout` to DBQueue; give SMTP a `DialContext` deadline.
- **Event-handler panics swallowed with no logging** — `framework/event/event.go:146-154` (`emitSafe`). Recover is deliberate (a flaky subscriber shouldn't veto a write) but nothing is logged, so a panicking `AfterCreate` ("send welcome email") silently no-ops. **Fix:** keep the recover, log the panic at Error.
- **MemoryQueue retries on the already-cancelled ctx** — `battery/queue/memory.go:119-133`: timeout-failed jobs re-enqueue with the `Done` ctx; the error is discarded (`_ =`). The exact jobs retries exist for get dropped. **Fix:** re-enqueue with a fresh `context.Background()`.
- **`owner.SetExtractor` is last-call-wins global with only a WARN** — `framework/owner/owner.go:44`. Two registrants overwrite by init order; wrong extractor scopes every `OwnerField` row to the wrong identity. **Fix:** consider failing hard on a second non-nil registration.

---

## Tier 4 — Secure-by-default template gaps

These make a generated app less safe than the docs imply. Several were flagged by both the security and footgun agents **[×2]**.

- **CSRF opt-in; login/register-CSRF unprotected · [×2]** — generated app deliberately doesn't mount `auth.CSRF()` (`blueprint.go:5243`); relies on SameSite + content-type gate, which don't cover login/register (no pre-existing cookie). Admin screens even render a `_csrf` hidden field implying protection. Also: CSRF cookie isn't `Secure` behind a TLS-terminating proxy (`core/middleware/csrf.go:174`, `r.TLS==nil`). **Fix:** have admin/auth batteries mount CSRF on their own mutation routes or fail-loud if unwrapped; honor `X-Forwarded-Proto`.
- **HSTS never emitted · [×2]** — `framework/app.go:467` passes empty `SecurityHeadersConfig{}` → `HSTSMaxAge=0`; nothing sets it, nothing warns. Prod HTTPS ships without HSTS. **Fix:** default a non-zero max-age in prod or surface a warning.
- **Blueprint `dev_mode` defaults to `true`** — `cmd/gofastr/blueprint.go:428,5160-5166`: `app.auth.enabled` without explicit `dev_mode` → `DevMode: true` (non-Secure cookies, per-process JWT secret). Loudly warned at generate time, but the emitted default is insecure. **Fix:** default `dev_mode:false`, opt into local.
- **`multi_tenant: true` emitted with no tenant resolver** — `cmd/gofastr/generate.go:481-482`; generator emits no `TenantMiddleware`/`SetTenantID`. `ApplyTenantScope` is fail-closed, so reads return empty and writes stamp empty tenant — silently broken while looking secure. **Fix:** emit tenant-resolution middleware when any entity is multi-tenant, or refuse to generate without one.
- **/auth/register has no default rate limit** — `battery/auth/core.go:273`; only login is throttled. Account-table flooding + email-bombing once an EmailSender is wired. **Fix:** add a default per-IP register limiter.
- **govulncheck: 4 `golang.org/x/image` DoS advisories** (GO-2026-5066/5062/5061/4961), reachable at `framework/image/image.go:180`. Only bites hosts calling `framework/image` on untrusted bytes (the upload path does not auto-decode). **Fix:** bump `golang.org/x/image` to v0.43.0.

---

## Tier 5 — Opt-in perimeter (lower urgency, off by default)

- **Markdown surface bypasses per-screen auth Policy** — `framework/uihost/uihost.go:855-860`, `agentready.go:587-599`, `uihost.go:1828-1850`. `Accept: text/markdown` negotiation, `/{path}/llm.md`, and `/llm-pages.md` resolve the screen and write markdown **without** running the `Policy` the HTML path enforces (`core-ui/app/app.go:194`). `curl -H 'Accept: text/markdown' /admin` returns 200 where HTML 302/403s. Opt-in (`WithPublicLLMMD` + `WithMarkdownNegotiation`), and `ScreenLoader` screens short-circuit without `Load`, so **no live per-user data leaks** — exposure is route/structure enumeration + any static content in a gated screen's `Render()`. The *entity* markdown handlers were hardened (401 without a user, `crud/llmmd.go:324-353`); the *page* ones weren't. **Fix:** route markdown paths through `ResolvePolicy`.
- **Kiln DNS-rebinding → local command exec** — `cmd/kiln/main.go:293-315` `originGuard` compares `Origin` to the request's own `Host`, not a localhost allowlist. Chained with `POST /kiln/agent {"custom":"<cmd>"}` (`agent_http.go:28-82`, `agent_watcher.go:186-203`), a malicious page passes the guard and reaches a command-exec path. Loopback-default, experimental, documented-unauthenticated — but the guard's "stops DNS-rebinding" promise is false. Also `reset_session`/`undo` have no server-side confirmation gate (`kiln/chat/server.go:556-557`). **Fix:** validate `Host` against a localhost allowlist; don't accept adapter/command selection over HTTP.
- **Host-header reflection into discovery docs** — `framework/wellknown.go:25-38`, `agentready.go:501-520` trust `X-Forwarded-Proto/Host` with no allowlist when `BaseURL` is unset. Reflected into api-catalog hrefs, MCP server-card `remotes[].url`, agent-card `serviceURL`, ACP `api_base_url`, `Link` headers — all unauthenticated. An attacker controlling Host (or a path-keyed cache) can make docs advertise `https://evil.com/mcp`. **Fix:** set `AgentReadyConfig.BaseURL` by default / warn when unset; sitemap and robots are already safe.

---

## Tier 6 — Consistency & docs drift

### Design system (one-styling-surface contract)
- **`.mrd-*` classes hardcoded in the generator · [×2]** — `cmd/gofastr/blueprint.go:2540,3757,3763` emit `mrd-chart`/`mrd-chart__title`/`mrd-muted` into **every** generated app (`examples/meridian/blueprint/resource.go:527`, `examples/ecommerce/.../resource.go:527`). No CSS styles them (dead markup today), but it's the exact tripwire CLAUDE.md documents as fixed in June — a live generator regression. **Fix:** compose existing `framework/ui` primitives or a real registered class; delete the `.mrd-*` literals.
- **`examples/backoffice` ships bespoke CSS · [×2]** — `main.go:38-86`: `.bo-public`/`.bo-card`/`.bo-field`/`.bo-btn` via `uihost.WithCustomCSS`, plus hand-rolled `<form>`/`<input>` duplicating `ui.AuthCard`/`ui.Card`/`ui.Form`/`ui.FormField` (which meridian composes correctly). Hard-Rule-7 violation in a copyable example. **Fix:** swap in the existing components.
- **`framework/uihost` builtin CSS has token-less hex** — `frameworkBuiltinCSS` (~485-528): `.fui-nav-toast`, skip-link chip, SPA progress bar use raw `#18181B`/`#4F46E5` with no `var(--*)` (the rest of the same file uses `var(--x, #fallback)`). A themed app still gets a fixed indigo progress bar. **Fix:** token-ify these three rules.
- **`style.Contribute` is an undocumented 5th styling surface** — `examples/site/styles.go` (~2,500 lines) ships page CSS through it; ARCHITECTURE.md's "One styling surface" table names only 4 owners. Policy gap, not a bug — the loophole a future change copies. **Fix:** either document it as a sanctioned host-site-shell surface with scope, or migrate `examples/site` off it.
- **7 undocumented `data-fui-*` attrs** (Hard Rule 5 requires documenting each): `data-fui-active`, `data-fui-cols`, `data-fui-menu-panel`, `data-fui-multiselect-name`, `data-fui-sidebar`, `data-fui-sticky`/`data-fui-z-tier`, `data-fui-viewport`. All CSS-selector-only (not runtime-JS). **Fix:** add to the ARCHITECTURE.md table (or carve out CSS-only hooks explicitly).

### Docs that won't compile / mislead
- **~8 doc code samples reference APIs that don't exist:** `app.Router` used as a field not a method (`auth.md:46`, `plugins.md:26,164`, `access-control.md:23,143`); `Router.With` (`access-control.md:23`); `u.HasRole` (`admin.md` ~145 — no such method); `migrate.Table` (actually in `framework/migrate`, `migrations.md` ~300); `Registry.Names()` (`plugins.md:166`); `cron.Scheduler.Every` with a callback (`cron.md:19-24` — no such method); phantom `Sign`/`Verify`/`sha256=` webhook helpers (`webhooks.md:116-118`). **Fix:** correct each; add a compile-checked doc-code-block harness to kill the class.
- **README `gofastr migrate diff`** — documented at `README.md:405`, but the subcommand was removed (`migrate_cmd.go:30-38` errors and exits 1). **Fix:** update to `migrate generate`.
- **CLI `--help` stale** — `audit` help omits `lint`; `migrate` help omits `generate`/`force`. `gofastr audit lint`, `gofastr new`, and the top-level table (missing `new`/`harness`/`audit`/`validate`/`version`) under-documented.
- **Blueprint key docs incomplete** — `auto_generate`/`read_only`/`hidden`/`pattern` field keys, `relations[].type: has_many`, and `app.theme` font tokens are load-bearing in real example blueprints but absent from `blueprints.md`/`entity-declarations.md`. `tutorial-blueprint-app.md:17-24` has a stale "next release / install @main" note for features shipped in 0.5.0.
- **Story drift:** `comparison.md:111-115` calls **ecommerce** the flagship; everywhere else it's **meridian** (comparison.md is unlinked from README nav but still reachable). "10 framework/ui primitives" (`README.md:381,434`) — actual ~90. Repo-tour "~25 subpackages" vs "not 17" seven lines apart. island/widget/component terminology blurred in `overview.md:108` and the README repo-tour table.
- **README doc index** omits 17 existing doc pages (`agent-ready.md`, `deploy.md`, `observability.md`, `cache.md`, …); unlike the site catalogue (which has a parity test), nothing guards it.

### Observability blind spots (ops)
- `framework/crud/*.go` logs internal errors via stdlib `log.Printf` (`crud.go:431,459,481,…`), bypassing every configured sink, uncorrelated with request ID.
- Bare `framework.NewApp` has **no access logging** by default (deliberately, to avoid duplicating `battery/log`) — but a host that doesn't add the battery is blind.
- Metrics (`WithMetrics`), tracing (`WithTracing`), and slow-query logging all exist but are opt-in and off by default with no signal if omitted. No Sentry-class error-reporting hook anywhere.

---

## Repo hygiene notes

- Two stale local build binaries in the repo root: `./gofastr` (19M), `./site` (14M) — gitignored, not committed, but should be cleaned or moved to `dist/`.
- Real TODO worth tracking: `core-ui/runtime/src/lightbox.js:189` — two Lightbox widgets on one page cross-talk (module-scoped handlers + unnamespaced signals).
- ~20 packages have zero unit tests (mostly batteries — they lean on example-app integration coverage).
- "near-zero deps" positioning vs 66 `require` entries (testcontainers, sqlite/cgo, OTel, chromedp). 0 `replace` directives; toolchain `go 1.26.4`.
- CI (`ci.yml`, `pages.yml`) is healthy — both jobs blocking, no `continue-on-error` — but has **no lint/fuzz/govulncheck gate** despite the security-forward positioning; `make lint`/`fuzz`/`security` exist locally, unwired. No release-automation/publish pipeline.

---

## Suggested sequencing

Framed by risk and dependency, not effort:

1. **Tier 1 (blockers) + T1.4 (2FA).** These are deploy-breaking or auth-breaking and share the "single-node defaults" root — worth a coordinated pass rather than one-offs.
2. **Tier 6 design-system [×2] items** (`.mrd-*` generator + backoffice CSS). Fast, high-symbolism — they violate your own documented-as-fixed rules and sit in copyable examples.
3. **Tier 2 high footguns** (WithConfig, admin-default, webhook SSRF, unscoped-CRUD lint, line_chart). Independent; parallelizable.
4. **Tier 6 doc-compile drift.** Cheapest trust win, fully mechanical; add the doc-code-block CI harness so it can't regress.
5. **Tier 4 template gaps** — bundle with the Tier 1 secure-defaults pass if you touch the generator anyway.
6. **Tier 3 reliability + Tier 5 opt-in perimeter** — lower urgency; Kiln items are experimental/loopback so they can trail.

A **compile-checked README/doc harness** and an **"any unscoped auto-exposed entity" lint** would each retroactively catch a whole class here — worth prioritizing as force-multipliers.
