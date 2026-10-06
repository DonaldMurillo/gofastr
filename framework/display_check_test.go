package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func displayCheckFields() []schema.Field {
	return []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}},
		{Name: "rush", Type: schema.Bool},
		{Name: "frozen", Type: schema.Enum, Values: []string{"a", "b"}, ReadOnly: true},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "notes", Type: schema.String, NoQuery: true},
		{Name: "kind", Type: schema.Enum, Values: []string{"x", "y"}, NoQuery: true},
		{Name: "paid_on", Type: schema.String},
	}
}

func tryDisplay(t *testing.T, d *entity.DisplayConfig) error {
	t.Helper()
	app := atomicTestApp(t)
	return app.TryEntity("invoices", EntityConfig{Fields: displayCheckFields(), Display: d})
}

func TestDisplayQueriesAcceptValid(t *testing.T) {
	err := tryDisplay(t, &entity.DisplayConfig{
		Views: []entity.ListView{{Key: "open", Where: `status = "open" and rush = true`}},
		Fields: map[string]entity.FieldDisplay{
			"paid_on": {ShowWhen: `status in ["paid"]`},
			"title":   {ShowWhen: `rush = false`},
			"notes":   {ShowWhen: `kind = "x"`},
		},
	})
	if err != nil {
		t.Fatalf("valid display refused: %v", err)
	}
}

func TestDisplayViewWhereRefused(t *testing.T) {
	cases := map[string]string{
		"syntax":  `status = `,
		"unknown": `nope = "x"`,
		"hidden":  `secret = "x"`,
		"noquery": `notes = "x"`,
	}
	for name, where := range cases {
		t.Run(name, func(t *testing.T) {
			err := tryDisplay(t, &entity.DisplayConfig{
				Views: []entity.ListView{{Key: "v", Where: where}},
			})
			if err == nil || !strings.Contains(err.Error(), `display view "v" where`) {
				t.Fatalf("Where %q: want a view-where refusal, got %v", where, err)
			}
			if !strings.Contains(err.Error(), `entity "invoices"`) {
				t.Fatalf("refusal must name the entity: %v", err)
			}
		})
	}
}

func TestDisplayShowWhenRefused(t *testing.T) {
	cases := []struct {
		name, show, want string
		hints            map[string]entity.FieldDisplay
	}{
		{"compound", `status = "paid" and rush = true`, "one `field = value`", nil},
		{"op", `status != "paid"`, "one `field = value`", nil},
		{"string", `title = "x"`, "must be an Enum or Bool", nil},
		{"readonly", `frozen = "a"`, "not an editable form field", nil},
		{"hidden", `secret = "x"`, "", nil},
		{"value", `status = "void"`, `"void" is not one of`, nil},
		{"listvalue", `status in ["paid", "void"]`, `"void" is not one of`, nil},
		{"self", `paid_on = "x"`, "cannot depend on itself", nil},
		{"omit", `status = "paid"`, "not an editable form field",
			map[string]entity.FieldDisplay{"status": {Omit: true}}},
		{"locked", `status = "paid"`, "not an editable form field",
			map[string]entity.FieldDisplay{"status": {Locked: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hints := map[string]entity.FieldDisplay{"paid_on": {ShowWhen: c.show}}
			for k, v := range c.hints {
				hints[k] = v
			}
			err := tryDisplay(t, &entity.DisplayConfig{Fields: hints})
			if err == nil || !strings.Contains(err.Error(), "display fields[paid_on] show_when") {
				t.Fatalf("ShowWhen %q: want a show_when refusal, got %v", c.show, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ShowWhen %q: want %q in %v", c.show, c.want, err)
			}
		})
	}
}

func TestDisplayAutoControllerRefused(t *testing.T) {
	app := atomicTestApp(t)
	fields := append(displayCheckFields(),
		schema.Field{Name: "stage", Type: schema.Enum, Values: []string{"a"}, AutoGenerate: schema.AutoTimestamp})
	err := app.TryEntity("invoices", EntityConfig{Fields: fields, Display: &entity.DisplayConfig{
		Fields: map[string]entity.FieldDisplay{"paid_on": {ShowWhen: `stage = "a"`}},
	}})
	if err == nil || !strings.Contains(err.Error(), "not an editable form field") {
		t.Fatalf("auto-generated controller: got %v", err)
	}
}
