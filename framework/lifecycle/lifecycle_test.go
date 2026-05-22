package lifecycle_test

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
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

func TestRunWithSignalsTermsOnSIGTERM(t *testing.T) {
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
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}

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

func TestSlowDrainerInterrupted(t *testing.T) {
	lc := lifecycle.New()
	lc.SetShutdownTimeout(10 * time.Millisecond)

	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		select {
		case <-time.After(50 * time.Millisecond):
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
	if elapsed > 40*time.Millisecond {
		t.Errorf("Shutdown elapsed %v, expected under 40ms", elapsed)
	}
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
