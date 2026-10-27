package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A process module has two front doors for enable/disable: the module
// manager (Modules().Enable, and the app_module_enable / _disable MCP
// control tools on top of it) and the supervisor (ProcessModules().Enable).
// Both must reach the same three places: the route gate, the module-tool
// gate, and the supervisor that spawns and drains the child. These tests
// drive the real demo module binary through the app's router and its MCP
// server.

// toggleTestApp registers the demo module on a fresh app, runs
// InitPlugins, and starts the supervisor loops (App.Start's step for
// process modules), without opening a listener.
func toggleTestApp(t *testing.T) (*App, ProcessModuleDescriptor) {
	t.Helper()
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db), WithMCPControl())
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", TrustTrusted)
	app.RegisterProcessModule(d, ApprovedGrants{"articles:read"})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	app.ProcessModules().StartLoops()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.ProcessModules().Close(ctx)
	})
	return app, d
}

func toggleGet(app *App, path string) int {
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

// waitStatus polls a route until it answers want, returning the last code.
func waitStatus(app *App, path string, want int, within time.Duration) int {
	deadline := time.Now().Add(within)
	code := toggleGet(app, path)
	for code != want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		code = toggleGet(app, path)
	}
	return code
}

func listsTool(app *App, name string) bool {
	return slices.ContainsFunc(app.MCP.ListTools(), func(tl mcp.Tool) bool { return tl.Name == name })
}

// TestProcessModuleNeverEnabled404s: a process module nobody enabled is
// indistinguishable from an uninstalled one (404), and the supervisor's
// own Enable/Disable move the route gate with it.
func TestProcessModuleNeverEnabled404s(t *testing.T) {
	app, d := toggleTestApp(t)
	if code := toggleGet(app, "/demo/hello"); code != http.StatusNotFound {
		t.Fatalf("never-enabled process module route = %d, want 404 (indistinguishable from uninstalled)", code)
	}
	if !slices.ContainsFunc(app.Modules().List(), func(mi ModuleInfo) bool { return mi.Name == d.Name && !mi.Enabled }) {
		t.Errorf("app_modules lists the never-enabled process module as enabled: %+v", app.Modules().List())
	}

	if err := app.ProcessModules().Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("supervisor enable: %v", err)
	}
	waitForStateTB(t, app.ProcessModules(), d.Name, StateReady, 8*time.Second)
	if code := waitStatus(app, "/demo/hello", http.StatusOK, 2*time.Second); code != http.StatusOK {
		t.Fatalf("after supervisor Enable reached Ready the route = %d, want 200", code)
	}

	if err := app.ProcessModules().Disable(context.Background(), d.Name); err != nil {
		t.Fatalf("supervisor disable: %v", err)
	}
	if code := toggleGet(app, "/demo/hello"); code != http.StatusNotFound {
		t.Errorf("after supervisor Disable the route = %d, want 404", code)
	}
}

// TestModuleControlToolsDriveProcess: the MCP control tools reach the
// supervisor, so app_module_enable spawns the child and lists its tools,
// and app_module_disable drains it and withdraws them.
func TestModuleControlToolsDriveProcess(t *testing.T) {
	app, d := toggleTestApp(t)
	const tool = "module.demo.ping"

	if _, err := app.MCP.CallTool(mcpCallerCtx(), "app_module_enable", map[string]any{"name": d.Name}); err != nil {
		t.Fatalf("app_module_enable: %v", err)
	}
	waitForStateTB(t, app.ProcessModules(), d.Name, StateReady, 8*time.Second)
	if code := waitStatus(app, "/demo/hello", http.StatusOK, 2*time.Second); code != http.StatusOK {
		t.Fatalf("enabled + Ready route = %d, want 200", code)
	}
	if !listsTool(app, tool) {
		t.Fatalf("enabled + Ready module does not list %s", tool)
	}

	if _, err := app.MCP.CallTool(mcpCallerCtx(), "app_module_disable", map[string]any{"name": d.Name}); err != nil {
		t.Fatalf("app_module_disable: %v", err)
	}
	if code := toggleGet(app, "/demo/hello"); code != http.StatusNotFound {
		t.Errorf("disabled module route = %d, want 404", code)
	}
	if listsTool(app, tool) {
		t.Errorf("SECURITY: [authz] app_module_disable left %s listed and callable", tool)
	}
	if _, err := app.MCP.CallTool(mcpCallerCtx(), tool, map[string]any{}); err == nil {
		t.Errorf("SECURITY: [authz] app_module_disable left %s callable", tool)
	}
	waitForStateTB(t, app.ProcessModules(), d.Name, StateInstalledDisabled, 8*time.Second)
	if pid := redLeaseSlotPID(app.ProcessModules(), d.Name); pid > 0 {
		t.Errorf("app_module_disable left the child running (pid %d)", pid)
	}
}
