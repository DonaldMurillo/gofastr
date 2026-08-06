package framework

import (
	"context"
	"errors"
	"testing"
)

// TestWithSeedNilIsNoOp pins that a nil seed hook is silently ignored rather
// than appended: a caller that conditionally passes a nil (e.g.
// app.WithSeed(maybeHook())) must not end up with a nil entry that panics when
// runSeedHooks fires the chain.
func TestWithSeedNilIsNoOp(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	before := len(app.seedHooks)
	ret := app.WithSeed(nil)
	if ret != app {
		t.Fatal("WithSeed did not return the App for chaining")
	}
	if len(app.seedHooks) != before {
		t.Fatalf("WithSeed(nil) appended %d nil hook(s)", len(app.seedHooks)-before)
	}
}

// TestSeedHookErrorAbortsChain pins that a WithSeed func returning a non-nil
// error stops the chain and surfaces that error, so a failing app-level seed
// fails boot loudly instead of running later hooks against partial data.
func TestSeedHookErrorAbortsChain(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	boom := errors.New("seed boom")
	ranFirst := false
	ranSecond := false
	app.WithSeed(func(context.Context) error { ranFirst = true; return boom })
	app.WithSeed(func(context.Context) error { ranSecond = true; return nil })

	err := app.runSeedHooks()
	if !ranFirst {
		t.Fatal("first seed hook did not run")
	}
	if ranSecond {
		t.Fatal("second seed hook ran after the first failed — the chain must abort")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("runSeedHooks returned %v, want %v", err, boom)
	}
}
