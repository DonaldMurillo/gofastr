package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/handler"
	"github.com/gofastr/gofastr/core/mcp"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/event"
	"github.com/gofastr/gofastr/framework/hook"
)

// ============================================================================
// Test Harness Tests
// ============================================================================

func TestHarnessCreation(t *testing.T) {
	app := NewApp()
	ta := TestHarness(t, app)

	if ta.App != app {
		t.Error("TestApp should wrap the provided App")
	}
	if ta.router == nil {
		t.Error("TestApp router should not be nil")
	}
}

func TestHarnessGetRequest(t *testing.T) {
	app := NewApp()
	app.Router.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "pong"})
	}))

	ta := TestHarness(t, app)
	ta.Get("/ping").
		AssertStatus(t, http.StatusOK).
		AssertHeader(t, "Content-Type", "application/json").
		AssertJSON(t, map[string]string{"message": "pong"})
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
	ta.Post("/echo", map[string]string{"hello": "world"}).
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]string{"hello": "world"})
}

func TestHarnessPutRequest(t *testing.T) {
	app := NewApp()
	app.Router.Put("/items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		body["id"] = id
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))

	ta := TestHarness(t, app)
	ta.Put("/items/42", map[string]string{"name": "updated"}).
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]string{"id": "42", "name": "updated"})
}

func TestHarnessDeleteRequest(t *testing.T) {
	app := NewApp()
	app.Router.Delete("/items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	ta := TestHarness(t, app)
	ta.Delete("/items/42").
		AssertStatus(t, http.StatusNoContent)
}

func TestHarnessRequestBuilder(t *testing.T) {
	app := NewApp()
	app.Router.Get("/auth", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	}))

	ta := TestHarness(t, app)
	ta.Request(http.MethodGet, "/auth", nil).
		WithHeader("Authorization", "Bearer test-token").
		Execute().
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]string{"token": "Bearer test-token"})
}

func TestHarnessAsUser(t *testing.T) {
	app := NewApp()
	app.Router.Get("/me", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := handler.GetUser(r.Context())
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user": user})
	}))

	ta := TestHarness(t, app)

	// Without user → 401
	ta.Get("/me").AssertStatus(t, http.StatusUnauthorized)

	// With user via WithContext
	ctx := handler.SetUser(context.Background(), map[string]string{"id": "user-1"})
	req := ta.Request(http.MethodGet, "/me", nil)
	req.request = req.request.WithContext(ctx)
	req.Execute().AssertStatus(t, http.StatusOK)
}

func TestHarnessAssertBodyContains(t *testing.T) {
	app := NewApp()
	app.Router.Get("/html", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>Hello, World!</h1>"))
	}))

	ta := TestHarness(t, app)
	ta.Get("/html").
		AssertStatus(t, http.StatusOK).
		AssertBodyContains(t, "Hello, World!")
}

func TestHarnessJSONDecode(t *testing.T) {
	app := NewApp()
	app.Router.Get("/data", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"count": 42, "items": []string{"a", "b"}})
	}))

	ta := TestHarness(t, app)
	resp := ta.Get("/data")
	resp.AssertStatus(t, http.StatusOK)

	var result map[string]any
	if err := resp.JSON(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if result["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", result["count"])
	}
}

func TestHarnessFluentChaining(t *testing.T) {
	app := NewApp()
	app.Router.Get("/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// Full fluent API: TestHarness(t, app).Get("/path").AssertStatus(200)
	TestHarness(t, app).
		Get("/health").
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]string{"status": "ok"}).
		AssertHeader(t, "Content-Type", "application/json")
}

// ============================================================================
// Plugin System Tests
// ============================================================================

func TestPluginRegistration(t *testing.T) {
	pm := NewPluginManager()
	p := &testPlugin{name: "logger"}

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

	p1 := &testPlugin{name: "auth"}
	p2 := &testPlugin{name: "auth"}

	if err := pm.Register(p1); err != nil {
		t.Fatalf("first register should succeed: %v", err)
	}
	if err := pm.Register(p2); err == nil {
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

func TestPluginRegisterNil(t *testing.T) {
	pm := NewPluginManager()

	if err := pm.Register(nil); err == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestPluginInitAll(t *testing.T) {
	pm := NewPluginManager()
	app := NewApp()

	p1 := &testPlugin{name: "first"}
	p2 := &testPlugin{name: "second"}

	pm.Register(p1)
	pm.Register(p2)

	if err := pm.InitAll(app); err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}

	if !p1.initialized {
		t.Error("first plugin should be initialized")
	}
	if !p2.initialized {
		t.Error("second plugin should be initialized")
	}
}

func TestPluginInitError(t *testing.T) {
	pm := NewPluginManager()
	app := NewApp()

	p := &testPlugin{name: "failing", initErr: fmt.Errorf("init failed")}
	pm.Register(p)

	if err := pm.InitAll(app); err == nil {
		t.Error("expected error when plugin init fails")
	}
}

func TestPluginHasRoutes(t *testing.T) {
	app := NewApp()

	p := &routesPlugin{name: "custom-routes"}
	app.RegisterPlugin(p)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins failed: %v", err)
	}

	// Verify the route was registered
	ta := TestHarness(t, app)
	ta.Get("/custom/hello").
		AssertStatus(t, http.StatusOK).
		AssertBodyContains(t, "hello from plugin")
}

func TestPluginHasTools(t *testing.T) {
	app := NewApp()

	p := &toolsPlugin{name: "custom-tools"}
	app.RegisterPlugin(p)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins failed: %v", err)
	}

	// Verify the tool was registered
	tools := app.MCP.ListTools()
	found := false
	for _, tool := range tools {
		if tool.Name == "custom_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected custom_tool to be registered in MCP server")
	}
}

