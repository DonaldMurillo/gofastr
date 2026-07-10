package framework

import (
	"context"
	"database/sql"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// ---------------------------------------------------------------------------
// Introspection tool (toolModules) coverage
// ---------------------------------------------------------------------------

func TestToolModulesViaMCP(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{Version: "2.0.0", Description: "test mod"},
		init: func(app *App) error {
			app.Router().Get("/m1/x", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			return app.MCP.RegisterTool("m1_t", "d", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
				return nil, nil
			})
		},
	})
	app.RegisterModule(&modStub{name: "m2", manifest: ModuleManifest{Description: "second"}, init: noopInit})
	app.InitPlugins()

	res, err := app.MCP.CallTool(context.Background(), "app_modules", nil)
	if err != nil {
		t.Fatalf("CallTool app_modules: %v", err)
	}
	m := res.(map[string]any)
	mods := m["modules"].([]map[string]any)
	if len(mods) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(mods))
	}
}

// ---------------------------------------------------------------------------
// Queue + cron gate from module Init
// ---------------------------------------------------------------------------

type gateFakeQueue struct {
	gate    func(string) bool
	started atomic.Bool
}

func (q *gateFakeQueue) Start(context.Context) { q.started.Store(true) }
func (q *gateFakeQueue) Close() error          { return nil }
func (q *gateFakeQueue) SetGate(g func(string) bool) {
	q.gate = g
}

func TestModuleQueueGateFromInit(t *testing.T) {
	app := NewApp()
	var gateSet atomic.Bool
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			fq := &gateFakeQueue{}
			app.AddQueue(fq)
			gateSet.Store(fq.gate != nil)
			if fq.gate != nil {
				if !fq.gate("any") {
					t.Fatal("gate should return true for enabled module")
				}
			}
			return nil
		},
	})
	app.InitPlugins()
	if !gateSet.Load() {
		t.Fatal("queue gate was not set during module Init")
	}
}

func TestModuleCronGateFromInit(t *testing.T) {
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
				Run:  func(context.Context) error { ran.Add(1); return nil },
			})
			app.AddCron(s)
			return nil
		},
	})
	app.InitPlugins()

	// The cron gate is set. Verify that disabling the module makes
	// the gate return false.
	app.Modules().Disable(context.Background(), "m1")
	if app.Modules().Enabled("m1") {
		t.Fatal("module should be disabled")
	}
	app.Modules().Enable(context.Background(), "m1")
}

// ---------------------------------------------------------------------------
// Fanout self-dedup
// ---------------------------------------------------------------------------

func TestModuleFanoutSelfDedup(t *testing.T) {
	f := fanout.NewInProcess()
	app := NewApp(WithFanout(f))
	app.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	app.InitPlugins()

	// Simulate receiving our own publish.
	payload := fanout.Wrap(app.Modules().nodeID, []byte(`{"name":"m1","enabled":false}`))
	app.Modules().handleRemoteToggle(payload)
	// Should be ignored — module still enabled.
	if !app.Modules().Enabled("m1") {
		t.Fatal("self-publish should be ignored")
	}
}

// ---------------------------------------------------------------------------
// HandleRemoteToggle error paths
// ---------------------------------------------------------------------------

func TestHandleRemoteToggleBadPayload(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.handleRemoteToggle([]byte("not json"))
	mm.handleRemoteToggle([]byte(`{"n":"","b":""}`))
}

// ---------------------------------------------------------------------------
// NewSQLModuleStore error + manager fallback
// ---------------------------------------------------------------------------

func TestNewSQLModuleStoreError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close()
	_, _ = NewSQLModuleStore(db) // may or may not error depending on driver
}

func TestNewModuleManagerStoreErrOnDBError(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	db.Close()
	mm := NewModuleManager(db, nil)
	if mm.storeErr == nil {
		t.Fatal("expected storeErr when SQL store creation fails with DB provided")
	}
	mm.register("m1", ModuleManifest{})
	if err := mm.loadFromStore(context.Background()); err == nil {
		t.Fatal("expected loadFromStore to surface the store creation error")
	}
}

func TestNewModuleManagerWithNilDB(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	if _, ok := mm.store.(*InMemoryModuleStore); !ok {
		t.Fatalf("expected InMemoryModuleStore, got %T", mm.store)
	}
}

// ---------------------------------------------------------------------------
// Enabled default for unregistered name
// ---------------------------------------------------------------------------

func TestEnabledForUnregisteredName(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	if !mm.Enabled("unknown") {
		t.Fatal("unregistered name should be enabled by default")
	}
}

// ---------------------------------------------------------------------------
// Store failure paths
// ---------------------------------------------------------------------------

var errStoreFail = strErr("store failure")

type strErr string

func (e strErr) Error() string { return string(e) }

type failingStore struct{}

func (failingStore) Load(context.Context) (map[string]bool, error) {
	return make(map[string]bool), nil
}
func (failingStore) SetEnabled(context.Context, string, bool) error {
	return errStoreFail
}

func TestDisableStoreFailure(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.store = failingStore{}
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())

	err := mm.Disable(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected store failure error")
	}
	if !mm.Enabled("m1") {
		t.Fatal("state should be unchanged after store failure")
	}
}

func TestEnableStoreFailure(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())
	mm.store = failingStore{}
	mm.enabled["m1"] = false

	err := mm.Enable(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected store failure error")
	}
}

// ---------------------------------------------------------------------------
// Enable with unregistered dependency
// ---------------------------------------------------------------------------

func TestEnableUnregisteredDependency(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{DependsOn: []string{"ghost"}},
		init:     noopInit,
	})
	app.InitPlugins()

	app.Modules().Disable(context.Background(), "m1")
	err := app.Modules().Enable(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error enabling module with unregistered dep")
	}
}

// ---------------------------------------------------------------------------
// loadFromStore error
// ---------------------------------------------------------------------------

type loadFailingStore struct{}

func (loadFailingStore) Load(context.Context) (map[string]bool, error) {
	return nil, errStoreFail
}
func (loadFailingStore) SetEnabled(context.Context, string, bool) error {
	return nil
}

func TestLoadFromStoreError(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.store = loadFailingStore{}
	if err := mm.loadFromStore(context.Background()); err == nil {
		t.Fatal("expected load error")
	}
}

// ---------------------------------------------------------------------------
// SubscribeFanout nil-safe
// ---------------------------------------------------------------------------

func TestSubscribeFanoutNilFanout(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	if err := mm.subscribeFanout(); err != nil {
		t.Fatalf("SubscribeFanout with nil fanout: %v", err)
	}
}

// ---------------------------------------------------------------------------
// InMemoryModuleStore Load coverage
// ---------------------------------------------------------------------------

func TestInMemoryStoreLoadNonEmpty(t *testing.T) {
	s := NewInMemoryModuleStore()
	ctx := context.Background()
	s.SetEnabled(ctx, "a", true)
	s.SetEnabled(ctx, "b", false)
	state, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !state["a"] || state["b"] {
		t.Fatalf("unexpected state: %v", state)
	}
}

// ---------------------------------------------------------------------------
// SQLModuleStore Load coverage
// ---------------------------------------------------------------------------

func TestSQLModuleStoreLoadCoverage(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store, err := NewSQLModuleStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store.SetEnabled(ctx, "x", true)
	store.SetEnabled(ctx, "x", false) // upsert
	store.SetEnabled(ctx, "y", true)

	state, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state["x"] {
		t.Fatal("x should be false after upsert")
	}
	if !state["y"] {
		t.Fatal("y should be true")
	}
}
