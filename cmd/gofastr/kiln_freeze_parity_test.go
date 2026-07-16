package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// This is the cross-package parity gate: Kiln's emitted YAML must decode and
// validate through the exact current generator, not a second approximation of
// the blueprint schema.
func TestKilnFreezeBlueprintParsesAndValidates(t *testing.T) {
	timestamps := false
	crud := true
	w := world.New()
	w.App = world.AppConfig{
		Name: "forge", Module: "example.com/forge", DBDriver: "sqlite",
		DBURL: "forge.db", StaticDir: "static", APIPrefix: "api", LLMMD: true,
		Theme:     map[string]string{"primary": "#4338ca"},
		ThemeDark: map[string]string{"primary": "#a5b4fc"},
		Auth:      world.AuthConfig{Enabled: true, DevMode: true, BasePath: "/auth"},
		PWA:       world.PWAConfig{Enabled: true, Name: "Forge", StartURL: "/", Scope: "/", Display: "standalone"},
	}
	w.Entities["tasks"] = &world.Entity{
		Name: "tasks", OwnerField: "user_id", CrossOwnerRead: "tasks:read:any",
		SearchFields: []string{"title"}, Timestamps: &timestamps, CRUD: &crud, MCP: true,
		Access:       &world.AccessDeclaration{Read: "tasks:read", Create: "tasks:write"},
		CursorFields: []string{"created_at", "id"},
		Indices:      []world.Index{{Name: "idx_tasks_status", Columns: []string{"status"}}},
		Properties:   map[string]any{"label": "Tasks"},
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "status", Type: "enum", Values: []string{"open", "done"}, Default: "open"},
		},
	}
	w.Pages["/"] = &world.Page{
		Path: "/", Name: "home", Title: "Forge", Description: "Task overview",
		Layout: &world.Layout{Name: "app"}, Access: world.PageAccess{Auth: true},
		Tree: world.Node{Kind: "stack", Props: map[string]any{"gap": "xl"}, Children: []world.Node{
			{Kind: "page_header", Props: map[string]any{"title": "Forge"}},
			{Kind: "grid", Props: map[string]any{"min": "14rem", "gap": "lg"}, Children: []world.Node{
				{Kind: "card", Props: map[string]any{"heading": "Tasks"}},
				{Kind: "card", Props: map[string]any{"heading": "Owners"}},
			}},
		}},
	}
	w.Nav = []world.NavItem{{Label: "Home", Href: "/"}}
	w.Seeds = []*world.Seed{{Entity: "tasks", Rows: []map[string]any{{"title": "Ship", "status": "open"}}}}

	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatalf("BlueprintYAML: %v", err)
	}
	bp, err := decodeBlueprintString(string(buf))
	if err != nil {
		t.Fatalf("current decoder rejected Kiln output: %v\n%s", err, buf)
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("current validator rejected Kiln output: %v\n%s", err, buf)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("current renderer rejected Kiln output: %v\n%s", err, buf)
	}
	screens := allScreenContent(files)
	for _, want := range []string{"ui.Stack(", "ui.Grid(", "ui.Card("} {
		if !strings.Contains(screens, want) {
			t.Fatalf("frozen layout did not reach current generator %q:\n%s", want, screens)
		}
	}
	if len(bp.Entities) != 1 || bp.Entities[0].OwnerField != "user_id" || len(bp.Entities[0].CursorFields) != 2 {
		t.Fatalf("entity surface drifted during freeze: %+v", bp.Entities)
	}
	if len(bp.Screens) != 1 || bp.Screens[0].Layout != "app" || !bp.Screens[0].Access.Auth {
		t.Fatalf("screen surface drifted during freeze: %+v", bp.Screens)
	}
	if bp.App.APIPrefix != "api" || !bp.App.PWA.Enabled || !bp.App.LLMMD {
		t.Fatalf("app surface drifted during freeze: %+v", bp.App)
	}
}
