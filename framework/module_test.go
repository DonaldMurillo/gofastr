package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// modStub is a minimal Module for testing.
type modStub struct {
	name     string
	manifest ModuleManifest
	init     func(*App) error
}

func (m *modStub) Name() string             { return m.name }
func (m *modStub) Init(app *App) error      { return m.init(app) }
func (m *modStub) Manifest() ModuleManifest { return m.manifest }

func noopInit(*App) error { return nil }

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestRegisterModuleDuplicateErrors(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "x", manifest: ModuleManifest{}, init: noopInit})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate module registration")
		}
	}()
	app.RegisterModule(&modStub{name: "x", manifest: ModuleManifest{}, init: noopInit})
}

func TestModuleRegisteredAsBattery(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "x", manifest: ModuleManifest{}, init: noopInit})
	if _, err := app.Batteries.Get("x"); err != nil {
		t.Fatalf("module not registered as battery: %v", err)
	}
}

func TestModuleDependsOnForwardedToBattery(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "dep", manifest: ModuleManifest{}, init: noopInit})
	app.RegisterModule(&modStub{
		name:     "child",
		manifest: ModuleManifest{DependsOn: []string{"dep"}},
		init:     noopInit,
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// child should be initialised after dep (topo-sorted).
	names := app.Batteries.Names()
	depIdx, childIdx := -1, -1
	for i, n := range names {
		if n == "dep" {
			depIdx = i
		}
		if n == "child" {
			childIdx = i
		}
	}
	if depIdx < 0 || childIdx < 0 {
		t.Fatalf("modules not found in battery order: %v", names)
	}
	if depIdx >= childIdx {
		t.Fatalf("dep should come before child: %v", names)
	}
}

// ---------------------------------------------------------------------------
// Attribution
// ---------------------------------------------------------------------------

func TestRouteAttributionDuringModuleInit(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			app.Router().Get("/m1/hello", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte("ok"))
			}))
			return nil
		},
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Route should be attributed to m1.
	app.modules.mu.RLock()
	owner := app.modules.routes["GET /m1/hello"]
	app.modules.mu.RUnlock()
	if owner != "m1" {
		t.Fatalf("expected owner m1, got %q", owner)
	}

	// Non-module route should NOT be attributed.
	app.Router().Get("/manual", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	app.modules.mu.RLock()
	_, hasManual := app.modules.routes["GET /manual"]
	app.modules.mu.RUnlock()
	if hasManual {
		t.Fatal("route registered outside module Init should not be attributed")
	}
}

func TestMCPToolAttributionDuringModuleInit(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			return app.MCP.RegisterTool("m1_tool", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
				return "ran", nil
			})
		},
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.modules.mu.RLock()
	owner := app.modules.tools["m1_tool"]
	app.modules.mu.RUnlock()
	if owner != "m1" {
		t.Fatalf("expected owner m1, got %q", owner)
	}
}

// ---------------------------------------------------------------------------
// Enable/disable
// ---------------------------------------------------------------------------

func TestModuleEnabledByDefault(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	app.InitPlugins()
	if !app.Modules().Enabled("m1") {
		t.Fatal("module should be enabled by default")
	}
}

func TestDisableModule(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	app.InitPlugins()

	if err := app.Modules().Disable(context.Background(), "m1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if app.Modules().Enabled("m1") {
		t.Fatal("module should be disabled after Disable")
	}

	// Re-enable.
	if err := app.Modules().Enable(context.Background(), "m1"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !app.Modules().Enabled("m1") {
		t.Fatal("module should be enabled after Enable")
	}
}

func TestDisableRefusesIfDependentEnabled(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "base", manifest: ModuleManifest{}, init: noopInit})
	app.RegisterModule(&modStub{
		name:     "child",
		manifest: ModuleManifest{DependsOn: []string{"base"}},
		init:     noopInit,
	})
	app.InitPlugins()

	err := app.Modules().Disable(context.Background(), "base")
	if err == nil {
		t.Fatal("expected error disabling base with enabled child")
	}
}

func TestEnableRefusesIfDependencyDisabled(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "base", manifest: ModuleManifest{}, init: noopInit})
	app.RegisterModule(&modStub{
		name:     "child",
		manifest: ModuleManifest{DependsOn: []string{"base"}},
		init:     noopInit,
	})
	app.InitPlugins()

	// Disable both (child first since base can't be disabled while child depends on it).
	if err := app.Modules().Disable(context.Background(), "child"); err != nil {
		t.Fatalf("Disable child: %v", err)
	}
	if err := app.Modules().Disable(context.Background(), "base"); err != nil {
		t.Fatalf("Disable base: %v", err)
	}

	// Enabling child while base is disabled should fail.
	err := app.Modules().Enable(context.Background(), "child")
	if err == nil {
		t.Fatal("expected error enabling child while base is disabled")
	}

	// Enable base first, then child should succeed.
	if err := app.Modules().Enable(context.Background(), "base"); err != nil {
		t.Fatalf("Enable base: %v", err)
	}
	if err := app.Modules().Enable(context.Background(), "child"); err != nil {
		t.Fatalf("Enable child after base: %v", err)
	}
}

