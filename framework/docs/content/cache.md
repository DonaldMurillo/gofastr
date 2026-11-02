# Cache

The `battery/cache` package provides a pluggable key/value cache behind
a `Cache` interface. Two backends ship: `MemoryCache` (in-process, LRU,
TTL-based cleanup) and `RedisCache` (any `RedisClient`-shaped value).
An HTTP middleware ships alongside the backends to cache GET responses
by URL.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/cache"

c := cache.NewMemoryCache(
    cache.WithTTL(5 * time.Minute),
    cache.WithPrefix("user:"),
    cache.WithMaxEntries(10_000), // LRU-evict past the cap
)
defer c.Close() // stops the background cleanup goroutine

_ = c.Set(ctx, "42", userJSON, 0)
var u User
_ = c.Get(ctx, "42", &u)
```

## The `Cache` interface

<!-- gofastr:compile
import "context"
import "time"
-->
```go
type Cache interface {
    Get(ctx context.Context, key string, dest any) error
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error
}
```

All call sites depend only on this interface. Swap `MemoryCache` for
`RedisCache` (or a future backend) without touching business logic.

`Set`'s `ttl` of `0` uses the configured default. A negative `ttl` is
honoured as "already expired": nothing is stored and any existing
entry under the key is dropped, on both `MemoryCache` and `RedisCache`
(which deletes the key and never hands the negative expiry to the
client: go-redis reads one as "no expiry") — a mis-computed lifetime
can never widen into retention. A negative `WithTTL` default behaves
the same. Via `GetOrSet`
a negative `ttl` still runs the loader, stores nothing, and surfaces
the miss.

## `MemoryCache`

`NewMemoryCache(opts...)` returns a goroutine-safe in-process store.

| Option | Default | Notes |
|---|---|---|
| `WithTTL(d)` | no expiry | Per-entry default; overridden by `Set`'s `ttl` arg. |
| `WithPrefix(p)` | none | Prepended to every key as `p:key`. |
| `WithCleanupInterval(d)` | 1 minute | How often the background goroutine evicts expired entries. |
| `WithMaxEntries(n)` | unbounded | LRU-evict least-recently-used entries past the cap. |

The background cleanup goroutine runs until `MemoryCache.Close()` is
called.  **Always call `Close()`**, or use the Battery wrapper
(below) so the framework calls it during graceful shutdown.

### Memory bounds

`WithMaxEntries(n)` caps live entries at `n`.  When the cap is reached,
the least-recently-used entry is evicted on the next `Set`.  Use this
whenever cache keys are influenced by untrusted input (path, query,
user id) to prevent unbounded memory growth (OOM/DoS).

A `MemoryCache` built with **neither** `WithTTL` **nor** `WithMaxEntries`
retains every distinct key forever, the OOM shape. `NewMemoryCache`
logs a WARN in that configuration; set a TTL, a size cap, or both to
silence it.

## `RedisCache`

```go
c := cache.NewRedisCache(redisClient,
    cache.WithTTL(10 * time.Minute),
    cache.WithPrefix("app:"),
)
```

`redisClient` must satisfy the `cache.RedisClient` interface (five
methods: `Get`, `Set`, `Del`, `Exists`, `FlushDB`). Any go-redis,
redigo, or mock client that implements those five methods works without
importing a specific library.

**`Get` must report a missing key with the driver's nil signal** (the
message go-redis spells `redis: nil`), or by wrapping
`cache.ErrCacheMiss`. Any other error is treated as a backend failure and
returned as itself, so a caller that fails closed on a miss — negative
caching, a revocation list — does not fail open during an outage.

### Optional: `KeyScanner`

An adapter may also implement:

```go
type KeyScanner interface {
    Keys(ctx context.Context, pattern string) ([]string, error)
}
```

`Clear` needs it **when the cache carries a prefix**. Clear removes the
entries this cache owns; without a way to enumerate them it can only
`FlushDB`, which on a shared database destroys every other namespace's
keys and any foreign keys too. A prefixed `Clear` therefore refuses when
the client cannot scan, rather than guessing. An unprefixed cache owns
the database by definition and still uses `FlushDB`.

```go
func (c adapter) Keys(ctx context.Context, pattern string) ([]string, error) {
    var out []string
    iter := c.rdb.Scan(ctx, 0, pattern, 500).Iterator()
    for iter.Next(ctx) {
        out = append(out, iter.Val())
    }
    return out, iter.Err()
}
```

### Prefixes and key shape

`WithPrefix("p")` stores under `p:<key>`. A `%` or `:` **inside** the
prefix is percent-encoded first (`%25`, `%3A`), so `WithPrefix("u:alice")`
stores under `u%3Aalice:<key>` and the first `:` is always the join.
Without that, `WithPrefix("u")` with key `:alice:x` and
`WithPrefix("u:alice")` with key `x` named the same Redis key, and one
namespace could read, overwrite, delete or `Clear` another's entries. A
prefix containing no `:` or `%` is unaffected.

`RedisCache` has no background goroutine and no `Close`. Expiry is
handled by the Redis server's own `maxmemory` policy.

## Stampede protection (`GetOrSet`)

```go
var u User
err := cache.GetOrSet(ctx, c, "user:42", time.Minute, &u,
    func(ctx context.Context) (any, error) {
        return db.FindUser(ctx, 42)
    })
