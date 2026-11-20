package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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
				{Name: "opens", Type: "timestamp", Default: "2026-01-02T09:30:15Z"},
				{Name: "ends", Type: "timestamp", Default: "2026-01-02T09:30:15.25Z"},
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
		"opens":    `ui.Control(ui.ControlConfig{Field: c, Type: "datetime-local", Name: "opens", Value: "2026-01-02T09:30:15", Step: "1"})`,
		"ends":     `ui.Control(ui.ControlConfig{Field: c, Type: "datetime-local", Name: "ends", Value: "2026-01-02T09:30:15.250", Step: "0.001"})`,
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

// A relation select starts with its Default drawn as an option of its
// own. The mount hook that loads the related rows names that option in
// place; appending a second option for the same row listed it twice.
func TestEntityFormRelationMountKeepsDefaultOption(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	js := appSpecEntityFormClientJS(appspec.AppSpecBlock{Kind: "entity_form", Entity: "items"}, map[string]framework.EntityDeclaration{
		"categories": {Name: "categories", Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
		"items":      {Name: "items", Fields: []framework.FieldDeclaration{{Name: "category", Type: "relation", To: "categories", Default: "3"}}},
	}, "/api")
	probe := `const option = (value, textContent) => ({ value, textContent });
const sel = {
  options: [option("", "Select"), option("3", "3")],
  dataset: {},
  getAttribute: (name) => (name === "data-rel-entity" ? "categories" : null),
  appendChild(o) { this.options.push(o); },
};
globalThis.window = {};
globalThis.document = {
  querySelector: () => ({ querySelectorAll: () => [sel] }),
  createElement: () => option("", ""),
};
globalThis.fetch = async () => ({ ok: true, json: async () => ({ data: [{ id: 3, name: "Cat 3" }, { id: 4, name: "Cat 4" }] }) });
await ` + strings.TrimSuffix(js, ";") + `;
console.log(JSON.stringify(sel.options.map((o) => [o.value, o.textContent])));
`
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(nodePath, "probe.mjs")
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, output)
	}
	want := `[["","Select"],["3","Cat 3"],["4","Cat 4"]]`
	if got := strings.TrimSpace(string(output)); got != want {
		t.Errorf("relation options after mount = %s; want %s", got, want)
	}
}

// The generator wrote a default with fmt.Sprint where entityui's create
// form writes it as a control holds it: a JSON default came out as Go's
// map[a:1] instead of {"a":1}, a date string kept its time, and a bool
// spelled "yes" left the box unticked. Each generated control starts at
// the text entityui's create form starts the same field at.
func TestEntityFormDefaultsReadLikeEntityUI(t *testing.T) {
	plus2 := time.FixedZone("plus2", 2*3600)
	day := time.Date(2026, 1, 2, 9, 30, 0, 0, plus2)
	for _, tc := range []struct {
		field framework.FieldDeclaration
		want  string
	}{
		{framework.FieldDeclaration{Type: "string", Default: "0x1F"}, `Value: "0x1F"`},
		{framework.FieldDeclaration{Type: "int", Default: 7}, `Value: "7", Step: "1"`},
		{framework.FieldDeclaration{Type: "decimal", Default: 12.5}, `Value: "12.5", Step: "0.0001"`},
		{framework.FieldDeclaration{Type: "bool", Default: true}, `Checked: true`},
		{framework.FieldDeclaration{Type: "bool", Default: "yes"}, `Checked: true`},
		{framework.FieldDeclaration{Type: "bool", Default: false}, `Checked: false`},
		{framework.FieldDeclaration{Type: "date", Default: day}, `Value: "2026-01-02"`},
		{framework.FieldDeclaration{Type: "date", Default: "2026-01-02T00:00:00Z"}, `Value: "2026-01-02"`},
		{framework.FieldDeclaration{Type: "timestamp", Default: day}, `Value: "2026-01-02T07:30"})`},
		{framework.FieldDeclaration{Type: "timestamp", Default: day.Add(15 * time.Second)}, `Value: "2026-01-02T07:30:15", Step: "1"`},
		{framework.FieldDeclaration{Type: "timestamp", Default: day.Add(15250 * time.Millisecond)}, `Value: "2026-01-02T07:30:15.250", Step: "0.001"`},
		{framework.FieldDeclaration{Type: "json", Default: map[string]any{"a": 1.0, "b": []any{"x"}}}, `Value: "{\"a\":1,\"b\":[\"x\"]}"`},
		{framework.FieldDeclaration{Type: "json", Default: []any{1.0, "x"}}, `Value: "[1,\"x\"]"`},
		{framework.FieldDeclaration{Type: "string"}, ``},
		{framework.FieldDeclaration{Type: "image", Default: "uploads/a.png"}, ``},
	} {
		tc.field.Name = "f"
		expr := appSpecEntityFormExpr(appspec.AppSpec{}, appspec.AppSpecScreen{Name: "home"},
			appspec.AppSpecBlock{Kind: "entity_form", Entity: "items"}, nil,
			map[string]framework.EntityDeclaration{"items": {Name: "items", Fields: []framework.FieldDeclaration{tc.field}}}, "/api")
		if tc.want == "" {
			if strings.Contains(expr, "Value: ") {
				t.Errorf("%s %v: want no starting value in:\n%s", tc.field.Type, tc.field.Default, expr)
			}
			continue
		}
		if !strings.Contains(expr, tc.want) {
			t.Errorf("%s %v: want %s in:\n%s", tc.field.Type, tc.field.Default, tc.want, expr)
		}
	}
}
