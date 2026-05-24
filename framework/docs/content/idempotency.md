# Idempotency keys

`core/middleware/idempotency.go` adds opt-in `Idempotency-Key` support
to unsafe writes (POST / PUT / PATCH / DELETE). Clients that retry a
flaky write can carry the same key and get the **original** response
back instead of a duplicated side effect.

## Wiring

The simplest form is `framework.WithIdempotency` — the middleware
slots into the default chain between `Logging` and `SecurityHeaders`:

```go
app := framework.NewApp(framework.WithIdempotency(middleware.IdempotencyConfig{
    Principal: func(r *http.Request) string {
        // Extract the authenticated subject — user-id, tenant-id, or both.
        return auth.UserID(r.Context())
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
    Principal: func(r *http.Request) string { return auth.UserID(r.Context()) },
})))
```

`Required: true` makes the header mandatory on unsafe writes — useful
on payment / order endpoints.

### Configure `Principal` — it's the cross-tenant defense

`Idempotency-Key` is client-controlled and frequently collides
(`"1"`, `"retry-1"`). Without principal namespacing, two authenticated
users posting to `/orders` with the same key see each other's cached
response — including any session cookie or auth header the handler
set on the original request.

`Principal` returns the authenticated subject id from the request; the
middleware folds that id into both the fingerprint and the storage
key, so two principals using the same `Idempotency-Key` get two
separate caches. Wire it from your auth middleware:

```go
Principal: func(r *http.Request) string {
    if u := auth.UserID(r.Context()); u != "" {
        return u
    }
    return auth.TenantID(r.Context()) // fall back to tenant for service-to-service
},
```

When `Principal` is unset, the middleware still runs — but the cache
is shared globally across callers and you accept the cross-request
leak. Set it.

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
| Store backend failure (FailOpen=false, default)   | `503 Service Unavailable` — fail closed         |
| Store backend failure (FailOpen=true)             | Pass through — fail open (legacy availability) |

The cache is keyed by `(principal, Idempotency-Key)`. The
**fingerprint** that guards against accidental key reuse is
`sha256(principal ∥ method ∥ path ∥ raw query ∥ Content-Type ∥ body)`.
Other headers (auth tokens, request IDs) are intentionally excluded
— they aren't part of the client's intent.

Only `2xx` responses are cached. `4xx` / `5xx` release the claim so
the client can retry against a recovered server.

## Stores

`IdempotencyStore` is a two-method interface:

```go
type IdempotencyStore interface {
    Begin(ctx, key, fingerprint string) (*IdempotentResponse, bool, error)
    Finish(ctx, key string, resp *IdempotentResponse) error
}
```

Two stores are bundled:

- `NewMemoryIdempotencyStore(ttl)` — single-process map.
- `NewSQLIdempotencyStore(db, opts...)` — SQL-backed (sqlite + postgres),
  creates `idempotency_keys` on first use. Options:
  - `WithSQLIdempotencyTable(name)` — override the default table name.
  - `WithSQLIdempotencyTTL(d)` — override the 24h cached-response TTL.
  - `WithSQLIdempotencyInFlightTTL(d)` — override the 30s in-flight
    claim TTL. Set above your worst-case handler latency.
  - `WithSQLIdempotencyDialect("postgres"|"sqlite")` — pin the dialect
    instead of auto-detecting.

The SQL store uses `INSERT … ON CONFLICT DO NOTHING` to claim rows
atomically; concurrent writers race without one of them surfacing a
PK-violation that would otherwise look like a store failure and
either bypass the middleware (legacy) or block legitimate retries
behind a 503 (current default).

For clustered deployments without a database, drop a Redis adapter
behind the same interface — only `Begin` and `Finish` need
implementing.

`Begin` returns one of:

- `(replay, true, nil)` — replay cached response, skip handler.
- `(nil, false, nil)` — fresh claim; caller proceeds and must `Finish`.
- `(nil, false, ErrFingerprintMismatch)` — same key, different request.
- `(nil, false, ErrInFlight)` — concurrent claim still running.
- `(nil, false, otherErr)` — backend failure; middleware fails closed
  by default (503) and falls through to the handler only when
  `FailOpen: true` is set.

`Finish(ctx, key, nil)` releases the claim without caching — used on
non-success responses.

`Finish` is invoked with a fresh `context.WithTimeout(context.Background(),
5*time.Second)`, NOT the request context. A client disconnect mid-
handler therefore does not strand the claim as in-flight until the
next reap cycle.

## Common mistakes

- **Don't forget `Principal`.** Without it the cache is global and a
  client-chosen `Idempotency-Key` collides across users.
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
  never enter the cache at all — request-id, date, security headers,
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
