package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RedisClient defines the minimal interface needed for a Redis cache backend.
// No specific Redis library is imported; implement this interface with your
// preferred Redis client (e.g. go-redis, redigo, etc.).
type RedisClient interface {
	// Get retrieves the string value for a key. Should return a redis nil
	// error when the key does not exist.
	Get(ctx context.Context, key string) (string, error)

	// Set stores a string value with an optional expiration. A TTL of 0
	// means no expiration.
	Set(ctx context.Context, key string, value string, ttl time.Duration) error

	// Del removes one or more keys.
	Del(ctx context.Context, keys ...string) error

	// Exists checks whether a key exists.
	Exists(ctx context.Context, key string) (bool, error)

	// FlushDB removes all keys from the current database.
	FlushDB(ctx context.Context) error
}

// RedisCache implements the Cache interface backed by a Redis store.
type RedisCache struct {
	client RedisClient
	cfg    config
}

// NewRedisCache creates a new Redis-backed cache.
func NewRedisCache(client RedisClient, opts ...Option) *RedisCache {
	cfg := applyOptions(opts...)
	return &RedisCache{
		client: client,
		cfg:    cfg,
	}
}

// Get retrieves a value from Redis and deserializes it into dest.
func (rc *RedisCache) Get(ctx context.Context, key string, dest any) error {
	k := rc.prefixedKey(key)
	val, err := rc.client.Get(ctx, k)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCacheMiss, err)
	}
	return json.Unmarshal([]byte(val), dest)
}

// Set stores a value in Redis with the given TTL.
func (rc *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	k := rc.prefixedKey(key)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	effectiveTTL := ttl
	if effectiveTTL == 0 {
		effectiveTTL = rc.cfg.defaultTTL
	}

	return rc.client.Set(ctx, k, string(data), effectiveTTL)
}

// Delete removes a key from Redis.
func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	k := rc.prefixedKey(key)
	return rc.client.Del(ctx, k)
}

// Exists checks whether a key exists in Redis.
func (rc *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	k := rc.prefixedKey(key)
	return rc.client.Exists(ctx, k)
}

// Clear removes all keys from the current Redis database.
func (rc *RedisCache) Clear(ctx context.Context) error {
	return rc.client.FlushDB(ctx)
}

func (rc *RedisCache) prefixedKey(key string) string {
	if rc.cfg.prefix == "" {
		return key
	}
	return rc.cfg.prefix + ":" + key
}
