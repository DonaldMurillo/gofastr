//go:build red

// RED TESTS — open findings, 2026-09-02 adversarial pass round 2 (tests-only; no fix applied).
//
// Property: panic isolation at extension points — host-supplied lifecycle
// callbacks (OnStart / OnReady / WithSeed / battery OnStart-OnStop) are
// third-party code the framework invokes; a panic in one must surface as an
// attributed error, never unwind through App.Start / App.Shutdown. Init is
// already isolated this way (initPluginSafe / initBatterySafe, battery.go:235),
// and lifecycle.Shutdown recovers drainer panics (lifecycle.go:185-189, which
// covers app-level OnStop hooks). Every other phase listed below is not.
//
// Surfaces: app.go runStartHooks startHooks loop, app.go Start readyHooks
// loop, seed.go runSeedHooks loop, battery.go StartAll, battery.go StopAll
// (reached from App.Shutdown at app.go:2733, BEFORE lc.Shutdown, so the
// lifecycle recover never sees it).
//
// Findings + fix directions are per-test below. Severity: production-facing —
// each surface runs host/plugin/battery code on every boot and every
// SIGTERM-driven shutdown of a real app.
package framework

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// redStartHarness builds the lightest app that reaches the Start phases under
// test: no DB, no entities, no plugins, signal handling off, banner muted,
// worktree port-remapping off (same recipe as start_bindfail_test.go /
// cov_start_test.go, which pin the error-return contracts these tests extend
// to panics).
func redStartHarness() *App {
	app := NewApp(WithoutDefaultMiddleware())
	app.Config.DisableSignalHandling = true
	app.startupOutput = &bytes.Buffer{}
	return app
}

// redRunStart runs fn, capturing both its error and any panic that escapes.
// The contract under test: the panic must NOT escape; Start must return an
// attributed error instead (the initPluginSafe precedent).
func redRun(fn func() error) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return fn(), nil
}

// TestAppRedOnStartPanicContained
// Finding: app.go runStartHooks fires every OnStart hook bare
// (`fn(a.appCtx)`); a panicking start hook unwinds through App.Start instead
// of aborting it with an attributed error. Contrast initPluginSafe /
// initBatterySafe, which already convert Init panics to errors.
// Fix direction: wrap each hook call (or the loop) in a recover that returns
// `fmt.Errorf("start hook panicked (panic type %T): …", v)`, mirroring
// initBatterySafe's secret-safe %T formatting.
func TestAppRedOnStartPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := redStartHarness()
	app.OnStart(func(context.Context) error { panic("onstart hook boom") })

	err, panicked := redRun(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [start-hook-panic] an OnStart hook panic escaped App.Start (panic: %v) — Init panics are isolated via initPluginSafe but start hooks are not, so a host-callback bug crashes boot instead of aborting it with an error", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [start-hook-panic] App.Start must return an attributed error for a panicking OnStart hook (like Init does), got %v", err)
	}
}

// TestAppRedOnReadyPanicContained
// Finding: the readyHooks loop in Start (`fn(ln.Addr().String())`) runs after
// the port is bound, bare. A panicking OnReady hook unwinds through Start,
// skipping srv.Serve and the abort teardown — the process is left with a
// bound-but-unserved port from the caller's perspective and no error.
// Fix direction: recover around the loop (or per hook) returning an
// attributed error through Start's existing abort path; a readiness callback
// bug should not take down the process that just booted cleanly.
func TestAppRedOnReadyPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := redStartHarness()
	app.OnReady(func(string) { panic("onready hook boom") })

	err, panicked := redRun(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [ready-hook-panic] an OnReady hook panic escaped App.Start after the listener was bound (panic: %v) — the app aborted no phase, returned no error, and never served", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [ready-hook-panic] App.Start must return an attributed error for a panicking OnReady hook, got %v", err)
	}
}

// TestAppRedSeedHookPanicContained
// Finding: runSeedHooks (seed.go) fires every WithSeed func bare; a panicking
// seed func unwinds through Start. WithSeed's doc frames hooks as
// error-returning app-level code invoked between migration and serving — the
// same trust position as Init, which IS isolated.
// Fix direction: recover per hook (or around the loop) in runSeedHooks,
// returning an attributed error so Start aborts through its normal
// partial-startup teardown instead of crashing.
func TestAppRedSeedHookPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := redStartHarness()
	app.WithSeed(func(context.Context) error { panic("seed hook boom") })

	err, panicked := redRun(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [seed-hook-panic] a WithSeed hook panic escaped App.Start (panic: %v) — seed hooks hold the same trust position as Init but run unrecovered", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [seed-hook-panic] App.Start must return an attributed error for a panicking WithSeed hook, got %v", err)
	}
}

// redPanicBattery is a BatteryLifecycle whose start and stop hooks panic:
// third-party battery code at the two phases BatteryManager drives bare.
type redPanicBattery struct{}

func (b *redPanicBattery) Name() string                  { return "red-panic" }
func (b *redPanicBattery) Init(*App) error               { return nil }
func (b *redPanicBattery) OnStart(context.Context) error { panic("battery OnStart boom") }
func (b *redPanicBattery) OnStop(context.Context) error  { panic("battery OnStop boom") }

// TestBatteryRedOnStartPanicContained
// Finding: BatteryManager.StartAll calls lc.OnStart(ctx) bare; a panicking
// battery start hook unwinds through App.Start. Battery Init is isolated
// (initBatterySafe) one phase earlier — the start phase is the gap.
// Fix direction: recover per battery in StartAll returning
// `battery %q start panicked (panic type %T)`, the initBatterySafe shape.
func TestBatteryRedOnStartPanicContained(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	app := redStartHarness()
	app.RegisterBattery(&redPanicBattery{})

	err, panicked := redRun(func() error { return app.Start("127.0.0.1:0") })
	if panicked != nil {
		t.Fatalf("SECURITY: [battery-start-panic] a battery OnStart panic escaped App.Start (panic: %v) — battery Init is isolated via initBatterySafe but StartAll is not", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [battery-start-panic] App.Start must return an attributed error for a panicking battery OnStart, got %v", err)
	}
}

// TestBatteryRedOnStopPanicContained
// Finding: BatteryManager.StopAll calls lc.OnStop(ctx) bare, and App.Shutdown
// invokes StopAll at app.go:2733 BEFORE lc.Shutdown — so the recover that
// lifecycle.Shutdown applies to drainer panics (lifecycle.go:185-189, which
// does contain app-level OnStop hooks) never sees battery stop hooks. A
// panicking battery OnStop crashes the SIGTERM-driven graceful shutdown of
// the whole process instead of surfacing as Shutdown's firstErr.
// Fix direction: recover per battery in StopAll, recording
// `battery %q stop panicked (panic type %T)` as firstErr (StopAll already
// collects firstErr and keeps draining the remaining batteries).
func TestBatteryRedOnStopPanicContained(t *testing.T) {
	app := redStartHarness()
	app.RegisterBattery(&redPanicBattery{})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	err, panicked := redRun(func() error { return app.Shutdown(context.Background()) })
	if panicked != nil {
		t.Fatalf("SECURITY: [battery-stop-panic] a battery OnStop panic escaped App.Shutdown (panic: %v) — StopAll runs before lc.Shutdown, so the lifecycle drainer recover never covers battery stop hooks and a SIGTERM shutdown crashes the process", panicked)
	}
	if err == nil || !strings.Contains(err.Error(), "panick") {
		t.Fatalf("SECURITY: [battery-stop-panic] App.Shutdown must return an attributed error for a panicking battery OnStop, got %v", err)
	}
}
