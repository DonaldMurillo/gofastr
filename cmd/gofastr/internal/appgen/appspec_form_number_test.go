package appgen

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A generated create form drew every number at the browser's default
// step of 1, so native validation refused 12.99 before the RPC ran and
// a required price could not be created with cents. Each number type
// carries the step its column admits, as entityui's form does: an
// integer steps by 1, a decimal by its four fraction digits, a float by
// any amount.
func TestEntityFormNumberSteps(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name: "products",
		Fields: []framework.FieldDeclaration{
			{Name: "stock", Type: "int"},
			{Name: "count", Type: "integer"},
			{Name: "price", Type: "decimal", Required: true},
			{Name: "weight", Type: "float"},
		},
	}
	expr := appSpecEntityFormExpr(appspec.AppSpec{},
		appspec.AppSpecScreen{Name: "home"},
		appspec.AppSpecBlock{Kind: "entity_form", Entity: "products"},
		nil, map[string]framework.EntityDeclaration{"products": decl}, "/api",
	)
	for name, step := range map[string]string{"stock": "1", "count": "1", "price": "0.0001", "weight": "any"} {
		re := regexp.MustCompile(`ui\.Control\(ui\.ControlConfig\{Field: c, Type: "number", Name: "` + name + `", Step: "` + regexp.QuoteMeta(step) + `"\}\)`)
		if !re.MatchString(expr) {
			t.Errorf("%s: want a number control with step %q:\n%s", name, step, expr)
		}
	}
	if !regexp.MustCompile(`ui\.FormField\(ui\.FormFieldConfig\{Label: i18nui\.FieldLabel\(ctx, nil, "products", "price", "Price"\), For: "field-price", Required: true,`).MatchString(expr) {
		t.Errorf("the required decimal's field lost Required:\n%s", expr)
	}
}
