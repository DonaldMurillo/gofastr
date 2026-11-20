package lifecycle_test

import (
	"context"
	"strings"
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

	for range 5 {
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

func TestDrainTimesOut(t *testing.T) {
	cases := []struct {
		name     string
		timeout  time.Duration // SetShutdownTimeout
		wait     time.Duration // how long the drainer stalls if not interrupted
		maxPause time.Duration // Shutdown must return within this
	}{
		// The drainer stalls past the deadline and Shutdown returns a
		// timeout error instead of waiting the drainer out.
		{"drain timeout", 50 * time.Millisecond, 5 * time.Second, 200 * time.Millisecond},
		// A drainer only just slower than the deadline is interrupted, not
		// waited for: Shutdown returns well before the drainer would.
		{"slow drainer interrupted", 10 * time.Millisecond, 50 * time.Millisecond, 40 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lc := lifecycle.New()
			lc.SetShutdownTimeout(tc.timeout)

			lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
				select {
				case <-time.After(tc.wait):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}))

			start := time.Now()
			err := lc.Shutdown(context.Background())
			elapsed := time.Since(start)
			if err == nil {
				t.Fatal("expected timeout error")
			}
			if elapsed > tc.maxPause {
				t.Errorf("Shutdown elapsed %v, expected under %v", elapsed, tc.maxPause)
			}
		})
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

func TestRunWithSignalsTermsOnSIGTERM(t *testing.T) {
	requireSIGTERMForTest(t)
	lc := lifecycle.New()
	var drained atomic.Bool
	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		drained.Store(true)
		return nil
	}))

	done := make(chan error, 1)
	go func() {
		done <- lc.RunWithSignals(context.Background())
	}()

	// Give the goroutine time to install the signal handler.
	time.Sleep(20 * time.Millisecond)
	sendSIGTERMForTest(t)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithSignals: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithSignals did not return after SIGTERM")
	}
	if !drained.Load() {
		t.Error("drainer was not called after SIGTERM")
	}
}

func TestRegisterAfterShutdownRejected(t *testing.T) {
	lc := lifecycle.New()
	lc.SetShutdownTimeout(200 * time.Millisecond)

	gate := make(chan struct{})
	release := make(chan struct{})
	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		close(gate)
		<-release
		return nil
	}))

	done := make(chan error, 1)
	go func() { done <- lc.Shutdown(context.Background()) }()

	<-gate
	if err := lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error { return nil })); err == nil {
		t.Error("RegisterDrainer after Shutdown should error")
	}
	if err := lc.RegisterHealthChecker(&mockHealthChecker{healthy: true}); err == nil {
		t.Error("RegisterHealthChecker after Shutdown should error")
	}
	close(release)
	<-done
}

func TestDrainerPanicIsolated(t *testing.T) {
	lc := lifecycle.New()
	var secondCalled atomic.Bool

	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		panic("boom")
	}))
	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		time.Sleep(5 * time.Millisecond)
		secondCalled.Store(true)
		return nil
	}))

	err := lc.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected error from panicking drainer")
	}
	if !secondCalled.Load() {
		t.Error("second drainer not called after first panicked")
	}
	// The recovered panic value should surface in the error.
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q missing panic value 'boom'", err.Error())
	}
}

func TestShutdownMarksUnhealthy(t *testing.T) {
	lc := lifecycle.New()
	lc.SetShutdownTimeout(500 * time.Millisecond)

	// Drainer blocks long enough for us to observe IsHealthy during drain.
	gate := make(chan struct{})
	released := make(chan struct{})
	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		close(gate)
		<-released
		return nil
	}))

	done := make(chan error, 1)
	go func() {
		done <- lc.Shutdown(context.Background())
	}()

	<-gate
	if lc.IsHealthy() {
		t.Error("IsHealthy returned true during Shutdown drain phase")
	}
	close(released)
	if err := <-done; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
