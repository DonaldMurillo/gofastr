package framework

import (
	"context"
	"errors"
	"fmt"
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

// A nil registrar is a wiring mistake at the call site, said there, not
// a nil-func panic deep inside InitPlugins.
func TestWithMCPToolsNilPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("WithMCPTools(nil) did not panic")
		}
		if !strings.Contains(fmt.Sprint(r), "nil registrar") {
			t.Fatalf("panic = %v, want it to name the nil registrar", r)
		}
	}()
	WithMCPTools(nil)
}

// Host registrars run BEFORE the dev-implied introspection set, so a
// host tool that shares a name with an introspection tool wins and the
// introspection side downgrades to a warning. The reverse order would
// fail the boot with a collision the host cannot avoid.
func TestWithMCPToolsRunsBeforeDevIntrospection(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := NewApp(WithMCPTools(registerNamed("app_routes")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "app_routes" && tool.Description == "test tool" {
			return
		}
	}
	t.Fatal("the host's app_routes did not survive dev-implied introspection")
}
