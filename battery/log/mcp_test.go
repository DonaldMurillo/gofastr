package log_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// helper: build an App with battery/log + EnableMCP and return both.
func newMCPApp(t *testing.T) *framework.App {
	t.Helper()
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "mcptest"}))
	sink := &memSink{}
	app.RegisterPlugin(log.New(log.Config{
		Sinks:       []log.Sink{sink},
		EnableMCP:   true,
		MCPRingSize: 64,
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	return app
}

// TestMCPToolsRegistered pins that all four log tools land on the
// App's MCP server when Config.EnableMCP is set.
func TestMCPToolsRegistered(t *testing.T) {
	app := newMCPApp(t)
	tools := app.MCP.ListTools()
	want := map[string]bool{
		"log_recent":    false,
		"log_filter":    false,
		"log_metrics":   false,
		"log_set_level": false,
	}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("MCP tool %q was not registered", name)
		}
	}
}

// TestMCPToolsDisabledWhenNotEnabled pins that the default Config{}
// does NOT register the MCP tools — opt-in only.
func TestMCPToolsDisabledWhenNotEnabled(t *testing.T) {
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "mcpoff"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{&memSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "log_recent" || tool.Name == "log_filter" ||
			tool.Name == "log_metrics" || tool.Name == "log_set_level" {
			t.Errorf("MCP tool %q registered without EnableMCP", tool.Name)
		}
	}
}

// TestMCPRecentReturnsRingEntries pins that log_recent reads the ring
// buffer and returns entries chronologically.
func TestMCPRecentReturnsRingEntries(t *testing.T) {
	app := newMCPApp(t)
	app.Router().Get("/p", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	for i := 0; i < 3; i++ {
		resp, _ := http.Get(srv.URL + "/p")
		resp.Body.Close()
	}

	result, err := app.MCP.CallTool(context.Background(), "log_recent", map[string]any{"limit": 50})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	entries := m["entries"].([]map[string]any)
	if len(entries) == 0 {
		t.Fatal("no entries returned — ring sink not wired?")
	}
	// At least one entry should be an http.access for /p
	var sawAccess bool
	for _, e := range entries {
		if e["msg"] == "http.access" && e["path"] == "/p" {
			sawAccess = true
			break
		}
	}
	if !sawAccess {
		t.Fatalf("no http.access entry for /p in: %v", entries)
	}
}

// TestMCPFilterByMessageSubstring pins the msg filter on log_filter.
func TestMCPFilterByMessageSubstring(t *testing.T) {
	app := newMCPApp(t)
	app.Logger().Info("worker.tick", "queue", "ingest")
	app.Logger().Info("worker.tock", "queue", "ingest")
	app.Logger().Error("disk.full", "path", "/var/log")

	result, err := app.MCP.CallTool(context.Background(), "log_filter", map[string]any{
		"msg":   "worker",
		"limit": 10,
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := result.(map[string]any)
	entries := m["entries"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (worker.tick + worker.tock); entries=%v", len(entries), entries)
	}
}

// TestMCPSetLevelMutatesThreshold pins log_set_level: changing the
// level should affect subsequent emissions.
func TestMCPSetLevelMutatesThreshold(t *testing.T) {
	app := newMCPApp(t)

	// At default Level=INFO, a DEBUG line is suppressed.
	app.Logger().Debug("invisible-at-info")

	// Switch to DEBUG via the tool.
	result, err := app.MCP.CallTool(context.Background(), "log_set_level", map[string]any{"level": "DEBUG"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	rm := result.(map[string]any)
	if rm["previous_level"] != "INFO" || rm["current_level"] != "DEBUG" {
		t.Fatalf("level transition reported wrong: %v", rm)
	}

	// Now DEBUG should be captured.
	app.Logger().Debug("visible-at-debug")

	result, err = app.MCP.CallTool(context.Background(), "log_recent", map[string]any{"limit": 50, "level": "DEBUG"})
	if err != nil {
		t.Fatal(err)
	}
	entries := result.(map[string]any)["entries"].([]map[string]any)
	var sawVisible, sawInvisible bool
	for _, e := range entries {
		if e["msg"] == "visible-at-debug" {
			sawVisible = true
		}
		if e["msg"] == "invisible-at-info" {
			sawInvisible = true
		}
	}
	if !sawVisible {
		t.Error("DEBUG entry after level change was not captured")
	}
	if sawInvisible {
		t.Error("DEBUG entry from before level change should still be suppressed")
	}
}

// TestMCPMetricsReturnsSnapshot pins log_metrics' output shape.
func TestMCPMetricsReturnsSnapshot(t *testing.T) {
	app := newMCPApp(t)
	result, err := app.MCP.CallTool(context.Background(), "log_metrics", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	for _, want := range []string{"post_stop_drops", "sink_write_failures", "webhook_dropped", "webhook_gave_up"} {
		if _, ok := m[want]; !ok {
			t.Errorf("metrics snapshot missing field %q", want)
		}
	}
}
