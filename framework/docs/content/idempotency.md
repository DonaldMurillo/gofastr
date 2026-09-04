# Idempotency keys

`core/middleware/idempotency.go` adds opt-in `Idempotency-Key` support
to unsafe writes (POST / PUT / PATCH / DELETE). Clients that retry a
flaky write can carry the same key and get the **original** response
back instead of a duplicated side effect.

## Wiring

The simplest form is `framework.WithIdempotency`. The middleware
slots into the default chain between `RequestID` and
`SecurityHeaders`:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/core/middleware"
import "net/http"
import "github.com/DonaldMurillo/gofastr/battery/auth"
-->
```go
app := framework.NewApp(framework.WithIdempotency(middleware.IdempotencyConfig{
    Principal: func(r *http.Request) string {
        // Extract the authenticated subject: user-id, tenant-id, or both.
        if u, ok := auth.SessionFrom(r.Context()); ok {
            return u.GetID()
        }
        return ""
    },
}))
```

For full control, mount it manually:

```go
import "github.com/DonaldMurillo/gofastr/core/middleware"

app.Use(router.Middleware(middleware.Idempotency(middleware.IdempotencyConfig{
    // All fields optional; defaults shown except Principal (set it!).
    // Store:            middleware.NewMemoryIdempotencyStore(24 * time.Hour),
    // TTL:              24 * time.Hour,
    // MaxBodyBytes:     1 << 20,
    // MaxResponseBytes: 1 << 20,
    // Methods:          []string{POST, PUT, PATCH, DELETE},
    // Required:         false,
    // FailOpen:         false, // default: fail closed (503) on store error
    Principal: func(r *http.Request) string {
        if u, ok := auth.SessionFrom(r.Context()); ok {
            return u.GetID()
        }
        return ""
    },
})))
```

`Required: true` makes the header mandatory on unsafe writes, useful
on payment / order endpoints.

### Configure `Principal`, the cross-tenant defense

`Idempotency-Key` is client-controlled and frequently collides
(`"1"`, `"retry-1"`). Without principal namespacing, two authenticated
users posting to `/orders` with the same key see each other's cached
response, including any session cookie or auth header the handler
set on the original request.

`Principal` returns the authenticated subject id from the request; the
middleware folds that id into both the fingerprint and the storage
key, so two principals using the same `Idempotency-Key` get two
separate caches. Wire it from your auth middleware:

```go
Principal: func(r *http.Request) string {
    // handler.GetUser returns the value your auth middleware stored,
    // as `any`. Assert it to whatever your app puts there.
    if u, ok := handler.GetUser(r.Context()); ok {
        if user, ok := u.(auth.User); ok && user.GetID() != "" {
            return user.GetID()
        }
    }
    // Fall back to the tenant for service-to-service calls.
    return framework.GetTenantID(r.Context())
},
```

`handler` is `core/handler` and `framework.GetTenantID` re-exports
`tenant.GetTenantID`. There is no exported "current user id" accessor.
The user value is whatever your middleware stored, so the assertion is
yours to write.

When `Principal` is unset, the middleware disables replay caching
entirely: it degrades to a pass-through that logs a warning and never
caches, so there is no shared namespace and no cross-request leak. The
cost is that you lose duplicate-suppression until you wire one. Set it
to enable caching.

### Headers stripped from replays

Even with principal namespacing, certain headers should never be
cached. The middleware strips these from the recorded response so a
replay cannot leak credential material:

- `Set-Cookie`
- `Cookie`
- `Authorization`
- `Proxy-Authorization`
- `WWW-Authenticate`

If your handler sets per-identity headers other than these (a custom
`X-Account-Token`, say), set a different header name or strip it
yourself before returning.

## Request / response semantics

| Situation                                         | Response                                      |
|---------------------------------------------------|-----------------------------------------------|
| GET / HEAD / OPTIONS                              | Pass through, no caching                       |
| Unsafe method without header (Required=false)     | Pass through, no caching                       |
| Unsafe method without header (Required=true)      | `400 Bad Request`                              |
| Header > 255 chars                                | `400 Bad Request`                              |
| First request for a key                           | Handler runs; 2xx response is cached           |
| Duplicate key + same body (cached)                | Cached response replayed, `Idempotent-Replay: true` |
| Duplicate key + different body                    | `422 Unprocessable Entity`                     |
| Duplicate key while first is still running        | `409 Conflict` + `Retry-After: 1`              |
| First request returned non-2xx                    | Claim released; retry runs the handler again   |
| Body larger than `MaxBodyBytes`                   | Pass through with `Idempotent-Bypass: body-too-large`; handler still sees full body |
| Store backend failure (FailOpen=false, default)   | `503 Service Unavailable`, fail closed         |
| Store backend failure (FailOpen=true)             | Pass through, fail open (legacy availability) |
| Cached row is corrupt (unparseable stored headers)| `503 Service Unavailable`, fail closed — a corrupt cached response is never replayed with silently-dropped headers |

The cache is keyed by `(principal, Idempotency-Key)`. The
**fingerprint** that guards against accidental key reuse is
`sha256(principal ∥ method ∥ path ∥ raw query ∥ Content-Type ∥ body)`.
Other headers (auth tokens, request IDs) are intentionally excluded.
They aren't part of the client's intent.

