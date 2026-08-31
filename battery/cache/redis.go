package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// KeyScanner is the optional capability a RedisClient implements when it
// can enumerate keys (Redis SCAN, or KEYS on a small database).
//
// RedisCache.Clear needs it. Clear's contract is "removes all entries
// from the cache" — the entries THIS instance owns — and every other
// operation routes through prefixedKey, but Clear issued FlushDB and
// wiped the whole database. With the per-tenant prefixes this battery
// documents, one tenant's Clear was every other tenant's data-loss
// event, plus any foreign keys sharing the database.
//
// With go-redis:
//
//	func (c adapter) Keys(ctx context.Context, pattern string) ([]string, error) {
//	  var out []string
//	  iter := c.rdb.Scan(ctx, 0, pattern, 500).Iterator()
//	  for iter.Next(ctx) { out = append(out, iter.Val()) }
//	  return out, iter.Err()
//	}
//
// A client that does not implement it can still be used: Clear then
// FlushDBs an UNPREFIXED cache (which owns the database by definition)
// and refuses on a prefixed one rather than destroying a neighbour's
// keys.
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
//
// A backend failure is NOT reported as ErrCacheMiss. The Cache interface
// documents that sentinel strictly as "the key does not exist or has
// expired", and wrapping every client error in it made a connection
// refusal, a timeout, and an auth rejection indistinguishable from an
// absent key — so a caller that fails closed on a miss (negative
// caching, a revocation list) failed OPEN for the duration of the
// outage. Only the client's documented nil-signal maps to a miss;
// anything else is returned as itself.
func (rc *RedisCache) Get(ctx context.Context, key string, dest any) error {
	k := rc.prefixedKey(key)
	val, err := rc.client.Get(ctx, k)
	if err != nil {
		if isRedisMiss(err) {
			return fmt.Errorf("%w: %s", ErrCacheMiss, key)
		}
		return fmt.Errorf("cache: redis get %q: %w", key, err)
	}
	return json.Unmarshal([]byte(val), dest)
}

// isRedisMiss reports whether err is the client's "no such key" signal
// rather than a backend failure.
//
// The RedisClient contract asks adapters to "return a redis nil error
// when the key does not exist", and every driver spells that its own
// way (go-redis returns redis.Nil, whose message is "redis: nil"). An
// adapter that wraps [ErrCacheMiss] is honoured directly; otherwise the
// documented message is matched. Anything unrecognised counts as a
// FAILURE, which is the safe direction: a caller told "backend error"
// when the key was merely absent retries or refuses, while one told
// "miss" during an outage proceeds as if the key were gone.
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

// Clear removes the entries this cache owns.
//
// For an UNPREFIXED cache that is the whole database, which it owns by
// definition, so FlushDB is correct. For a prefixed one it is only the
// keys prefixedKey can produce: FlushDB there destroyed every other
// namespace sharing the database, plus foreign keys owned by neither.
// Scoping needs [KeyScanner]; without it, a prefixed Clear refuses
// rather than guessing, because the wrong guess is unrecoverable.
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
	keys, err := scanner.Keys(ctx, want+"*")
	if err != nil {
		return fmt.Errorf("cache: Clear scan %q: %w", want, err)
	}
	// Filter client-side rather than trusting the glob: a prefix
	// containing *, ?, or [ would match more than it names.
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

// keyPrefix returns the literal string every key of this cache starts
// with, "" for an unprefixed cache.
func (rc *RedisCache) keyPrefix() string {
	if rc.cfg.prefix == "" {
		return ""
	}
	return escapeKeySegment(rc.cfg.prefix) + ":"
}

func (rc *RedisCache) prefixedKey(key string) string {
	return rc.keyPrefix() + key
}

// escapeKeySegment doubles ':' inside a prefix so that joining it to a
// key with a single ':' is injective.
//
// Plain concatenation is not. WithPrefix("u:alice") and
// WithPrefix("u:alice:admin") both produce "u:alice:admin:x" — the
// first from key "admin:x", the second from key "x" — so one tenant
// could read, overwrite, or Delete another's entries by choosing a key
// that spans the boundary. Doubling the separator inside the prefix
// makes the prefix segment unambiguous, since a single ':' can then
// only be the join.
//
// Keys change shape for a prefix that CONTAINS ':'. A prefix without
// one is unaffected, which is every prefix that was unambiguous
// already.
func escapeKeySegment(s string) string {
	return strings.ReplaceAll(s, ":", "::")
}
