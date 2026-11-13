package appgen

import (
	"context"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func TestViewStubWiring(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"}, Entities: []framework.EntityDeclaration{{
		Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}, {Key: "named", Where: `title = "hello"`}}},
	}}}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("caller-dependent view must validate: %v", err)
	}
	files, err := RenderAppSpecCode(bp)
	if err != nil {
		t.Fatal(err)
	}
	byName := filesByName(files)
	if !strings.Contains(byName["app.go"], "fwApp.EntityUI(withViewStubs(fwApp, appExtensions))") {
		t.Fatalf("app.go does not wire fail-closed view stubs")
	}
	stub := byName["view_stubs.go"]
	for _, want := range []string{"Filter != nil", "return nil, fmt.Errorf(", "filter is not written yet", "extensions.go"} {
		if !strings.Contains(stub, want) {
			t.Errorf("view_stubs.go missing %q", want)
		}
	}
	if !strings.Contains(byName["view_stubs_posts_init.go"], `{"posts", "mine"}`) || strings.Contains(byName["view_stubs_posts_init.go"], `{"posts", "named"}`) {
		t.Error("declarative view received a stub")
	}
	bp.Entities[0].Display = nil
	files, err = RenderAppSpecCode(bp)
	if err != nil {
		t.Fatal(err)
	}
	byName = filesByName(files)
	if byName["view_stubs.go"] != stub || !strings.Contains(byName["app.go"], "withViewStubs") {
		t.Error("base view-stub seam must be independent of the view list")
	}
}

type viewStubHost struct {
	entityui.Host
	reg entity.Registry
}

func (h viewStubHost) Registry() entity.Registry { return h.reg }

func TestViewStubFailsClosed(t *testing.T) {
	reg := framework.NewRegistry()
	e := entity.Define("posts", entity.EntityConfig{Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}, {Key: "owned"}}}})
	if err := reg.Register(e); err != nil {
		t.Fatal(err)
	}
	want := &filter.Predicate{Field: "id", Op: filter.OpEq, Value: "caller"}
	owned := entityui.Extensions{Entities: map[string]entityui.Extension{"posts": {Views: map[string]entityui.ViewFunc{
		"owned": {Filter: func(context.Context) (*filter.Predicate, error) { return want, nil }},
		"mine":  {Show: func(context.Context) bool { return false }},
	}}}}
	ext := fillViewStubs(reg, owned, [][2]string{{"posts", "mine"}, {"posts", "owned"}})
	if _, err := entityui.New(viewStubHost{reg: reg}, ext); err != nil {
		t.Fatalf("stubbed extensions refuse boot: %v", err)
	}
	got, err := ext.Entities["posts"].Views["mine"].Filter(context.Background())
	if got != nil || err == nil || !strings.Contains(err.Error(), `view "mine"`) || !strings.Contains(err.Error(), "filter is not written yet") || !strings.Contains(err.Error(), "extensions.go") {
		t.Fatalf("stub filter = %v, %v; want actionable refusal", got, err)
	}
	got, err = ext.Entities["posts"].Views["owned"].Filter(context.Background())
	if got != want || err != nil {
		t.Fatalf("owned filter replaced: %v, %v", got, err)
	}
	if owned.Entities["posts"].Views["mine"].Filter != nil {
		t.Error("owned extensions mutated")
	}
	if ext.Entities["posts"].Views["mine"].Show == nil {
		t.Error("owned Show callback lost")
	}
	// The nil-map shape is the freshly scaffolded extensions.go.
	fresh := fillViewStubs(reg, entityui.Extensions{}, [][2]string{{"posts", "mine"}, {"posts", "owned"}})
	if _, err := entityui.New(viewStubHost{reg: reg}, fresh); err != nil {
		t.Fatalf("fresh scaffold refuses boot: %v", err)
	}
}

