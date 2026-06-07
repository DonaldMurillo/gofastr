# battery/cache

Pluggable key/value cache plus a cache-aware HTTP middleware. Sits
behind a `Cache` interface so call sites stay backend-agnostic.

**Use this when** the prompt mentions: cache, memoization, "cache the
response", "stop hitting the DB every request", TTL, in-memory cache,
Redis, "remember this for N seconds".

**Import:** `github.com/DonaldMurillo/gofastr/battery/cache`

**Shape:**
```go
c := cache.NewMemoryCache(
    cache.WithTTL(5*time.Minute),
    cache.WithPrefix("user:"),
    cache.WithMaxEntries(10_000), // bound memory; LRU-evict past the cap
)
_ = c.Set(ctx, "42", userJSON)
val, err := c.Get(ctx, "42")

// HTTP middleware caches successful GETs by URL:
r.Use(cache.CacheMiddleware(c, 30*time.Second))

// Stampede-safe read-through: the loader runs exactly ONCE even when
// many goroutines miss "42" at the same time (singleflight-backed).
var u User
_ = cache.GetOrSet(ctx, c, "42", time.Minute, &u,
    func(ctx context.Context) (any, error) { return loadUser(ctx, 42) })
```

**Bounding memory (`WithMaxEntries`):** by default `MemoryCache` is
**unbounded** — fine for a fixed, trusted keyspace. When keys are
influenced by untrusted input (path, query, user id), set
`WithMaxEntries(n)` so the cache LRU-evicts once it reaches `n` live
entries instead of growing without bound (OOM/DoS). `RedisCache`
relies on the Redis server's own `maxmemory` policy and ignores this.

**Stampede protection (`GetOrSet`):** `GetOrSet(ctx, cache, key, ttl,
dest, loader)` collapses concurrent misses on the same key so the
expensive loader runs exactly once and all waiters share the result;
a loader error propagates and is never cached.

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `MemoryCache` instead:
- `type cache struct { mu sync.Mutex; m map[string]entry }` with a
  hand-rolled `Set/Get/cleanup` loop
- `time.AfterFunc` per entry to schedule eviction
- A `map[string]time.Time` next to the data map to track TTLs
- Anything with the words "simple in-memory cache for now" in a comment

`MemoryCache` ships with prefix namespacing, periodic cleanup, the
`Cache` interface, and a drop-in `RedisCache` swap when you outgrow
the in-process version.

**Swapping backends:** the `Cache` interface (`Get`/`Set`/`Delete`/
`Clear`/`Exists`) is the contract — `NewRedisCache(client, opts...)`
takes any `RedisClient`-shaped value, so the call site doesn't change
when you move from memory to Redis.
