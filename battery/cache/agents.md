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
)
_ = c.Set(ctx, "42", userJSON)
val, err := c.Get(ctx, "42")

// HTTP middleware caches successful GETs by URL:
r.Use(cache.CacheMiddleware(c, 30*time.Second))
```

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
