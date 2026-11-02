package cache

import (
	"context"
	"testing"
	"time"
)

// goRedisSemantics is a RedisClient whose Set reads its ttl the way the
// go-redis adapter the docs point at does: ttl > 0 is EX/PX, -1 is
// KEEPTTL, and anything else, a negative included, is a plain SET with
// no expiry. That is a conforming adapter: RedisClient only says "a TTL
// of 0 means no expiration".
type goRedisSemantics struct {
	stubRedisClient
	persistent map[string]bool
}

func (g *goRedisSemantics) Set(ctx context.Context, k, v string, ttl time.Duration) error {
	if g.persistent == nil {
		g.persistent = map[string]bool{}
	}
	if ttl == -1 { // KEEPTTL: the old expiry, if any, stays
		return g.stubRedisClient.Set(ctx, k, v, ttl)
	}
	g.persistent[k] = ttl <= 0
	return g.stubRedisClient.Set(ctx, k, v, ttl)
}

// TestRedisCache_NegativeTTLNotImmortal is the Redis twin of
// TestMemoryCache_NegativeTTLNotImmortal. The Cache contract says a
// negative ttl stores nothing and drops what the key held. RedisCache.Set
// handed the negative duration to the client, and a go-redis adapter
// turns it into a SET with no expiry: the entry the caller said was
// already gone was stored forever, over the old value.
func TestRedisCache_NegativeTTLNotImmortal(t *testing.T) {
	ctx := context.Background()
	for _, ttl := range []time.Duration{-time.Second, -1} {
		client := &goRedisSemantics{}
		c := NewRedisCache(client, WithPrefix("app"))

		if err := c.Set(ctx, "neg", "v", ttl); err != nil {
			t.Fatalf("Set(neg, %v): %v", ttl, err)
		}
		if err := c.Get(ctx, "neg", new(string)); err == nil {
			t.Errorf("ttl=%v: RedisCache.Set with a negative ttl stored a readable entry (persistent=%v): "+
				"a negative lifetime must store nothing", ttl, client.persistent["app:neg"])
		}

		if err := c.Set(ctx, "over", "old", time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := c.Set(ctx, "over", "new", ttl); err != nil {
			t.Fatalf("Set(over, %v): %v", ttl, err)
		}
		if err := c.Get(ctx, "over", new(string)); err == nil {
			t.Errorf("ttl=%v: a negative Set left a live entry under the key; it must retire it", ttl)
		}

		if err := GetOrSet(ctx, c, "gos", ttl, new(string), func(context.Context) (any, error) {
			return "loaded", nil
		}); err == nil {
			t.Errorf("ttl=%v: GetOrSet with a negative ttl reported success; the miss must surface", ttl)
		}
		if ok, _ := c.Exists(ctx, "gos"); ok {
			t.Errorf("ttl=%v: GetOrSet with a negative ttl stored the value", ttl)
		}
	}

	// A negative default (WithTTL) is the same caller intent.
	client := &goRedisSemantics{}
	c := NewRedisCache(client, WithTTL(-time.Minute))
	if err := c.Set(ctx, "k", "v", 0); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Exists(ctx, "k"); ok {
		t.Errorf("a negative default ttl stored an entry")
	}

	// Control: a positive ttl still stores.
	c = NewRedisCache(&goRedisSemantics{})
	if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, "k", new(string)); err != nil {
		t.Errorf("positive ttl: Get = %v, want the stored value", err)
	}
}
