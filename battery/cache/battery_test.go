package cache_test

import (
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
