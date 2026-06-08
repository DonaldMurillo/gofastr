package main

import (
	"os"
	"os/exec"
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
	// Generated apps must render node trees via the leaf kiln/noderender
	// package (core-ui/html + core/render + kiln/world only), NOT kiln/render —
	// which drags Kiln's authoring engine (kiln/expr, kiln/effect, framework)
	// into a shipped app.
	if !strings.Contains(screens, "kiln/noderender") {
		t.Errorf("screens.go should import kiln/noderender:\n%s", screens)
	}
	if strings.Contains(screens, `"github.com/DonaldMurillo/gofastr/kiln/render"`) {
		t.Errorf("screens.go must NOT import the heavy kiln/render package")
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

// TestRenderBlueprintNodeAppBuildsWithoutAuthoringEngine is the build test the
// G1 audit flagged as missing: it renders a blueprint with freeform node
// blocks to a temp module, compiles it, and asserts the generated `blueprint`
// (screens) package does NOT pull Kiln's authoring engine (kiln/expr,
// kiln/effect) or the heavy kiln/render package. Without this, an import-shape
// regression (re-importing kiln/render) would be invisible.
func TestRenderBlueprintNodeAppBuildsWithoutAuthoringEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("build test")
	}
	const repoRoot = "../.."
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	// A minimal blueprint with a freeform node block (Kind+Props+Children →
	// triggers the node-renderer import path) and nothing else, so the build
	// test isolates G1 (kiln decoupling) from unrelated endpoint/stub codegen.
	bp := Blueprint{
		App: BlueprintApp{Name: "Node", Module: "example.com/node"},
		Screens: []BlueprintScreen{{
			Name: "home", Title: "Home", Type: "page",
			Body: []BlueprintBlock{
				{
					Kind:     "section",
					Props:    map[string]any{"class": "hero", "style": "color:red", "onclick": "x()"},
					Children: []BlueprintBlock{{Type: "p", Text: "hi"}},
				},
				{Type: "link", Href: "/x", Text: "Go"}, // non-node block → exercises html.* too
			},
		}},
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	// Only the generated screens package matters for G1 (it's what renders node
	// trees). It's a self-contained `package blueprint` importing framework
	// packages — no project-local imports — so it builds standalone against a
	// generic module + replace, sidestepping the main.go/entities module-path
	// wiring.
	dir := t.TempDir()
	wrote := false
	for _, f := range files {
		if !strings.HasPrefix(f.name, "blueprint/") {
			continue
		}
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
		wrote = true
	}
	if !wrote {
		t.Fatal("no blueprint/ files rendered")
	}
	goMod := "module example.com/bpapp\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + absRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	gocache := filepath.Join(t.TempDir(), "gocache")

	build := exec.Command("go", "build", "-mod=mod", "./blueprint")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOCACHE="+gocache, "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated blueprint app did not build: %v\n%s", err, out)
	}

	deps := exec.Command("go", "list", "-mod=mod", "-deps", "./blueprint")
	deps.Dir = dir
	deps.Env = append(os.Environ(), "GOCACHE="+gocache, "GOFLAGS=-mod=mod")
	out, err := deps.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./blueprint: %v\n%s", err, out)
	}
	for _, banned := range []string{
		"gofastr/kiln/expr",
		"gofastr/kiln/effect",
		"gofastr/kiln/render", // the heavy package; leaf kiln/noderender is fine
	} {
		if strings.Contains(string(out), banned) {
			t.Errorf("generated blueprint package pulls %q — authoring engine leaked into a shipped app", banned)
		}
	}
}
