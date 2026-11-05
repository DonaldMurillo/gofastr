package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestEntityFormUsesWireNames(t *testing.T) {
	var fields []framework.FieldDeclaration
	for _, kind := range []string{"string", "text", "enum", "relation", "bool", "int", "date", "datetime", "file", "email"} {
		fields = append(fields, framework.FieldDeclaration{Name: kind + "_line_1", Type: kind, To: "postal_addresses", Values: []string{"home", "work"}})
	}
	expression := appSpecEntityFormExpr(appspec.AppSpec{}, appspec.AppSpecScreen{Name: "home"},
		appspec.AppSpecBlock{Kind: "entity_form", Entity: "addresses"}, nil,
		map[string]framework.EntityDeclaration{"addresses": {Name: "addresses", Fields: fields}}, "/api")
	for _, field := range fields {
		wire := field.Type + "Line1"
		if !strings.Contains(expression, `Name: "`+wire+`"`) || strings.Contains(expression, `Name: "`+field.Name+`"`) {
			t.Errorf("%s control must use wire key %q, not column %q:\n%s", field.Type, wire, field.Name, expression)
		}
		if !strings.Contains(expression, `"field-`+field.Name+`"`) || !strings.Contains(expression, `"addresses", "`+field.Name+`",`) {
			t.Errorf("%s control lost its column-based ID or label lookup", field.Type)
		}
	}
	if !strings.Contains(expression, `"data-rel-entity": "postal_addresses"`) {
		t.Error("relation lookup lost its entity name")
	}
}
