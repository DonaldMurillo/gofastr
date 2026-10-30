package framework

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// countingRunner counts the children it starts.
type countingRunner struct {
	inner  Runner
	starts atomic.Int32
}

func (r *countingRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	r.starts.Add(1)
	return r.inner.Start(ctx, spec)
}

// gatedRunner holds Start until gate closes, and keeps the child it
// started.
type gatedRunner struct {
	inner   Runner
	entered chan struct{}
	gate    chan struct{}
	child   atomic.Pointer[RunningChild]
}

func (r *gatedRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	r.entered <- struct{}{}
	<-r.gate
	c, err := r.inner.Start(ctx, spec)
	if err == nil {
		r.child.Store(&c)
	}
	return c, err
}

// childProbe is a battery whose OnStop records whether the module's
// child was still running when batteries began to stop.
type childProbe struct {
	sup        *ProcessModuleSupervisor
	name       string
	liveAtStop atomic.Bool
}

func (*childProbe) Name() string                  { return "child-probe" }
func (*childProbe) Init(*App) error               { return nil }
func (*childProbe) OnStart(context.Context) error { return nil }
func (p *childProbe) OnStop(context.Context) error {
	sl := p.sup.Slot(p.name)
	sl.mu.RLock()
	p.liveAtStop.Store(sl.child != nil)
	sl.mu.RUnlock()
	return nil
}

// App.Shutdown drained a process module's child but left its supervise
// loop running, and the drained exit read as a crash, so the loop started
// a new child during or after shutdown. The drain also ran after the
// batteries had stopped, while the design drains children first so their
// in-flight reverse calls still reach the queue and DB (2026-10-27 S1).
func TestShutdownStopsProcessModules(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	d := descriptorForChild(t, childModeEcho)
	app.RegisterProcessModule(d, ApprovedGrants{"articles:read"})

	sup := app.ProcessModules()
	sup.cfg.PollInterval = 20 * time.Millisecond
	sup.cfg.BackoffMin = 5 * time.Millisecond
	sup.cfg.BackoffMax = 20 * time.Millisecond
	sup.cfg.DrainPerModule = 2 * time.Second
	runner := &countingRunner{inner: &envRunner{inner: &TrustedProcessRunner{}, env: childEnv(childModeEcho, nil)}}
	sup.Slot(d.Name).runner = runner
	probe := &childProbe{sup: sup, name: d.Name}
	app.RegisterBattery(probe)

	_, done := startForTest(t, app)
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatal(err)
	}
	waitForState(t, sup, d.Name, StateReady, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done
	if probe.liveAtStop.Load() {
		t.Error("batteries stopped while the module's child was still running")
	}
	// Ten poll intervals: long enough for a live loop to respawn.
	time.Sleep(200 * time.Millisecond)
	if n := runner.starts.Load(); n != 1 {
		t.Errorf("started %d children, want 1: shutdown restarted the drained module", n)
	}
	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	child, state := sl.child, sl.state
	sl.mu.RUnlock()
	if child != nil {
		t.Errorf("a child is running after Shutdown (state %s)", state)
	}
	if state == StateCrashed {
		t.Error("the drained exit was recorded as a crash")
	}
}

// heldWatchRunner starts children whose first Wait, the exit watcher the
// supervisor starts at spawn, returns only once gate closes. Every later
// Wait passes straight through.
type heldWatchRunner struct {
	inner Runner
	gate  chan struct{}
}

func (r *heldWatchRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	c, err := r.inner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &heldWatchChild{RunningChild: c, gate: r.gate}, nil
}

type heldWatchChild struct {
	RunningChild
	gate  chan struct{}
	waits atomic.Int32
}

func (c *heldWatchChild) Wait() error {
	err := c.RunningChild.Wait()
	if c.waits.Add(1) == 1 {
		<-c.gate
	}
	return err
}

// The shutdown drain waited for the child to exit but left it in the slot
// for the exit watcher to clear, so batteries could stop while the slot
// still held a child that had already exited, and the window grew with
// load. Drain now settles the slot itself, as a disable drain does.
func TestDrainClearsTheSlotBeforeBatteriesStop(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	d := descriptorForChild(t, childModeEcho)
	app.RegisterProcessModule(d, ApprovedGrants{"articles:read"})

	sup := app.ProcessModules()
	sup.cfg.PollInterval = 20 * time.Millisecond
	sup.cfg.DrainPerModule = 2 * time.Second
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	sup.Slot(d.Name).runner = &heldWatchRunner{
		inner: &envRunner{inner: &TrustedProcessRunner{}, env: childEnv(childModeEcho, nil)},
		gate:  gate,
	}
	probe := &childProbe{sup: sup, name: d.Name}
	app.RegisterBattery(probe)

	_, done := startForTest(t, app)
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatal(err)
	}
	waitForState(t, sup, d.Name, StateReady, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done
	if probe.liveAtStop.Load() {
		t.Error("batteries stopped while the slot still held the drained child")
	}
	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	peer, codec, lastExit := sl.peer, sl.codec, sl.lastExit
	sl.mu.RUnlock()
	if peer != nil || codec != nil || lastExit != "drained" {
		t.Errorf("after Shutdown: peer kept %t, codec kept %t, last exit %q, want neither and drained", peer != nil, codec != nil, lastExit)
	}
}

// Close killed only the child a slot already held, so a spawn still
// inside the runner when Close ran installed its child afterwards and it
// ran on with nothing left to stop it (2026-10-27 S48).
func TestCloseRefusesALateChild(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	runner := &gatedRunner{
		inner:   &envRunner{inner: &TrustedProcessRunner{}, env: childEnv(childModeEcho, nil)},
		entered: make(chan struct{}, 1),
		gate:    make(chan struct{}),
	}
	t.Cleanup(func() {
		if c := runner.child.Load(); c != nil {
			_ = (*c).Kill()
		}
	})
	sup := newBareTestSupervisor(t, newTestStore(t), runner)
	d := descriptorForChild(t, childModeEcho)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatal(err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the spawn never reached the runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.Close(ctx); err != nil {
		t.Fatal(err)
	}
	close(runner.gate)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sl := sup.Slot(d.Name)
		sl.mu.RLock()
		child, state := sl.child, sl.state
		sl.mu.RUnlock()
		if child != nil || state == StateReady {
			t.Fatalf("a child started after Close was installed (state %s)", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runner.child.Load() == nil {
		t.Fatal("the runner never started the late child, so the refusal went untested")
	}
}
