package cache

import (
	"context"
	"testing"
	"time"
)

func TestCacheNegativeTTLIgnoresDiscardedValue(t *testing.T) {
	memory := NewMemoryCache()
	defer memory.Close()
	for name, cache := range map[string]Cache{"memory": memory, "redis": NewRedisCache(newGoRedisLikeClient())} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := cache.Set(ctx, "key", "original", time.Hour); err != nil {
				t.Fatal(err)
			}
			if err := cache.Set(ctx, "key", make(chan int), -time.Second); err != nil {
				t.Fatal(err)
			}
			if ok, err := cache.Exists(ctx, "key"); err != nil || ok {
				t.Fatalf("Exists = %t, %v; want false, nil", ok, err)
			}
		})
	}
}

func TestMemoryCacheLenExcludesExpiredEntries(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()
	if err := c.Set(ctx, "live", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "expired", 2, time.Hour); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.items["expired"].expiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if got := c.Len(); got != 1 {
		t.Fatalf("Len = %d; want 1", got)
	}
}
