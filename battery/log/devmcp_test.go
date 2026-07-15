package log

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// In the dev loop the debug tools auto-enable on a ZERO Config — the
// generated apps rely on this ("livereload for agents"). Outside dev
// the explicit fields stay the only path.
func TestDevLoopAutoEnablesMCPTools(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{discardSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range app.MCP.ListTools() {
		got[tool.Name] = true
	}
	for _, want := range []string{"log_recent", "log_filter", "log_metrics", "log_set_level"} {
		if !got[want] {
			t.Errorf("dev loop did not auto-enable %q", want)
		}
	}
}

func TestNoDevNoMCPToolsOnZeroConfig(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{discardSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "log_recent" || tool.Name == "log_set_level" {
			t.Fatalf("zero Config outside dev registered %s", tool.Name)
		}
	}
}