func TestPluginHasMiddleware(t *testing.T) {
	app := NewApp()
	var middlewareCalled bool

	p := &middlewarePlugin{
		name: "custom-middleware",
		setup: func(a *App) {
			a.Router.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					middlewareCalled = true
					next.ServeHTTP(w, r)
				})
			})
		},
	}
	app.RegisterPlugin(p)

	// Init plugins FIRST so middleware is added before routes
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins failed: %v", err)
	}

	// Add a route AFTER middleware is registered
	app.Router.Get("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ta := TestHarness(t, app)
	ta.Get("/test").AssertStatus(t, http.StatusOK)

	if !middlewareCalled {
		t.Error("expected middleware plugin to be called")
	}
}

func TestPluginWithApp(t *testing.T) {
	app := NewApp()
	p := &testPlugin{name: "via-app"}

	app.RegisterPlugin(p)

	got, err := app.Plugins.Get("via-app")
	if err != nil {
		t.Fatalf("failed to get plugin: %v", err)
	}
	if got.Name() != "via-app" {
		t.Errorf("expected plugin name %q, got %q", "via-app", got.Name())
	}
}

func TestPluginNames(t *testing.T) {
	pm := NewPluginManager()
	pm.Register(&testPlugin{name: "alpha"})
	pm.Register(&testPlugin{name: "beta"})

	names := pm.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

// ============================================================================
// Integration Tests — Full Request Lifecycle
// ============================================================================

func TestIntegrationDefineEntityRegisterRoutesHTTP(t *testing.T) {
	// Define entity
	app := NewApp()
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})

	// Register a simple route manually (no DB needed)
	app.Router.Get("/posts/count", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"count": 0})
	}))

	// Make HTTP request and assert response
	ta := TestHarness(t, app)
	ta.Get("/posts/count").
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]any{"count": float64(0)})

	// Verify entity was registered
	e, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatalf("entity not found: %v", err)
	}
	if e.GetName() != "posts" {
		t.Errorf("expected entity name %q, got %q", "posts", e.GetName())
	}
	if len(e.GetFields()) != 5 {
		t.Errorf("expected 5 fields (2 user + id + timestamps), got %d", len(e.GetFields()))
	}
}

func TestIntegrationMiddlewareChainOnRoutes(t *testing.T) {
	app := NewApp()

	var order []string

	// Global middleware
	app.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "global-in")
			next.ServeHTTP(w, r)
			order = append(order, "global-out")
		})
	})

	// API group with its own middleware
	api := app.Router.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "api-in")
			next.ServeHTTP(w, r)
			order = append(order, "api-out")
		})
	})

	api.Get("/posts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{"post1", "post2"})
	}))

	ta := TestHarness(t, app)
	ta.Get("/api/posts").AssertStatus(t, http.StatusOK)

	expected := []string{"global-in", "api-in", "handler", "api-out", "global-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("expected order %v, got %v", expected, order)
		}
	}
}

func TestIntegrationPluginRegistrationAndInit(t *testing.T) {
	app := NewApp()

	// Register multiple plugins
	var initOrder []string

	app.RegisterPlugin(&orderPlugin{name: "alpha", initOrder: &initOrder})
	app.RegisterPlugin(&orderPlugin{name: "beta", initOrder: &initOrder})
	app.RegisterPlugin(&orderPlugin{name: "gamma", initOrder: &initOrder})

	// Init plugins
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins failed: %v", err)
	}

	// Verify order
	if len(initOrder) != 3 {
		t.Fatalf("expected 3 inits, got %d", len(initOrder))
	}
	if initOrder[0] != "alpha" || initOrder[1] != "beta" || initOrder[2] != "gamma" {
		t.Errorf("expected init order [alpha, beta, gamma], got %v", initOrder)
	}

	// Verify all plugins are accessible
	for _, name := range []string{"alpha", "beta", "gamma"} {
		p, err := app.Plugins.Get(name)
		if err != nil {
			t.Errorf("plugin %q not found: %v", name, err)
		}
		if p.Name() != name {
			t.Errorf("expected plugin name %q, got %q", name, p.Name())
		}
	}
}

func TestIntegrationPluginWithRoutesAndTools(t *testing.T) {
	app := NewApp()

	// Register a full-featured plugin
	plugin := &fullPlugin{
		name: "analytics",
	}
	app.RegisterPlugin(plugin)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins failed: %v", err)
	}

	// Test the plugin's route
	ta := TestHarness(t, app)
	ta.Get("/analytics/stats").
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]any{"events": float64(0)})

	// Test the plugin's tool
	tools := app.MCP.ListTools()
	found := false
	for _, tool := range tools {
		if tool.Name == "analytics_track" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected analytics_track tool to be registered")
	}
}

