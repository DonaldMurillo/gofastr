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

func (s *listState) applyColsParam() {
	names := s.parseColsNames()
	if len(names) == 0 || !s.validColsNames(names) {
		return
	}
	for _, name := range names {
		if !slices.Contains(s.available, name) {
			return
		}
	}
	s.setColumns(names)
}

func (s *listState) setColumns(names []string) {
	if tf := s.m.titleField(); tf != "" && !slices.Contains(names, tf) {
		names = append([]string{tf}, names...)
	}
	s.columns = names
}

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
		Label:      i18nui.EntityColumns.Text(ctx),
		Columns:    cols,
		ResetHref:  s.dropColsHref(),
		ResetLabel: i18nui.FilterReset.Text(ctx),
		Ctx:        ctx,
	})
}

func swapped(names []string, i, j int) []string {
	out := slices.Clone(names)
	out[i], out[j] = out[j], out[i]
	return out
}

func (s *listState) dropColsHref() string {
	return listHref(s.path, s.carryWithSort(s.p.cols))
}

func (s *listState) carryWithSort(exclude ...string) url.Values {
	q := s.carry(exclude...)
	for _, k := range []string{s.p.sort, s.p.dir} {
		if v := strings.TrimSpace(s.q.Get(k)); v != "" {
			q.Set(k, v)
		}
	}
	return q
}
