package framework

import (
	"context"
	"testing"
)

// covPlugin is a no-op plugin used to populate app_plugins.
type covPlugin struct{ name string }

func (p covPlugin) Name() string      { return p.name }
func (p covPlugin) Init(_ *App) error { return nil }

// app_plugins lists registered plugin names.
func TestCovToolPlugins(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterPlugin(covPlugin{name: "cov-plugin"})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_plugins", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	plugins := m["plugins"].([]string)
	found := false
	for _, n := range plugins {
		if n == "cov-plugin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cov-plugin not in %v", plugins)
	}
}

// app_batteries lists registered batteries with deps + init status.
func TestCovToolBatteries(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterBattery(&mockBattery{name: "cov-batt"})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_batteries", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	batts := m["batteries"].([]map[string]any)
	if len(batts) != 1 || batts[0]["name"] != "cov-batt" {
		t.Fatalf("unexpected batteries: %v", batts)
	}
	if batts[0]["initialized"] != true {
		t.Fatalf("battery should be initialized: %v", batts[0])
	}
}

// framework_docs_search returns hits and respects the int limit param.
func TestCovToolDocsSearch(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// "entity" is a near-certain substring across the docs corpus. Use a
	// float64 limit to mirror JSON-decoded MCP params.
	res, err := app.MCP.CallTool(context.Background(), "framework_docs_search",
		map[string]any{"term": "entity", "limit": float64(3)})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	if m["term"] != "entity" {
		t.Fatalf("term echo wrong: %v", m["term"])
	}
	hits := m["hits"].([]map[string]any)
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for 'entity'")
	}
}

// toolDocsSearch accepts int and int64 limit param types (the non-float64
// branches of the type switch).
func TestCovToolDocsSearchLimitTypes(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	// int limit
	if _, err := app.toolDocsSearch(context.Background(),
		map[string]any{"term": "entity", "limit": int(2)}); err != nil {
		t.Fatalf("int limit: %v", err)
	}
	// int64 limit
	if _, err := app.toolDocsSearch(context.Background(),
		map[string]any{"term": "entity", "limit": int64(2)}); err != nil {
		t.Fatalf("int64 limit: %v", err)
	}
}

// framework_docs_get on an unknown topic surfaces the not-found error.
func TestCovToolDocsGetNotFound(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if _, err := app.MCP.CallTool(context.Background(), "framework_docs_get",
		map[string]any{"topic": "does-not-exist-xyz"}); err == nil {
		t.Fatal("expected not-found error")
	}
}
