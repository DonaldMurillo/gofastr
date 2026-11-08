package framework

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDevEntityCatalogCollisionBoots(t *testing.T) {
	for _, mode := range []string{"production", "development"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOFASTR_ENV", mode)
			t.Setenv("GOFASTR_DEV", "1")
			db := newStoreDB(t)
			defer db.Close()
			app := NewApp(WithDB(db), WithMCPIntrospection())
			app.Entity("contracts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
			if err := app.InitPlugins(); err != nil {
				t.Fatalf("%s init: %v", mode, err)
			}
			if !app.MCP.HasTool("contracts_explain") || !app.MCP.HasTool("contracts_capabilities") {
				t.Fatal("collision skipped non-colliding catalog tools")
			}
		})
	}
}

func TestDevExplicitCollisionStillFails(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "development")
	app := NewApp(WithMCPIntrospection())
	if err := app.MCP.RegisterTool("contracts_list", "host tool", nil, func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := app.InitPlugins(); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("explicit collision = %v", err)
	}
}

func TestDevEntityBuiltinCollisionsBoot(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "development")
	for _, tc := range []struct{ name, move, sibling string }{
		{"app", "readiness", "app_routes"}, {"app_module", "enable", "app_module_disable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newStoreDB(t)
			defer db.Close()
			app := NewApp(WithDB(db), WithMCPIntrospection(), WithMCPControl())
			app.Entity(tc.name, entity.EntityConfig{
				Fields: []schema.Field{{Name: "status", Type: schema.Enum, Values: []string{"draft", "ready"}, Default: "draft"}},
				States: &entity.StatesConfig{Field: "status", Initial: []string{"draft"}, Transitions: []entity.Transition{{Key: tc.move, From: []string{"draft"}, To: "ready"}}},
			})
			if err := app.InitPlugins(); err != nil {
				t.Fatal(err)
			}
			if !app.MCP.HasTool(tc.sibling) || !app.MCP.HasTool(tc.name+"_"+tc.move) || !app.MCP.HasTool("contracts_list") {
				t.Fatal("collision skipped non-colliding builtins")
			}
		})
	}
}

func TestReplacedDevToolCollisionFails(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "development")
	db := newStoreDB(t)
	defer db.Close()
	app := NewApp(WithDB(db), WithMCPIntrospection())
	app.Entity("contracts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	app.MCP.UnregisterTool("contracts_list")
	if err := app.MCP.RegisterTool("contracts_list", "explicit replacement", nil, func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := app.InitPlugins(); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("explicit replacement collision = %v", err)
	}
}
