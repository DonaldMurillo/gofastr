package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

// ============================================================================
// Plugin System Tests
// ============================================================================

func TestPluginRegistration(t *testing.T) {
	pm := NewPluginManager()
	p := &mockPlugin{name: "logger"}

	if err := pm.Register(p); err != nil {
		t.Fatalf("failed to register plugin: %v", err)
	}

	got, err := pm.Get("logger")
	if err != nil {
		t.Fatalf("failed to get plugin: %v", err)
	}
	if got.Name() != "logger" {
		t.Errorf("expected plugin name %q, got %q", "logger", got.Name())
	}
}

func TestPluginDuplicateRegistration(t *testing.T) {
	pm := NewPluginManager()
	pm.Register(&mockPlugin{name: "auth"})
	if err := pm.Register(&mockPlugin{name: "auth"}); err == nil {
		t.Error("expected error for duplicate plugin name")
	}
}

func TestPluginGetNotFound(t *testing.T) {
	pm := NewPluginManager()
	_, err := pm.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing plugin")
	}
}

func TestPluginInitAll(t *testing.T) {
	pm := NewPluginManager()
	app := NewApp()

	p1 := &mockPlugin{name: "first"}
	p2 := &mockPlugin{name: "second"}

	pm.Register(p1)
	pm.Register(p2)

	if err := pm.InitAll(app); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}
	if !p1.initialized || !p2.initialized {
		t.Error("both plugins should be initialized")
	}
}

func TestPluginAll(t *testing.T) {
	pm := NewPluginManager()
	pm.Register(&mockPlugin{name: "a"})
	pm.Register(&mockPlugin{name: "b"})

	all := pm.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(all))
	}
	if all[0].Name() != "a" || all[1].Name() != "b" {
		t.Errorf("unexpected order: %v", all)
	}
}

func TestPluginWithApp(t *testing.T) {
	app := NewApp()
	p := &mockPlugin{name: "via-app"}
	app.RegisterPlugin(p)

	got, err := app.Plugins.Get("via-app")
	if err != nil {
		t.Fatalf("failed to get plugin: %v", err)
	}
	if got.Name() != "via-app" {
		t.Errorf("expected plugin name %q, got %q", "via-app", got.Name())
	}
}

// ============================================================================
// Integration: Entity + Registry + App
// ============================================================================

func TestIntegrationEntityRegistration(t *testing.T) {
	app := NewApp()
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})

	e, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatalf("entity not found: %v", err)
	}
	if e.GetName() != "posts" {
		t.Errorf("expected entity name %q, got %q", "posts", e.GetName())
	}
	if len(e.GetFields()) != 3 {
		t.Errorf("expected 3 fields, got %d", len(e.GetFields()))
	}
}

func TestIntegrationMiddlewareChain(t *testing.T) {
	app := NewApp()
	var order []string

	app.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1")
			next.ServeHTTP(w, r)
		})
	})

	app.Router.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "pong"})
	}))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/ping")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertJSON(t, map[string]string{"message": "pong"})

	expected := []string{"mw1", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
}

func TestIntegrationCRUDRoutesRegistered(t *testing.T) {
	app := NewApp()
	app.Entity("items", EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String, Required: true},
		},
	})

	e, _ := app.Registry.Get("items")
	// We can't test actual DB operations without a database,
	// but we can verify the entity was configured correctly
	if e.GetName() != "items" {
		t.Errorf("expected entity name 'items', got %q", e.GetName())
	}
	if e.GetTable() != "items" {
		t.Errorf("expected table 'items', got %q", e.GetTable())
	}
}

func TestIntegrationEventsOnCRUD(t *testing.T) {
	app := NewApp()

	var received []string
	app.Events().On(EntityCreated, func(ctx context.Context, event Event) error {
		received = append(received, string(event.Type))
		return nil
	})

	err := app.Events().Emit(context.Background(), Event{
		Type: EntityCreated,
		Data: map[string]any{"entity": "posts", "id": "123"},
	})
	if err != nil {
		t.Fatalf("failed to emit event: %v", err)
	}
	if len(received) != 1 || received[0] != string(EntityCreated) {
		t.Errorf("expected [created], got %v", received)
	}
}

func TestIntegrationHookRegistry(t *testing.T) {
	app := NewApp()
	hooks := app.HookRegistry("posts")

	var calls []string
	hooks.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		calls = append(calls, "before")
		return nil
	})
	hooks.RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
		calls = append(calls, "after")
		return nil
	})

	hooks.ExecuteHooks(context.Background(), BeforeCreate, nil)
	hooks.ExecuteHooks(context.Background(), AfterCreate, nil)

	if len(calls) != 2 || calls[0] != "before" || calls[1] != "after" {
		t.Errorf("expected [before after], got %v", calls)
	}
}

// ============================================================================
// Test Harness Tests
// ============================================================================

func TestHarnessCreation(t *testing.T) {
	app := NewApp()
	ta := TestHarness(t, app)
	defer ta.Close()

	if ta.App != app {
		t.Error("TestApp should wrap the provided App")
	}
}

func TestHarnessGetRequest(t *testing.T) {
	app := NewApp()
	app.Router.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "pong"})
	}))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/ping")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertJSON(t, map[string]string{"message": "pong"})
}

func TestHarnessPostRequest(t *testing.T) {
	app := NewApp()
	app.Router.Post("/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Post("/echo", map[string]string{"hello": "world"})
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "world")
}

func TestHarnessDeleteRequest(t *testing.T) {
	app := NewApp()
	app.Router.Delete("/items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Delete("/items/42")
	resp.AssertStatus(t, http.StatusNoContent)
}

// ============================================================================
// Mock Plugin
// ============================================================================

type mockPlugin struct {
	name        string
	initialized bool
}

func (p *mockPlugin) Name() string        { return p.name }
func (p *mockPlugin) Init(app *App) error {
	p.initialized = true
	return nil
}
