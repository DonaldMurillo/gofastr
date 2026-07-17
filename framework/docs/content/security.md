# Security defaults

`core/middleware` provides the defensive HTTP primitives the framework
composes by default. Most apps should accept the defaults and override
specific knobs rather than rebuild the chain.

## The default stack

`framework.NewApp` installs this middleware chain on `app.Router` unless
you pass `WithoutDefaultMiddleware()` (or call `app.Use(...)` before
registering entities, which also disables it):

```go
middleware.Recovery()
middleware.RequestID()
middleware.Logging()
middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})
middleware.Timeout(30 * time.Second)
```

`Recovery` is outermost so a panic anywhere below it produces a clean
`500`. `RequestID` runs next so every later log line carries the trace
ID. `Timeout` is innermost — a `30s` deadline that cancels the request
context if the handler hangs.

## SecurityHeaders

```go
middleware.SecurityHeaders(middleware.SecurityHeadersConfig{
    ContentSecurityPolicy: "default-src 'self'; img-src 'self' https://cdn.example.com",
    ReferrerPolicy:        "strict-origin-when-cross-origin",
    FrameOptions:          "SAMEORIGIN",
    PermissionsPolicy:     "geolocation=(self)",
})
```

| Header                    | Default                                                                          |
|---------------------------|----------------------------------------------------------------------------------|
| `Content-Security-Policy` | `default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'` |
| `X-Content-Type-Options`  | `nosniff` (always, not configurable)                                            |
| `Referrer-Policy`         | `no-referrer`                                                                    |
| `X-Frame-Options`         | `DENY`                                                                           |
| `Permissions-Policy`      | `geolocation=(), microphone=(), camera=()`                                       |
| `Strict-Transport-Security` | `max-age=31536000` (1 year) — **HTTPS responses only** |

**HSTS is on by default.** `Strict-Transport-Security` is emitted with a
one-year `max-age` whenever the request is HTTPS — direct TLS, or a
TLS-terminating proxy that sets `X-Forwarded-Proto: https` (the app
sees plain HTTP there). Plain-HTTP local dev never receives it. Set
`HSTSMaxAge: -1` to disable, a positive value to change the age, or
`HSTSIncludeSub` / `HSTSPreload` to extend it.

The CSP default works with the built-in UI runtime because all CSS and
scripts are served as external resources under `/__gofastr/*`. If you
embed third-party scripts, fonts, or frames you must override
`ContentSecurityPolicy` explicitly — do not relax it with
`'unsafe-inline'` globally.

### Configuring the default chain's headers

The example above constructs the middleware by hand. The default chain
installed by `NewApp` is configurable through `AppConfig.SecurityHeaders`
(or the `framework.WithSecurityHeaders(cfg)` option), so you can relax a
single directive — e.g. allow `style-src 'unsafe-inline'` for a
third-party CSS dependency — without shadowing the whole chain with your
own `SecurityHeaders` middleware:

```go
app := framework.NewApp(framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
    ContentSecurityPolicy: "default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:",
}))
```

Unset fields keep their built-in defaults (the strict CSP, `Referrer-Policy:
no-referrer`, `X-Frame-Options: DENY`, …), so a partial override never
silently drops a defensive header. The zero value reproduces the original
strict defaults exactly.

## CORS

```go
middleware.CORS(middleware.CORSConfig{
    AllowedOrigins:   []string{"https://app.example.com"},
    AllowedMethods:   []string{http.MethodGet, http.MethodPost},
    AllowedHeaders:   []string{"Authorization", "Content-Type"},
    AllowCredentials: true,
    MaxAge:           600,
})
```

CORS is **not** in the default chain. Add it explicitly if your API
serves browser clients on another origin.

## CSRF

```go
middleware.CSRF(middleware.CSRFConfig{
    CookieName:   "fui_csrf",
    HeaderName:   "X-CSRF-Token",
    Skip:         middleware.SkipBearerAuth(),
    SecretKey:    loadCSRFKeyFromEnv(), // 32+ random bytes
    CookieSecure: true,                 // production HTTPS
})
```

Issues a cookie on safe requests; requires the matching header on
mutating requests (`POST`, `PUT`, `PATCH`, `DELETE`).
`SkipBearerAuth()` is the shipped helper that bypasses CSRF on
requests with `Authorization: Bearer …` — appropriate for pure API
deployments where the browser is not involved.

