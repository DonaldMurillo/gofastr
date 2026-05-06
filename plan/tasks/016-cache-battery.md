# 016 — Cache Battery

**Phase:** 2 (Batteries) | **Depends on:** 004

## Goal
Pluggable cache. Interface in core, in-memory implementation in battery.

## Deliverables
- [ ] `Cache` interface: `Get(ctx, key) (any, bool)`, `Set(ctx, key, value, ttl)`, `Delete(ctx, key)`, `Clear(ctx)`, `Has(ctx, key) bool`
- [ ] `MemoryCache` implementation: sync.RWMutex + map with TTL expiration
- [ ] TTL: background goroutine cleans expired entries
- [ ] Key helpers: `EntityKey(entity, id)`, `QueryKey(entity, hash)` — consistent key format
- [ ] Cache middleware: HTTP response caching with configurable TTL
- [ ] Invalidation helper: invalidate on entity update/delete

## Acceptance Criteria
- Set + Get returns same value within TTL
- Expired entries return not-found
- Clear removes all entries
- Concurrent access safe (race test)
