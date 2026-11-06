package framework

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// closeCounter wraps a listener and counts Close calls.
type closeCounter struct {
	*pipeListener
	closes atomic.Int32
}

func (c *closeCounter) Close() error {
	c.closes.Add(1)
	return c.pipeListener.Close()
}

// A Shutdown during boot reaches the HTTP host before its Run: Stop
// releases the caller's listener, no server is ever adopted, and neither
// the banner nor OnReady runs. Serve still closes the listener exactly
// once and returns nil.
func TestServeStopDuringBootReleasesListener(t *testing.T) {
	app := quietServeApp()
	banner := app.startupOutput.(*bytes.Buffer)
	var readyRan atomic.Bool
	app.OnReady(func(string) { readyRan.Store(true) })
	entered := make(chan struct{})
	app.OnStart(func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	ln := &closeCounter{pipeListener: newPipeListener()}
	done := make(chan error, 1)
	go func() { done <- app.Serve(ln) }()
	<-entered

	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(sctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// Stop ran inside the teardown, before Serve returned.
	if !ln.isClosed() {
		t.Fatal("Shutdown returned with the caller's listener still open")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	if n := ln.closes.Load(); n != 1 {
		t.Fatalf("listener closed %d times, want 1", n)
	}
	app.serverMu.Lock()
	srv, wrapped := app.server, app.serverWrapped
	app.serverMu.Unlock()
	if srv != nil || wrapped {
		t.Fatalf("a stopped boot built a server (server %v, wrapped %v)", srv != nil, wrapped)
	}
	if readyRan.Load() || banner.Len() != 0 {
		t.Fatalf("OnReady ran %v, banner %q", readyRan.Load(), banner.String())
	}
}

// stopAtValidation requests a Shutdown from the last boot phase and
// returns once the stop is recorded: the window between the final phase
// and the host's start.
type stopAtValidation struct{ app *App }

func (stopAtValidation) Mount(*router.Router) {}

func (v stopAtValidation) ValidateBoot() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = v.app.Shutdown(ctx)
	}()
	for !v.app.stopRequestedNow() {
		time.Sleep(time.Millisecond)
	}
}

type countingHost struct{ runs, stops atomic.Int32 }

func (h *countingHost) Run(ctx context.Context, _ *Session) error {
	h.runs.Add(1)
	<-ctx.Done()
	return nil
}

func (h *countingHost) Stop(context.Context) error { h.stops.Add(1); return nil }

// A Shutdown that lands after the last boot phase but before the host
// starts is caught where boot ends: the host never runs, ServicesReady
// never closes, and the teardown still completes.
func TestStopAtEndOfBootSkipsHost(t *testing.T) {
	app := quietServeApp()
	app.Mount(stopAtValidation{app: app})
	var stopped atomic.Int32
	app.OnStop(func() error { stopped.Add(1); return nil })
	host := &countingHost{}
	done := make(chan error, 1)
	go func() { done <- app.Run(context.Background(), host) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after a stop at the end of boot")
	}
	select {
	case <-app.ServicesReady():
		t.Fatal("ServicesReady closed after Shutdown began")
	default:
	}
	if host.runs.Load() != 0 || host.stops.Load() != 1 || stopped.Load() != 1 {
		t.Fatalf("host runs %d, host stops %d, OnStop %d", host.runs.Load(), host.stops.Load(), stopped.Load())
	}
}

// gatedStartBattery's OnStart blocks until release closes, then returns
// nil, ignoring its cancelled context.
type gatedStartBattery struct{ entered, release chan struct{} }

func (*gatedStartBattery) Name() string    { return "gated-start" }
func (*gatedStartBattery) Init(*App) error { return nil }
func (b *gatedStartBattery) OnStart(context.Context) error {
	close(b.entered)
	<-b.release
	return nil
}
func (*gatedStartBattery) OnStop(context.Context) error { return nil }

// A Shutdown that lands while a battery's OnStart runs stops
// runStartHooks before the process-module supervisor starts its loops,
// even when that OnStart then returns nil. The check right after
// Batteries.StartAll is the only guard in front of StartLoops: the
// per-hook checks cover only the OnStart hooks after it, so an OnStart
// hook that never runs proves nothing about the loops. A loop started
// there would spawn module children after the stop.
func TestStopDuringBatteryStartSkipsProcessModuleLoops(t *testing.T) {
	app := quietServeApp()
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{Store: newTestStore(t), Broker: NopBroker{}})
	if err != nil {
		t.Fatal(err)
	}
	app.processModules = sup
	bat := &gatedStartBattery{entered: make(chan struct{}), release: make(chan struct{})}
	app.RegisterBattery(bat)
	var hookRan atomic.Bool
	app.OnStart(func(context.Context) error { hookRan.Store(true); return nil })
	done := make(chan error, 1)
	go func() { done <- app.Run(context.Background(), ServicesHost()) }()
	<-bat.entered

	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopped <- app.Shutdown(ctx)
	}()
	for !app.stopRequestedNow() {
		time.Sleep(time.Millisecond)
	}
	close(bat.release)
	for name, ch := range map[string]chan error{"Shutdown": stopped, "Run": done} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not return", name)
		}
	}
	if sup.loopsStarted.Load() {
		t.Fatal("process-module loops started after Shutdown began")
	}
	if hookRan.Load() {
		t.Fatal("an OnStart hook ran after Shutdown began")
	}
}

