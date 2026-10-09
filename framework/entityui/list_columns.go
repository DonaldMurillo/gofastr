package entityui

import (
	"context"
	"net/url"
	"slices"
	"strings"

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

// columnsMenu draws the columns control (ui.ColumnPicker): one row per
// available field in display order (the hidden ones after), each a
// link to the same URL with that column toggled in cols, and beside a
// shown one, links that move it one place; the title field is on and
// locked, since it carries the record link. Reset drops cols. Every row is a navigation that keeps the other
// params and the page: columns change what a row shows, not which rows
// match.
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
	order := make([]string, 0, len(s.available))
	for _, c := range s.columns {
		if slices.Contains(s.available, c) {
			order = append(order, c)
		}
	}
	if tf != "" && !shown[tf] && slices.Contains(s.available, tf) {
		// The default resolution may leave the title field out (a
		// builder's Columns without it); its row still names it,
		// pinned on.
		order = append(order, tf)
		shown[tf] = true
	}
	for _, c := range s.available {
		if !shown[c] {
			order = append(order, c)
		}
	}

	href := func(names []string) string {
		q := s.carryWithSort(s.p.cols)
		q.Set(s.p.cols, strings.Join(names, ","))
		return listHref(s.path, q)
	}
	var shownOrder []string
	for _, c := range order {
		if shown[c] {
			shownOrder = append(shownOrder, c)
		}
	}
	cols := make([]ui.ColumnChoice, 0, len(order))
	for _, name := range order {
		c := ui.ColumnChoice{Label: m.label(ctx, name), Shown: shown[name], Locked: tf != "" && name == tf}
		if !c.Locked {
			next := make([]string, 0, len(order))
			for _, o := range order {
				if o == name {
					if !shown[o] {
						next = append(next, o)
					}
				} else if shown[o] {
					next = append(next, o)
				}
			}
			c.ToggleHref = href(next)
		}
		if i := slices.Index(shownOrder, name); i >= 0 {
			if i > 0 {
				c.UpHref = href(swapped(shownOrder, i, i-1))
			}
			if i < len(shownOrder)-1 {
				c.DownHref = href(swapped(shownOrder, i, i+1))
			}
		}
		cols = append(cols, c)
	}
	return ui.ColumnPicker(ui.ColumnPickerConfig{
		ID:         "eui-" + listIDSafe(s.key, m.name) + "-cols",
		Label:      i18nui.T(ctx, i18nui.KeyEntityColumns),
		Columns:    cols,
		ResetHref:  s.dropColsHref(),
		ResetLabel: i18nui.T(ctx, i18nui.KeyFilterReset),
		Ctx:        ctx,
	})
}

// swapped is a copy of names with i and j swapped.
func swapped(names []string, i, j int) []string {
	out := slices.Clone(names)
	out[i], out[j] = out[j], out[i]
	return out
}

// dropColsHref is the same URL with no cols param: the list's resolved
// columns, whatever else the URL carries.
func (s *listState) dropColsHref() string {
	return listHref(s.path, s.carryWithSort(s.p.cols))
}

// carryWithSort is carry plus the URL's sort and direction: a columns
// change keeps the order the reader picked, where a narrowing link
// (a view, a filter) resets it.
func (s *listState) carryWithSort(exclude ...string) url.Values {
	q := s.carry(exclude...)
	for _, k := range []string{s.p.sort, s.p.dir} {
		if v := strings.TrimSpace(s.q.Get(k)); v != "" {
			q.Set(k, v)
		}
	}
	return q
}
