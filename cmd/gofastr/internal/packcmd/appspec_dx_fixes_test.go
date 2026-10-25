package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// stubFontFetcher swaps in a fake font fetcher for the duration of a test so
// the suite never touches the network.
func stubFontFetcher(t *testing.T, fn func(family string) ([]byte, error)) {
	t.Helper()
	prev := appgen.FontFetcher
	appgen.FontFetcher = fn
	t.Cleanup(func() { appgen.FontFetcher = prev })
}

// ---- Item 2: chart/stat source registration ------------------------------

// A chart sourced from an entity that has no list/detail screen reads
// through appUI.GroupBars with no registry entry to register.
func TestChartSourceNeedsNoRegistry(t *testing.T) {
	crudOn := true
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Dash", Module: "example.com/dash"},
		Entities: []framework.EntityDeclaration{{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "tickets", Exposure: &framework.ExposureDeclaration{CRUD: &crudOn}, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
		},
		}},
		Screens: []appspec.AppSpecScreen{{
			Name:  "dashboard",
			Route: "/",
			Body: []appspec.AppSpecBlock{{
				Kind: "bar_chart",
				Props: map[string]any{
					"title":  "By status",
					"source": map[string]any{"entity": "tickets", "group_by": "status"},
				},
			}},
		}},
	}
	files := mustRenderAppSpecFiles(t, bp)
	// The chart reads through appUI.GroupBars, which resolves the entity at
	// render time: a sourced entity with no screen needs no registry entry
	// and no crud file. The dashboard renders through the app's one UI.
	screens := allScreenContent(files)
	if !strings.Contains(screens, `appUI.GroupBars(ctx, "tickets", "status")`) {
		t.Fatalf("bar_chart must read through appUI.GroupBars:\n%s", screens)
	}
	if f := filesByName(files)["screen_tickets_crud.go"]; f != "" {
		t.Fatalf("a screen-less sourced entity must not grow a crud file:\n%s", f)
	}
	if f := filesByName(files)["resource.go"]; f != "" {
		t.Fatalf("the resource registry is gone with the resource engine:\n%s", f)
	}
}
