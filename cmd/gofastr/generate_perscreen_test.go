package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// perscreenBP is a blueprint with a standalone screen, a CRUD entity (list +
// detail screens → synthesized new/edit), and nav so appLayout is wired.
func perscreenBP() Blueprint {
	crud := true
	return Blueprint{
		App: BlueprintApp{Name: "Test", Module: "example.com/test", DBDriver: "sqlite"},
		Entities: []framework.EntityDeclaration{
			{Name: "customers", Table: "customers", CRUD: &crud, Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string"},
			}},
		},
		Screens: []BlueprintScreen{
			{Name: "home", Route: "/", Title: "Home", Layout: "marketing"},
			{Name: "customers", Route: "/customers", Title: "Customers", Layout: "app", Body: []BlueprintBlock{
				{Kind: "entity_list", Entity: "customers", Create: true},
			}},
			{Name: "customer_detail", Route: "/customers/{id}", Title: "Customer", Layout: "app", Body: []BlueprintBlock{
				{Kind: "entity_detail", Entity: "customers"},
			}},
		},
		Nav: []BlueprintNavItem{{Label: "Customers", Href: "/customers"}},
	}
}

func fileContent(files []generatedFile, name string) string {
	for _, f := range files {
		if f.name == name {
			return f.content
		}
	}
	return ""
}

func sortedFileNames(files []generatedFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.name)
	}
	sort.Strings(out)
	return out
}

// TestScreenSeamIdenticalForAnyCount is the additive-file property:
// screens_register.go is byte-identical whether the project declares one
// screen or many. Adding a screen is a new file, never an edit to the seam.
func TestScreenSeamIdenticalForAnyCount(t *testing.T) {
	one := Blueprint{App: BlueprintApp{Name: "T", Module: "m"}, Screens: []BlueprintScreen{
		{Name: "solo", Route: "/", Title: "Solo"},
	}}
	oneFiles, err := renderBlueprintFiles(one)
	if err != nil {
		t.Fatal(err)
	}
	manyFiles, err := renderBlueprintFiles(perscreenBP())
	if err != nil {
		t.Fatal(err)
	}
	seamOne := fileContent(oneFiles, "screens_register.go")
	seamMany := fileContent(manyFiles, "screens_register.go")
	if seamOne == "" {
		t.Fatal("screens_register.go not emitted for one-screen project")
	}
	if seamOne != seamMany {
		t.Fatalf("screens_register.go must be byte-identical regardless of screen count:\n--- one ---\n%s\n--- many ---\n%s", seamOne, seamMany)
	}
	for _, screen := range []string{`"home"`, `"customers"`, `"customer_detail"`, `"solo"`, `HomeScreen`, `CustomersScreen`} {
		if strings.Contains(seamOne, screen) {
			t.Fatalf("screens_register.go must not name a screen %q:\n%s", screen, seamOne)
		}
	}
}

// TestPerScreenFileLayout asserts one file per authored non-CRUD screen,
// CRUD screens grouped per entity into one file, and app.go naming no screen
// type at all.
func TestPerScreenFileLayout(t *testing.T) {
	files, err := renderBlueprintFiles(perscreenBP())
	if err != nil {
		t.Fatal(err)
	}
	names := sortedFileNames(files)
	for _, want := range []string{"screens_register.go", "screen_home.go", "screen_customers_crud.go"} {
		if fileContent(files, want) == "" {
			t.Fatalf("missing %q; files=%v", want, names)
		}
	}
	if fileContent(files, "screens.go") != "" {
		t.Fatal("aggregated screens.go must not be emitted in the per-screen layout")
	}
	crud := fileContent(files, "screen_customers_crud.go")
	for _, want := range []string{
		"type CustomersScreen struct",
		"type CustomerDetailScreen struct",
		"type CustomersNewScreen struct",
		"type CustomersEditScreen struct",
		"mountCustomersScreen(",
		"mountCustomersNewScreen(",
		"mountCustomersEditScreen(",
	} {
		if !strings.Contains(crud, want) {
			t.Fatalf("screen_customers_crud.go missing %q:\n%s", want, crud)
		}
	}
	app := fileContent(files, "app.go")
	if !strings.Contains(app, "mountGenerated(") {
		t.Fatalf("app.go must call mountGenerated to mount screens:\n%s", app)
	}
	for _, bad := range []string{"&HomeScreen", "&CustomersScreen", "site.Register(", "site.RegisterScreen("} {
		if strings.Contains(app, bad) {
			t.Fatalf("app.go must not contain %q (screens live in their own files):\n%s", bad, app)
		}
	}
}

