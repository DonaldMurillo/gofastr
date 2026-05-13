package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestPrintFreezeDiffEmpty(t *testing.T) {
	var buf bytes.Buffer
	printFreezeDiff(&buf, journal.NewSession())
	out := buf.String()
	if !strings.Contains(out, "(empty world") {
		t.Errorf("expected empty-world note in output: %q", out)
	}
}

func TestPrintFreezeDiffPopulated(t *testing.T) {
	sess := journal.NewSession()
	sess.World.App = world.AppConfig{Name: "demo", JSONCase: "camel"}
	sess.World.Entities["posts"] = &world.Entity{
		Name:       "posts",
		SoftDelete: true,
		MCP:        true,
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "author_id", Type: "relation", To: "users"},
		},
	}
	sess.World.Pages["/dashboard"] = &world.Page{
		Path: "/dashboard", Title: "Dashboard",
		Tree: world.Node{Kind: "div"},
	}
	sess.World.Hooks = []*world.Hook{
		{ID: "audit", Entity: "posts", When: "after_create", Action: world.Action{Kind: "emit_event"}},
	}
	sess.World.Routes = []*world.Route{
		{Method: "GET", Path: "/health", Action: world.Action{Kind: "respond_json"}},
	}
	sess.World.Seeds = []*world.Seed{
		{Entity: "posts", Rows: []map[string]any{{"title": "first"}, {"title": "second"}}},
	}
	sess.Plans["p1"] = &journal.Plan{
		PlanID: "p1", Steps: []string{"drop posts"}, Approved: true,
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}

	var buf bytes.Buffer
	printFreezeDiff(&buf, sess)
	out := buf.String()

	// The big rocks are present.
	for _, want := range []string{
		`app: "demo"`, "json: camel",
		"entities (1):", "+ posts [2 fields] soft_delete mcp",
		"- title : string (required)",
		"- author_id : relation (→users)",
		"pages (1):", `/dashboard — "Dashboard"`,
		"hooks (1):", "audit on posts/after_create — emit_event",
		"routes (1):", "GET /health — respond_json",
		"seeds (1):", "+ posts [2 rows]",
		"plans (1):", "p1 [approved]", "targets: 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}
