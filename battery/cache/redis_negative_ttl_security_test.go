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
	c := NewRedisCache(cl, WithPrefix("app"))

	// -1 is KEEPTTL in go-redis, the shape a plain -time.Second does not
	// cover: over a live key it would keep the old expiry, so both
	// negative durations take the same "already expired" path.
	for _, ttl := range []time.Duration{-time.Second, -1} {
		if err := c.Set(ctx, "neg", "v", ttl); err != nil {
			t.Fatalf("Set(neg, %v): %v", ttl, err)
		}
		var got string
		if err := c.Get(ctx, "neg", &got); !errors.Is(err, ErrCacheMiss) {
			t.Errorf("ttl=%v: a negative TTL stored a live entry (Get = %q, err %v); it must store nothing", ttl, got, err)
		}

		if err := c.Set(ctx, "over", "old", time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := c.Set(ctx, "over", "new", ttl); err != nil {
			t.Fatalf("Set(over, %v): %v", ttl, err)
		}
		if err := c.Get(ctx, "over", &got); !errors.Is(err, ErrCacheMiss) {
			t.Errorf("ttl=%v: a negative-TTL Set left the key live (Get = %q, err %v); it must drop it", ttl, got, err)
		}

		if err := GetOrSet(ctx, c, "gos", ttl, new(string), func(context.Context) (any, error) {
			return "loaded", nil
		}); !errors.Is(err, ErrCacheMiss) {
			t.Errorf("ttl=%v: GetOrSet returned %v, want ErrCacheMiss", ttl, err)
		}
		if ok, err := c.Exists(ctx, "gos"); err != nil || ok {
			t.Errorf("ttl=%v: Exists after GetOrSet = (%t, %v), want false, nil", ttl, ok, err)
		}
	}

	// A negative default from WithTTL takes the same path.
	cd := NewRedisCache(cl, WithTTL(-time.Minute))
	if err := cd.Set(ctx, "k", "v", 0); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := cd.Get(ctx, "k", &got); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("a negative default TTL stored a live entry (Get = %q, err %v)", got, err)
	}
	if ok, err := cd.Exists(ctx, "k"); err != nil || ok {
		t.Errorf("Exists after Set with a negative default TTL = (%t, %v), want false, nil", ok, err)
	}

	// Control: a positive ttl still stores.
	cp := NewRedisCache(cl)
	if err := cp.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	var ctrl string
	if err := cp.Get(ctx, "k", &ctrl); err != nil || ctrl != "v" {
		t.Errorf("positive ttl: Get = (%q, %v), want the stored value", ctrl, err)
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