Only `2xx` responses are cached. `4xx` / `5xx` release the claim so
the client can retry against a recovered server.

## Stores

`IdempotencyStore` is a two-method interface:

```go
type IdempotencyStore interface {
    Begin(ctx, key, fingerprint string) (*IdempotentResponse, bool, error)
    Finish(ctx, key, fingerprint string, resp *IdempotentResponse) error
}
```

`Finish` is fingerprint-bound: it must only persist into (or release) the row
that `Begin` created under that exact fingerprint. An in-flight claim can expire
while its handler is still running and be re-claimed by a *different* caller
under a *different* fingerprint; if `Finish` wrote by key alone, the first
caller's late `Finish` would staple its response onto the second caller's row,
and because `Begin` never rewrites the fingerprint column, the second caller's
retry would match the fingerprint and be served the **first caller's body**.
That is a cross-user disclosure whenever `Principal` returns a user/tenant id,
so the fingerprint is a required parameter, not an optional one: a third-party
store that ignores it is silently vulnerable. The bundled memory and SQL stores
both scope their `UPDATE`/`DELETE` by `key AND fingerprint`.

Two stores are bundled:

- `NewMemoryIdempotencyStore(ttl)`: single-process map.
- `NewSQLIdempotencyStore(db, opts...)`: SQL-backed (sqlite + postgres),
  creates `idempotency_keys` on first use. Options:
  - `WithSQLIdempotencyTable(name)`: override the default table name.
  - `WithSQLIdempotencyTTL(d)`: override the 24h cached-response TTL.
  - `WithSQLIdempotencyInFlightTTL(d)`: override the 30s in-flight
    claim TTL. Set above your worst-case handler latency.
  - `WithSQLIdempotencyDialect("postgres"|"sqlite")`: pin the dialect
    instead of auto-detecting.

The SQL store uses `INSERT … ON CONFLICT DO NOTHING` to claim rows
atomically; concurrent writers race without one of them surfacing a
PK-violation that would otherwise look like a store failure and
either bypass the middleware (legacy) or block legitimate retries
behind a 503 (current default).

For clustered deployments without a database, drop a Redis adapter
behind the same interface. Only `Begin` and `Finish` need
implementing. `Finish` receives the fingerprint the claim was created
with; a custom store MUST scope its write and its release to the row
matching that fingerprint (see the note above), or a late `Finish` from
an expired claim can overwrite another principal's row.

`Begin` returns one of:

- `(replay, true, nil)`: replay cached response, skip handler.
- `(nil, false, nil)`: fresh claim; caller proceeds and must `Finish`.
- `(nil, false, ErrFingerprintMismatch)`: same key, different request.
- `(nil, false, ErrInFlight)`: concurrent claim still running.
- `(nil, false, otherErr)`: backend failure; middleware fails closed
  by default (503) and falls through to the handler only when
  `FailOpen: true` is set.

`Finish(ctx, key, fingerprint, nil)` releases the claim without caching,
used on non-success responses.

`Finish` is invoked with a fresh `context.WithTimeout(context.Background(),
5*time.Second)`, NOT the request context. A client disconnect mid-
handler therefore does not strand the claim as in-flight until the
next reap cycle.

## Common mistakes

- **Don't forget `Principal`.** Without it the middleware disables
  replay caching entirely (a pass-through that logs a warning) rather
  than cache into a namespace shared across callers. You silently
  lose duplicate-suppression, but you don't leak cross-request.
- **Don't ignore the fingerprint in a custom `Finish`.** `Finish` is
  fingerprint-bound for a reason: a claim can expire mid-handler and be
  re-claimed by another principal, and a `Finish` that writes by key alone
  then overwrites that principal's row and serves the first caller's body
  on retry. Scope the write (`UPDATE … WHERE key AND fingerprint`) and the
  release (`DELETE … WHERE key AND fingerprint`), as the bundled stores do.
- **Don't put state-mutating side effects in middleware that runs
  before `Idempotency`.** The cached response only covers downstream
  handlers; anything that happened earlier in the chain runs every
  time the client retries.
- **Don't enable `Required: true` on read-only routes** by mounting
  the middleware at the root. Mount it on the unsafe sub-router, or
  scope `Methods` if your router doesn't allow per-route mounting.
- **Don't rely on the memory store across instances.** It's a
  single-process map. Bound replays will diverge between replicas.
- **Don't cache streaming responses.** The recorder buffers the body
  in memory up to `MaxResponseBytes` (default 1 MiB); anything over
  that size streams through unchanged and is not cached. The client's
  first response is still correct, but a retry will re-run the
  handler.
- **Don't expect cached headers to include request-scoped values.**
  Only headers the handler itself writes are cached, and the
  always-stripped identity headers (Set-Cookie, Authorization, …)
  never enter the cache at all. Request-id, date, security headers,
  and other middleware-set values come from the *replay* request's
  chain, not the original.
- **Handlers slower than 30 seconds may race with retries.** The
  in-flight TTL defaults to 30 seconds; raise it via
  `WithSQLIdempotencyInFlightTTL` if your real-world handler latency
  exceeds that, or tighten your handler timeout instead.
- **Don't switch to `FailOpen: true` without a plan.** Trading a 503
  for "let the write through unguarded" gives back the duplicate-side-
  effect protection idempotency exists to provide. Use it only when
  availability beats correctness for the specific route.
