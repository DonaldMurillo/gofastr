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
	ctx := context.Background()
	mc := NewMemoryCache(WithCleanupInterval(5 * time.Millisecond))
	if err := NewBattery(mc).OnStop(ctx); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
	select {
	case <-mc.stopCh:
	default:
		t.Fatal("OnStop left the MemoryCache cleanup goroutine running")
	}

	if err := mc.Set(ctx, "k", "v", 0); err != nil {
		t.Fatalf("Set after OnStop: %v", err)
	}
	var got string
	if err := mc.Get(ctx, "k", &got); err != nil || got != "v" {
		t.Errorf("Get after OnStop = (%q, %v), want v, nil", got, err)
	}
}