func TestIntegrationEventsOnCRUD(t *testing.T) {
	app := NewApp()

	var receivedEvents []event.Event
	app.Events().On(event.EntityCreated, func(ctx context.Context, event event.Event) error {
		receivedEvents = append(receivedEvents, event)
		return nil
	})

	// Emit event (simulating what would happen on create)
	err := app.Events().Emit(context.Background(), event.Event{
		Type: event.EntityCreated,
		Data: map[string]any{"entity": "posts", "id": "123"},
	})
	if err != nil {
		t.Fatalf("failed to emit event: %v", err)
	}

	if len(receivedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(receivedEvents))
	}
	if receivedEvents[0].Type != event.EntityCreated {
		t.Errorf("expected event type %q, got %q", event.EntityCreated, receivedEvents[0].Type)
	}
}

func TestIntegrationHookRegistry(t *testing.T) {
	app := NewApp()

	var hookCalls []string
	hooks := app.HookRegistry("posts")
	hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
		hookCalls = append(hookCalls, "before-create")
		return nil
	})
	hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
		hookCalls = append(hookCalls, "after-create")
		return nil
	})

	// Execute hooks
	err := hooks.ExecuteHooks(context.Background(), hook.BeforeCreate, map[string]any{"title": "test"})
	if err != nil {
		t.Fatalf("ExecuteHooks failed: %v", err)
	}
	err = hooks.ExecuteHooks(context.Background(), hook.AfterCreate, map[string]any{"title": "test"})
	if err != nil {
		t.Fatalf("ExecuteHooks failed: %v", err)
	}

	if len(hookCalls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookCalls))
	}
	if hookCalls[0] != "before-create" || hookCalls[1] != "after-create" {
		t.Errorf("expected [before-create, after-create], got %v", hookCalls)
	}
}

func TestIntegrationHandlerAdapter(t *testing.T) {
	app := NewApp()

	type createPostInput struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}

	type createPostOutput struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}

	h := handler.HandlerAdapter(func(ctx context.Context, in createPostInput) (createPostOutput, error) {
		return createPostOutput{
			ID:     "new-id",
			Title:  in.Title,
			Status: "created",
		}, nil
	})

	app.Router.Post("/posts", h)

	ta := TestHarness(t, app)
	ta.Post("/posts", map[string]string{"title": "Hello", "body": "World"}).
		AssertStatus(t, http.StatusOK).
		AssertJSON(t, map[string]string{
			"id":     "new-id",
			"title":  "Hello",
			"status": "created",
		})
}

// ============================================================================
// Test Plugin Implementations
// ============================================================================

// testPlugin is a minimal plugin for basic registration/init tests.
type testPlugin struct {
	name        string
	initialized bool
	initErr     error
}

func (p *testPlugin) Name() string { return p.name }
func (p *testPlugin) Init(app *App) error {
	p.initialized = true
	return p.initErr
}

// routesPlugin implements HasRoutes.
type routesPlugin struct {
	name string
}

func (p *routesPlugin) Name() string        { return p.name }
func (p *routesPlugin) Init(app *App) error { return nil }
func (p *routesPlugin) RegisterRoutes(r *router.Router) {
	r.Get("/custom/hello", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "hello from plugin"})
	}))
}

// toolsPlugin implements HasTools.
type toolsPlugin struct {
	name string
}

func (p *toolsPlugin) Name() string        { return p.name }
func (p *toolsPlugin) Init(app *App) error { return nil }
func (p *toolsPlugin) RegisterTools(server *mcp.Server) {
	server.RegisterTool("custom_tool", "A custom tool", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]string{"result": "ok"}, nil
	})
}

// middlewarePlugin implements HasMiddleware.
type middlewarePlugin struct {
	name  string
	setup func(a *App)
}

func (p *middlewarePlugin) Name() string        { return p.name }
func (p *middlewarePlugin) Init(app *App) error { return nil }
func (p *middlewarePlugin) RegisterMiddleware(app *App) {
	if p.setup != nil {
		p.setup(app)
	}
}

// orderPlugin tracks initialization order.
type orderPlugin struct {
	name      string
	initOrder *[]string
}

func (p *orderPlugin) Name() string        { return p.name }
func (p *orderPlugin) Init(app *App) error { *p.initOrder = append(*p.initOrder, p.name); return nil }

// fullPlugin implements HasRoutes + HasTools.
type fullPlugin struct {
	name string
}

func (p *fullPlugin) Name() string        { return p.name }
func (p *fullPlugin) Init(app *App) error { return nil }

func (p *fullPlugin) RegisterRoutes(r *router.Router) {
	r.Get("/analytics/stats", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"events": float64(0)})
	}))
}

func (p *fullPlugin) RegisterTools(server *mcp.Server) {
	server.RegisterTool("analytics_track", "Track an analytics event", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"event": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]string{"tracked": "ok"}, nil
	})
}
