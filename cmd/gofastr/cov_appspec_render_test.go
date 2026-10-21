package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestRenderAppSpecNodeOnlyScreenBuilds proves bug 2 is fixed: a
// screen whose body is a single node block (Kind set) with a plain
// child must not import core-ui/html, because every node and its
// children render via the node renderer, never html.*. Previously this
// produced "imported and not used: core-ui/html".
func TestRenderAppSpecNodeOnlyScreenBuilds(t *testing.T) {
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
	goMod := "module example.com/appspec\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	// The generated app is now flat package main. Scaffold it into a gen/
	// subpackage of the temp module (OutputDir=gen) so main.go's entities
	// import resolves, then build the whole app.
	bp := AppSpec{
		App: AppSpecApp{Name: "NodeOnly", Module: "example.com/appspec", OutputDir: "gen"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []AppSpecScreen{{
			Name:  "home",
			Title: "Home",
			Type:  "page",
			Body: []AppSpecBlock{{
				Kind:     "section",
				Children: []AppSpecBlock{{Type: "p", Text: "child"}},
			}},
		}},
	}
	files, err := renderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
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

func TestAppSpecDriverImport(t *testing.T) {
	cases := map[string]string{
		"": "github.com/DonaldMurillo/gofastr/sqlite/stdlib", "sqlite": "github.com/DonaldMurillo/gofastr/sqlite/stdlib",
		"sqlite3": "github.com/DonaldMurillo/gofastr/sqlite/stdlib", "postgres": "github.com/lib/pq",
		"postgresql": "github.com/lib/pq", "mysql": "",
	}
	for in, want := range cases {
		if got := appSpecDriverImport(in); got != want {
			t.Errorf("appSpecDriverImport(%q)=%q want %q", in, got, want)
		}
	}
}

// covT_richAppSpec builds an app spec exercising entity_list blocks,
// islands, widgets, node-renderer blocks with props/children/actions,
// postgres driver, screens with actions, and an empty-body screen.
func covT_richAppSpec() AppSpec {
	return AppSpec{
		App: AppSpecApp{
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
		Screens: []AppSpecScreen{
			{
				Name:  "home",
				Title: "Home",
				Type:  "page",
				Body: []AppSpecBlock{
					{Type: "heading", Level: 2, Text: "Posts", Props: map[string]any{"x": "y"},
						Actions:  []AppSpecAction{{Name: "save", Event: "input", ClientJS: "x()"}, {Name: "alt", Event: "click"}},
						Children: []AppSpecBlock{{Type: "p", Text: "child"}}},
					{Kind: "entity_list", Entity: "posts", Limit: 5, EmptyText: "Nothing", Text: "All Posts"},
					{Type: "p", Text: "island", Island: "live"},
					{Type: "p", Text: "widget", Widget: "card"},
					{Type: "link", Href: "/x", Text: "Go"},
				},
			},
			{Name: "empty", Title: "Empty", Type: "drawer"}, // no body → html.Heading path
		},
		Endpoints:  []settings.Endpoint{{Name: "archive", Entity: "posts", Method: "POST", Path: "{id}/archive", Handler: "entities/posts_endpoints.go#ArchivePost"}},
		Middleware: []AppSpecNamedStub{{Name: "logging"}},
		Plugins:    []AppSpecNamedStub{{Name: "metrics"}},
		Helpers:    []AppSpecNamedStub{{Name: "fmtx"}},
	}
}

func TestRenderAppSpecFilesRichShape(t *testing.T) {
	files, err := renderAppSpecFiles(covT_richAppSpec())
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.name] = f.content
	}
	if _, ok := got["main.go"]; !ok {
		t.Fatal("expected main.go")
	}
	screens := allScreenContent(files)
	for _, want := range []string{"HomeScreen", "EmptyScreen", `appUI.List("posts")`, "island.NewIsland", "component.NewWidget", "ui.Link", "uinode.Node"} {
		if !strings.Contains(screens, want) {
			t.Errorf("screens.go missing %q", want)
		}
	}
	// Generated apps must render node trees via the leaf core-ui/noderender
	// package (core-ui/html + core/render + core-ui/node only), NOT kiln/render,
	// which drags Kiln's authoring engine (kiln/expr, kiln/effect, framework)
	// into a shipped app.
	if !strings.Contains(screens, "core-ui/noderender") {
		t.Errorf("screens.go should import core-ui/noderender:\n%s", screens)
	}
	if strings.Contains(screens, `"github.com/DonaldMurillo/gofastr/kiln/render"`) {
		t.Errorf("screens.go must NOT import the heavy kiln/render package")
	}
	// The endpoint lands in the settings file with its stub and registration.
	for name, want := range map[string]string{
		"gofastr.settings.yml":        "handler: \"entities/posts_endpoints.go#ArchivePost\"",
		"entities/posts_endpoints.go": "func ArchivePost(ctx context.Context, in ArchivePostInput) (*Posts, error)",
		"entities/endpoints.gen.go":   `app.EntityEndpoints("posts"`,
		"entities/client/client.go":   ") Archive(ctx context.Context, id string)",
	} {
		if !strings.Contains(got[name], want) {
			t.Errorf("%s missing %q:\n%s", name, want, got[name])
		}
	}
	main := got["main.go"]
	if !strings.Contains(main, "lib/pq") {
		t.Fatalf("postgres main should import lib/pq:\n%s", main)
	}
}

func TestRenderAppSpecMainSqliteDefault(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Name: "S", Module: "ex.com/s"}, Entities: []framework.EntityDeclaration{{Name: "x", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}}}}
	out := renderAppSpecMain(bp)
	if !strings.Contains(out, "github.com/DonaldMurillo/gofastr/sqlite/stdlib") {
		t.Fatalf("default sqlite driver expected:\n%s", out)
	}
}

