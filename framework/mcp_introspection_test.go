package framework

import (
	"context"
	"net/http"
	"testing"
)

// TestMCPIntrospectionDisabledByDefault pins the opt-in contract:
// NewApp without WithMCPIntrospection() registers no introspection
// tools.
func TestMCPIntrospectionDisabledByDefault(t *testing.T) {
	app := NewApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		switch tool.Name {
		case "app_routes", "app_plugins", "app_batteries", "app_config", "app_readiness":
			t.Errorf("introspection tool %q registered without WithMCPIntrospection()", tool.Name)
		}
	}
}

// TestMCPIntrospectionRegistersTools pins that WithMCPIntrospection()
// installs all five tools.
func TestMCPIntrospectionRegistersTools(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	want := map[string]bool{
		"app_routes":    false,
		"app_plugins":   false,
		"app_batteries": false,
		"app_config":    false,
		"app_readiness": false,
	}
	for _, tool := range app.MCP.ListTools() {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("introspection tool %q was not registered", name)
		}
	}
}

// TestMCPAppRoutesReturnsRegisteredRoutes pins app_routes against a few
// hand-registered routes.
func TestMCPAppRoutesReturnsRegisteredRoutes(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.Router().Get("/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	app.Router().Post("/users", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	result, err := app.MCP.CallTool(context.Background(), "app_routes", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	routes := m["routes"].([]map[string]any)

	want := map[string]string{"/health": "GET", "/users": "POST"}
	got := map[string]string{}
	for _, r := range routes {
		got[r["pattern"].(string)] = r["method"].(string)
	}
	for pattern, method := range want {
		if got[pattern] != method {
			t.Errorf("missing %s %s; got=%v", method, pattern, got)
		}
	}
}

// TestMCPAppConfigReturnsSnapshot pins app_config against a set
// AppConfig.
func TestMCPAppConfigReturnsSnapshot(t *testing.T) {
	app := NewApp(
		WithMCPIntrospection(),
		WithConfig(AppConfig{Name: "introspect-test", DebugEndpoints: true}),
	)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "app_config", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "introspect-test" {
		t.Errorf("name = %v, want introspect-test", m["name"])
	}
	if m["debug_endpoints"] != true {
		t.Errorf("debug_endpoints = %v, want true", m["debug_endpoints"])
	}
}

// TestMCPAppReadinessReportsChecks pins app_readiness round-trips the
// registered checks correctly.
func TestMCPAppReadinessReportsChecks(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterReadiness("good", func(_ context.Context) error { return nil })
	app.RegisterReadiness("bad", func(_ context.Context) error { return errSentinel })
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	result, err := app.MCP.CallTool(context.Background(), "app_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	if m["ready"] != false {
		t.Errorf("ready = %v, want false (one check fails)", m["ready"])
	}
	checks := m["checks"].([]map[string]any)
	statusByName := map[string]string{}
	for _, c := range checks {
		statusByName[c["name"].(string)] = c["status"].(string)
	}
	if statusByName["good"] != "ok" {
		t.Errorf("good check status = %v, want ok", statusByName["good"])
	}
	if statusByName["bad"] != "error" {
		t.Errorf("bad check status = %v, want error", statusByName["bad"])
	}
}

type sentinelErr struct{}

func (sentinelErr) Error() string { return "intentional failure" }

var errSentinel = sentinelErr{}
