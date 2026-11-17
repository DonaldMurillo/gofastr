package entityui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

const titleSep = " · "

const maxTitleHops = 1

func (u *UI) rowTitles(ctx context.Context, m *meta, rows []map[string]any, hops int) []string {
	parts := make([][]string, len(rows))
	for _, name := range m.titleFields() {
		f, ok := m.field(name)
		if !ok || m.hiddenFromCaller(f) {
			continue
		}
		var labels map[string]string
		if f.Type == schema.Relation {
			if hops >= maxTitleHops {
				continue
			}
			labels = u.resolveLabelsAt(ctx, m, name, columnIDs(m, rows, name), hops+1)
			if labels == nil {
				continue
			}
		}
		for i, row := range rows {
			v := cell(m.rowValue(row, name))
			if labels != nil {
				v = labels[v]
			}
			if v != "" {
				parts[i] = append(parts[i], v)
			}
		}
	}
	out := make([]string, len(rows))
	for i, p := range parts {
		out[i] = strings.Join(p, titleSep)
	}
	return out
}

func (u *UI) recordTitle(ctx context.Context, m *meta, row map[string]any) string {
	if t := u.rowTitles(ctx, m, []map[string]any{row}, 0)[0]; t != "" {
		return t
	}
	return m.singular(ctx)
}

func (u *UI) titleMap(ctx context.Context, m *meta, rows []map[string]any, hops int) map[string]string {
	titles := u.rowTitles(ctx, m, rows, hops)
	labels := make(map[string]string, len(rows))
	for i, row := range rows {
		id := m.rowID(row)
		if id == "" || !canReadRecord(ctx, m.ch, id) {
			continue
		}
		labels[id] = id
		if titles[i] != "" {
			labels[id] = titles[i]
		}
	}
	return labels
}

func columnIDs(m *meta, rows []map[string]any, col string) []string {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if v := cell(m.rowValue(row, col)); v != "" && !seen[v] {
			seen[v] = true
			ids = append(ids, v)
		}
	}
	return ids
}

func (u *UI) pageTitles(ctx context.Context, s *listState, rows []map[string]any) {
	titles := u.rowTitles(ctx, s.m, rows, 0)
	s.titles = make(map[string]string, len(rows))
	for i, row := range rows {
		s.titles[s.m.rowID(row)] = titles[i]
	}
}

func (s *listState) rowTitle(ctx context.Context, row map[string]any) string {
	if t := s.titles[s.m.rowID(row)]; t != "" {
		return t
	}
	return s.m.singular(ctx)
}