// TestAppResourcesLivesInCrudFile is the critical additive invariant: an
// entity's resource wiring (appResources entry) lives in its per-entity crud
// screen file, never in app.go.
func TestAppResourcesLivesInCrudFile(t *testing.T) {
	files, err := renderBlueprintFiles(perscreenBP())
	if err != nil {
		t.Fatal(err)
	}
	crud := fileContent(files, "screen_customers_crud.go")
	if !strings.Contains(crud, `appResources["customers"] = ResourceConfig{`) {
		t.Fatalf("screen_customers_crud.go must carry the customers appResources wiring:\n%s", crud)
	}
	if !strings.Contains(crud, `fwApp.MustCrudHandler("customers")`) {
		t.Fatalf("screen_customers_crud.go must wire the CrudHandler via fwApp:\n%s", crud)
	}
	if strings.Contains(fileContent(files, "app.go"), `appResources["customers"]`) {
		t.Fatal("app.go must NOT carry the customers appResources entry")
	}
}

// TestPerScreenFilesCompile builds the generated app to confirm the seam,
// self-registration, and the layout-var references all type-check.
func TestPerScreenFilesCompile(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	files, err := renderBlueprintFiles(perscreenBP())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated per-screen app did not build: %v\n%s", err, out)
	}
}

// TestPackRoundTripPerScreenFiles packs a freshly generated per-screen app
// with authored screens in non-lexical (file-name) order and recovers the
// exact authored set in authored order.
func TestPackRoundTripPerScreenFiles(t *testing.T) {
	crud := true
	bp := Blueprint{
		App: BlueprintApp{Name: "T", Module: "example.com/rt", DBDriver: "sqlite"},
		Entities: []framework.EntityDeclaration{
			{Name: "zebra", CRUD: &crud, Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		},
		Screens: []BlueprintScreen{
			{Name: "zebra", Route: "/zebra", Title: "Zebras", Body: []BlueprintBlock{{Kind: "entity_list", Entity: "zebra", Create: true}}},
			{Name: "alpha", Route: "/alpha", Title: "Alpha"},
			{Name: "zebra_detail", Route: "/zebra/{id}", Title: "Zebra", Body: []BlueprintBlock{{Kind: "entity_detail", Entity: "zebra"}}},
		},
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("packed %d screens, want 3 (synthesized new/edit dropped): %+v", len(got), got)
	}
	want := []string{"zebra", "alpha", "zebra_detail"}
	gotNames := make([]string, len(got))
	for i, s := range got {
		gotNames[i] = s.Name
	}
	if !reflect.DeepEqual(want, gotNames) {
		t.Fatalf("screen order not recovered: want %v, got %v", want, gotNames)
	}
}

// TestScreenFileNameCollisionGuard ensures a screen named "shared" is
// re-prefixed so it never shadows the screen_shared.go nodeComponent helper.
func TestScreenFileNameCollisionGuard(t *testing.T) {
	if got := screenFileName("shared"); got != "screen_screen_shared.go" {
		t.Errorf("screenFileName(%q) = %q, want screen_screen_shared.go", "shared", got)
	}
	if got := screenFileName("home"); got != "screen_home.go" {
		t.Errorf("screenFileName(%q) = %q, want screen_home.go", "home", got)
	}
}

// TestPackLegacyAggregatedScreens verifies the pack legacy fallback still
// reads a pre-per-screen project: all screen structs in one screens.go, the
// site.Register calls in app.go's RegisterGenerated, no screen_*.go files.
func TestPackLegacyAggregatedScreens(t *testing.T) {
	dir := t.TempDir()
	const legacyScreens = `package main

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Home" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *HomeScreen) Render() render.HTML {
	return render.Tag("div", nil, nil)
}
`
	const legacyApp = `package main

import "database/sql"

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "screens.go"), []byte(legacyScreens), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(legacyApp), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := packReadScreens(dir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if len(got) != 1 || got[0].Name != "home" || got[0].Route != "/" {
		t.Fatalf("legacy fallback did not recover the home screen: %+v", got)
	}
}