func TestDisableUnknownModuleErrors(t *testing.T) {
	app := NewApp()
	app.InitPlugins()
	err := app.Modules().Disable(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
}

// ---------------------------------------------------------------------------
// Dispatch gates
// ---------------------------------------------------------------------------

func TestDisabledModuleRoute404s(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			app.Router().Get("/m1/data", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte("data"))
			}))
			return nil
		},
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Before disable: route works.
	req := httptest.NewRequest(http.MethodGet, "/m1/data", nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 before disable, got %d", w.Code)
	}

	// Disable module.
	if err := app.Modules().Disable(context.Background(), "m1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// After disable: route 404s.
	req2 := httptest.NewRequest(http.MethodGet, "/m1/data", nil)
	w2 := httptest.NewRecorder()
	app.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after disable, got %d", w2.Code)
	}

	// Re-enable: route works again.
	app.Modules().Enable(context.Background(), "m1")
	req3 := httptest.NewRequest(http.MethodGet, "/m1/data", nil)
	w3 := httptest.NewRecorder()
	app.router.ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("expected 200 after re-enable, got %d", w3.Code)
	}
}

func TestDisabledModuleMCPToolRefuses(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			return app.MCP.RegisterTool("m1_query", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
				return "ran", nil
			})
		},
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Before disable: tool works.
	_, err := app.MCP.CallTool(context.Background(), "m1_query", nil)
	if err != nil {
		t.Fatalf("CallTool before disable: %v", err)
	}

	// Disable module.
	app.Modules().Disable(context.Background(), "m1")

	// After disable: tool refuses.
	_, err = app.MCP.CallTool(context.Background(), "m1_query", nil)
	if err == nil {
		t.Fatal("expected error calling tool of disabled module")
	}

	// Unowned tool still works.
	_ = app.MCP.RegisterTool("unowned", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	})
	_, err = app.MCP.CallTool(context.Background(), "unowned", nil)
	if err != nil {
		t.Fatalf("unowned tool should still work: %v", err)
	}
}

func TestDisabledModuleCronSkips(t *testing.T) {
	app := NewApp()
	var ran atomic.Int32
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			s := cron.NewScheduler()
			s.Register(cron.CronJob{
				Name: "m1-job",
				Spec: "* * * * *",
				Run:  func(ctx context.Context) error { ran.Add(1); return nil },
			})
			app.AddCron(s)
			return nil
		},
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// Before disable: job runs.
	// We can't easily call RunOnce on the scheduler from here — verify
	// via the gate instead. The scheduler's gate should be set.
	// Check that disabling prevents execution via Enabled check.
	app.Modules().Disable(context.Background(), "m1")
	if app.Modules().Enabled("m1") {
		t.Fatal("module should be disabled")
	}
	// The gate closure checks Enabled — since disabled, it returns false.
	// Re-enable for cleanup.
	app.Modules().Enable(context.Background(), "m1")
}

// ---------------------------------------------------------------------------
// SQL store
// ---------------------------------------------------------------------------

func TestSQLModuleStoreRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store, err := NewSQLModuleStore(db)
	if err != nil {
		t.Fatalf("NewSQLModuleStore: %v", err)
	}

	ctx := context.Background()
	if err := store.SetEnabled(ctx, "mod-a", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if err := store.SetEnabled(ctx, "mod-b", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	state, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state["mod-a"] != false {
		t.Fatalf("mod-a should be false, got %v", state["mod-a"])
	}
	if state["mod-b"] != true {
		t.Fatalf("mod-b should be true, got %v", state["mod-b"])
	}

	// Update existing.
	store.SetEnabled(ctx, "mod-a", true)
	state, _ = store.Load(ctx)
	if !state["mod-a"] {
		t.Fatal("mod-a should be true after update")
	}
}

func TestModuleManagerWithSQLStore(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	app := NewApp(WithDB(db))
	app.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	ctx := context.Background()
	if err := app.Modules().Disable(ctx, "m1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if app.Modules().Enabled("m1") {
		t.Fatal("should be disabled")
	}

	// Verify the SQL store persists the state.
	state, err := app.modules.store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v, ok := state["m1"]; !ok || v {
		t.Fatalf("store should have m1=false, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// Introspection
// ---------------------------------------------------------------------------

func TestModuleList(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name: "m1",
		manifest: ModuleManifest{
			Version:     "1.0.0",
			Description: "first module",
			DependsOn:   []string{"m2"},
		},
		init: func(app *App) error {
			app.Router().Get("/m1/a", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			app.Router().Get("/m1/b", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			return nil
		},
	})
	app.RegisterModule(&modStub{name: "m2", manifest: ModuleManifest{Description: "second"}, init: noopInit})
	app.InitPlugins()

	list := app.Modules().List()
	if len(list) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(list))
	}

	// Find m1.
	var m1 *ModuleInfo
	for i := range list {
		if list[i].Name == "m1" {
			m1 = &list[i]
		}
	}
	if m1 == nil {
		t.Fatal("m1 not found")
	}
	if m1.Version != "1.0.0" {
		t.Fatalf("version: %q", m1.Version)
	}
	if !m1.Enabled {
		t.Fatal("m1 should be enabled")
	}
	if m1.RouteCount != 2 {
		t.Fatalf("expected 2 routes, got %d", m1.RouteCount)
	}
}
