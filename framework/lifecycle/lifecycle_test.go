package lifecycle_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

func TestShutdownWithNoDrainers(t *testing.T) {
	lc := lifecycle.New()
	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with no drainers: %v", err)
	}
}

func TestDrainCalled(t *testing.T) {
	lc := lifecycle.New()
	var called atomic.Bool

	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		called.Store(true)
		return nil
	}))

	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !called.Load() {
		t.Error("drainer was not called")
	}
}

func TestMultipleDrainersCalled(t *testing.T) {
	lc := lifecycle.New()
	var count atomic.Int32

	for i := 0; i < 5; i++ {
		lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
			count.Add(1)
			return nil
		}))
	}

	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := count.Load(); got != 5 {
		t.Errorf("drainers called = %d, want 5", got)
	}
}

func TestDrainTimeout(t *testing.T) {
	lc := lifecycle.New()
	lc.SetShutdownTimeout(50 * time.Millisecond)

	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	start := time.Now()
	err := lc.Shutdown(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("drain took %v, should have timed out sooner", elapsed)
	}
}

func TestIsHealthyNoCheckers(t *testing.T) {
	lc := lifecycle.New()
	if !lc.IsHealthy() {
		t.Error("should be healthy with no checkers")
	}
}

func TestIsHealthyWithCheckers(t *testing.T) {
	lc := lifecycle.New()

	healthy := &mockHealthChecker{healthy: true}
	lc.RegisterHealthChecker(healthy)

	if !lc.IsHealthy() {
		t.Error("should be healthy")
	}

	healthy.healthy = false
	if lc.IsHealthy() {
		t.Error("should be unhealthy")
	}
}

type mockHealthChecker struct {
	healthy bool
}

func (m *mockHealthChecker) IsHealthy() bool { return m.healthy }
