package entityui

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// relationFacetCap bounds the records a relation facet lists as options,
// ported from resource's label cap: the options are read once per render,
// so the read is bounded.
const relationFacetCap = 1000

// labelResolver holds the related-record titles for one page render,
// keyed by the column that points at them. A nil map for a column marks
// the relation REDACTED: present, so the cell knows it is a relation,
// but with no titles to show. Leaving the column absent instead made the
// cell fall through and print the raw foreign key — a bare id where a
// name belongs, useless to a reader and an unnecessary disclosure of
// internal ids.
type labelResolver map[string]map[string]string

// refused reports whether a column's relation was refused for this
// caller: the related entity's own gate said no.
func (r labelResolver) refused(col string) bool {
	labels, ok := r[col]
	return ok && labels == nil
}

// title resolves one id; false when the relation is redacted or the id
// has no row the read returned.
func (r labelResolver) title(col, id string) (string, bool) {
	if r.refused(col) {
		return "", false
	}
	labels := r[col]
	if labels == nil {
		return "", false
	}
	t, ok := labels[id]
	return t, ok
}

// resolveLabels resolves the related records' titles for one relation
// column's page ids: ONE IN read per relation, through the related
// entity's own crud handle, after ITS read gate, with read hooks, reading
// only the primary key and the title column. The rows belong to the
// related entity, so its posture decides whether this caller may see its
// names at all — a list for an open entity must not become a window onto
// a gated one.
func (u *UI) resolveLabels(ctx context.Context, m *meta, col string, ids []string) map[string]string {
	target, tm, ok := u.relationTarget(ctx, m, col)
	if !ok {
		// Refused: the caller may not read the related entity.
		return nil
	}
	if len(ids) == 0 {
		return map[string]string{}
	}
	pred := &filter.Predicate{Field: tm.pk, Op: filter.OpIn, Values: ids}
	opts := crud.ListOptions{Where: pred, Fields: tm.readTitleFields(), Limit: len(ids)}
	rows, err := tm.ch.ListAll(crud.WithReadHooks(ctx), opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation labels", "entity", m.name, "column", col, "target", target.GetName(), "error", err)
		// A failed label read is redaction, not raw ids.
		return nil
	}
	return titleMap(tm, rows)
}

// relationFacetOptions lists a relation facet's options: the related
// entity's records labelled by its title field, capped. A refused
// relation shows no options, so the facet is not drawn.
func (u *UI) relationFacetOptions(ctx context.Context, m *meta, col string) []ui.FacetOption {
	target, tm, ok := u.relationTarget(ctx, m, col)
	if !ok {
		return nil
	}
	opts := crud.ListOptions{Fields: tm.readTitleFields(), Limit: relationFacetCap}
	rows, err := tm.ch.ListAll(crud.WithReadHooks(ctx), opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation facet", "entity", m.name, "column", col, "target", target.GetName(), "error", err)
		return nil
	}
	titles := titleMap(tm, rows)
	out := make([]ui.FacetOption, 0, len(titles))
	for id, title := range titles {
		out = append(out, ui.FacetOption{Value: id, Label: title})
	}
	// Ordered by label for a stable, glanceable dropdown.
	slices.SortStableFunc(out, func(a, b ui.FacetOption) int {
		if a.Label == b.Label {
			return strings.Compare(a.Value, b.Value)
		}
		return strings.Compare(a.Label, b.Label)
	})
	return out
}

// meta, and reports whether this caller may read it. ResolveTarget
// honours versioned entities; the plain registry Get would pick the
// unversioned declaration under a versioned one.
func (u *UI) relationTarget(ctx context.Context, m *meta, col string) (*entity.Entity, *meta, bool) {
	f, ok := m.field(col)
	if !ok || f.Type != schema.Relation || f.To == "" {
		return nil, nil, false
	}
	target, err := entity.ResolveTarget(u.host.Registry(), m.e, f.To)
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation target", "entity", m.name, "column", col, "target", f.To, "error", err)
		return nil, nil, false
	}
	tm, err := u.meta(target.GetName())
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation meta", "entity", m.name, "column", col, "target", target.GetName(), "error", err)
		return nil, nil, false
	}
	if !canRead(ctx, tm.ch) {
		return nil, nil, false
	}
	return target, tm, true
}

// readTitleFields are the columns a title read asks for: the primary key
// plus the title field, nothing else.
func (m *meta) readTitleFields() []string {
	fields := []string{m.pk}
	if tf := m.titleField(); tf != "" && tf != m.pk {
		fields = append(fields, tf)
	}
	return fields
}

// titleMap turns title-read rows into id → title. A row with no title
// value falls back to its id, the same value a picker would submit.
func titleMap(m *meta, rows []map[string]any) map[string]string {
	labels := make(map[string]string, len(rows))
	tf := m.titleField()
	for _, row := range rows {
		id := cell(rowValue(row, m.pk))
		if id == "" {
			continue
		}
		title := id
		if tf != "" {
			if t := cell(rowValue(row, tf)); t != "" {
				title = t
			}
		}
		labels[id] = title
	}
	return labels
}
