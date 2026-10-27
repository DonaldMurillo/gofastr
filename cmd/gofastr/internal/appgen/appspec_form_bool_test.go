package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A generated create form's bool checkbox had no Value, so a checked box
// posted "on", which the bool validator refuses: the create answered 400
// for every ticked box. It submits "true" now, behind a hidden "false"
// the runtime's serializer folds it over (entityui's pair), so an
// unticked box writes false instead of the declared default. A default
// of true starts the box ticked, and a required bool is not a required
// checkbox: unticked is an answer.
func TestEntityFormBoolPostsTrue(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name: "products",
		Fields: []framework.FieldDeclaration{
			{Name: "active", Type: "bool", Default: true},
			{Name: "archived", Type: "bool", Required: true},
		},
	}
	expr := appSpecEntityFormExpr(appspec.AppSpec{},
		appspec.AppSpecScreen{Name: "home"},
		appspec.AppSpecBlock{Kind: "entity_form", Entity: "products"},
		nil, map[string]framework.EntityDeclaration{"products": decl}, "/api",
	)
	for _, tc := range []struct{ name, checked string }{{"active", "true"}, {"archived", "false"}} {
		hidden := `html.Input(html.InputConfig{Type: "hidden", Name: "` + tc.name + `", Value: "false"})`
		box := `ui.Checkbox(ui.ToggleConfig{Name: "` + tc.name + `", Label: i18nui.FieldLabel(ctx, nil, "products", "` + tc.name + `", `
		h, b := strings.Index(expr, hidden), strings.Index(expr, box)
		if h < 0 || b < 0 || h > b {
			t.Fatalf("%s: want the hidden false before the checkbox:\n%s", tc.name, expr)
		}
		rest := expr[b:]
		rest = rest[:strings.Index(rest, "})")]
		if !strings.Contains(rest, `Value: "true"`) || !strings.Contains(rest, "Checked: "+tc.checked) || strings.Contains(rest, "Required") {
			t.Errorf("%s checkbox = %s, want Value \"true\", Checked: %s, no Required", tc.name, rest, tc.checked)
		}
	}
}
