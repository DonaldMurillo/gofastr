package appgen

import (
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// perscreenBP is an app spec with a standalone screen, a CRUD entity (list +
// detail screens → synthesized new/edit), and nav so appLayout is wired.
func perscreenBP() appspec.AppSpec {
	crud := true
	return appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Test", Module: "example.com/test", DBDriver: "sqlite"},
		Entities: []framework.EntityDeclaration{
			{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "customers", Table: "customers", Exposure: &framework.ExposureDeclaration{CRUD: &crud}, Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string"},
			}},
		},
		Screens: []appspec.AppSpecScreen{
			{Name: "home", Route: "/", Title: "Home", Layout: "marketing"},
			{Name: "customers", Route: "/customers", Title: "Customers", Layout: "app", Body: []appspec.AppSpecBlock{
				{Kind: "entity_list", Entity: "customers", Create: true},
			}},
			{Name: "customer_detail", Route: "/customers/{id}", Title: "Customer", Layout: "app", Body: []appspec.AppSpecBlock{
				{Kind: "entity_detail", Entity: "customers"},
			}},
		},
		Nav: []appspec.AppSpecNavItem{{Label: "Customers", Href: "/customers"}},
	}
}

func fileContent(files []GeneratedFile, name string) string {
	for _, f := range files {
		if f.Name == name {
			return f.Content
		}
	}
	return ""
}

func sortedFileNames(files []GeneratedFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// TestScreenSeamIdenticalForAnyCount is the additive-file property:
// screens_register.go is byte-identical whether the project declares one
// screen or many. Adding a screen is a new file, never an edit to the seam.
func TestScreenSeamIdenticalForAnyCount(t *testing.T) {
	one := appspec.AppSpec{App: appspec.AppSpecApp{Name: "T", Module: "m"}, Screens: []appspec.AppSpecScreen{
		{Name: "solo", Route: "/", Title: "Solo"},
	}}
	oneFiles, err := RenderAppSpecFiles(one)
	if err != nil {
		t.Fatal(err)
	}
	manyFiles, err := RenderAppSpecFiles(perscreenBP())
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
	files, err := RenderAppSpecFiles(perscreenBP())
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
		"type CustomersCreateScreen struct",
		"mountCustomersScreen(",
		"mountCustomersCreateScreen(",
		"mountCustomerDetailScreen(",
	} {
		if !strings.Contains(crud, want) {
			t.Fatalf("screen_customers_crud.go missing %q:\n%s", want, crud)
		}
	}
	// The edit screen is gone with its route: the record page holds the
	// edit form, so no edit type or mount exists anywhere.
	for _, gone := range []string{"CustomersEditScreen", "CustomersNewScreen", "/edit"} {
		if strings.Contains(crud, gone) {
			t.Fatalf("screen_customers_crud.go still carries %q:\n%s", gone, crud)
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

// TestCrudScreensLiveInCrudFile is the critical additive invariant: an
// entity's screens live in its per-entity crud screen file, never in
// app.go, and render through the appUI builders.
func TestCrudScreensLiveInCrudFile(t *testing.T) {
	files, err := RenderAppSpecFiles(perscreenBP())
	if err != nil {
		t.Fatal(err)
	}
	crud := fileContent(files, "screen_customers_crud.go")
	if !strings.Contains(crud, `appUI.List("customers")`) {
		t.Fatalf("screen_customers_crud.go must render the customers list through appUI:\n%s", crud)
	}
	if strings.Contains(fileContent(files, "app.go"), `appUI.List("customers")`) {
		t.Fatal("app.go must NOT carry the customers screen render")
	}
}
