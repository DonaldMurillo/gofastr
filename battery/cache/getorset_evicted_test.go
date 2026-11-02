package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

// evictOnSetCache stores nothing: every Set is evicted before anyone
// reads it back, the way a bounded LRU under pressure or Redis under
// maxmemory loses a fresh entry to the next writer.
type evictOnSetCache struct{ *MemoryCache }

func (c *evictOnSetCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.MemoryCache.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	return c.MemoryCache.Delete(ctx, key)
}

// TestGetOrSetReturnsLoadedValueWhenEntryIsGone pins that a successful
// load is not reported as a miss. GetOrSet read every waiter's dest back
// from the cache after the fill; when the entry was evicted (a bounded
// cache, Redis maxmemory) or had already expired (a short TTL) by then,
// the caller got ErrCacheMiss although the loader returned a value, so
// cache pressure turned into request errors.
func TestGetOrSetReturnsLoadedValueWhenEntryIsGone(t *testing.T) {
	type payload struct {
		N  int64  `json:"n"`
		S  string `json:"s"`
		ID int64  `json:"id"`
	}
	want := payload{N: 7, S: "loaded", ID: 1<<62 + 1}
	ctx := context.Background()

	caches := map[string]struct {
		c   Cache
		ttl time.Duration
	}{
		"evicted": {&evictOnSetCache{NewMemoryCache(WithCleanupInterval(0))}, time.Minute},
		"expired": {NewMemoryCache(WithCleanupInterval(0)), time.Nanosecond},
	}
	for name, tc := range caches {
		t.Run(name, func(t *testing.T) {
			var calls int
			var mu sync.Mutex
			var got payload
			err := GetOrSet(ctx, tc.c, "k", tc.ttl, &got, func(context.Context) (any, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				time.Sleep(time.Millisecond)
				return want, nil
			})
			if err != nil {
				t.Fatalf("GetOrSet after a successful load: %v", err)
			}
			if got != want {
				t.Errorf("dest = %+v, want %+v", got, want)
			}
			if calls != 1 {
				t.Errorf("loader ran %d times, want 1", calls)
			}
		})
	}

	// Joined waiters get the loaded value too, not a miss.
	t.Run("waiters", func(t *testing.T) {
		c := &evictOnSetCache{NewMemoryCache(WithCleanupInterval(0))}
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				<-start
				var got payload
				if err := GetOrSet(ctx, c, "w", time.Minute, &got, func(context.Context) (any, error) {
					time.Sleep(20 * time.Millisecond)
					return want, nil
				}); err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- context.Canceled
				}
			})
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("waiter: %v", err)
		}
	})
}
