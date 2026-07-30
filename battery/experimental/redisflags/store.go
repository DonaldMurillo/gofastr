// Package redisflags provides a Redis-backed feature flag store for
// the core/featureflag evaluator. Complements the existing in-memory
// and SQL stores.
package redisflags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by a RedisClient implementation when the key
// is not present. Distinguishing this from connection/protocol errors
// matters: a real error must not be silently treated as "flag absent".
var ErrNotFound = errors.New("redisflags: key not found")

// Store implements featureflag.Store backed by Redis.
type Store struct {
	client RedisClient
	prefix string
}

// RedisClient is the minimal Redis interface needed.
//
// Scan + MGet exist so List can paginate over keys without blocking the
// server with KEYS. The interface is intentionally narrow; an adapter
// for go-redis is straightforward.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Scan(ctx context.Context, cursor uint64, pattern string, count int64) (uint64, []string, error)
	MGet(ctx context.Context, keys ...string) ([]string, error)
}

// Flag represents a feature flag stored in Redis.
type Flag struct {
	Key         string    `json:"key"`
	Enabled     bool      `json:"enabled"`
	RolloutPct  int       `json:"rolloutPercent,omitempty"` // 0-100
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Config configures the Redis flag store.
type Config struct {
	// KeyPrefix is prepended to all Redis keys. Default: "flag:".
	KeyPrefix string
}

// New creates a new Redis-backed feature flag store.
func New(client RedisClient, cfg Config) *Store {
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "flag:"
	}
	return &Store{
		client: client,
		prefix: prefix,
	}
}

// Get retrieves a flag by key. Returns (nil, nil) if not found.
// All other transport errors are surfaced to the caller.
func (s *Store) Get(ctx context.Context, key string) (*Flag, error) {
	val, err := s.client.Get(ctx, s.prefix+key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("redisflags: get %q: %w", key, err)
	}
	var flag Flag
	if err := json.Unmarshal([]byte(val), &flag); err != nil {
		return nil, fmt.Errorf("redisflags: unmarshal %q: %w", key, err)
	}
	if err := validateFlag(&flag); err != nil {
		return nil, fmt.Errorf("redisflags: get %q: %w", key, err)
	}
	return &flag, nil
}

// validateFlag enforces the RolloutPct bound on the READ path as well as
// the write path. Set is not the only writer Redis has: another replica, an
// older binary, or an ops script can put a value there directly, so a bound
// checked only in Set is a bound checked only on the caller that was
// already going to behave. Consumers evaluate rollout as
// `hash % 100 < RolloutPct`, where a stored 10000 is a silent 100% rollout
// and a stored -1 inverts the comparison.
//
// Rejecting (rather than clamping) surfaces the corruption, and because
// IsEnabled maps a Get error to false, the failure direction is closed.
func validateFlag(f *Flag) error {
	if f.RolloutPct < 0 || f.RolloutPct > 100 {
		return fmt.Errorf("stored RolloutPct %d out of range (must be 0..100)", f.RolloutPct)
	}
	return nil
}

// Set saves a flag. Validates RolloutPct ∈ [0, 100].
func (s *Store) Set(ctx context.Context, flag *Flag) error {
	if flag.RolloutPct < 0 || flag.RolloutPct > 100 {
		return fmt.Errorf("redisflags: invalid RolloutPct %d (must be 0..100)", flag.RolloutPct)
	}
	flag.UpdatedAt = time.Now()
	data, err := json.Marshal(flag)
	if err != nil {
		return fmt.Errorf("redisflags: marshal %q: %w", flag.Key, err)
	}
	return s.client.Set(ctx, s.prefix+flag.Key, string(data), 0) // no TTL
}

// Delete removes a flag.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.prefix+key)
}

// List returns all flags. Uses Scan to iterate keys and MGet to batch-
// fetch values; never blocks Redis with KEYS.
func (s *Store) List(ctx context.Context) ([]*Flag, error) {
	pattern := s.prefix + "*"
	var allKeys []string
	var cursor uint64
	for {
		next, keys, err := s.client.Scan(ctx, cursor, pattern, 100)
		if err != nil {
			return nil, fmt.Errorf("redisflags: scan: %w", err)
		}
		allKeys = append(allKeys, keys...)
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(allKeys) == 0 {
		return nil, nil
	}

	vals, err := s.client.MGet(ctx, allKeys...)
	if err != nil {
		return nil, fmt.Errorf("redisflags: mget: %w", err)
	}

	flags := make([]*Flag, 0, len(vals))
	for _, v := range vals {
		if v == "" {
			continue
		}
		var flag Flag
		if err := json.Unmarshal([]byte(v), &flag); err != nil {
			continue
		}
		if err := validateFlag(&flag); err != nil {
			continue // same skip-the-bad-entry policy as a decode failure
		}
		flags = append(flags, &flag)
	}
	return flags, nil
}

// IsEnabled checks if a flag is enabled. Returns false if not found
// or on transport errors (caller can use Get to distinguish).
func (s *Store) IsEnabled(ctx context.Context, key string) bool {
	flag, err := s.Get(ctx, key)
	if err != nil || flag == nil {
		return false
	}
	return flag.Enabled
}
