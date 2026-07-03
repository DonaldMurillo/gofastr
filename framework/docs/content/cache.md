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

## `MemoryCache`

`NewMemoryCache(opts...)` returns a goroutine-safe in-process store.

| Option | Default | Notes |
|---|---|---|
| `WithTTL(d)` | no expiry | Per-entry default; overridden by `Set`'s `ttl` arg. |
| `WithPrefix(p)` | none | Prepended to every key as `p:key`. |
| `WithCleanupInterval(d)` | 1 minute | How often the background goroutine evicts expired entries. |
| `WithMaxEntries(n)` | unbounded | LRU-evict least-recently-used entries past the cap. |

The background cleanup goroutine runs until `MemoryCache.Close()` is
called.  **Always call `Close()`** — or use the Battery wrapper
(below) so the framework calls it during graceful shutdown.

### Memory bounds

`WithMaxEntries(n)` caps live entries at `n`.  When the cap is reached,
the least-recently-used entry is evicted on the next `Set`.  Use this
whenever cache keys are influenced by untrusted input (path, query,
user id) to prevent unbounded memory growth (OOM/DoS).

A `MemoryCache` built with **neither** `WithTTL` **nor** `WithMaxEntries`
retains every distinct key forever — the OOM shape. `NewMemoryCache`
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

`RedisCache` has no background goroutine and no `Close` — expiry is
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
propagates and is never cached.

## HTTP caching middleware

```go
r.Use(cache.CacheMiddleware(c, 30*time.Second))
// or with an explicit response-size cap:
r.Use(cache.CacheMiddlewareWithLimit(c, 30*time.Second, 2<<20)) // 2 MiB
```

The middleware is RFC 9111-compliant by default:

- Only `GET` requests are cached.
- Requests with `Authorization` or `Cookie` headers bypass the cache.
- `Cache-Control: no-cache` / `no-store` directives are honoured.
- `Set-Cookie` responses and non-2xx/3xx responses are never stored.
- `Vary` headers are respected; `Vary: *` disables caching.
- Responses larger than `maxBodyBytes` (default 8 MiB) are streamed
  without caching.
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
  goroutine runs until `Close()` is called — every cache you construct
  and drop leaks one. Register it via `cache.NewBattery(c)` so graceful
  shutdown closes it for you.
- **Caching untrusted-input keys without `WithMaxEntries`.** Keys
  derived from paths, query strings, or user ids let a client grow the
  cache without bound (OOM/DoS). Set the cap; LRU eviction handles the
  rest.
- **Expecting `CacheMiddleware` to cache authenticated traffic.**
  Requests carrying `Authorization` or `Cookie` headers bypass the
  cache by design (RFC 9111), and `Set-Cookie` responses are never
  stored. If every response says `X-Cache: MISS`, check the request
  headers before suspecting the middleware.
- **Hand-rolling get-miss-then-set under concurrency.** N concurrent
  misses run the loader N times (thundering herd). `cache.GetOrSet`
  collapses concurrent misses per key via singleflight so the loader
  runs exactly once — and a loader error propagates without being
  cached.