func TestViewStubsAdditive(t *testing.T) {
	for _, hasViews := range []bool{false, true} {
		t.Run(fmt.Sprint(hasViews), func(t *testing.T) {
			bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"}, Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}}}
			if hasViews {
				bp.Entities[0].Display = &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}}}
			}
			files, err := RenderAppSpecCode(bp)
			if err != nil {
				t.Fatal(err)
			}
			initial := filesByName(files)
			if !strings.Contains(initial["app.go"], "fwApp.EntityUI(withViewStubs(fwApp, appExtensions))") || initial["view_stubs.go"] == "" {
				t.Fatal("base app must always wire the additive view-stub seam")
			}
			added := bp.Entities[0]
			added.Name = "comments"
			added.Display = &entity.DisplayConfig{Views: []entity.ListView{{Key: "assigned"}}}
			files, err = RenderAppSpecCode(appspec.AppSpec{Entities: []framework.EntityDeclaration{added}})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if _, exists := initial[f.Name]; !exists {
					initial[f.Name] = f.Content
				}
			}
			if !strings.Contains(initial["view_stubs_comments_init.go"], `{"comments", "assigned"}`) {
				t.Fatal("--add must emit a new per-entity view registration even when existing files are skipped")
			}
			if hasViews && !strings.Contains(initial["view_stubs_posts_init.go"], `{"posts", "mine"}`) {
				t.Fatal("--add lost the existing view registration")
			}
		})
	}
}

// generateAppSpec's --add branch skips every existing file, derived files
// included. Exercise that writer for both starting states without builds.
func TestViewStubsAddFiles(t *testing.T) {
	for _, hasViews := range []bool{false, true} {
		t.Run(fmt.Sprint(hasViews), func(t *testing.T) {
			yml := addTestAppSpec("example.com/addtest")
			if hasViews {
				yml = strings.Replace(yml, "  - name: posts\n", "  - name: posts\n    display:\n      views:\n        - key: mine\n          label: Mine\n", 1)
			}
			dir, path := clitest.AddSetup(t, yml)
			clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: path}) })
			read := func(name string) string {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			beforeApp, beforeBase := read("app.go"), read("view_stubs.go")
			var beforePosts string
			if hasViews {
				beforePosts = read("view_stubs_posts_init.go")
			}
			fragment := filepath.Join(dir, "fragment.yml")
			clitest.WriteFileAll(t, fragment, "entities:\n  - name: comments\n    fields:\n      - name: body\n        type: string\n    display:\n      views:\n        - key: assigned\n          label: Assigned\n")
			var code int
			out := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: fragment, add: true}) })
			})
			if code != -1 {
				t.Fatalf("--add failed: %d\n%s", code, out)
			}
			if got := read("view_stubs_comments_init.go"); !strings.Contains(got, `{"comments", "assigned"}`) {
				t.Fatalf("missing additive registration: %s", got)
			}
			marker, _, ok := codegen.ReadMarker("view_stubs_comments_init.go", read("view_stubs_comments_init.go"))
			if !ok || marker.Kind != codegen.KindScaffold || marker.Generator != "app" {
				t.Fatalf("view registration must be app scaffold: %#v", marker)
			}
			if read("app.go") != beforeApp || read("view_stubs.go") != beforeBase || !strings.Contains(beforeApp, "EntityUI(withViewStubs(fwApp, appExtensions))") {
				t.Fatal("--add changed existing wiring or base seam")
			}
			if hasViews && read("view_stubs_posts_init.go") != beforePosts {
				t.Fatal("--add changed existing view registration")
			}
		})
	}
}

func TestGenerateNavWarns(t *testing.T) {
	_, path := clitest.AddSetup(t, addTestAppSpec("example.com/addtest")+"nav:\n  - label: Owned\n    href: /owned\n")
	out := clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: path}) })
	if !strings.Contains(out, "nav[0].href") || !strings.Contains(out, "strict boot refuses") {
		t.Fatalf("generation omitted nav warning:\n%s", out)
	}
}

