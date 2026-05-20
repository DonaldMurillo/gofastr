package framework

import (
	"context"
	"fmt"
	"testing"
)

// mockBattery is a test Battery implementation that records lifecycle calls.
type mockBattery struct {
	name       string
	initCalled bool
	initErr    error
	started    bool
	startErr   error
	stopped    bool
	stopErr    error
	routes     []string
	middleware bool
	hooks      bool
	tools      bool
}

func (m *mockBattery) Name() string { return m.name }
func (m *mockBattery) Init(app *App) error {
	m.initCalled = true
	return m.initErr
}
func (m *mockBattery) OnStart(ctx context.Context) error {
	m.started = true
	return m.startErr
}
func (m *mockBattery) OnStop(ctx context.Context) error {
	m.stopped = true
	return m.stopErr
}

// Verify mockBattery satisfies the interfaces at compile time.
var _ BatteryLifecycle = (*mockBattery)(nil)

func TestBatteryManager_RegisterAndInit(t *testing.T) {
	bm := NewBatteryManager()
	b1 := &mockBattery{name: "alpha"}

	if err := bm.Register(b1); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := bm.Register(&mockBattery{name: "beta"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	app := &App{}
	if err := bm.InitAll(app); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	if !b1.initCalled {
		t.Fatal("alpha should be initialized")
	}
}

func TestBatteryManager_DuplicateName(t *testing.T) {
	bm := NewBatteryManager()
	_ = bm.Register(&mockBattery{name: "dup"})
	err := bm.Register(&mockBattery{name: "dup"})
	if err == nil {
		t.Fatal("expected error for duplicate battery name")
	}
}

func TestBatteryManager_NilBattery(t *testing.T) {
	bm := NewBatteryManager()
	err := bm.Register(nil)
	if err == nil {
		t.Fatal("expected error for nil battery")
	}
}

func TestBatteryManager_DependencyOrder(t *testing.T) {
	bm := NewBatteryManager()
	var order []string

	a := &depsBattery{name: "alpha", onInit: func() { order = append(order, "alpha") }}
	b := &depsBattery{name: "beta", onInit: func() { order = append(order, "beta") }}
	c := &depsBattery{name: "gamma", onInit: func() { order = append(order, "gamma") }}

	// gamma depends on beta, beta depends on alpha
	_ = bm.Register(a)
	_ = bm.Register(b, "alpha")
	_ = bm.Register(c, "beta")

	app := &App{}
	if err := bm.InitAll(app); err != nil {
		t.Fatalf("InitAll: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 inits, got %d", len(order))
	}
	if order[0] != "alpha" || order[1] != "beta" || order[2] != "gamma" {
		t.Fatalf("expected [alpha beta gamma], got %v", order)
	}
}

func TestBatteryManager_MissingDependency(t *testing.T) {
	bm := NewBatteryManager()
	_ = bm.Register(&mockBattery{name: "orphan"}, "nonexistent")

	app := &App{}
	err := bm.InitAll(app)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestBatteryManager_CircularDependency(t *testing.T) {
	bm := NewBatteryManager()
	_ = bm.Register(&mockBattery{name: "a"}, "b")
	_ = bm.Register(&mockBattery{name: "b"}, "a")

	app := &App{}
	err := bm.InitAll(app)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}

func TestBatteryManager_StartStop(t *testing.T) {
	bm := NewBatteryManager()
	b := &mockBattery{name: "lifecycle"}
	_ = bm.Register(b)

	app := &App{}
	if err := bm.InitAll(app); err != nil {
		t.Fatalf("InitAll: %v", err)
	}

	ctx := context.Background()
	if err := bm.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if !b.started {
		t.Fatal("battery should be started")
	}

	if err := bm.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if !b.stopped {
		t.Fatal("battery should be stopped")
	}
}

func TestBatteryManager_StopReverseOrder(t *testing.T) {
	bm := NewBatteryManager()
	var stopOrder []string

	a := &depsBattery{name: "alpha", onStop: func() { stopOrder = append(stopOrder, "alpha") }}
	b := &depsBattery{name: "beta", onStop: func() { stopOrder = append(stopOrder, "beta") }}

	_ = bm.Register(a)
	_ = bm.Register(b, "alpha")

	app := &App{}
	_ = bm.InitAll(app)

	ctx := context.Background()
	_ = bm.StartAll(ctx)
	_ = bm.StopAll(ctx)

	// beta (dependent) stops before alpha (dependency)
	if len(stopOrder) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(stopOrder))
	}
	if stopOrder[0] != "beta" || stopOrder[1] != "alpha" {
		t.Fatalf("expected [beta alpha] stop order, got %v", stopOrder)
	}
}

func TestBatteryManager_Get(t *testing.T) {
	bm := NewBatteryManager()
	b := &mockBattery{name: "finder"}
	_ = bm.Register(b)

	found, err := bm.Get("finder")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found.Name() != "finder" {
		t.Fatalf("expected finder, got %q", found.Name())
	}

	_, err = bm.Get("missing")
	if err == nil {
		t.Fatal("expected error for missing battery")
	}
}

func TestBatteryManager_GetAs(t *testing.T) {
	bm := NewBatteryManager()
	b := &mockBattery{name: "typed"}
	_ = bm.Register(b)

	typed, err := GetAs[*mockBattery](bm, "typed")
	if err != nil {
		t.Fatalf("GetAs: %v", err)
	}
	if typed.name != "typed" {
		t.Fatalf("expected typed, got %q", typed.name)
	}
}

func TestBatteryManager_Names(t *testing.T) {
	bm := NewBatteryManager()
	_ = bm.Register(&mockBattery{name: "delta"})
	_ = bm.Register(&mockBattery{name: "charlie"}, "delta")

	app := &App{}
	_ = bm.InitAll(app)

	names := bm.Names()
	if len(names) != 2 || names[0] != "delta" || names[1] != "charlie" {
		t.Fatalf("expected [delta charlie], got %v", names)
	}
}

func TestBatteryManager_InitError(t *testing.T) {
	bm := NewBatteryManager()
	b := &mockBattery{name: "failer", initErr: fmt.Errorf("boom")}
	_ = bm.Register(b)

	app := &App{}
	err := bm.InitAll(app)
	if err == nil {
		t.Fatal("expected init error")
	}
	if err.Error() != `battery "failer" init failed: boom` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApp_RegisterBattery(t *testing.T) {
	app := NewApp()
	b := &mockBattery{name: "via-app"}
	app.RegisterBattery(b)

	if len(app.Batteries.entries) != 1 {
		t.Fatal("battery should be registered")
	}
}

func TestApp_RegisterBatteryPanicsOnDup(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate battery")
		}
	}()
	app := NewApp()
	app.RegisterBattery(&mockBattery{name: "dup"})
	app.RegisterBattery(&mockBattery{name: "dup"})
}

func TestApp_InitPlugins_WithBatteries(t *testing.T) {
	app := NewApp()
	b := &mockBattery{name: "inited"}
	app.RegisterBattery(b)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if !b.initCalled {
		t.Fatal("battery should be initialized via InitPlugins")
	}
}

// depsBattery is a configurable battery for testing dependency ordering.
type depsBattery struct {
	name   string
	onInit func()
	onStop func()
}

func (d *depsBattery) Name() string              { return d.name }
func (d *depsBattery) Init(_ *App) error {
	if d.onInit != nil {
		d.onInit()
	}
	return nil
}
func (d *depsBattery) OnStart(_ context.Context) error { return nil }
func (d *depsBattery) OnStop(_ context.Context) error {
	if d.onStop != nil {
		d.onStop()
	}
	return nil
}
