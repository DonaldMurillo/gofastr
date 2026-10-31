package cache

import (
	"context"
	"testing"
	"time"
)

// MemoryCache.Close returns nothing, so OnStop's io.Closer assertion
// never matched and the cleanup goroutine outlived the app (2026-10-27
// B18).
func TestBatteryStopEndsMemoryCleanup(t *testing.T) {
	mc := NewMemoryCache(WithCleanupInterval(time.Hour))
	if err := NewBattery(mc).OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
	select {
	case <-mc.stopCh:
	default:
		t.Fatal("OnStop left the MemoryCache cleanup goroutine running")
	}
}
