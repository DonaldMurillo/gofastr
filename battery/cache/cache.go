package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"
)

// ErrCacheMiss is returned when a key is not found in the cache.
var ErrCacheMiss = errors.New("cache: key not found")

// Cache defines the interface for cache implementations.
type Cache interface {
	// Get retrieves a value from the cache and deserializes it into dest.
	// Returns ErrCacheMiss if the key does not exist or has expired.
	Get(ctx context.Context, key string, dest any) error

	// Set stores a value in the cache with the given TTL.
	// A TTL of 0 means the entry uses the default TTL.
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// Exists checks whether a key exists in the cache and has not expired.
	Exists(ctx context.Context, key string) (bool, error)

	// Clear removes all entries from the cache.
	Clear(ctx context.Context) error
}

// Item represents a cached entry.
type Item struct {
	Key       string
	Value     any
	ExpiresAt time.Time
}

// Loader produces a value to populate the cache on a miss.
type Loader func(ctx context.Context) (any, error)

// getOrSetGroup collapses concurrent GetOrSet misses for the same
// (cache instance, key) so the loader runs exactly once under contention
// (cache-stampede / thundering-herd protection).
var getOrSetGroup singleflight.Group

// GetOrSet returns the cached value for key, deserialized into dest. On a miss
// it invokes loader exactly once — even when many goroutines miss the same key
// concurrently — stores the result with the given TTL, and shares it with all
// waiters. A loader error is propagated and never cached.
//
// dest must be a non-nil pointer. The loader's returned value is round-tripped
// through the cache (JSON) so the value written into dest is the cached form.
func GetOrSet(ctx context.Context, c Cache, key string, ttl time.Duration, dest any, loader Loader) error {
	// Fast path: already cached.
	if err := c.Get(ctx, key, dest); err == nil {
		return nil
	}

	// Collapse concurrent misses per cache instance + key.
	flightKey := fmt.Sprintf("%p:%s", c, key)
	_, err, _ := getOrSetGroup.Do(flightKey, func() (any, error) {
		// Re-check: another waiter may have populated the cache while we
		// queued for the singleflight slot.
		if err := c.Get(ctx, key, dest); err == nil {
			return nil, nil
		}
		val, lerr := loader(ctx)
		if lerr != nil {
			return nil, lerr
		}
		if serr := c.Set(ctx, key, val, ttl); serr != nil {
			return nil, serr
		}
		return nil, nil
	})
	if err != nil {
		return err
	}

	// Read back so every waiter (including the leader) fills dest from the
	// canonical cached representation.
	return c.Get(ctx, key, dest)
}

// config holds cache configuration set via options.
type config struct {
	defaultTTL      time.Duration
	prefix          string
	cleanupInterval time.Duration
	maxEntries      int // 0 means unbounded
}

// Option configures a cache instance.
type Option func(*config)

// WithTTL sets the default TTL for cache entries.
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		c.defaultTTL = d
	}
}

// WithPrefix sets a key prefix for all cache operations.
func WithPrefix(p string) Option {
	return func(c *config) {
		c.prefix = p
	}
}

// WithCleanupInterval sets the interval for the background cleanup goroutine
// in MemoryCache. Defaults to 1 minute if not set.
func WithCleanupInterval(d time.Duration) Option {
	return func(c *config) {
		c.cleanupInterval = d
	}
}

// WithMaxEntries bounds a MemoryCache to at most n live entries. Once the cap
// is reached, inserting a new key evicts the least-recently-used entry (LRU).
//
// The zero-config default is UNBOUNDED (n <= 0), preserving historical
// behavior. Set this whenever cache keys are influenced by untrusted input to
// avoid unbounded memory growth (OOM/DoS). It has no effect on RedisCache,
// where eviction is governed by the Redis server's own maxmemory policy.
func WithMaxEntries(n int) Option {
	return func(c *config) {
		if n < 0 {
			n = 0
		}
		c.maxEntries = n
	}
}

func applyOptions(opts ...Option) config {
	cfg := config{
		defaultTTL:      0, // no expiry by default
		cleanupInterval: time.Minute,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}
