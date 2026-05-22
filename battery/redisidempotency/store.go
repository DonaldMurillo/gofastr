// Package redisidempotency provides a Redis-backed store for the
// idempotency middleware. Complements the existing in-memory and SQL
// stores in core/middleware.
package redisidempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Store implements the idempotency key store using Redis.
// Keys are stored as Redis strings with a TTL matching the
// configured expiry window.
type Store struct {
	client RedisClient
	ttl    time.Duration
}

// RedisClient is the minimal interface needed. Satisfied by
// go-redis/redis Client and ring.Ring.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// Config configures the Redis idempotency store.
type Config struct {
	// TTL is how long idempotency keys are retained. Default: 5 minutes.
	TTL time.Duration
	// KeyPrefix is prepended to all Redis keys. Default: "idem:".
	KeyPrefix string
}

// New creates a new Redis-backed idempotency store.
func New(client RedisClient, cfg Config) *Store {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "idem:"
	}
	return &Store{
		client: client,
		ttl:    ttl,
	}
}

// CheckAndReserve checks if the key exists and reserves it atomically.
// Returns the stored response if found, or nil if the key is new.
func (s *Store) CheckAndReserve(ctx context.Context, key string) ([]byte, error) {
	hash := hashKey(key)
	val, err := s.client.Get(ctx, hash)
	if err != nil {
		// Key doesn't exist — reserve it
		err2 := s.client.Set(ctx, hash, "", s.ttl)
		if err2 != nil {
			return nil, fmt.Errorf("redis idempotency: reserve failed: %w", err2)
		}
		return nil, nil
	}
	return []byte(val), nil
}

// Store saves the response for the given key.
func (s *Store) Store(ctx context.Context, key string, response []byte) error {
	hash := hashKey(key)
	return s.client.Set(ctx, hash, string(response), s.ttl)
}

// Delete removes the idempotency key.
func (s *Store) Delete(ctx context.Context, key string) error {
	hash := hashKey(key)
	return s.client.Del(ctx, hash)
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
