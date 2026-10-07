package entityui

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The columns menu. State lives in the namespaced cols param: a
// comma-separated list of field names in display order (the checkbox
// form's multiple-values spelling — cols=a&cols=b — parses too, and
// submits in DOM order). Every name must be a visible, non-omitted
// field; an unknown, Hidden, omitted or duplicate name makes the whole
// param ignored, falling back to the list's resolved columns, never an
// error page: it is someone's bookmark, not a configuration bug. The
// title field carries the record link, so it may not be hidden — a cols
// that leaves it out gets it back in first position. Columns change
// what a row shows, not which rows match, so the page stays.

// parseColsNames reads the cols param's names: every value it carries,
// split on commas, trimmed, empties dropped; nil when the param is
// absent or names nothing.
func (s *listState) parseColsNames() []string {
	raw := s.q[s.p.cols]
	if len(raw) == 0 {
		return nil
	}
	var out []string
	for _, v := range raw {
		for _, name := range strings.Split(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// validColsNames reports whether names is a cols value this list
// accepts: every name a visible, non-omitted field, none listed twice.
func (s *listState) validColsNames(names []string) bool {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if _, ok := s.m.field(name); !ok || s.m.omitted(name) {
			return false
		}
		if seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

// applyColsParam settles the shown columns from the cols param: an
// explicit param in the URL wins over a saved view's columns, and a
// refused value is ignored wholesale — the default resolution stands.
func (s *listState) applyColsParam() {
	names := s.parseColsNames()
	if len(names) == 0 || !s.validColsNames(names) {
		return
	}
	s.setColumns(names)
}

// setColumns settles the shown columns on names, keeping the title
// field first when the list leaves it out: it carries the record link.
func (s *listState) setColumns(names []string) {
	if tf := s.m.titleField(); tf != "" && !slices.Contains(names, tf) {
		names = append([]string{tf}, names...)
	}
	s.columns = names
}

// columnsMenu draws the columns control: a disclosure holding a GET
// form — one checkbox per available field, checked for shown, the
// title field pinned on — with the list's other params as hidden
// inputs, move links that rewrite cols in display order, and a Reset
// link that drops it. The form keeps the page: columns do not change
// the row set.
func (b *ListBuilder) columnsMenu(ctx context.Context, s *listState) render.HTML {
	if !b.colsMenu || len(s.available) == 0 {
		return ""
	}
	m := s.m
	tf := m.titleField()
	shown := make(map[string]bool, len(s.columns))
	for _, c := range s.columns {
		shown[c] = true
	}
	// The checkbox order is the display order, the hidden ones after.
	order := make([]string, 0, len(s.available))
	for _, c := range s.columns {
		if slices.Contains(s.available, c) {
			order = append(order, c)
		}
	}
	if tf != "" && !shown[tf] && slices.Contains(s.available, tf) {
		// The default resolution may leave the title field out (a
		// builder's Columns without it); the checkbox still names it,
		// pinned on.
		order = append(order, tf)
		shown[tf] = true
	}
	for _, c := range s.available {
		if !shown[c] {
			order = append(order, c)
		}
	}

	fields := make([]render.HTML, 0, len(order)+len(s.q)+1)
	for _, k := range slices.Sorted(maps.Keys(s.q)) {
		if k == s.p.cols {
			continue
		}
		if vs := s.q[k]; len(vs) > 0 {
			fields = append(fields, hiddenInput(k, vs[0]))
		}
	}
	prefix := "eui-" + listIDSafe(s.key, m.name) + "-col"
	for _, name := range order {
		isTitle := tf != "" && name == tf
		row := []render.HTML{ui.Checkbox(ui.ToggleConfig{
			Name:     s.p.cols,
			ID:       prefix + "-" + name,
			Value:    name,
			Checked:  shown[name] || isTitle,
			Disabled: isTitle,
			Label:    m.label(ctx, name),
		})}
		row = append(row, b.columnMoves(ctx, s, name)...)
		fields = append(fields, ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, row...))
	}
	return ui.Collapsible(ui.CollapsibleConfig{
		Summary: i18nui.T(ctx, i18nui.KeyEntityColumns),
	}, ui.Form(ui.FormConfig{
		Action:      s.path,
		Method:      "GET",
		Ctx:         ctx,
		SubmitLabel: i18nui.T(ctx, i18nui.KeyFilterApply),
	}, fields...), ui.LinkButton(ui.LinkButtonConfig{
		Label:   i18nui.T(ctx, i18nui.KeyFilterReset),
		Href:    s.dropColsHref(),
		Variant: ui.ButtonGhost,
	}))
}

// columnMoves are one shown column's move links: up and down anchors
// that rewrite cols with the column swapped, keeping every other param.
// A column with nowhere to move draws nothing.
func (b *ListBuilder) columnMoves(ctx context.Context, s *listState, name string) []render.HTML {
	i := slices.Index(s.columns, name)
	if i < 0 {
		return nil
	}
	var out []render.HTML
	add := func(label, aria string, j int) {
		cols := slices.Clone(s.columns)
		cols[i], cols[j] = cols[j], cols[i]
		q := s.carry(s.p.cols)
		q.Set(s.p.cols, strings.Join(cols, ","))
		out = append(out, ui.Link(ui.LinkConfig{
			Href: listHref(s.path, q),
			Text: label,
			ExtraAttrs: html.Attrs{
				"aria-label": aria,
			},
		}))
	}
	if i > 0 {
		add("↑", i18nui.TVars(ctx, i18nui.KeyEntityColumnsUp, map[string]string{"column": s.m.label(ctx, name)}), i-1)
	}
	if i < len(s.columns)-1 {
		add("↓", i18nui.TVars(ctx, i18nui.KeyEntityColumnsDown, map[string]string{"column": s.m.label(ctx, name)}), i+1)
	}
	return out
}

// dropColsHref is the same URL with no cols param: the list's resolved
// columns, whatever else the URL carries.
func (s *listState) dropColsHref() string {
	return listHref(s.path, s.carry(s.p.cols))
}
