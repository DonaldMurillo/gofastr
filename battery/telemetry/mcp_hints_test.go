package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
)

func TestMCPToolsCarryListingHints(t *testing.T) {
	const read = `{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}`
	for name, setup := range map[string]struct {
		dev string
		cfg telemetry.Config
	}{
		"opted in":    {"", telemetry.Config{EnableMCP: true}},
		"dev implied": {"1", telemetry.Config{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOFASTR_DEV", setup.dev)
			t.Setenv("GOFASTR_ENV", "")
			seen := 0
			for _, tool := range start(t, setup.cfg).app.MCP.ListTools() {
				if !strings.HasPrefix(tool.Name, "telemetry_") {
					continue
				}
				seen++
				if raw, _ := json.Marshal(tool.Annotations); string(raw) != read {
					t.Errorf("%s: annotations %s, want %s", tool.Name, raw, read)
				}
			}
			if seen != 5 {
				t.Fatalf("saw %d telemetry tools, want 5", seen)
			}
		})
	}
}
