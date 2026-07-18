package framework

import (
	"context"
	"net/http"
	"strings"
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
// installs the introspection + docs tools.
func TestMCPIntrospectionRegistersTools(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	want := map[string]bool{
		"app_routes":            false,
		"app_plugins":           false,
		"app_batteries":         false,
		"app_config":            false,
		"app_readiness":         false,
		"framework_docs_list":   false,
		"framework_docs_get":    false,
		"framework_docs_search": false,
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

// TestMCPFrameworkDocsListReturnsTopics verifies framework_docs_list
// surfaces the embedded markdown tree.
func TestMCPFrameworkDocsListReturnsTopics(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "framework_docs_list", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool framework_docs_list: %v", err)
	}
	m := result.(map[string]any)
	count := m["count"].(int)
	if count == 0 {
		t.Fatal("framework_docs_list returned 0 topics — embed broken?")
	}
}

// TestMCPFrameworkDocsCapabilityMapDiscovery pins parity between the embedded
// docs, task-oriented search, and the live app's MCP discovery surface.
func TestMCPFrameworkDocsCapabilityMapDiscovery(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "framework_docs_get",
		map[string]any{"topic": "ui-capability-map"})
	if err != nil {
		t.Fatalf("framework_docs_get ui-capability-map: %v", err)
	}
	if markdown, _ := result.(map[string]any)["markdown"].(string); !strings.Contains(markdown, "Live dashboards") {
		t.Fatalf("MCP capability map is missing live-dashboard guidance")
	}

	result, err = app.MCP.CallTool(context.Background(), "framework_docs_search",
		map[string]any{"term": "live dashboard"})
	if err != nil {
		t.Fatalf("framework_docs_search: %v", err)
	}
	hits := result.(map[string]any)["hits"].([]map[string]any)
	found := false
	for _, hit := range hits {
		if hit["topic"] == "ui-capability-map" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("MCP search did not route live dashboard to ui-capability-map: %+v", hits)
	}
}

// TestMCPFrameworkDocsGetReadsTopic exercises a known topic round-trip.
func TestMCPFrameworkDocsGetReadsTopic(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// List first to discover a real topic name (avoids hardcoding).
	list, err := app.MCP.CallTool(context.Background(), "framework_docs_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	topics := list.(map[string]any)["topics"].([]map[string]any)
	if len(topics) == 0 {
		t.Skip("no embedded topics")
	}
	name := topics[0]["name"].(string)

	result, err := app.MCP.CallTool(context.Background(), "framework_docs_get", map[string]any{"topic": name})
	if err != nil {
		t.Fatalf("CallTool framework_docs_get: %v", err)
	}
	m := result.(map[string]any)
	if md, _ := m["markdown"].(string); md == "" {
		t.Errorf("markdown body empty for topic %q", name)
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

// TestMCPAppReadinessAllOKReportsReady catches the regression where the
// aggregate "ready" bool was derived from a Status field the underlying
// helper never sets, so all-passing checks still reported ready=false.
func TestMCPAppReadinessAllOKReportsReady(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterReadiness("good", func(_ context.Context) error { return nil })
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "app_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	if m["ready"] != true {
		t.Errorf("ready = %v, want true (all checks pass)", m["ready"])
	}
}

// TestMCPAppReadinessNoChecksNotReady pins that an app with no
// readiness checks registered reports ready=false + a reason — rather
// than silently reporting ready=true, which would hide a wiring miss.
func TestMCPAppReadinessNoChecksNotReady(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "app_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	if m["ready"] != false {
		t.Errorf("ready = %v, want false (no checks registered)", m["ready"])
	}
	if _, ok := m["reason"]; !ok {
		t.Error("expected `reason` field explaining the not-ready state")
	}
}

// TestMCPAppReadinessRedactsEvenWhenVerbose pins that app_readiness
// does NOT honour the App's verbose-readiness flag — /mcp may have a
// different trust boundary than /readyz, so raw error text must never
// leak through introspection.
func TestMCPAppReadinessRedactsEvenWhenVerbose(t *testing.T) {
	app := NewApp(
		WithMCPIntrospection(),
		WithVerboseReadiness(),
	)
	app.RegisterReadiness("bad", func(_ context.Context) error {
		return errSentinel
	})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	result, err := app.MCP.CallTool(context.Background(), "app_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	checks := m["checks"].([]map[string]any)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
	got, _ := checks[0]["error"].(string)
	if got == errSentinel.Error() {
		t.Errorf("error leaked raw text %q under verbose flag — /mcp must redact", got)
	}
}

type sentinelErr struct{}

func (sentinelErr) Error() string { return "intentional failure" }

var errSentinel = sentinelErr{}
