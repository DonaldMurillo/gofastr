package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
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
	clitest.VariantBuild(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/appspec\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	// The generated app is now flat package main. Scaffold it into a gen/
	// subpackage of the temp module (OutputDir=gen) so main.go's entities
	// import resolves, then build the whole app.
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "NodeOnly", Module: "example.com/appspec", OutputDir: "gen"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []appspec.AppSpecScreen{{
			Name:  "home",
			Title: "Home",
			Type:  "page",
			Body: []appspec.AppSpecBlock{{
				Kind:     "section",
				Children: []appspec.AppSpecBlock{{Type: "p", Text: "child"}},
			}},
		}},
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, "gen", file.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.Content), 0o644); err != nil {
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

func TestRenderAppSpecMainSqliteDefault(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "S", Module: "ex.com/s"}, Entities: []framework.EntityDeclaration{{Name: "x", Fields: []framework.FieldDeclaration{{Name: "n", Type: "string"}}}}}
	out := RenderAppSpecMain(bp)
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
	clitest.VariantBuild(t)
	const repoRoot = "../../../.."
	goVersion, err := clitest.RepoGoVersion(repoRoot)
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
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Node", Module: "example.com/node"},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Title: "Home", Type: "page",
			Body: []appspec.AppSpecBlock{
				{
					Kind:     "section",
					Props:    map[string]any{"class": "hero", "style": "color:red", "onclick": "x()"},
					Children: []appspec.AppSpecBlock{{Type: "p", Text: "hi"}},
				},
				{Type: "link", Href: "/x", Text: "Go"}, // non-node block → exercises html.* too
			},
		}},
	}
	files, err := RenderAppSpecFiles(bp)
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
		p := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	goMod := "module example.com/node\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + absRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
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
	spec := func(path string) appspec.AppSpec {
		return appspec.AppSpec{
			App:       appspec.AppSpecApp{Name: "S", Module: "example.com/s"},
			Entities:  []framework.EntityDeclaration{{Name: "orders", Fields: []framework.FieldDeclaration{{Name: "total", Type: "decimal"}}}},
			Endpoints: []settings.Endpoint{{Name: "move_line", Entity: "orders", Method: "POST", Path: path, Handler: "entities/orders_endpoints.go#MoveLine"}},
		}
	}
	files, err := RenderAppSpecFiles(spec("{id}/lines/{line_id}/move"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	stub := filesByName(files)[filepath.Join("entities", "orders_endpoints.go")]
	clitest.AssertContains(t, stub, "\tID string `path:\"id\" json:\"-\"`\n\tLineID string `path:\"line_id\" json:\"-\"`\n")
	if _, err := RenderAppSpecFiles(spec("{id}/lines/{i_d}/move")); err == nil || !strings.Contains(err.Error(), "both become input field ID") {
		t.Fatalf("render err = %v, want the folded field refused", err)
	}
}
