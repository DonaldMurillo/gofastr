package appgen

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A generated create form drew every control empty, so an untouched
// field with a Default submitted "" and the row stored that instead of
// the Default an API create without the field gets. Each control starts
// at its field's Default, as entityui's create form does, and a field
// with no Default still starts empty.
func TestEntityFormStartsAtDefaults(t *testing.T) {
	decls := map[string]framework.EntityDeclaration{
		"categories": {Name: "categories", Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		"items": {
			Name: "items",
			Fields: []framework.FieldDeclaration{
				{Name: "code", Type: "string", Default: "0x1F"},
				{Name: "qty", Type: "int", Default: 7},
				{Name: "price", Type: "decimal", Default: 12.5},
				{Name: "status", Type: "enum", Values: []string{"draft", "live"}, Default: "live"},
				{Name: "category", Type: "relation", To: "categories", Default: "3"},
				{Name: "notes", Type: "text", Default: "hello"},
				{Name: "due", Type: "date", Default: "2026-01-02"},
				{Name: "starts", Type: "timestamp", Default: "2026-01-02T09:30:00Z"},
				{Name: "label", Type: "string"},
			},
		},
	}
	expr := appSpecEntityFormExpr(appspec.AppSpec{},
		appspec.AppSpecScreen{Name: "home"},
		appspec.AppSpecBlock{Kind: "entity_form", Entity: "items"},
		nil, decls, "/api",
	)
	for name, want := range map[string]string{
		"code":     `ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "code", Value: "0x1F"})`,
		"qty":      `ui.Control(ui.ControlConfig{Field: c, Type: "number", Name: "qty", Value: "7", Step: "1"})`,
		"price":    `ui.Control(ui.ControlConfig{Field: c, Type: "number", Name: "price", Value: "12.5", Step: "0.0001"})`,
		"status":   `Required: false, Selected: "live"})`,
		"category": `Required: false, Selected: "3", ExtraAttrs: html.Attrs{"data-rel-entity": "categories"}})`,
		"notes":    `ui.TextArea(ui.TextAreaConfig{Ctx: ctx, Name: "notes", Label: i18nui.FieldLabel(ctx, nil, "items", "notes", "Notes"), ID: "field-notes", Required: false, Value: "hello"})`,
		"due":      `ui.DateField(ui.DateFieldConfig{Ctx: ctx, Name: "due", Label: i18nui.FieldLabel(ctx, nil, "items", "due", "Due"), ID: "field-due", Required: false, Value: "2026-01-02"})`,
		"starts":   `ui.Control(ui.ControlConfig{Field: c, Type: "datetime-local", Name: "starts", Value: "2026-01-02T09:30"})`,
		"label":    `ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "label"})`,
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("%s: want %s in:\n%s", name, want, expr)
		}
	}
	if n := len(regexp.MustCompile(`Selected: `).FindAllString(expr, -1)); n != 2 {
		t.Errorf("want Selected on the enum and relation only, got %d:\n%s", n, expr)
	}
}

// The relation select draws its Default as an option of its own before
// the mount hook loads the related rows, so the hook names that option
// instead of adding a second one with the same value.
func TestEntityFormRelationMountKeepsDefaultOption(t *testing.T) {
	js := appSpecEntityFormClientJS(appspec.AppSpecBlock{Kind: "entity_form", Entity: "items"}, map[string]framework.EntityDeclaration{
		"categories": {Name: "categories", Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		"items":      {Name: "items", Fields: []framework.FieldDeclaration{{Name: "category", Type: "relation", To: "categories", Default: "3"}}},
	}, "/api")
	if !strings.Contains(js, "Array.from(sel.options).find((o) => o.value === value)") {
		t.Errorf("the mount hook does not reuse an existing option:\n%s", js)
	}
}
