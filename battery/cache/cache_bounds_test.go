package cache

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// c-1: a bounded MemoryCache must never exceed its configured cap; inserting
// past the cap evicts older entries instead of growing without bound.
func TestMemoryCacheBoundedEvicts(t *testing.T) {
	const cap = 100
	c := NewMemoryCache(WithMaxEntries(cap))
	defer c.Close()
	ctx := context.Background()

	// Insert far more distinct keys than the cap allows.
	for i := 0; i < cap*10; i++ {
		if err := c.Set(ctx, "k:"+strconv.Itoa(i), i, 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	if n := c.Len(); n > cap {
		t.Fatalf("cache exceeded cap: have %d entries, want <= %d", n, cap)
	}
}

// c-1: the most recently used entries should survive eviction (LRU).
func TestMemoryCacheLRUKeepsRecent(t *testing.T) {
	const cap = 3
	c := NewMemoryCache(WithMaxEntries(cap))
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "a", 1, 0)
	_ = c.Set(ctx, "b", 2, 0)
	_ = c.Set(ctx, "c", 3, 0)

	// Touch "a" so it becomes most-recently-used.
	var v int
	if err := c.Get(ctx, "a", &v); err != nil {
		t.Fatalf("Get a: %v", err)
	}

	// Insert "d" — this should evict the least-recently-used, which is "b".
	_ = c.Set(ctx, "d", 4, 0)

	if c.Len() > cap {
		t.Fatalf("cache exceeded cap after insert: %d > %d", c.Len(), cap)
	}

	// "a" (recently touched), "c", and "d" should remain; "b" should be gone.
	if err := c.Get(ctx, "a", &v); err != nil {
		t.Errorf("expected 'a' to survive (recently used), got %v", err)
	}
	if err := c.Get(ctx, "b", &v); err != ErrCacheMiss {
		t.Errorf("expected 'b' to be evicted as LRU, got %v", err)
	}
	if err := c.Get(ctx, "d", &v); err != nil {
		t.Errorf("expected 'd' to be present, got %v", err)
	}
}

// c-1: zero-config (no WithMaxEntries) keeps the unbounded default behavior.
func TestMemoryCacheUnboundedByDefault(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	for i := 0; i < 5000; i++ {
		_ = c.Set(ctx, "k:"+strconv.Itoa(i), i, 0)
	}
	if c.Len() != 5000 {
		t.Fatalf("default cache should be unbounded: have %d, want 5000", c.Len())
	}
}

// c-2: concurrent GetOrSet misses on the same key must invoke the loader exactly once.
func TestGetOrSetSingleLoaderInvocation(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	const goroutines = 200
	var calls int32

	loader := func(ctx context.Context) (any, error) {
		atomic.AddInt32(&calls, 1)
		// Simulate slow work so all goroutines pile up on the same key.
		time.Sleep(20 * time.Millisecond)
		return "loaded-value", nil
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make([]error, goroutines)
	vals := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			var dest string
			errs[idx] = GetOrSet(ctx, c, "hot-key", time.Minute, &dest, loader)
			vals[idx] = dest
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("loader invoked %d times, want exactly 1 (thundering herd not collapsed)", got)
	}
	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("GetOrSet[%d] error: %v", i, errs[i])
		}
		if vals[i] != "loaded-value" {
			t.Fatalf("GetOrSet[%d] value = %q, want loaded-value", i, vals[i])
		}
	}
}

// c-2: a cache HIT must not invoke the loader at all.
func TestGetOrSetHitSkipsLoader(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	if err := c.Set(ctx, "warm", "cached", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var called bool
	loader := func(ctx context.Context) (any, error) {
		called = true
		return "fresh", nil
	}

	var dest string
	if err := GetOrSet(ctx, c, "warm", time.Minute, &dest, loader); err != nil {
		t.Fatalf("GetOrSet: %v", err)
	}
	if called {
		t.Error("loader should not be called on cache hit")
	}
	if dest != "cached" {
		t.Errorf("got %q, want cached", dest)
	}
}

// c-2: a loader error propagates and is not cached.
func TestGetOrSetLoaderErrorNotCached(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	ctx := context.Background()

	wantErr := fmt.Errorf("boom")
	loader := func(ctx context.Context) (any, error) {
		return nil, wantErr
	}

	var dest string
	if err := GetOrSet(ctx, c, "err-key", time.Minute, &dest, loader); err == nil {
		t.Fatal("expected loader error to propagate")
	}

	// Key must not have been stored.
	ok, _ := c.Exists(ctx, "err-key")
	if ok {
		t.Error("loader error should not be cached")
	}
}
