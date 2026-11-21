# Rate limiting (`framework/ratelimit`)

GoFastr ships two limiters for two distinct jobs:

| Need | Use |
| --- | --- |
| "At most N attempts per window, then hard lockout" (login, checkout, password reset) | `framework/ratelimit` (this page; emits `429` + `Retry-After` only) |
| "Sustained API rate with burst" (e.g. 1 req/s, burst 60, with `RateLimit-*` budget headers) | `core/middleware.RateLimit` (token bucket) |

`battery/auth` already applies `framework/ratelimit` across login, registration, 2FA, magic links, and password resets automatically.

## Quickstart

```go
import (
	"time"

	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

checkoutLimit := ratelimit.NewLimiter(ratelimit.Config{
	MaxAttempts:   20,
	Window:        time.Minute,
	BlockDuration: 5 * time.Minute,
})

mux.Handle("POST /api/checkout", checkoutLimit.Middleware()(checkoutHandler))
```

Throttle by a custom key (API key, user ID, or `framework.AgentRateLimitKey` for Web Bot Auth) with `MiddlewareByKey`:

```go
mux.Use(byKey.MiddlewareByKey(func(r *http.Request) string {
	return r.Header.Get("X-Api-Key")
}))
```

Key methods: `NewLimiter`, `Allow`, `AllowContext`, `Admit` (distinguishes store error from spent budget), `Middleware`, `MiddlewareByKey`, `ClientIP`, `ClientKey` (collapses IPv6 addresses to `/64` so one subscriber can't evade limits by rotating host addresses), and `IPKey`.

## Shared store for multiple replicas

Without `Config.Store`, limits live in process memory (per replica). Share one budget across replicas using `auth.NewSQLRateLimitStore` (or any custom `ratelimit.Store` backed by Redis/Postgres), namespaced by `Config.Scope`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "database/sql"
var db *sql.DB
stmt: _ = loginLimit
import "github.com/DonaldMurillo/gofastr/framework/ratelimit"
import "time"
stmt: _ = checkoutLimit
-->
```go
shared := auth.NewSQLRateLimitStore(db, "rate_limits")
loginLimit := ratelimit.NewLimiter(ratelimit.Config{
	MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute,
	Store: shared, Scope: "login_ip",
})
checkoutLimit := ratelimit.NewLimiter(ratelimit.Config{
	MaxAttempts: 20, Window: time.Minute, BlockDuration: 5 * time.Minute,
	Store: shared, Scope: "checkout",
})
```

Store errors **fail closed** (deny). Custom stores must record the attempt before counting (or use an atomic counter) so concurrent bursts cannot slip past `MaxAttempts`.

## Proxies and `X-Forwarded-For`

`X-Forwarded-For` is ignored unless `TrustForwardedFor: true` (enable only behind a reverse proxy you control). Reading goes **right-to-left**: with one proxy tier, `TrustForwardedFor: true` picks the rightmost entry; with multiple tiers (CDN → LB), list every proxy CIDR in `TrustedProxies`.

`Config.DevMode: true` bypasses checks for local tooling only — never set it in production.

## Common mistakes

- **Using the token bucket for brute-force protection:** continuous refill never imposes a hard lockout; use `framework/ratelimit`.
- **Enabling `TrustForwardedFor` without a stripping proxy:** lets attackers rotate fake `X-Forwarded-For` headers to bypass IP limits.
- **Expecting a fleet-wide cap without `Config.Store`:** in-memory limits multiply by replica count.
- **Keying directly on arbitrary unbounded user input:** prefer stable identities (IP `/64`, user ID, hashed API key).
