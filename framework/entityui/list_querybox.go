package entityui

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The query box: the filter typed by hand. The chips narrow what a
// reader picked; the box is where they write it. It is its own GET form
// to the page's path, named the list's filter param, so it submits
// exactly what the chips and every other link read: one source of truth
// in the URL, and a bad text keeps the filter-did-not-apply warning
// instead of failing the page. The form carries the request state it
// does not own as hidden inputs — the same round trip the toolbar's
// form makes — so applying a typed filter keeps the search, the facets,
// the view and the sort. The page param is dropped: a new narrowing
// starts on page one.

// queryBoxForm draws the query box: a labelled text field and an Apply
// button, prefilled with the active filter text (an open saved view's
// included) and helped by the entity's queryable field names. It sits in
// a collapsible among the list's tools, open while a filter is set.
func (b *ListBuilder) queryBoxForm(ctx context.Context, s *listState) render.HTML {
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
	fields := make([]render.HTML, 0, len(s.q)+2)
	for _, k := range slices.Sorted(maps.Keys(s.q)) {
		if k == s.p.filter || k == s.p.page {
			continue
		}
		if vs := s.q[k]; len(vs) > 0 {
			fields = append(fields, hiddenInput(k, vs[0]))
		}
	}
	fields = append(fields, ui.TextField(ui.TextFieldConfig{
		Name:  s.p.filter,
		ID:    "eui-" + listIDSafe(s.key, m.name) + "-filter",
		Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxField),
		Value: s.filterText,
		Help: i18nui.TVars(ctx, i18nui.KeyEntityQueryBoxHelp, map[string]string{
			"fields": strings.Join(names, ", "),
		}),
	}))
	return ui.Collapsible(ui.CollapsibleConfig{
		Summary: i18nui.T(ctx, i18nui.KeyEntityQueryBoxLabel),
		Open:    s.filterText != "",
		Name:    s.toolGroup(),
	}, ui.Form(ui.FormConfig{
		Action:      s.path,
		Method:      "GET",
		Ctx:         ctx,
		SubmitLabel: i18nui.T(ctx, i18nui.KeyFilterApply),
	}, fields...))
}

// toolGroup names the exclusive set the list's tool collapsibles share
// (the query box, the columns menu, the save-view form): opening one
// closes the others, so the tools take one panel's height at most.
func (s *listState) toolGroup() string {
	return "eui-" + listIDSafe(s.key, s.m.name) + "-tools"
}
