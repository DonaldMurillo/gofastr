package cache

import (
	"context"
	"errors"
	"time"
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

// config holds cache configuration set via options.
type config struct {
	defaultTTL      time.Duration
	prefix          string
	cleanupInterval time.Duration
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
