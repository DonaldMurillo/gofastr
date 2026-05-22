// Package redisidempotency provides a Redis-backed store for the
// idempotency middleware. Complements the existing in-memory and SQL
// stores in core/middleware.
package redisidempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by the RedisClient implementation when a key
// does not exist. Distinguishing this from connection / protocol errors
// matters: a real error must not be silently treated as "key absent".
var ErrNotFound = errors.New("redisidempotency: key not found")

// ErrInFlight signals that a key has been reserved but not yet stored.
// Callers can use this to wait/retry rather than mistaking the reservation
// for a stored empty (204) response.
var ErrInFlight = errors.New("redisidempotency: request in flight")

// reservedSentinel is the value written to Redis at reservation time.
// It must never be a valid HTTP response body. The leading non-printable
// byte makes accidental collision with caller-supplied bytes essentially
// impossible.
const reservedSentinel = "\x00__reserved__"

// Store implements the idempotency key store using Redis.
// Keys are stored as Redis strings with a TTL matching the
// configured expiry window.
type Store struct {
	client          RedisClient
	ttl             time.Duration
	prefix          string
	maxResponseSize int
}

// RedisClient is the minimal interface needed. Satisfied by
// go-redis/redis Client and ring.Ring. SetNX is used to perform an
// atomic "reserve-if-absent" — TOCTOU-free reservation.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// Config configures the Redis idempotency store.
type Config struct {
	// TTL is how long idempotency keys are retained. Default: 5 minutes.
	TTL time.Duration
	// KeyPrefix is prepended to all Redis keys. Default: "idem:".
	KeyPrefix string
	// MaxResponseSize bounds the bytes accepted by Store. 0 disables
	// the check (caller is responsible). Recommended: a few MB.
	MaxResponseSize int
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
		client:          client,
		ttl:             ttl,
		prefix:          prefix,
		maxResponseSize: cfg.MaxResponseSize,
	}
}

// CheckAndReserve atomically reserves the key if absent or returns the
// stored response. Returns:
//   - (response, nil) when a stored response exists.
//   - (nil, nil) when this caller won the reservation race.
//   - (nil, ErrInFlight) when another caller has reserved the key but
//     has not yet stored a response.
//   - (nil, err) on transport errors.
func (s *Store) CheckAndReserve(ctx context.Context, key string) ([]byte, error) {
	hashed := s.hashKey(key)

	ok, err := s.client.SetNX(ctx, hashed, reservedSentinel, s.ttl)
	if err != nil {
		return nil, fmt.Errorf("redisidempotency: reserve: %w", err)
	}
	if ok {
		// We are the first caller — caller proceeds to compute & Store.
		return nil, nil
	}

	// Someone else holds the key; fetch its current value.
	val, err := s.client.Get(ctx, hashed)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Race: TTL expired between SetNX and Get. Treat as a fresh
			// reservation attempt by recursing once — but bound the
			// retry to a single shot to avoid livelock.
			ok2, err2 := s.client.SetNX(ctx, hashed, reservedSentinel, s.ttl)
			if err2 != nil {
				return nil, fmt.Errorf("redisidempotency: re-reserve: %w", err2)
			}
			if ok2 {
				return nil, nil
			}
			return nil, ErrInFlight
		}
		return nil, fmt.Errorf("redisidempotency: get: %w", err)
	}
	if val == reservedSentinel {
		return nil, ErrInFlight
	}
	return []byte(val), nil
}

// Store saves the response for the given key.
func (s *Store) Store(ctx context.Context, key string, response []byte) error {
	if s.maxResponseSize > 0 && len(response) > s.maxResponseSize {
		return fmt.Errorf("redisidempotency: response too large (%d > %d)", len(response), s.maxResponseSize)
	}
	hashed := s.hashKey(key)
	return s.client.Set(ctx, hashed, string(response), s.ttl)
}

// Delete removes the idempotency key.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.hashKey(key))
}

func (s *Store) hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return s.prefix + hex.EncodeToString(h[:])
}
