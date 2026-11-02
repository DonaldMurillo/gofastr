package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// goRedisLikeClient models the TTL semantics of go-redis, the client
// the RedisClient docs show an adapter for: Set with a positive ttl
// expires, ttl == -1 is KEEPTTL, and every other non-positive ttl is
// "no expiration". The stub records every ttl it is handed so a test
// can see a negative one reach the wire.
type goRedisLikeClient struct {
	mu       sync.Mutex
	vals     map[string]string
	immortal map[string]bool
	ttls     []time.Duration
}

func newGoRedisLikeClient() *goRedisLikeClient {
	return &goRedisLikeClient{vals: map[string]string{}, immortal: map[string]bool{}}
}

func (c *goRedisLikeClient) Get(_ context.Context, k string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.vals[k]
	if !ok {
		return "", errors.New("redis: nil")
	}
	return v, nil
}

func (c *goRedisLikeClient) Set(_ context.Context, k, v string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttls = append(c.ttls, ttl)
	c.vals[k] = v
	c.immortal[k] = ttl <= 0 && ttl != -1
	return nil
}

func (c *goRedisLikeClient) Del(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.vals, k)
		delete(c.immortal, k)
	}
	return nil
}

func (c *goRedisLikeClient) Exists(_ context.Context, k string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.vals[k]
	return ok, nil
}

func (c *goRedisLikeClient) FlushDB(context.Context) error { return nil }

// TestRedisCache_NegativeTTLNotImmortal is the Redis twin of
// TestMemoryCache_NegativeTTLNotImmortal. The Cache contract says a
// negative TTL is "already expired": nothing stored, any existing entry
// dropped, never widened into retention. RedisCache handed the negative
// duration straight to RedisClient.Set, and a go-redis adapter turns a
// non-positive expiration (other than -1, KEEPTTL) into a SET with no
// expiry at all: the maximal inversion, an entry that never expires, and
// the opposite of what MemoryCache does with the same call.
func TestRedisCache_NegativeTTLNotImmortal(t *testing.T) {
	ctx := context.Background()
	cl := newGoRedisLikeClient()
	c := NewRedisCache(cl, WithPrefix("t"))

	if err := c.Set(ctx, "neg", "v", -time.Second); err != nil {
		t.Fatalf("Set(neg): %v", err)
	}
	var got string
	if err := c.Get(ctx, "neg", &got); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("a negative TTL stored a live entry (Get = %q, err %v); it must store nothing", got, err)
	}

	// Over an existing entry: the negative lifetime retires it.
	if err := c.Set(ctx, "over", "old", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "over", "new", -time.Second); err != nil {
		t.Fatalf("Set(over, neg): %v", err)
	}
	if err := c.Get(ctx, "over", &got); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("a negative-TTL Set left the key live (Get = %q, err %v); it must drop it", got, err)
	}

	// A negative default from WithTTL takes the same path.
	cd := NewRedisCache(cl, WithPrefix("d"), WithTTL(-time.Minute))
	if err := cd.Set(ctx, "k", "v", 0); err != nil {
		t.Fatal(err)
	}
	if err := cd.Get(ctx, "k", &got); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("a negative default TTL stored a live entry (Get = %q, err %v)", got, err)
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	for _, ttl := range cl.ttls {
		if ttl < 0 {
			t.Errorf("RedisClient.Set received a negative ttl %v; adapters read it as no expiry or KEEPTTL", ttl)
		}
	}
	for k, imm := range cl.immortal {
		if imm {
			t.Errorf("key %q was stored with no expiry", k)
		}
	}
}
