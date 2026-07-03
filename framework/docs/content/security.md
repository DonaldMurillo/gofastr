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
    Requests: 100,
    Window:   time.Minute,
    KeyFunc:  func(r *http.Request) string { return r.RemoteAddr },
})
```

Token-bucket per key. `KeyFunc` defaults to `RemoteAddr`. Tune
`Requests`/`Window` per route by composing two `RateLimit` middlewares
in different `middleware.Chain` calls.

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

## Common mistakes

- **Relaxing CSP to fix a broken third-party script.** Override only
  the directive you need (`script-src`, `style-src`) — never replace
  `default-src 'self'` with `'unsafe-inline'`.
- **Skipping `Recovery` because the app doesn't panic.** It does
  eventually. Without it, a single panic terminates the request handler
  goroutine without writing a response, leaving the client hanging.
- **Composing CORS before `RequestID`.** Preflights still need trace
  IDs; keep `RequestID` first.