func TestEntityViewStubFilesAreBuilt(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"}}
	for _, name := range []string{"test", "js", "arm", "screen"} {
		bp.Entities = append(bp.Entities, framework.EntityDeclaration{Name: name, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}, Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}}}})
	}
	files, err := RenderAppSpecCode(bp)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var names []string
	for _, f := range files {
		if strings.Contains(f.Content, "generatedViewStubs = append") {
			if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Content), 0600); err != nil {
				t.Fatal(err)
			}
			names = append(names, f.Name)
		}
	}
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 4 {
		t.Fatalf("got %d registrations, want 4", len(names))
	}
	for _, entity := range []string{"test", "js", "arm", "screen"} {
		name := "view_stubs_" + entity + "_init.go"
		if !slices.Contains(pkg.GoFiles, name) {
			t.Errorf("%s excluded from normal build: GoFiles=%v, TestGoFiles=%v, IgnoredGoFiles=%v", name, pkg.GoFiles, pkg.TestGoFiles, pkg.IgnoredGoFiles)
		}
	}
}

func TestViewStubsIgnoreStaleRegistrations(t *testing.T) {
	for _, change := range []string{"entity_removed", "view_renamed", "view_has_where", "display_removed"} {
		t.Run(change, func(t *testing.T) {
			reg := framework.NewRegistry()
			display := &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}}}
			stale := [2]string{"posts", "old"}
			switch change {
			case "entity_removed":
				stale[0] = "removed"
			case "view_has_where":
				display.Views = append(display.Views, entity.ListView{Key: "old", Where: `id = "one"`})
			case "display_removed":
				stale[0] = "no_display"
				if err := reg.Register(entity.Define("no_display", entity.EntityConfig{})); err != nil {
					t.Fatal(err)
				}
			}
			if err := reg.Register(entity.Define("posts", entity.EntityConfig{Display: display})); err != nil {
				t.Fatal(err)
			}
			ext := fillViewStubs(reg, entityui.Extensions{}, [][2]string{stale, {"posts", "mine"}})
			if _, err := entityui.New(viewStubHost{reg: reg}, ext); err != nil {
				t.Errorf("stale registration refused EntityUI boot: %v", err)
			}
			if _, exists := ext.Entities[stale[0]].Views[stale[1]]; exists {
				t.Errorf("stale %q/%q received a filter", stale[0], stale[1])
			}
			live := ext.Entities["posts"].Views["mine"].Filter
			if live == nil {
				t.Fatal("remaining where-less view lost its default filter")
			}
			if pred, err := live(context.Background()); pred != nil || err == nil || !strings.Contains(err.Error(), "filter is not written yet") {
				t.Fatalf("live filter=%v,%v; want fail-closed refusal", pred, err)
			}
		})
	}
}

func TestViewStubsVersionedEntity(t *testing.T) {
	reg := framework.NewRegistry()
	for _, version := range []string{"v1", "v2"} {
		e := entity.Define("posts", entity.EntityConfig{Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}}}})
		e.Version = version
		if err := reg.Register(e); err != nil {
			t.Fatal(err)
		}
	}
	ext := fillViewStubs(reg, entityui.Extensions{}, [][2]string{{"posts", "mine"}})
	if ext.Entities["posts"].Views["mine"].Filter == nil {
		t.Fatal("where-less view of a versioned entity got no default filter")
	}
}

func TestViewStubFilenameDoesNotCollideWithScreen(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{{Name: "screen", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}, Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "mine"}}}}},
		Screens:  []appspec.AppSpecScreen{{Name: "view_stubs", Route: "/guide", Title: "generatedViewStubs = append"}}}
	files, err := RenderAppSpecCode(bp)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if seen[file.Name] {
			t.Errorf("generator emits duplicate filename %s", file.Name)
		}
		seen[file.Name] = true
	}
	for _, name := range []string{"screen_view_stubs.go", "view_stubs_screen_init.go"} {
		if !seen[name] {
			t.Errorf("missing distinct file %s", name)
		}
	}
}
