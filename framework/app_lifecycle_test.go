package framework

import (
	"context"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// App exposes a *lifecycle.Lifecycle.
func TestAppLifecycleExposed(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if app.Lifecycle() == nil {
		t.Fatal("App.Lifecycle() returned nil")
	}
}

// OnStop hooks register with Lifecycle and fire on Shutdown.
func TestOnStopRoutedThroughLifecycle(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var ran atomic.Int32
	app.OnStop(func() error { ran.Add(1); return nil })
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if ran.Load() != 1 {
		t.Fatalf("OnStop fn ran %d times, want 1", ran.Load())
	}
}

// Drainer registered via App.Lifecycle().RegisterDrainer fires on Shutdown.
func TestRegisterDrainerFiresOnShutdown(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var drained atomic.Int32
	err := app.Lifecycle().RegisterDrainer(lifecycle.DrainFunc(func(_ context.Context) error {
		drained.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatalf("RegisterDrainer: %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if drained.Load() != 1 {
		t.Fatalf("drainer ran %d times, want 1", drained.Load())
	}
}

// RunWithSignals invokes Shutdown when SIGTERM arrives.
func TestRunWithSignalsHandlesSIGTERM(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var ran atomic.Int32
	app.OnStop(func() error { ran.Add(1); return nil })

	done := make(chan error, 1)
	go func() { done <- app.RunWithSignals(context.Background()) }()

	// Give the signal handler a moment to install.
	time.Sleep(20 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithSignals: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithSignals did not return within 2s of SIGTERM")
	}
	if ran.Load() != 1 {
		t.Fatalf("OnStop ran %d times, want 1", ran.Load())
	}
}
