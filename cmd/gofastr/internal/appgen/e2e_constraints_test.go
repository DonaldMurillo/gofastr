package appgen

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestE2ESkipsUnfillableConstraints(t *testing.T) {
	for _, field := range []framework.FieldDeclaration{
		{Name: "value", Type: "int", Required: true, Min: new(float64(10))},
		{Name: "value", Type: "float", Required: true, Max: new(float64(-1))},
		{Name: "value", Type: "string", Required: true, Min: new(float64(20))},
		{Name: "value", Type: "string", Required: true, Pattern: "^[A-Z]{4}$"},
	} {
		specification := appspec.AppSpec{Entities: []framework.EntityDeclaration{
			{Name: "alpha", Fields: []framework.FieldDeclaration{field}},
			{Name: "beta", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}}},
		}, Screens: []appspec.AppSpecScreen{
			{Name: "alpha", Route: "/alpha", Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "alpha", Create: true}}},
			{Name: "beta", Route: "/beta", Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "beta", Create: true}}},
		}}
		target, found := appSpecE2EWritableTarget(specification)
		if !found || target.entity != "beta" {
			t.Errorf("constrained %s picked invalid synthetic target: %+v, %v", field.Type, target, found)
		}
	}
}