**Always set `SecretKey` explicitly in production.** The middleware
will autogenerate one if omitted, but that key rotates every process
restart — and the auditable signing seam moves into the binary
instead of into your secret store. Source it from your config /
secret manager the same way you'd source `SessionSecret`. With
`SecretKey` set AND `CookieSecure=true`, the cookie also gets the
`__Host-` prefix in production, blocking subdomain cookie-injection
attacks.

On the next safe-method request (GET / HEAD / OPTIONS) the middleware
**self-heals** stale or tampered cookies: it verifies any incoming
cookie against `SecretKey` + `AdditionalKeys` and silently re-mints
one if the signature doesn't validate. This means a process restart
(which rotates an auto-generated key) or a key rotation that drops
the previous secret no longer leaves browsers stranded with a cookie
that's guaranteed to 403 the next POST. To carry tokens across a
planned rotation without bouncing in-flight forms, list the previous
secret(s) in `AdditionalKeys`; drain once the old tokens have
expired.

## Rate limiting

```go
middleware.RateLimit(middleware.RateLimitConfig{
    Capacity:    100,         // peak burst
    RefillEvery: time.Minute, // +RefillBy tokens per interval
    RefillBy:    100,
})
```

Token-bucket per key. `KeyFunc` defaults to `RemoteAddr` (X-Forwarded-For
is ignored unless `TrustProxyHeaders` + `TrustedProxies` are set). Tune
`Capacity`/`RefillEvery`/`RefillBy` per route by composing two `RateLimit`
middlewares in different `middleware.Chain` calls.

On every response that passes through it (both allowed and 429) the
middleware also emits the IETF-draft budget headers so well-behaved API
clients can self-pace: `RateLimit-Limit` (the configured `Capacity`),
`RateLimit-Remaining` (tokens left after this request), and
`RateLimit-Reset` (whole seconds, rounded up, until the bucket is back at
full capacity). Set `OmitBudgetHeaders: true` to suppress them when the
per-response header cost matters at scale or an upstream cache would shard
by remaining budget; `Retry-After` on the 429 path is unaffected. The auth
battery's own limiter (`battery/auth`) intentionally exposes **only**
`Retry-After` and never the budget headers — a live remaining-attempt count
on login / password-reset endpoints would hand an attacker exact brute-force
pacing information.

## OpenAPI coverage for auth endpoints

