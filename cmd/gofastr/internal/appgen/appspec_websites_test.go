package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An entity_list or entity_detail inside a section or card is the same
// screen as one at the top of the body: the list's create: true gets its
// /create screen, the detail mounts as a drawer over the list, both land
// in the entity's CRUD file, and the generated e2e test targets them.
func TestAppSpec_NestedCRUDBlocksSynthesized(t *testing.T) {
	section := func(children ...appspec.AppSpecBlock) []appspec.AppSpecBlock {
		return []appspec.AppSpecBlock{{Kind: "section", Props: map[string]any{"heading": "Rows"}, Children: []appspec.AppSpecBlock{
			{Kind: "card", Children: children},
		}}}
	}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Shop", Module: "example.com/shop", APIPrefix: "api"},
		Entities: []framework.EntityDeclaration{
			{Name: "widgets", Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string", Required: true},
			}},
		},
		Nav: []appspec.AppSpecNavItem{{Label: "Widgets", Href: "/app/widgets"}},
		Screens: []appspec.AppSpecScreen{
			{Name: "widgets", Route: "/app/widgets", Layout: "app", Body: section(
				appspec.AppSpecBlock{Kind: "entity_list", Entity: "widgets", Fields: []string{"name"}, Create: true},
			)},
			{Name: "widget_detail", Route: "/app/widgets/{id}", Layout: "app", Body: section(
				appspec.AppSpecBlock{Kind: "entity_detail", Entity: "widgets"},
			)},
		},
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("validateAppSpec: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	crudFile := fileContent(files, "screen_widgets_crud.go")
	if crudFile == "" {
		t.Fatalf("the nested list and detail screens were not classified as CRUD; files=%v", sortedFileNames(files))
	}
	clitest.AssertContains(t, crudFile, `appUI.Create("widgets").Base("/app/widgets")`)
	clitest.AssertContains(t, crudFile, `"/app/widgets/create"`)
	clitest.AssertContains(t, crudFile, `record.Intercept = &app.Intercept{From: "/app/widgets", As: app.ScreenDrawer}`)
	target, ok := appSpecE2EWritableTarget(bp)
	if !ok || target.newRoute != "/app/widgets/create" || target.detailBase != "/app/widgets" {
		t.Fatalf("e2e target = %+v, %v; want the nested list's create and detail routes", target, ok)
	}
}

// TestAppSpec_ExtensionSeamShipsAlways: the entityui seam ships even
// with zero entities, so a later --add entity screen never edits an owned
// file, and it imports only the entityui package.
func TestAppSpec_ExtensionSeamShipsAlways(t *testing.T) {
	if !strings.Contains(appSpecExtensionsGo, `"github.com/DonaldMurillo/gofastr/framework/entityui"`) {
		t.Fatal("extensions.go must import framework/entityui")
	}
	if !strings.Contains(appSpecExtensionsGo, "var appExtensions = entityui.Extensions{}") {
		t.Fatal("extensions.go must declare the empty appExtensions value")
	}
	for _, banned := range []string{"appResources", "ui/resource"} {
		if strings.Contains(appSpecExtensionsGo, banned) {
			t.Errorf("extensions.go still names the resource engine (%q)", banned)
		}
	}
	// An entity-less app still gets the seam.
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Seam", Module: "example.com/seam"}}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	if fileContent(files, "extensions.go") == "" {
		t.Fatal("extensions.go must ship beside app.go even with no entities")
	}
}
