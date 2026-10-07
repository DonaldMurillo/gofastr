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

// columnsMenu draws the columns control: a menu of checkbox rows, one
// per available field in display order (the hidden ones after), each a
// link to the same URL with that column toggled in cols; the title
// field is checked and disabled, since it carries the record link. A
// Reset row drops cols. Every row is a navigation that keeps the other
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

	items := make([]ui.MenuItem, 0, len(order)+2)
	for _, name := range order {
		if tf != "" && name == tf {
			items = append(items, ui.MenuItem{Label: m.label(ctx, name), Check: true, Checked: true, Disabled: true})
			continue
		}
		next := make([]string, 0, len(order))
		for _, c := range order {
			if c == name {
				if !shown[c] {
					next = append(next, c)
				}
			} else if shown[c] {
				next = append(next, c)
			}
		}
		q := s.carryWithSort(s.p.cols)
		q.Set(s.p.cols, strings.Join(next, ","))
		items = append(items, ui.MenuItem{
			Label:   m.label(ctx, name),
			Href:    listHref(s.path, q),
			Check:   true,
			Checked: shown[name],
		})
	}
	items = append(items, ui.MenuItem{Separator: true}, ui.MenuItem{
		Label: i18nui.T(ctx, i18nui.KeyFilterReset),
		Href:  s.dropColsHref(),
	})
	return ui.Menu(ui.MenuConfig{
		ID:    "eui-" + listIDSafe(s.key, m.name) + "-cols",
		Label: i18nui.T(ctx, i18nui.KeyEntityColumns),
		Icon:  "columns",
		Items: items,
	})
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
