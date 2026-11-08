package framework

import (
	"context"
	"testing"
)

func TestReadinessToolReportsDraining(t *testing.T) {
	for _, withCheck := range []bool{true, false} {
		app := NewApp(WithMCPIntrospection())
		calls := 0
		if withCheck {
			app.RegisterReadiness("healthy", func(context.Context) error { calls++; return nil })
		}
		if err := app.InitPlugins(); err != nil {
			t.Fatal(err)
		}
		app.draining.Store(true)
		result, err := app.MCP.CallTool(t.Context(), "app_readiness", nil)
		if err != nil {
			t.Fatal(err)
		}
		got := result.(map[string]any)
		if got["ready"] != false || got["reason"] != "draining" || calls != 0 {
			t.Fatalf("readiness=%v checks run=%d", got, calls)
		}
	}
}
