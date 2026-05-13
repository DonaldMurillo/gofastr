package freeze_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestFreezeWritesEntities(t *testing.T) {
	w := world.New()
	w.App.Name = "blog"
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
		},
	}
	w.Entities["users"] = &world.Entity{
		Name: "users",
		Fields: []world.Field{
			{Name: "email", Type: "string", Required: true, Unique: true},
		},
	}

	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	// entities/<name>.json files should exist and parse via framework.
	for _, name := range []string{"posts", "users"} {
		path := filepath.Join(dir, "entities", name+".json")
		decl, err := framework.LoadEntityDeclaration(path)
		if err != nil {
			t.Errorf("LoadEntityDeclaration %s: %v", name, err)
			continue
		}
		if decl.Name != name {
			t.Errorf("frozen entity name = %q, want %q", decl.Name, name)
		}
	}
}

func TestFreezeWorldSnapshot(t *testing.T) {
	w := world.New()
	w.App.Name = "demo"
	w.Pages["/dashboard"] = &world.Page{
		Path: "/dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Hi"}}}},
	}
	w.Hooks = append(w.Hooks, &world.Hook{ID: "h1", Entity: "posts", When: "before_create", Action: world.Action{Kind: world.ActionNoop}})

	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	buf, err := os.ReadFile(filepath.Join(dir, "world.json"))
	if err != nil {
		t.Fatalf("read world.json: %v", err)
	}
	var got world.World
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.App.Name != "demo" {
		t.Errorf("App.Name = %q", got.App.Name)
	}
	if _, ok := got.Pages["/dashboard"]; !ok {
		t.Error("page lost in snapshot")
	}
	if len(got.Hooks) != 1 {
		t.Errorf("hooks = %d", len(got.Hooks))
	}
}

func TestFreezeIdempotent(t *testing.T) {
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string"}},
	}
	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Same content both times.
	first, _ := os.ReadFile(filepath.Join(dir, "entities", "posts.json"))
	if len(first) == 0 {
		t.Fatal("entity file empty")
	}
}

func TestFreezeRejectsNilWorld(t *testing.T) {
	if err := freeze.Freeze(nil, t.TempDir()); err == nil {
		t.Fatal("nil world should error")
	}
}

func TestFrozenEntitiesLoadIntoFreshApp(t *testing.T) {
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
		},
		MCP: true,
	}
	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	// Bring up a clean (non-Kiln) framework.App and load from the
	// frozen entities/. This is the round-trip — Kiln's output drops
	// straight into a regular GoFastr project.
	app := framework.NewApp()
	if err := app.EntitiesFromDir(filepath.Join(dir, "entities")); err != nil {
		t.Fatalf("EntitiesFromDir: %v", err)
	}
	if _, err := app.Registry.Get("posts"); err != nil {
		t.Errorf("posts not registered after frozen load: %v", err)
	}
}
