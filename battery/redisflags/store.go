// Package redisflags provides a Redis-backed feature flag store for
// the core/featureflag evaluator. Complements the existing in-memory
// and SQL stores.
package redisflags

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Store implements featureflag.Store backed by Redis.
type Store struct {
	client RedisClient
	prefix string
}

// RedisClient is the minimal Redis interface needed.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
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

// Get retrieves a flag by key. Returns nil if not found.
func (s *Store) Get(ctx context.Context, key string) (*Flag, error) {
	val, err := s.client.Get(ctx, s.prefix+key)
	if err != nil {
		return nil, nil // not found
	}
	var flag Flag
	if err := json.Unmarshal([]byte(val), &flag); err != nil {
		return nil, fmt.Errorf("redis flags: unmarshal %q: %w", key, err)
	}
	return &flag, nil
}

// Set saves a flag.
func (s *Store) Set(ctx context.Context, flag *Flag) error {
	flag.UpdatedAt = time.Now()
	data, err := json.Marshal(flag)
	if err != nil {
		return fmt.Errorf("redis flags: marshal %q: %w", flag.Key, err)
	}
	return s.client.Set(ctx, s.prefix+flag.Key, string(data), 0) // no TTL
}

// Delete removes a flag.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.prefix+key)
}

// List returns all flags.
func (s *Store) List(ctx context.Context) ([]*Flag, error) {
	keys, err := s.client.Keys(ctx, s.prefix+"*")
	if err != nil {
		return nil, err
	}

	var flags []*Flag
	for _, k := range keys {
		val, err := s.client.Get(ctx, k)
		if err != nil {
			continue
		}
		var flag Flag
		if err := json.Unmarshal([]byte(val), &flag); err != nil {
			continue
		}
		flags = append(flags, &flag)
	}
	return flags, nil
}

// IsEnabled checks if a flag is enabled. Returns false if not found.
func (s *Store) IsEnabled(ctx context.Context, key string) bool {
	flag, err := s.Get(ctx, key)
	if err != nil || flag == nil {
		return false
	}
	return flag.Enabled
}
