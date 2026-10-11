package entityui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// titleSep joins a composite title's parts: "Ada Lovelace · Pro".
const titleSep = " · "

// maxTitleHops bounds how far a relation part is followed. A
// subscription titled by its customer reads the customer's own title,
// but a relation part inside THAT title is left out: two entities
// titled by each other cannot recurse, and a title costs at most one
// read per relation part.
const maxTitleHops = 1

// rowTitles names rows of m, one title per row in order: the title
// fields' values joined with " · ". A Relation part reads as the related
// record's own title, one IN read per relation part for all the rows,
// through the related entity's gate and read hooks. hops counts the
// relations already followed to reach these rows. A part is left out
// when its relation is refused or past maxTitleHops, when the read did
// not return its id, when the field is masked, and when it is empty; a
// row with no part left reads "" and the caller picks the fallback.
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
			labels = u.resolveLabelsAt(ctx, m, name, columnIDs(rows, name), hops+1)
			if labels == nil {
				// Refused: the caller may not read the related entity.
				continue
			}
		}
		for i, row := range rows {
			v := cell(rowValue(row, name))
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

// recordTitle names one row for headings, breadcrumbs and links: its
// title, else the entity's singular name.
func (u *UI) recordTitle(ctx context.Context, m *meta, row map[string]any) string {
	if t := u.rowTitles(ctx, m, []map[string]any{row}, 0)[0]; t != "" {
		return t
	}
	return m.singular(ctx)
}

// titleMap names title-read rows id → title. A row with no title falls
// back to its id, the same value a picker would submit.
func (u *UI) titleMap(ctx context.Context, m *meta, rows []map[string]any, hops int) map[string]string {
	titles := u.rowTitles(ctx, m, rows, hops)
	labels := make(map[string]string, len(rows))
	for i, row := range rows {
		id := cell(rowValue(row, m.pk))
		if id == "" {
			continue
		}
		labels[id] = id
		if titles[i] != "" {
			labels[id] = titles[i]
		}
	}
	return labels
}

// columnIDs is the distinct non-empty values of one column over rows.
func columnIDs(rows []map[string]any, col string) []string {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if v := cell(rowValue(row, col)); v != "" && !seen[v] {
			seen[v] = true
			ids = append(ids, v)
		}
	}
	return ids
}

// pageTitles names one page's rows by id, read once for the page, so a
// row's checkbox and menu do not each read its relation parts again.
func (u *UI) pageTitles(ctx context.Context, s *listState, rows []map[string]any) {
	titles := u.rowTitles(ctx, s.m, rows, 0)
	s.titles = make(map[string]string, len(rows))
	for i, row := range rows {
		s.titles[cell(rowValue(row, s.m.pk))] = titles[i]
	}
}

// rowTitle is a page row's title from pageTitles, else the singular.
func (s *listState) rowTitle(ctx context.Context, row map[string]any) string {
	if t := s.titles[cell(rowValue(row, s.m.pk))]; t != "" {
		return t
	}
	return s.m.singular(ctx)
}
