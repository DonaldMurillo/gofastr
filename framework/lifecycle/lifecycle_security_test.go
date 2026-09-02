package lifecycle_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// TestTimeoutRaceShutdownVsSetter asserts that reading lc.timeout during
// Shutdown is synchronised against a concurrent SetShutdownTimeout. Run
// under `go test -race` to surface the unsynchronised read/write.
func TestTimeoutRaceShutdownVsSetter(t *testing.T) {
	var wg sync.WaitGroup

	// A drainer that blocks briefly so Shutdown's timeout read overlaps
	// with the concurrent setter.
	blocker := lifecycle.DrainFunc(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil
	})

	for range 50 {
		lc := lifecycle.New()
		if err := lc.RegisterDrainer(blocker); err != nil {
			t.Fatalf("RegisterDrainer: %v", err)
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			lc.SetShutdownTimeout(10 * time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			_ = lc.Shutdown(context.Background())
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Property: drain work runs EXACTLY ONCE per registration, no matter how
// many times Shutdown is called — the idempotent-shutdown contract. A
// re-entrant Shutdown that re-ran drainers would double-close connections
// and double-flush queues during a signal-handling race.
// ---------------------------------------------------------------------------

func TestShutdownDrainsExactlyOnce(t *testing.T) {
	lc := lifecycle.New()
	var first, second atomic.Int32
	lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error {
		first.Add(1)
		return nil
	}))
	lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error {
		second.Add(1)
		return nil
	}))

	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v, want nil no-op", err)
	}
	if n := first.Load(); n != 1 {
		t.Errorf("drainer 1 ran %d times across two Shutdowns, want 1", n)
	}
	if n := second.Load(); n != 1 {
		t.Errorf("drainer 2 ran %d times across two Shutdowns, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Same property under CONCURRENT Shutdown calls (SIGINT racing SIGTERM, or
// two goroutines both running the shutdown path): exactly one drain.
// ---------------------------------------------------------------------------

func TestConcurrentShutdownDrainsAtMostOnce(t *testing.T) {
	lc := lifecycle.New()
	var drains atomic.Int32
	lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error {
		drains.Add(1)
		return nil
	}))

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = lc.Shutdown(context.Background())
		}()
	}
	wg.Wait()

	if n := drains.Load(); n != 1 {
		t.Errorf("drain ran %d times under %d concurrent Shutdowns, want 1", n, callers)
	}
}

// ---------------------------------------------------------------------------
// Property: PrependDrainer runs BEFORE every previously-registered drainer
// (the documented LIFO encoding app-level stop hooks rely on: last
// registered = first drained).
// ---------------------------------------------------------------------------

func TestPrependDrainerRunsFirst(t *testing.T) {
	lc := lifecycle.New()
	var mu sync.Mutex
	var order []string
	record := func(name string) lifecycle.DrainFunc {
		return func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	if err := lc.RegisterDrainer(record("A")); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := lc.PrependDrainer(record("B")); err != nil {
		t.Fatalf("prepend B: %v", err)
	}
	if err := lc.AppendDrainer(record("C")); err != nil {
		t.Fatalf("append C: %v", err)
	}

	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	mu.Lock()
	got := fmt.Sprint(order)
	mu.Unlock()
	if got != "[B A C]" {
		t.Errorf("drain order = %s, want [B A C] (prepended first, appended in order)", got)
	}
}

// ---------------------------------------------------------------------------
// Property: EVERY registration surface refuses once Shutdown has begun
// (late registrations dropped, snapshot deterministic) — asserted across
// all four surfaces, not just the two the existing test covers.
// ---------------------------------------------------------------------------

func TestAllRegisterSurfacesRefuseAfterShutdown(t *testing.T) {
	lc := lifecycle.New()
	if err := lc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	surfaces := []struct {
		name string
		call func() error
	}{
		{"RegisterDrainer", func() error {
			return lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error { return nil }))
		}},
		{"AppendDrainer", func() error {
			return lc.AppendDrainer(lifecycle.DrainFunc(func(context.Context) error { return nil }))
		}},
		{"PrependDrainer", func() error {
			return lc.PrependDrainer(lifecycle.DrainFunc(func(context.Context) error { return nil }))
		}},
		{"RegisterHealthChecker", func() error {
			return lc.RegisterHealthChecker(&stubHealthChecker{})
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			if err := s.call(); !errors.Is(err, lifecycle.ErrShuttingDown) {
				t.Errorf("SECURITY: [lifecycle] %s after Shutdown = %v, want ErrShuttingDown (late registration must be dropped, not queued)", s.name, err)
			}
		})
	}
}

// stubHealthChecker is a minimal always-healthy HealthChecker.
type stubHealthChecker struct{}

func (*stubHealthChecker) IsHealthy() bool { return true }