// TestRenderAppSpecNodeAppBuildsWithoutAuthoringEngine is the build test the
// G1 audit flagged as missing: it renders an app spec with freeform node
// blocks to a temp module, compiles it, and asserts the generated `app spec`
// (screens) package does NOT pull Kiln's authoring engine (kiln/expr,
// kiln/effect) or the heavy kiln/render package. Without this, an import-shape
// regression (re-importing kiln/render) would be invisible.
func TestRenderAppSpecNodeAppBuildsWithoutAuthoringEngine(t *testing.T) {
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

	// A minimal app spec with a freeform node block (Kind+Props+Children →
	// triggers the node-renderer import path) and nothing else, so the build
	// test isolates G1 (kiln decoupling) from unrelated endpoint/stub codegen.
	bp := AppSpec{
		App: AppSpecApp{Name: "Node", Module: "example.com/node"},
		Screens: []AppSpecScreen{{
			Name: "home", Title: "Home", Type: "page",
			Body: []AppSpecBlock{
				{
					Kind:     "section",
					Props:    map[string]any{"class": "hero", "style": "color:red", "onclick": "x()"},
					Children: []AppSpecBlock{{Type: "p", Text: "hi"}},
				},
				{Type: "link", Href: "/x", Text: "Go"}, // non-node block → exercises html.* too
			},
		}},
	}
	files, err := renderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	// The generated app is now a flat `package main` at the root. Building it
	// whole exercises the screens' node-renderer path, which is what renders
	// node trees, and lets us assert the authoring engine never leaks into a
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
	build := exec.Command("go", "build", "-mod=mod", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated app did not build: %v\n%s", err, out)
	}

	deps := exec.Command("go", "list", "-mod=mod", "-deps", ".")
	deps.Dir = dir
	deps.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
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
			t.Errorf("generated app spec package pulls %q — authoring engine leaked into a shipped app", banned)
		}
	}
}

// Each path value becomes its own input field, spelled the way Go spells
// an id; two values that fold to one field name are refused, since the stub
// would not compile.
func TestEndpointStubPathFields(t *testing.T) {
	spec := func(path string) AppSpec {
		return AppSpec{
			App:       AppSpecApp{Name: "S", Module: "example.com/s"},
			Entities:  []framework.EntityDeclaration{{Name: "orders", Fields: []framework.FieldDeclaration{{Name: "total", Type: "decimal"}}}},
			Endpoints: []settings.Endpoint{{Name: "move_line", Entity: "orders", Method: "POST", Path: path, Handler: "entities/orders_endpoints.go#MoveLine"}},
		}
	}
	files, err := renderAppSpecFiles(spec("{id}/lines/{line_id}/move"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	stub := filesByName(files)[filepath.Join("entities", "orders_endpoints.go")]
	assertContains(t, stub, "\tID string `path:\"id\" json:\"-\"`\n\tLineID string `path:\"line_id\" json:\"-\"`\n")
	if _, err := renderAppSpecFiles(spec("{id}/lines/{i_d}/move")); err == nil || !strings.Contains(err.Error(), "both become input field ID") {
		t.Fatalf("render err = %v, want the folded field refused", err)
	}
}
