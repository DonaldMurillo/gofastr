package packcmd

import (
	"go/ast"
	"go/parser"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestPack_EntityListFilterRoundTrips proves the pinned filter survives
// pack: the emitted `.Where("featured", "true")` reads back into the
// entity_list block's filter, so a packed spec regenerates the pin.
func TestPack_EntityListFilterRoundTrips(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Folio", Module: "example.com/folio", DBDriver: "sqlite", DBURL: "file:f.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "projects",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "featured", Type: "bool"},
			},
		}},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Route: "/",
			Body: []appspec.AppSpecBlock{{
				Kind: "entity_list", Entity: "projects",
				Fields: []string{"title"}, Limit: 3, Filter: "featured=true",
			}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	got, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	if len(got.Screens) != 1 || len(got.Screens[0].Body) != 1 {
		t.Fatalf("fixture misread: %+v", got.Screens)
	}
	if f := got.Screens[0].Body[0].Filter; f != "featured=true" {
		t.Fatalf("the pinned filter did not survive pack: %q", f)
	}
}

// TestReverseEntityUIRefusesTwoWherePins: the app spec's filter holds one
// equality, so a builder chain pinning two (hand-written app code) is not
// reversible — pack leaves the screen to its difference report instead of
// silently dropping a pin.
func TestReverseEntityUIRefusesTwoWherePins(t *testing.T) {
	src := `appUI.List("projects").Where("featured", "true").Where("status", "open").RenderCtx(ctx)`
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := reverseEntityUI(e.(*ast.CallExpr), nil); ok {
		t.Fatalf("a chain with two Where pins reversed to %+v; one equality is the app spec's whole filter", b)
	}
}
