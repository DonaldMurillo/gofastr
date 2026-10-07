package entityui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
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
// filter text (an open saved view's included) and helped by the
// entity's queryable field names.
func (b *ListBuilder) queryField(ctx context.Context, s *listState) render.HTML {
	if !b.queryBox {
		return ""
	}
	m := s.m
	names := make([]string, 0, len(m.fields))
	for _, f := range m.fields {
		if f.NoQuery {
			continue
		}
		names = append(names, f.Name)
	}
	return ui.TextField(ui.TextFieldConfig{
		Name:  s.p.filter,
		ID:    "eui-" + listIDSafe(s.key, m.name) + "-filter",
		Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxField),
		Value: s.filterText,
		Help: i18nui.TVars(ctx, i18nui.KeyEntityQueryBoxHelp, map[string]string{
			"fields": strings.Join(names, ", "),
		}),
	})
}
