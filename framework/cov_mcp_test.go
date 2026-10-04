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
