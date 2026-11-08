package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestEntityListFilterEmitsWhere pins the R8-EX3-01 emission: an
// entity_list's `filter: field=value` becomes ListBuilder.Where on the
// list's builder chain, with the column's declared name (a camelJSON
// spelling resolves to it) so entityui's byName lookup finds the field.
func TestEntityListFilterEmitsWhere(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "D", Module: "ex.com/d"},
		Entities: []framework.EntityDeclaration{{
			Name: "projects",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
				{Name: "featured", Type: "bool"},
			},
		}},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Route: "/",
			Body: []appspec.AppSpecBlock{{
				Kind: "entity_list", Entity: "projects",
				Fields: []string{"title"}, Filter: "featured=true",
			}},
		}},
	}
	files, err := RenderAppSpecCode(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var src strings.Builder
	for _, f := range files {
		src.WriteString(f.Content)
	}
	if !strings.Contains(src.String(), `.Where("featured", "true")`) {
		t.Fatalf("the pinned filter did not reach the list builder:\n%s", src.String())
	}
}
