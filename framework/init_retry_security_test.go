package framework

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

func TestFailedMCPInitCanRetry(t *testing.T) {
	failure := errors.New("registrar unavailable")
	attempts := 0
	ready := false
	app := NewApp(WithMCPTools(func(server *mcp.Server) error {
		attempts++
		if !ready {
			return failure
		}
		return registerNamed("retried_tool")(server)
	}), WithMCPTools(registerNamed("later_tool")))
	for attempt := 0; attempt < 2; attempt++ {
		if err := app.InitPlugins(); !errors.Is(err, failure) {
			t.Fatalf("failed init attempt %d = %v, want registrar error", attempt, err)
		}
	}
	ready = true
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || len(app.MCP.ListTools()) != 2 {
		t.Fatalf("attempts = %d, tools = %v; want three attempts and both tools", attempts, app.MCP.ListTools())
	}
	if err := app.InitPlugins(); err != nil || attempts != 3 {
		t.Fatalf("successful init did not latch: attempts=%d, error=%v", attempts, err)
	}
}