Auth endpoints registered by `AuthManager.RegisterRoutes` (login,
register, logout, /auth/me, /auth/2fa/*, /auth/oauth/*, magic-link,
verify-email, forgot-password, reset-password, /auth/accounts,
/auth/unlink/{provider}) are **not** currently part of the
auto-generated OpenAPI spec.

`framework/openapi.EntityOpenAPI` walks the entity registry to emit
schemas for entity CRUD routes. Plugin-registered HTTP handlers go
through `router.Post / router.Get / …` directly and don't carry
schema metadata that the spec generator can consume. There is no
plugin → OpenAPI extension hook today.

Until that hook lands, the auth surface is documented through this
file, the plugin source comments, and integration tests. If your
deployment needs an OpenAPI document that includes the auth routes,
hand-write them into a sibling spec and merge with the generated one
in the gateway / docs pipeline.

## The full inventory

`core/middleware` exports:

- `RequestID()` — generates or echoes `X-Request-ID`.
- `Recovery()` — turns panics into `500` with structured log line.
- `Logging()` / `LoggingFn(getLogger)` / `LoggingWithWriter(io.Writer)` —
  structured request log. `LoggingFn` reads the logger per-request so
  plugins can swap it after the chain is wired.
- `SampledLogging(sampleN, slowThreshold)` — logs 1-in-N requests but
  always logs errors (status ≥ 400) and slow ones (duration >
  `slowThreshold`). Preferred for production paths where the unsampled
  `Logging()` cost dominates the middleware chain.
- `DiscardLogging()` — request-timing wrapper that emits no log lines;
  for high-throughput surfaces where structured logging is handled by
  an upstream proxy or APM agent.
- `SecurityHeaders(SecurityHeadersConfig)` — defensive headers above.
- `CORS(CORSConfig)` — cross-origin headers + preflight.
- `CSRF(CSRFConfig)` — double-submit cookie pattern.
- `RateLimit(RateLimitConfig)` — token-bucket per key.
- `Timeout(d)` — per-request deadline; cancels context on expiry.
- `NewMetrics()` + `MetricsMiddleware` + `MetricsHandler` — RED metrics.
- `Tracing(TracingConfig)` — OpenTelemetry span around each request.

Each has a `*_test.go` you can read for the exact behaviour.

## Availability notes

- **SQLite serialises writes.** Concurrent write load can climb to
  100ms+ p99 latencies and starve out non-write traffic — a soft DoS
  vector for any endpoint that writes. Set `MaxOpenConns(1)` on the
  `*sql.DB`, keep writes off the request path where possible (queue +
  background worker), or run Postgres. Full discussion in
  `docs/migrations.md` §Concurrency model.

## Owner isolation and `CrossOwnerRead`

Entities with `OwnerField` scope every read/write to the requesting
user's rows — the framework refuses anonymous requests (401) and
injects `WHERE <owner_field> = <ctx user id>` into every query so a
user can never see or mutate another user's data. `CrossOwnerRead`
optionally widens this for **reads only**: when the request context
holds the named RBAC permission, List/Get/Count span all owners.
Writes (Create/Update/Delete) stay owner-scoped regardless, and
multi-tenant isolation is preserved — a granted context in tenant A
never sees tenant B rows. The widening is fail-closed: no access policy
in context ⇒ no widening. See
[entity-declarations](entity-declarations.md) → "CrossOwnerRead".

## Default CRUD authentication

Prior to this section's introduction, an entity declaring **neither**
`OwnerField` **nor** `Access` got zero enforcement from auto-CRUD: List,
Get, Create, Update, and Delete were all reachable by an anonymous
caller — an unauthenticated `POST /api/<entity>` returned 201 and
persisted the row. Generated MCP tools inherited the same gap, since
`RegisterEntityMCPTools` dispatches entity tools through the same router
+ middleware chain as REST.

Auto-CRUD is now secure-by-default: `framework/crud`'s `requireScope`
chokepoint requires an authenticated session (`core/handler.GetUser`)
for every operation on an entity that declares none of `OwnerField`,
`Access`, or `Public`. The three opt-outs, in the order they're checked:

1. **`OwnerField` set** — the existing `RequireOwner` gate already
   requires an authenticated owner for every operation; the new
   session gate is redundant there and steps aside.
2. **`Access` declared** (any operation, even a partial block) — RBAC
   governs the entity "as today": a blank permission for an operation
   leaves it un-gated by RBAC, and the new session gate does not layer
   an extra requirement on top.
3. **`Public: true`** — a deliberate, full opt-out. Every operation,
   reads and writes, is open to anonymous callers — the framework's
   pre-secure-by-default behaviour for that entity. Meant for content
   that's genuinely public (a contact form, a blog's comments), not as
   a workaround for a 401 during development. An entity that wants
   public reads but gated writes uses `Access` instead (a blank
   `Read` + a real `Create` permission).

Because entity MCP tools dispatch through the router, this gate governs
them automatically — no separate `mcp.Gated`/`auth.MCPUser` wiring is
needed for generated CRUD tools (that machinery remains for *custom*
tools registered directly via `app.MCP.RegisterTool` or
`Endpoint.MCPHandler`, which bypass route middleware; see
[agent-ready](agent-ready.md)).

`gofastr generate` prints a warning listing every entity left publicly
readable/writable (`public: true`) so a generated app's open surface is
never silent. See [entity-declarations](entity-declarations.md) →
"Default CRUD authentication" for the blueprint YAML shape.

## Common mistakes

- **Relaxing CSP to fix a broken third-party script.** Override only
  the directive you need (`script-src`, `style-src`) — never replace
  `default-src 'self'` with `'unsafe-inline'`.
- **Skipping `Recovery` because the app doesn't panic.** It does
  eventually. Without it, a single panic terminates the request handler
  goroutine without writing a response, leaving the client hanging.
- **Composing CORS before `RequestID`.** Preflights still need trace
  IDs; keep `RequestID` first.