// A panic in the deferred start still ends the phase. Left marked
// running, it would take every later Shutdown's teardown and never run
// it. Hooks and batteries recover their own panics, so the panic comes
// from a supervisor slot StartLoops cannot start. shutdownMu is held so
// the Shutdown the failure triggers waits until the slot is removed;
// the teardown's Drain would fault on it too.
func TestDeferredStartPanicEndsThePhase(t *testing.T) {
	app := quietServeApp()
	sup, err := NewProcessModuleSupervisor(SupervisorConfig{Store: newTestStore(t), Broker: NopBroker{}})
	if err != nil {
		t.Fatal(err)
	}
	sup.slots["faulty"] = nil
	app.processModules = sup
	var stopped atomic.Int32
	app.OnStop(func() error { stopped.Add(1); return nil })

	app.shutdownMu.Lock()
	panicked := func() (v any) {
		defer func() { v = recover() }()
		app.startDeferred()
		return nil
	}()
	app.serverMu.Lock()
	running := app.deferredStarting
	app.serverMu.Unlock()
	sup.mu.Lock()
	delete(sup.slots, "faulty")
	sup.mu.Unlock()
	app.shutdownMu.Unlock()

	if panicked == nil {
		t.Fatal("the faulty slot did not panic the deferred start")
	}
	if running {
		t.Fatal("a panic left the deferred start marked running")
	}
	deadline := time.Now().Add(10 * time.Second)
	for stopped.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stopped.Load() != 1 {
		t.Fatalf("the panicked deferred start did not shut the App down (OnStop ran %d times)", stopped.Load())
	}
	select {
	case <-app.ServicesReady():
		t.Fatal("ServicesReady closed after the deferred start panicked")
	default:
	}
}

// A Shutdown that completes after boot but before the HTTP host adopts
// its server (here from a WrapServer wrapper, which runs while the
// handler is composed) leaves Start nothing to serve: no server is
// adopted, OnReady never fires, and Start returns nil.
func TestStartAdoptsNoServerAfterShutdown(t *testing.T) {
	app := quietServeApp()
	var readyRan atomic.Bool
	app.OnReady(func(string) { readyRan.Store(true) })
	app.WrapServer(func(h http.Handler) http.Handler {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		return h
	})
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(10 * time.Second):
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv != nil {
			_ = srv.Shutdown(sctx)
		}
		t.Fatal("Start served after Shutdown had finished")
	}
	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()
	if srv != nil || readyRan.Load() {
		t.Fatalf("server adopted %v, OnReady ran %v", srv != nil, readyRan.Load())
	}
}

// sqliteCoded mimics modernc.org/sqlite's *Error: a result code behind a
// Code method. A cancelled BEGIN comes back as code 9, SQLITE_INTERRUPT.
// The real error cannot be produced on demand (the interrupt lands only
// inside the BEGIN itself); the race tests in host_run_test.go meet it.
type sqliteCoded int

func (c sqliteCoded) Error() string { return fmt.Sprintf("sqlite (%d)", int(c)) }
func (c sqliteCoded) Code() int     { return int(c) }

// reportsCanceled reports itself as context.Canceled the way net's dial
// error does, with no Unwrap.
type reportsCanceled struct{}

func (reportsCanceled) Error() string        { return "operation was canceled" }
func (reportsCanceled) Is(target error) bool { return target == context.Canceled }

// stopShaped decides which boot failures a pending stop swallows: only
// errors made entirely of the stop's cancellation.
func TestStopShaped(t *testing.T) {
	independent := errors.New("battery config is invalid")
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"canceled", context.Canceled, true},
		{"wrapped canceled", fmt.Errorf("run seeds: seed notes: %w", context.Canceled), true},
		{"dial canceled", &net.OpError{Op: "dial", Net: "tcp", Err: reportsCanceled{}}, true},
		{"tx rolled back by its context", fmt.Errorf("auto-migrate: migrate: commit: %w", sql.ErrTxDone), true},
		{"sqlite interrupt", fmt.Errorf("auto-migrate: migrate: begin tx: %w", sqliteCoded(9)), true},
		{"joined cancellations", errors.Join(context.Canceled, sql.ErrTxDone), true},
		{"sqlite busy", fmt.Errorf("begin tx: %w", sqliteCoded(5)), false},
		{"deadline", context.DeadlineExceeded, false},
		{"independent", fmt.Errorf("init plugins: %w", independent), false},
		{"joined with an independent failure", errors.Join(context.Canceled, independent), false},
		{"multi-wrap with an independent failure", fmt.Errorf("%w; %w", context.Canceled, independent), false},
		{"message only", errors.New("context canceled"), false},
	} {
		if got := stopShaped(tc.err); got != tc.want {
			t.Errorf("%s: stopShaped(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}
