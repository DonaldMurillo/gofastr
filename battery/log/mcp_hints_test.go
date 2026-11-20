package log

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestMCPToolsCarryListingHints(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	const (
		read   = `{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}`
		toggle = `{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}`
	)
	want := map[string]string{"log_recent": read, "log_filter": read, "log_metrics": read, "log_set_level": toggle}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(New(Config{Sinks: []Sink{discardSink{}}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	got := map[string]string{}
	for _, tool := range app.MCP.ListTools() {
		raw, _ := json.Marshal(tool.Annotations)
		got[tool.Name] = string(raw)
	}
	for name, hints := range want {
		if got[name] != hints {
			t.Errorf("%s: annotations %s, want %s", name, got[name], hints)
		}
	}
}
