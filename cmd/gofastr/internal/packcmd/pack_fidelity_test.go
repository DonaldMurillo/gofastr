package packcmd

import (
	"go/ast"
	"go/parser"
	"maps"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An entity whose create is open to anyone (a contact form, an inquiry)
// is generated as Create: framework.Anyone, a selector, not a string
// literal. Pack read only literals, so the packed spec lost the grant and
// the app regenerated from it refused every anonymous submission.
func TestPack_AccessAnyoneRoundTrips(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Contact", Module: "example.com/contact", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "contacts",
			Exposure: &framework.ExposureDeclaration{Access: &framework.AccessDeclaration{
				Read: "contacts:read", Create: framework.Anyone, Update: "contacts:write", Delete: "contacts:admin",
			}},
			Fields: []framework.FieldDeclaration{{Name: "email", Type: "string", Required: true}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	got, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].Exposure == nil || got.Entities[0].Exposure.Access == nil {
		t.Fatalf("access lost on read-back: %#v", got.Entities)
	}
	acc := got.Entities[0].Exposure.Access
	want := framework.AccessDeclaration{Read: "contacts:read", Create: framework.Anyone, Update: "contacts:write", Delete: "contacts:admin"}
	if *acc != want {
		t.Errorf("access = %+v, want %+v", *acc, want)
	}
}

// The generator writes a section's label, class and id into its
// SectionConfig; pack read only heading, eyebrow and description, so a
// labelled, classed section (examples/lms's Featured Courses) packed bare.
func TestPack_SectionLabelClassIDRoundTrip(t *testing.T) {
	src := `ui.Section(ui.SectionConfig{Ctx: ctx, Heading: "", Eyebrow: "", Description: "", Label: "Featured Courses", Class: "featured-courses", ID: "featured"}, ui.Stack(ui.StackConfig{Align: ui.AlignStart}))`
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := reverseBlock(e.(*ast.CallExpr), nil)
	if !ok || b.Kind != "section" {
		t.Fatalf("reverseBlock = %+v, %v; want a section", b, ok)
	}
	want := map[string]any{"label": "Featured Courses", "class": "featured-courses", "id": "featured"}
	if !maps.Equal(b.Props, want) {
		t.Errorf("section props = %v, want %v", b.Props, want)
	}
}

// The generator writes app.theme.dark as an overlay on the framework's
// dark palette: theme.DarkColors = uitheme.Default().DarkColors, then one
// index assignment per key, plus a derived ink for a fill the spec names
// without its -fg. Pack read only the old map-literal spelling, so the
// whole dark palette (and the header's theme toggle, which hangs off it)
// vanished from the packed spec.
func TestPack_DarkThemeOverlayRoundTrips(t *testing.T) {
	dark := map[string]string{"background": "#15141B", "primary": "#8B80F2", "text": "#ECEAF3"}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name: "Dark", Module: "example.com/dark", DBDriver: "sqlite", DBURL: "file:x.db",
			Theme:     map[string]string{"primary": "#4338CA"},
			ThemeDark: dark,
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	got, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	if !maps.Equal(got.App.ThemeDark, dark) {
		t.Errorf("theme.dark = %v, want %v (the derived primary-fg ink is not authored)", got.App.ThemeDark, dark)
	}
}
