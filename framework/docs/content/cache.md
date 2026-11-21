# Cache (`battery/cache`)

`battery/cache` provides a pluggable key/value cache (`MemoryCache` with LRU + TTL cleanup, or `RedisCache` over any `RedisClient`-shaped adapter), single-flight stampede protection (`GetOrSet`), and conservative HTTP response caching middleware.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/cache"

c := cache.NewMemoryCache(
	cache.WithTTL(5*time.Minute),
	cache.WithPrefix("user:"),
	cache.WithMaxEntries(10_000), // LRU eviction cap
)
app.Batteries.Register(cache.NewBattery(c)) // closes cleanup goroutine on Shutdown

_ = c.Set(ctx, "42", user, 0)
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

- Values encode as JSON; numbers ≥ 2^53 preserve exact precision as `json.Number` when decoded into untyped interfaces.
- `ttl == 0` uses the cache default; a negative `ttl` expires and deletes the key immediately on both `MemoryCache` and `RedisCache`.

## Backends

- **`MemoryCache`:** goroutine-safe in-process LRU cache. Always set `WithTTL` and/or `WithMaxEntries` (building one with neither logs a WARN because unbounded keys leak memory) and close it via `defer c.Close()` or `cache.NewBattery(c)`.
- **`RedisCache`:** wraps any 5-method `cache.RedisClient` (`Get`, `Set`, `Del`, `Exists`, `FlushDB`). Map driver miss errors to `redis: nil` or `cache.ErrCacheMiss` (other errors surface as backend failures). Implement optional `cache.KeyScanner` (`Keys(ctx, pattern)`) when calling `Clear` on a prefixed Redis cache so `Clear` never flushes unrelated keys.

## Stampede protection (`GetOrSet`)

```go
var u User
err := cache.GetOrSet(ctx, c, "user:42", time.Minute, &u, func(ctx context.Context) (any, error) {
	return db.FindUser(ctx, 42)
})
```

Collapses concurrent cache misses on the same key into a single loader call. Errors propagate without being cached, individual waiters respect their own context deadlines, and the shared loader cancels only when all waiters leave.

## HTTP caching middleware

```go
r.Use(cache.CacheMiddleware(c, 30*time.Second))
// Or with a custom max response size (default 8 MiB):
r.Use(cache.CacheMiddlewareWithLimit(c, 30*time.Second, 2<<20))
```

Conservative shared-cache rules:
- Caches `GET` responses only (bypassing `Range`, `X-Gofastr-Navigate`, SSE/WebSocket upgrades, and responses > `maxBodyBytes`).
- **Never caches authenticated traffic:** requests with `Authorization`, `Cookie`, `X-API-Key`, or `X-Gofastr-Embed`, and responses with `Set-Cookie`, `304`, or `Cache-Control: no-store`/`no-cache` bypass caching.
- Honors `s-maxage`/`max-age` caps, `Vary` headers (including `WithI18n` and `WithLocalePrefixes`), and evicts a URL's cached `GET` on unsafe requests (`POST`/`PUT`/`PATCH`/`DELETE`).
- Sets `X-Cache: HIT`, `MISS`, or `BYPASS`.

## Common mistakes

- **Forgetting `Close()` on `MemoryCache`:** leaks the cleanup goroutine. Register it with `cache.NewBattery(c)`.
- **Caching untrusted keys without `WithMaxEntries`:** enables unbounded memory growth (OOM/DoS).
- **Expecting `CacheMiddleware` to cache logged-in pages:** authenticated requests (`Cookie`, `Authorization`, `X-API-Key`, `X-Gofastr-Embed`) always bypass the shared cache by design.
- **Hand-rolling check-then-load under concurrency:** use `cache.GetOrSet` to prevent thundering herds.
