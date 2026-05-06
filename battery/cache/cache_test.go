package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryCacheSetGetRoundTrip(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	original := user{Name: "Alice", Age: 30}
	if err := c.Set(ctx, "user:1", original, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got user
	if err := c.Get(ctx, "user:1", &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != original {
		t.Errorf("got %v, want %v", got, original)
	}
}

func TestMemoryCacheExpiration(t *testing.T) {
	c := NewMemoryCache(WithCleanupInterval(10 * time.Millisecond))
	defer c.Close()
	ctx := context.Background()

	if err := c.Set(ctx, "short", "value", 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Should exist immediately.
	var got string
	if err := c.Get(ctx, "short", &got); err != nil {
		t.Fatalf("Get before expiry: %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	if err := c.Get(ctx, "short", &got); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss after expiry, got %v", err)
	}
}

func TestMemoryCacheDelete(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	if err := c.Set(ctx, "delkey", "val", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete(ctx, "delkey"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var got string
	if err := c.Get(ctx, "delkey", &got); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestMemoryCacheClear(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "a", 1, 0)
	_ = c.Set(ctx, "b", 2, 0)
	_ = c.Set(ctx, "c", 3, 0)

	if err := c.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	for _, key := range []string{"a", "b", "c"} {
		var got int
		if err := c.Get(ctx, key, &got); err != ErrCacheMiss {
			t.Errorf("expected ErrCacheMiss for key %q after clear, got %v", key, err)
		}
	}
}

func TestMemoryCacheConcurrentAccess(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	const goroutines = 100
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // writers + readers + deleters

	// Writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = c.Set(ctx, "key", id, 0)
			}
		}(i)
	}

	// Readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				var v int
				_ = c.Get(ctx, "key", &v)
			}
		}()
	}

	// Deleters
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = c.Delete(ctx, "key")
			}
		}()
	}

	wg.Wait()
}

func TestMemoryCacheMissReturnsError(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	var got string
	err := c.Get(ctx, "nonexistent", &got)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}

func TestMemoryCacheExists(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	ok, err := c.Exists(ctx, "exists-key")
	if err != nil {
		t.Fatalf("Exists on missing key: %v", err)
	}
	if ok {
		t.Error("expected Exists to return false for missing key")
	}

	_ = c.Set(ctx, "exists-key", "val", 0)
	ok, err = c.Exists(ctx, "exists-key")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("expected Exists to return true for existing key")
	}
}

func TestMemoryCacheDefaultTTL(t *testing.T) {
	c := NewMemoryCache(WithTTL(50 * time.Millisecond))
	defer c.Close()
	ctx := context.Background()

	// Set with TTL=0 should use the default TTL.
	if err := c.Set(ctx, "ttlkey", "val", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got string
	if err := c.Get(ctx, "ttlkey", &got); err != nil {
		t.Fatalf("Get before expiry: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if err := c.Get(ctx, "ttlkey", &got); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss after default TTL expired, got %v", err)
	}
}

func TestMemoryCacheWithPrefix(t *testing.T) {
	c := NewMemoryCache(WithPrefix("myapp"))
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "key", "val", 0)

	var got string
	if err := c.Get(ctx, "key", &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "val" {
		t.Errorf("got %q, want %q", got, "val")
	}
}
