package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// WithMCPControl is the mutating half of the MCP surface: a locally
// connected agent can toggle modules on the RUNNING app. Registered
// separately from WithMCPIntrospection so read-only introspection can
// ship to surfaces where mutation must stay off.
func TestMCPControlTogglesModules(t *testing.T) {
	app := NewApp(WithMCPControl())
	app.RegisterModule(&modStub{name: "reports", manifest: ModuleManifest{}, init: noopInit})
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	res, err := app.MCP.CallTool(context.Background(), "app_module_disable", map[string]any{"name": "reports"})
	if err != nil {
		t.Fatalf("app_module_disable: %v", err)
	}
	if app.Modules().Enabled("reports") {
		t.Fatal("module still enabled after app_module_disable")
	}
	out := res.(map[string]any)
	if out["enabled"] != false || out["name"] != "reports" {
		t.Fatalf("unexpected disable payload: %#v", out)
	}

	if _, err := app.MCP.CallTool(context.Background(), "app_module_enable", map[string]any{"name": "reports"}); err != nil {
		t.Fatalf("app_module_enable: %v", err)
	}
	if !app.Modules().Enabled("reports") {
		t.Fatal("module still disabled after app_module_enable")
	}
}

func TestMCPControlUnknownModuleErrors(t *testing.T) {
	app := NewApp(WithMCPControl())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	_, err := app.MCP.CallTool(context.Background(), "app_module_enable", map[string]any{"name": "ghost"})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("want not-registered error, got %v", err)
	}
	if _, err := app.MCP.CallTool(context.Background(), "app_module_disable", map[string]any{}); err == nil {
		t.Fatal("missing name must error, not toggle something")
	}
}

// The dev loop implies the whole MCP agent surface without any option:
// `gofastr dev` (GOFASTR_DEV) is livereload for agents. Introspection
// AND control tools register on a plain NewApp().
func TestDevLoopImpliesMCPSurface(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := NewApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range app.MCP.ListTools() {
		got[tool.Name] = true
	}
	for _, want := range []string{"app_routes", "framework_docs_search", "app_module_enable", "app_module_disable"} {
		if !got[want] {
			t.Errorf("dev loop did not imply tool %q", want)
		}
	}
}

func TestDevMCPOptOutEnv(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_MCP", "0")
	app := NewApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if n := len(app.MCP.ListTools()); n != 0 {
		t.Fatalf("GOFASTR_DEV_MCP=0 must suppress the implied surface, got %d tools", n)
	}
}

func TestDevMCPOffInProductionEnv(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "production")
	app := NewApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if n := len(app.MCP.ListTools()); n != 0 {
		t.Fatalf("production env must win over GOFASTR_DEV, got %d tools", n)
	}
}

// Older scaffolds hand-mount POST /mcp. The dev-implied auto-mount must
// yield to it — running an existing app under `gofastr dev` can never
// become a route-conflict panic.
func TestDevImpliedMountYieldsToManualMCPRoute(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	app := NewApp()
	app.Router().Handle("POST", "/mcp", app.MCP)

	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Start failed (dev auto-mount conflicted with manual /mcp?): %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// In the dev loop, entity DATA tools are implied too: every
// CRUD-enabled entity serves its MCP tools without `MCP: true` — the
// local agent can read and write app data. Outside dev the explicit
// flag stays the only path.
func TestDevLoopImpliesEntityMCPTools(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createPostsTable(t, db)
	app := NewApp(WithDB(db))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		// No MCP: true — dev implies it for CRUD-enabled entities.
	})
	got := map[string]bool{}
	for _, tool := range app.MCP.ListTools() {
		got[tool.Name] = true
	}
	for _, want := range []string{"posts_list", "posts_create", "posts_update", "posts_delete"} {
		if !got[want] {
			t.Errorf("dev loop did not imply entity tool %q", want)
		}
	}
}

func TestNoDevEntityMCPStaysOptIn(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createPostsTable(t, db)
	app := NewApp(WithDB(db))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "posts_create" {
			t.Fatal("entity MCP tools registered outside dev without MCP: true")
		}
	}
}

// Control tools must not piggyback on introspection: read-only opt-in
// stays read-only.
func TestIntrospectionAloneRegistersNoControlTools(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for _, tool := range app.MCP.ListTools() {
		if tool.Name == "app_module_enable" || tool.Name == "app_module_disable" {
			t.Fatalf("introspection-only app registered mutating tool %s", tool.Name)
		}
	}
}
