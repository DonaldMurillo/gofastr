package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// RedisClient defines the minimal interface needed for a Redis cache
// backend.
type RedisClient interface {
	// Get retrieves the string value for a key.
	Get(ctx context.Context, key string) (string, error)

	// Set stores a string value with an optional expiration.
	Set(ctx context.Context, key string, value string, ttl time.Duration) error

	// Del removes one or more keys.
	Del(ctx context.Context, keys ...string) error

	// Exists checks whether a key exists.
	Exists(ctx context.Context, key string) (bool, error)

	// FlushDB removes all keys from the current database.
	FlushDB(ctx context.Context) error
}

// KeyScanner is the optional capability a RedisClient implements when it
// can enumerate keys (Redis SCAN, or KEYS on a small database).
type KeyScanner interface {
	Keys(ctx context.Context, pattern string) ([]string, error)
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
		if isRedisMiss(err) {
			return fmt.Errorf("%w: %s", ErrCacheMiss, key)
		}
		return fmt.Errorf("cache: redis get %q: %w", key, err)
	}
	return jsonexact.Unmarshal([]byte(val), dest)
}

func isRedisMiss(err error) bool {
	if errors.Is(err, ErrCacheMiss) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "redis: nil") || msg == "nil"
}

// Set stores a value in Redis with the given TTL.
func (rc *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	k := rc.prefixedKey(key)
	effectiveTTL := ttl
	if effectiveTTL == 0 {
		effectiveTTL = rc.cfg.defaultTTL
	}

	if effectiveTTL < 0 {
		return rc.client.Del(ctx, k)
	}

	data, err := json.Marshal(value)
	if err != nil {
		return err
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

// Clear removes the entries this cache owns.
func (rc *RedisCache) Clear(ctx context.Context) error {
	if rc.cfg.prefix == "" {
		return rc.client.FlushDB(ctx)
	}
	scanner, ok := rc.client.(KeyScanner)
	if !ok {
		return fmt.Errorf("cache: Clear on the %q namespace needs a RedisClient implementing KeyScanner; "+
			"flushing the database instead would delete every other namespace's keys", rc.cfg.prefix)
	}
	want := rc.keyPrefix()
	keys, err := scanner.Keys(ctx, escapeGlob(want)+"*")
	if err != nil {
		return fmt.Errorf("cache: Clear scan %q: %w", want, err)
	}
	owned := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.HasPrefix(k, want) {
			owned = append(owned, k)
		}
	}
	if len(owned) == 0 {
		return nil
	}
	return rc.client.Del(ctx, owned...)
}

func escapeGlob(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '*', '?', '[', ']':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (rc *RedisCache) keyPrefix() string {
	if rc.cfg.prefix == "" {
		return ""
	}
	return escapeKeySegment(rc.cfg.prefix) + ":"
}

func (rc *RedisCache) prefixedKey(key string) string {
	return rc.keyPrefix() + key
}

func escapeKeySegment(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	return strings.ReplaceAll(s, ":", "%3A")
}
