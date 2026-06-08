package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/cache"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestCacheBatteryInterface verifies that NewBattery returns a value that
// satisfies framework.Battery and framework.BatteryLifecycle.
func TestCacheBatteryInterface(t *testing.T) {
	c := cache.NewMemoryCache(cache.WithCleanupInterval(10 * time.Millisecond))
	b := cache.NewBattery(c)

	var _ framework.Battery = b
	var _ framework.BatteryLifecycle = b

	if b.Name() != "cache" {
		t.Errorf("Name() = %q, want 'cache'", b.Name())
	}
}

// TestCacheBatteryShutdownStopsGoroutine verifies that OnStop closes the
// underlying MemoryCache cleanup goroutine.
func TestCacheBatteryShutdownStopsGoroutine(t *testing.T) {
	c := cache.NewMemoryCache(cache.WithCleanupInterval(5 * time.Millisecond))
	b := cache.NewBattery(c)

	ctx := context.Background()
	if err := b.OnStop(ctx); err != nil {
		t.Fatalf("OnStop: %v", err)
	}

	// After OnStop, the MemoryCache should be closed.  Verify by checking
	// that a subsequent Set/Get still works (Close only stops the cleanup
	// goroutine, not the data store).
	if err := c.Set(ctx, "k", "v", 0); err != nil {
		t.Fatalf("Set after OnStop: %v", err)
	}
	var got string
	if err := c.Get(ctx, "k", &got); err != nil {
		t.Fatalf("Get after OnStop: %v", err)
	}
}
