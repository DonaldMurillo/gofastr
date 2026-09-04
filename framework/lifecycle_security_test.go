package framework

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Panic isolation at the app's lifecycle extension points: host-supplied
// start/ready/seed hooks and battery OnStart/OnStop hooks run under the
// same recover-to-attributed-error isolation Init already has
// (initPluginSafe / initBatterySafe), so a panicking callback aborts the
// phase with an error instead of unwinding through App.Start /
// App.Shutdown.

// panicStartHarness builds the lightest app that reaches the Start phases
// under test: no DB, no entities, no plugins, signal handling off, banner
// muted, worktree port-remapping off (same recipe as
// start_bindfail_test.go / cov_start_test.go, which pin the
// error-return contracts these tests extend to panics).
func panicStartHarness() *App {
	app := NewApp(WithoutDefaultMiddleware())
	app.Config.DisableSignalHandling = true
	app.startupOutput = &bytes.Buffer{}
	return app
}

// runCapturingPanic runs fn, capturing both its error and any panic that
// escapes. The contract under test: the panic must NOT escape; the phase
// must return an attributed error instead (the initPluginSafe precedent).
func runCapturingPanic(fn func() error) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return fn(), nil
}

// TestOnStartHookPanicContained: a panicking OnStart hook must surface as
// an attributed error from App.Start, never as an escaping panic.
func TestOnStartHookPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := panicStartHarness()
	app.OnStart(func(context.Context) error { panic("onstart hook boom") })

	err, panicked := runCapturingPanic(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [start-hook-panic] an OnStart hook panic escaped App.Start (panic: %v) — Init panics are isolated via initPluginSafe but start hooks are not, so a host-callback bug crashes boot instead of aborting it with an error", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [start-hook-panic] App.Start must return an attributed error for a panicking OnStart hook (like Init does), got %v", err)
	}
}

// TestOnReadyHookPanicContained: a panicking OnReady hook must surface as
// an attributed error from App.Start (through the abort teardown that
// drains the just-bound listener), never as an escaping panic that leaves
// a bound-but-unserved port.
func TestOnReadyHookPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := panicStartHarness()
	app.OnReady(func(string) { panic("onready hook boom") })

	err, panicked := runCapturingPanic(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [ready-hook-panic] an OnReady hook panic escaped App.Start after the listener was bound (panic: %v) — the app aborted no phase, returned no error, and never served", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [ready-hook-panic] App.Start must return an attributed error for a panicking OnReady hook, got %v", err)
	}
}

// TestSeedHookPanicContained: a panicking WithSeed hook must surface as
// an attributed error from App.Start; seed hooks hold the same trust
// position as Init and run unrecovered before it.
func TestSeedHookPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := panicStartHarness()
	app.WithSeed(func(context.Context) error { panic("seed hook boom") })

	err, panicked := runCapturingPanic(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [seed-hook-panic] a WithSeed hook panic escaped App.Start (panic: %v) — seed hooks hold the same trust position as Init but run unrecovered", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [seed-hook-panic] App.Start must return an attributed error for a panicking WithSeed hook, got %v", err)
	}
}

// lifecyclePanicBattery is a BatteryLifecycle whose start and stop hooks
// panic: third-party battery code at the two phases BatteryManager drives.
type lifecyclePanicBattery struct{}

func (b *lifecyclePanicBattery) Name() string                  { return "lifecycle-panic" }
func (b *lifecyclePanicBattery) Init(*App) error               { return nil }
func (b *lifecyclePanicBattery) OnStart(context.Context) error { panic("battery OnStart boom") }
func (b *lifecyclePanicBattery) OnStop(context.Context) error  { panic("battery OnStop boom") }

// TestBatteryOnStartPanicContained: a panicking battery OnStart must
// surface as an attributed error from App.Start; battery Init is isolated
// (initBatterySafe) one phase earlier and the start phase must match it.
func TestBatteryOnStartPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := panicStartHarness()
	app.RegisterBattery(&lifecyclePanicBattery{})

	err, panicked := runCapturingPanic(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [battery-start-panic] a battery OnStart panic escaped App.Start (panic: %v) — battery Init is isolated via initBatterySafe but StartAll is not", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [battery-start-panic] App.Start must return an attributed error for a panicking battery OnStart, got %v", err)
	}
}

// TestBatteryOnStopPanicContained: a panicking battery OnStop must
// surface as App.Shutdown's error while the remaining batteries still
// stop; StopAll runs before lc.Shutdown, so the lifecycle drainer's
// recover never covers battery stop hooks and a SIGTERM shutdown must
// not crash on one battery's bug.
func TestBatteryOnStopPanicContained(t *testing.T) {
	app := panicStartHarness()
	app.RegisterBattery(&lifecyclePanicBattery{})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	err, panicked := runCapturingPanic(func() error { return app.Shutdown(context.Background()) })
	if panicked != nil {
		t.Fatalf("SECURITY: [battery-stop-panic] a battery OnStop panic escaped App.Shutdown (panic: %v) — StopAll runs before lc.Shutdown, so the lifecycle drainer recover never covers battery stop hooks and a SIGTERM shutdown crashes the process", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [battery-stop-panic] App.Shutdown must return an attributed error for a panicking battery OnStop, got %v", err)
	}
}
