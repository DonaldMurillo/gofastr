package framework

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

func noopTool(ctx context.Context, params map[string]any) (any, error) { return "ok", nil }

func registerNamed(name string) func(*mcp.Server) error {
	return func(s *mcp.Server) error {
		return s.RegisterTool(name, "test tool", map[string]any{"type": "object"}, noopTool)
	}
}

// WithMCPTools runs the registrar against the app's MCP server at init,
// so a leaf package can add tools without importing the framework root.
func TestWithMCPToolsRegistersAtInit(t *testing.T) {
	app := NewApp(WithMCPTools(registerNamed("leaf_tool")))
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "leaf_tool" {
			t.Fatal("registrar ran before InitPlugins")
		}
	}
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "leaf_tool" {
			return
		}
	}
	t.Fatal("leaf_tool not registered after InitPlugins")
}

// A registrar's error fails the boot and names the registrar's problem,
// the way a plugin Init error does.
func TestWithMCPToolsErrorFailsInit(t *testing.T) {
	boom := errors.New("leaf registrar failed")
	app := NewApp(WithMCPTools(func(*mcp.Server) error { return boom }))
	err := app.InitPlugins()
	if !errors.Is(err, boom) {
		t.Fatalf("InitPlugins err = %v, want wrapping %v", err, boom)
	}
	if !strings.Contains(err.Error(), "MCP tools") {
		t.Fatalf("error does not say which stage failed: %v", err)
	}
}

// Two registrars claiming one name collide inside the MCP server, and
// that collision fails the boot too.
func TestWithMCPToolsCollisionFailsInit(t *testing.T) {
	app := NewApp(WithMCPTools(registerNamed("dup_tool")), WithMCPTools(registerNamed("dup_tool")))
	if err := app.InitPlugins(); err == nil || !strings.Contains(err.Error(), "dup_tool") {
		t.Fatalf("InitPlugins err = %v, want a dup_tool collision", err)
	}
}
