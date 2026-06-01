package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestBlueprintDriverImport(t *testing.T) {
	cases := map[string]string{
		"": "github.com/mattn/go-sqlite3", "sqlite": "github.com/mattn/go-sqlite3",
		"sqlite3": "github.com/mattn/go-sqlite3", "postgres": "github.com/lib/pq",
		"postgresql": "github.com/lib/pq", "mysql": "",
	}
	for in, want := range cases {
		if got := blueprintDriverImport(in); got != want {
			t.Errorf("blueprintDriverImport(%q)=%q want %q", in, got, want)
		}
	}
}

// covT_richBlueprint builds a blueprint exercising entity_list blocks,
// islands, widgets, node-renderer blocks with props/children/actions,
// postgres driver, screens with actions, and an empty-body screen.
func covT_richBlueprint() Blueprint {
	return Blueprint{
		App: BlueprintApp{
			Name:      "Rich",
			Module:    "example.com/rich",
			StaticDir: "public",
			DBDriver:  "postgres",
			DBURL:     "postgres://localhost/rich",
			OutputDir: "./gen",
			Theme:     map[string]string{"primary": "#fff"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []BlueprintScreen{
			{
				Name:  "home",
				Title: "Home",
				Type:  "page",
				Body: []BlueprintBlock{
					{Type: "heading", Level: 2, Text: "Posts", Props: map[string]any{"x": "y"},
						Actions:  []BlueprintAction{{Name: "save", Event: "input", ClientJS: "x()"}, {Name: "alt", Event: "click"}},
						Children: []BlueprintBlock{{Type: "p", Text: "child"}}},
					{Kind: "entity_list", Entity: "posts", Limit: 5, EmptyText: "Nothing", Text: "All Posts"},
					{Type: "p", Text: "island", Island: "live"},
					{Type: "p", Text: "widget", Widget: "card"},
					{Type: "link", Href: "/x", Text: "Go"},
				},
			},
			{Name: "empty", Title: "Empty", Type: "drawer"}, // no body → html.Heading path
		},
		Endpoints:  []BlueprintEndpoint{{Name: "health", Method: "GET", Path: "/health"}},
		Middleware: []BlueprintNamedStub{{Name: "logging"}},
		Plugins:    []BlueprintNamedStub{{Name: "metrics"}},
		Helpers:    []BlueprintNamedStub{{Name: "fmtx"}},
	}
}

func TestRenderBlueprintFilesRichShape(t *testing.T) {
	files, err := renderBlueprintFiles(covT_richBlueprint())
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.name] = f.content
	}
	if _, ok := got["main.go"]; !ok {
		t.Fatal("expected main.go")
	}
	screens := got[filepath.Join("blueprint", "screens.go")]
	for _, want := range []string{"HomeScreen", "EmptyScreen", "entity-list", "island.NewIsland", "component.NewWidget", "html.Link", "world.Node"} {
		if !strings.Contains(screens, want) {
			t.Errorf("screens.go missing %q", want)
		}
	}
	main := got["main.go"]
	if !strings.Contains(main, "lib/pq") {
		t.Fatalf("postgres main should import lib/pq:\n%s", main)
	}
}

func TestRenderBlueprintMainSqliteDefault(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Name: "S", Module: "ex.com/s"}, Entities: []framework.EntityDeclaration{{Name: "x", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}}}}
	out := renderBlueprintMain(bp)
	if !strings.Contains(out, "go-sqlite3") {
		t.Fatalf("default sqlite driver expected:\n%s", out)
	}
}

func TestEntityListHelpers(t *testing.T) {
	block := BlueprintBlock{Kind: "entity_list", Entity: "posts", Limit: 0, EmptyText: ""}
	screen := BlueprintScreen{Name: "home"}
	expr := renderBlueprintEntityListNodeExpression(screen, block, []int{0})
	if !strings.Contains(expr, "gofastr-entity-list") {
		t.Fatalf("entity list expr: %s", expr)
	}
	name := blueprintEntityListActionName(screen, block, []int{0, 1})
	if !strings.Contains(name, "entity_list") {
		t.Fatalf("action name: %s", name)
	}
	js := blueprintEntityListClientJS(block)
	if js == "" {
		t.Fatal("client JS empty")
	}
	if !isEntityListBlock(BlueprintBlock{Type: "entity_list"}) {
		t.Fatal("isEntityListBlock by Type")
	}
}
