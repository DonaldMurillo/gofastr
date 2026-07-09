package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestRenderBlueprintStubsValidGoWithoutHandler proves bug 1 is fixed:
// an endpoint with no Handler set (only Name/Method/Path) must still
// render valid Go in stubs.go. Previously this emitted
// "func (w http.ResponseWriter, r *http.Request) {" which Go parses as
// a method with multiple receivers ("method has multiple receivers").
func TestRenderBlueprintStubsValidGoWithoutHandler(t *testing.T) {
	bp := Blueprint{
		App:       BlueprintApp{Name: "NoHandler", Module: "example.com/nohandler"},
		Endpoints: []BlueprintEndpoint{{Name: "health", Method: "GET", Path: "/health"}},
	}
	src := renderBlueprintStubs(bp)
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "stubs.go", src, parser.AllErrors); err != nil {
		t.Fatalf("stubs.go is not valid Go: %v\n%s", err, src)
	}
	// The handler must be derived from Name and registered consistently.
	if !strings.Contains(src, "func Health(") {
		t.Fatalf("expected handler derived from endpoint Name:\n%s", src)
	}
}

// TestRenderBlueprintStubsSkipsAnonymousEndpoint proves that when both
// Handler and Name are empty there is no stub emitted at all (and the
// file stays valid Go).
func TestRenderBlueprintStubsSkipsAnonymousEndpoint(t *testing.T) {
	bp := Blueprint{
		App:       BlueprintApp{Name: "Anon", Module: "example.com/anon"},
		Endpoints: []BlueprintEndpoint{{Method: "GET", Path: "/x"}},
	}
	src := renderBlueprintStubs(bp)
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "stubs.go", src, parser.AllErrors); err != nil {
		t.Fatalf("stubs.go is not valid Go: %v\n%s", err, src)
	}
	if strings.Contains(src, "http.ResponseWriter") {
		t.Fatalf("expected no handler stub for anonymous endpoint:\n%s", src)
	}
}

// TestRenderBlueprintNodeOnlyScreenBuilds proves bug 2 is fixed: a
// screen whose body is a single node block (Kind set) with a plain
// child must not import core-ui/html, because every node and its
// children render via the node renderer, never html.*. Previously this
// produced "imported and not used: core-ui/html".
func TestRenderBlueprintNodeOnlyScreenBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skips module build in -short")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/blueprint\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	// The generated app is now flat package main. Scaffold it into a gen/
	// subpackage of the temp module (OutputDir=gen) so main.go's entities
	// import resolves, then build the whole app.
	bp := Blueprint{
		App: BlueprintApp{Name: "NodeOnly", Module: "example.com/blueprint", OutputDir: "gen"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []BlueprintScreen{{
			Name:  "home",
			Title: "Home",
			Type:  "page",
			Body: []BlueprintBlock{{
				Kind:     "section",
				Children: []BlueprintBlock{{Type: "p", Text: "child"}},
			}},
		}},
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "-mod=mod", "-o", filepath.Join(t.TempDir(), "app"), "./gen")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node-only screen app did not build: %v\n%s", err, output)
	}
}

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
	screens := allScreenContent(files)
	for _, want := range []string{"HomeScreen", "EmptyScreen", `appResources["posts"]`, "island.NewIsland", "component.NewWidget", "html.Link", "uinode.Node"} {
		if !strings.Contains(screens, want) {
			t.Errorf("screens.go missing %q", want)
		}
	}
	// Generated apps must render node trees via the leaf core-ui/noderender
	// package (core-ui/html + core/render + core-ui/node only), NOT kiln/render —
	// which drags Kiln's authoring engine (kiln/expr, kiln/effect, framework)
	// into a shipped app.
	if !strings.Contains(screens, "core-ui/noderender") {
		t.Errorf("screens.go should import core-ui/noderender:\n%s", screens)
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
	js := blueprintEntityListClientJS(block, "/api")
	if js == "" {
		t.Fatal("client JS empty")
	}
	if !strings.Contains(js, "/api") {
		t.Fatalf("client JS should fetch under api base: %s", js)
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
	// The generated app is now a flat `package main` at the root. Building it
	// whole exercises the screens' node-renderer path, which is what renders
	// node trees — and lets us assert the authoring engine never leaks into a
	// shipped app. (Entity-less apps still ship the entities/register.go seam
	// and import <module>/entities, so go.mod below must match app.module.)
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	goMod := "module example.com/node\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + absRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	gocache := filepath.Join(t.TempDir(), "gocache")

	build := exec.Command("go", "build", "-mod=mod", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOCACHE="+gocache, "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated app did not build: %v\n%s", err, out)
	}

	deps := exec.Command("go", "list", "-mod=mod", "-deps", ".")
	deps.Dir = dir
	deps.Env = append(os.Environ(), "GOCACHE="+gocache, "GOFLAGS=-mod=mod")
	out, err := deps.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps .: %v\n%s", err, out)
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
