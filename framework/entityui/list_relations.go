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

const relationFacetCap = 1000

type labelResolver map[string]map[string]string

func (r labelResolver) refused(col string) bool {
	labels, ok := r[col]
	return ok && labels == nil
}

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

func (u *UI) resolveLabels(ctx context.Context, m *meta, col string, ids []string) map[string]string {
	return u.resolveLabelsAt(ctx, m, col, ids, 0)
}

func (u *UI) resolveLabelsAt(ctx context.Context, m *meta, col string, ids []string, hops int) map[string]string {
	target, tm, ok := u.relationTarget(ctx, m, col)
	if !ok {
		return nil
	}
	if len(ids) == 0 {
		return map[string]string{}
	}
	pred := &filter.Predicate{Field: tm.pk, Op: filter.OpIn, Values: ids}
	opts := crud.ListOptions{Where: pred, Fields: tm.readTitleFields(), Limit: len(ids)}
	rows, err := tm.listRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation labels", "entity", m.name, "column", col, "target", target.GetName(), "error", err)
		return nil
	}
	return u.titleMap(ctx, tm, rows, hops)
}

func (u *UI) relatedBase(ctx context.Context, m *meta, col string) (string, bool) {
	if u.recordPath == nil {
		return "", false
	}
	target, _, ok := u.relationTarget(ctx, m, col)
	if !ok {
		return "", false
	}
	return u.recordPath(target)
}

func (u *UI) relationFacetOptions(ctx context.Context, m *meta, col string) []ui.FacetOption {
	target, tm, ok := u.relationTarget(ctx, m, col)
	if !ok {
		return nil
	}
	opts := crud.ListOptions{Fields: tm.readTitleFields(), Limit: relationFacetCap}
	rows, err := tm.listRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: relation facet", "entity", m.name, "column", col, "target", target.GetName(), "error", err)
		return nil
	}
	titles := u.titleMap(ctx, tm, rows, 0)
	out := make([]ui.FacetOption, 0, len(titles))
	for id, title := range titles {
		out = append(out, ui.FacetOption{Value: id, Label: title})
	}
	slices.SortStableFunc(out, func(a, b ui.FacetOption) int {
		if a.Label == b.Label {
			return strings.Compare(a.Value, b.Value)
		}
		return strings.Compare(a.Label, b.Label)
	})
	return out
}

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
	if !canReadRelated(ctx, tm.ch) {
		return nil, nil, false
	}
	return target, tm, true
}

func (m *meta) readTitleFields() []string {
	fields := slices.Clone(m.keys)
	for _, tf := range m.titleFields() {
		if !slices.Contains(fields, tf) {
			fields = append(fields, tf)
		}
	}
	return fields
}
