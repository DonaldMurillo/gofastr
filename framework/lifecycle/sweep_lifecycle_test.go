package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

func TestDefaultDrainTimeoutIsThirtySeconds(t *testing.T) {
	lc := lifecycle.New()
	var left time.Duration
	lc.RegisterDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		d, _ := ctx.Deadline()
		left = time.Until(d)
		return nil
	}))
	_ = lc.Shutdown(context.Background())
	if left < 29*time.Second || left > 30*time.Second {
		t.Fatalf("drain deadline %v away, want thirty seconds", left)
	}
}

func TestShutdownReturnsTheFirstDrainError(t *testing.T) {
	lc := lifecycle.New()
	first, second := errors.New("first"), errors.New("second")
	lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error { return first }))
	lc.RegisterDrainer(lifecycle.DrainFunc(func(context.Context) error { return second }))
	if err := lc.Shutdown(context.Background()); !errors.Is(err, first) || errors.Is(err, second) {
		t.Fatalf("Shutdown = %v, want only the first drainer's error", err)
	}
}

func TestRunWithSignalsReturnsNilWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := lifecycle.New().RunWithSignalsUsing(ctx, func(context.Context) error { called = true; return nil })
	if err != nil || called {
		t.Fatalf("RunWithSignalsUsing on an ended ctx = %v (shutdown called %v), want nil without a shutdown", err, called)
	}
}