```

`GetOrSet` collapses concurrent misses on the same key so the loader
runs exactly once and all waiters share the result.  A loader error
propagates and is never cached. The shared loader keeps the first
caller's context values (trace, auth) but not its cancellation, so one
caller cancelling their request does not fail the loader for every
waiter joined behind it. Each caller still leaves at its own deadline
with `ctx.Err()`; the loader's context is cancelled when the last
waiter leaves, and a fill nobody waits for any more stores nothing, so
it cannot overwrite a value a later caller filled. A cache with value
receivers has no identity to share a fill by, so each caller fills on
its own, and still leaves at its deadline.

## HTTP caching middleware

```go
r.Use(cache.CacheMiddleware(c, 30*time.Second))
// or with an explicit response-size cap:
r.Use(cache.CacheMiddlewareWithLimit(c, 30*time.Second, 2<<20)) // 2 MiB
```

The middleware is RFC 9111-compliant by default:

- Only `GET` requests are cached.
- An unsafe request (`POST`, `PUT`, `PATCH`, `DELETE`, any method but
  `GET`, `HEAD`, `OPTIONS` and `TRACE`) evicts the stored `GET` of its
  own URL, path and query, on every locale under `WithLocalePrefixes`,
  after its handler runs (RFC 9111 §4.4). Only that URL: a `DELETE
  /items/1` leaves a stored `/items` list in place until its TTL. The
  eviction runs whatever the status, so a refused request costs at
  most one origin fetch. A `GET` that started before the change
  committed can still store the old body, which then lasts its TTL.
- Requests with `Authorization`, `Cookie`, `X-API-Key`, or `X-Gofastr-Embed`
  headers bypass the cache, the same credential headers the framework uses to
  tell callers apart. `X-Gofastr-Embed` is an embeddable surface's grant: it
  authenticates a request exactly as the others do, and it deliberately
  travels without a cookie or bearer token, so a credential check that looked
  only for those saw a per-subject response as anonymous and served it to the
  next grant holder. An API key had the same hole.
- `Cache-Control: no-cache` / `no-store` directives are honoured.
- `Set-Cookie` responses and non-2xx/3xx responses are never stored,
  and neither are `304` responses: a 304 is a conditional-reply
  artifact, not a representation, and the key encodes no conditional
  headers — a stored 304 would be replayed to every unconditional GET
  as an empty-body `HIT` (RFC 9111 §4.3.1).
- `Vary` headers are respected, including ones middleware outside the
  cache set (`WithI18n` adds `Vary: Accept-Language, X-Locale`, so each
  language gets its own entry); `Vary: *` disables caching.
- Under `WithLocalePrefixes` the router strips `/es` before middleware
  runs, so the key carries the URL's locale: `/es/pricing` and
  `/pricing` are separate entries.
- Responses larger than `maxBodyBytes` (default 8 MiB) are streamed
  without caching.
- A handler that flushes (an SSE stream) or hijacks the connection (a
  websocket upgrade) gets the client writer from then on, and its
  response is not stored, so the middleware can sit in front of
  `/__gofastr/sse` and upgrade routes.
- The `X-Cache: HIT` / `MISS` header is set on every response.

## Battery wrapper (lifecycle)

`cache.NewBattery(c)` wraps any `Cache` in a `framework.Battery` +
`framework.BatteryLifecycle` adapter so the cleanup goroutine is
stopped cleanly during graceful shutdown.

```go
c := cache.NewMemoryCache(cache.WithTTL(5 * time.Minute))
app.Batteries.Register(cache.NewBattery(c))

// Retrieve from within another battery or route:
b, _ := app.Batteries.Get("cache")
cb := b.(*cache.Battery)
_ = cb.Cache().Get(ctx, "key", &dest)
```

`Battery.OnStop` calls `Close()` on any cache that implements
`io.Closer` (i.e., `MemoryCache`).  `RedisCache` does not implement
`io.Closer`, so `OnStop` is a no-op for Redis.

Other batteries can declare `"cache"` as a dependency at registration:

```go
app.Batteries.Register(myBattery, "cache") // cache is initialised first
```

## Agent inventory

Importing `battery/cache` registers a snippet in `agentsinv`.  The
entry appears in the generated `AGENTS.md` output from
`gofastr agents sync` so AI agents know when and how to use the cache.

## Common mistakes

- **Forgetting `Close()` on a `MemoryCache`.** The background cleanup
  goroutine runs until `Close()` is called. Every cache you construct
  and drop leaks one. Register it via `cache.NewBattery(c)` so graceful
  shutdown closes it for you.
- **Caching untrusted-input keys without `WithMaxEntries`.** Keys
  derived from paths, query strings, or user ids let a client grow the
  cache without bound (OOM/DoS). Set the cap; LRU eviction handles the
  rest.
- **Expecting `CacheMiddleware` to cache authenticated traffic.**
  Requests carrying `Authorization`, `Cookie`, `X-API-Key` or
  `X-Gofastr-Embed` headers bypass the cache by design (RFC 9111), and `Set-Cookie` responses are never
  stored. If every response says `X-Cache: MISS`, check the request
  headers before suspecting the middleware.
- **Hand-rolling get-miss-then-set under concurrency.** N concurrent
  misses run the loader N times (thundering herd). `cache.GetOrSet`
  collapses concurrent misses per key into one shared fill so the loader
  runs exactly once, and a loader error propagates without being
  cached. Each caller waits under its own context: one whose deadline
  passes returns `ctx.Err()` while the fill runs on for the others, and
  the loader's context is cancelled once no caller is left waiting.
