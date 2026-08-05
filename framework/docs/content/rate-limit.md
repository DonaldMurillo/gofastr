# Rate limiting

`framework/ratelimit` is the general-purpose HTTP rate limiter. It enforces a
per-key **sliding window with lockout**: up to `MaxAttempts` requests are
admitted inside `Window`; the next request trips a hard block lasting
`BlockDuration` during which every request for that key gets `429` with a
`Retry-After` header. This is the "at most N per period, then lock out" shape.

Use it for brute-force surfaces, write-heavy endpoints, and any route where a
steady refill rate is the wrong model. For steady API throughput
("1 req/s, burst 60") use the token-bucket middleware instead — see
[Two limiters, two jobs](#two-limiters-two-jobs).

## Quickstart — throttle one route

```go
import (
    "net/http"
    "time"

    "github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// At most 20 checkouts per IP per minute; the 21st trips a 5-minute lockout.
checkoutLimit := ratelimit.NewLimiter(ratelimit.Config{
    MaxAttempts:   20,
    Window:        time.Minute,
    BlockDuration: 5 * time.Minute,
})

mux.Handle("POST /api/checkout", checkoutLimit.Middleware()(checkoutHandler))
```

`Middleware()` keys on the client IP by default. Over-limit responses are
`429 Too Many Requests` with a `Retry-After` header (whole seconds); the body is
a plain `rate limit exceeded`.

## Throttle by something other than IP

`MiddlewareByKey` takes a function that derives the bucket identity from the
request — API key, authenticated user id, route param, anything. This is the
escape hatch when per-IP is too coarse.

```go
// Per API key, not per IP.
byKey := ratelimit.NewLimiter(ratelimit.Config{
    MaxAttempts:   100,
    Window:        time.Hour,
    BlockDuration: time.Hour,
})

mux.Use(byKey.MiddlewareByKey(func(r *http.Request) string {
    return r.Header.Get("X-Api-Key")
}))
```

A `nil` key func falls back to the client IP, so `Middleware()` is just
`MiddlewareByKey` with the default extractor.

## Key functions

| Symbol | What it does |
|---|---|
| `ratelimit.NewLimiter(cfg Config) *Limiter` | Construct a limiter; zero fields default to MaxAttempts=10, Window=15m, BlockDuration=30m. |
| `(*Limiter).Allow(key string) (bool, time.Duration)` | Record one attempt for `key`; returns allowed + retry-after. Use the context form on HTTP paths. |
| `(*Limiter).AllowContext(ctx, key)` | Same, observing request cancellation when a shared `Store` is set. |
| `(*Limiter).Middleware()` | `func(http.Handler) http.Handler`, keyed by client IP. |
| `(*Limiter).MiddlewareByKey(keyFunc)` | Same, keyed by a custom extractor. |
| `ratelimit.ClientIP(r, trustXFF)` | The default IP extractor; honours `X-Forwarded-For` only when `trustXFF` is true. |

`Config` fields: `MaxAttempts`, `Window`, `BlockDuration`, `TrustForwardedFor`,
`Store`, `Scope`, `DevMode`.

## Two limiters, two jobs

GoFastr ships two limiters with deliberately different semantics. Pick by the
question you are answering:

| Question | Use |
|---|---|
| "At most N per period, then block" (login, checkout, password reset) | `framework/ratelimit` (this package) |
| "Sustained rate with a burst" (public API throughput, "1 req/s, burst 60") | `core/middleware.RateLimit` (token bucket) |

The token-bucket middleware also emits the IETF-draft `RateLimit-Limit` /
`RateLimit-Remaining` / `RateLimit-Reset` budget headers so well-behaved API
clients can self-pace. This package emits **only** `Retry-After`: a live
remaining-attempt count on a lockout-style limiter would hand an attacker exact
brute-force pacing information on security-sensitive routes, and adds nothing
useful elsewhere.

## The auth battery already uses it

`battery/auth` builds every built-in brute-force limiter — login (per-IP and
per-account), register, 2FA challenge, magic-link send, password-reset,
email-verification — on top of this limiter. Those limiters are configured
through `AuthConfig` (`LoginRateLimit`, `RegisterRateLimit`, …) and apply
automatically when you mount the auth battery; you do not wire them by hand.
See [security.md](security.md) → "Rate limiting" for the auth-specific posture,
and the auth source for the per-endpoint scopes.

For your own non-auth routes, use this package directly as shown in the
quickstart above.

## Per-replica vs. shared budget

With a nil `Config.Store` the window lives in **process memory**, so the budget
is **per replica**: N replicas each admit `MaxAttempts`. That is the right
default for a single instance and for routes where per-replica throttling is
acceptable.

For a replica-wide (or fleet-wide) budget — so a coordinated burst across
replicas can't multiply the limit by N — supply a `Store`.
`battery/auth.SQLRateLimitStore` is the reference implementation (SQLite or
PostgreSQL); one store instance backs many limiters, with keys namespaced by
`Config.Scope` so a login limiter and a checkout limiter sharing one store never
collide:

```go
shared := auth.NewSQLRateLimitStore(db, "rate_limits")
// One store, two independent budgets:
loginLimit := ratelimit.NewLimiter(ratelimit.Config{
    MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute,
    Store: shared, Scope: "login_ip",
})
checkoutLimit := ratelimit.NewLimiter(ratelimit.Config{
    MaxAttempts: 20, Window: time.Minute, BlockDuration: 5 * time.Minute,
    Store: shared, Scope: "checkout",
})
```

On a store error the limiter **fails closed** (denies) — degrading the backend
must never lift the limit. A custom Redis/etcd backend only needs to satisfy the
`ratelimit.Store` interface.

## X-Forwarded-For and proxies

`ClientIP` (and therefore the default `Middleware()`) ignores
`X-Forwarded-For` unless `Config.TrustForwardedFor` is true. A client talking
directly to the origin can put any value in that header; trusting it
unconditionally would let one `curl` with a rotating `X-Forwarded-For` bypass
every per-IP limit. Enable `TrustForwardedFor` **only** behind a reverse proxy
you control that strips client-supplied XFF.

## DevMode

`Config.DevMode: true` makes `AllowContext` admit every attempt without touching
either backend — a relief valve for local screenshot / verification tooling that
hammers an endpoint from localhost. Production must never set it; the default
keeps the limiter fail-closed.

## Common mistakes

- **Reaching for the token bucket when you mean "N then lock out".**
  `core/middleware.RateLimit` refills continuously, so a brute-force attacker
  never gets a hard block — they just slow to the refill rate. Use this package
  for lockout semantics.
- **Trusting `X-Forwarded-For` without a stripping proxy.** A rotating XFF
  header gives the attacker a fresh bucket per request. Leave
  `TrustForwardedFor` false unless you sit behind a proxy you control.
- **Counting on a fleet-wide budget with the in-memory store.** With no `Store`,
  each replica admits the full `MaxAttempts` independently. Plug in a shared
  `Store` when the budget must hold across replicas.
- **Keying a public limiter on attacker-chosen input without a cap.** This
  package bounds its key map (idle keys are reclaimed, then soonest-expiring
  blocks under flood), but a custom key func over untrusted input (raw email,
  URL) still widens the key space — prefer stable identities (user id, hashed
  API key).
- **Expecting budget headers.** This limiter emits only `Retry-After` by design
  (see [Two limiters, two jobs](#two-limiters-two-jobs)). If a client needs
  `RateLimit-Remaining` to self-pace, that route wants the token bucket.
