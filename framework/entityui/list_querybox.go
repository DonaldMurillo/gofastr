package entityui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The query box: the filter typed by hand. The chips narrow what a
// reader picked; the box is where they write it. It is a field of the
// toolbar's GET form, inside the Filters dropdown, named the list's
// filter param, so it submits exactly what the chips and every other
// link read: one source of truth in the URL, and a bad text keeps the
// filter-did-not-apply warning instead of failing the page. The page
// param is dropped with the rest of the toolbar's: a new narrowing
// starts on page one.

// queryField draws the query box's field, prefilled with the active
// filter text (an open saved view's included). Its help says how to
// write one: the operators, how to quote and join, an example built
// from the entity's own fields (the placeholder too), and the
// queryable field names.
func (b *ListBuilder) queryField(ctx context.Context, s *listState) render.HTML {
	if !b.queryBox {
		return ""
	}
	m := s.m
	queryable := make([]schema.Field, 0, len(m.fields))
	names := make([]string, 0, len(m.fields))
	for _, f := range m.fields {
		if f.NoQuery {
			continue
		}
		queryable = append(queryable, f)
		names = append(names, f.Name)
	}
	help := []string{i18nui.T(ctx, i18nui.KeyEntityQueryBoxHelp)}
	example := queryExample(queryable)
	if example != "" {
		help = append(help, i18nui.TVars(ctx, i18nui.KeyEntityQueryBoxExample, map[string]string{"example": example}))
	}
	help = append(help, i18nui.TVars(ctx, i18nui.KeyEntityQueryBoxFields, map[string]string{
		"fields": strings.Join(names, ", "),
	}))
	return ui.TextField(ui.TextFieldConfig{
		Name:        s.p.filter,
		ID:          "eui-" + listIDSafe(s.key, m.name) + "-filter",
		Label:       i18nui.T(ctx, i18nui.KeyEntityQueryBoxField),
		Value:       s.filterText,
		Placeholder: example,
		Help:        strings.Join(help, " "),
	})
}

// queryExample is a filter the parser accepts, written from the
// entity's own fields: the first two of an enum against its first
// value, a number against 100, a text field's contains and a
// boolean's true, joined with "and". It never names the id, a relation
// (its value is another record's id), a date or an enum value the
// quotes would have to escape, and is "" when nothing else is left.
func queryExample(fields []schema.Field) string {
	var enum, number, text, flag string
	for _, f := range fields {
		switch {
		case f.Name == "id":
		case f.Type == schema.Enum:
			if enum == "" && len(f.Values) > 0 && !strings.ContainsAny(f.Values[0], `"\`) {
				enum = f.Name + ` = "` + f.Values[0] + `"`
			}
		case f.Type == schema.Int, f.Type == schema.Float, f.Type == schema.Decimal:
			if number == "" {
				number = f.Name + " > 100"
			}
		case f.Type == schema.String, f.Type == schema.Text:
			if text == "" {
				text = f.Name + ` contains "a"`
			}
		case f.Type == schema.Bool:
			if flag == "" {
				flag = f.Name + " = true"
			}
		}
	}
	parts := make([]string, 0, 2)
	for _, p := range []string{enum, number, text, flag} {
		if p != "" && len(parts) < 2 {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " and ")
}
