package entityui

import (
	"context"
	"log/slog"
	"net/url"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// Steps turns on the drawer's previous and next record. Drawn as an
// intercepted drawer over this entity's list (the builder's Base), the
// drawer's bar steps through the list's rows in the order the list
// shows them: its view, saved view, search, filter, facets and sort,
// read as the caller with the list's own read hooks. Each step renders
// in the same drawer. The full page, and a drawer over any other page,
// draw none. Off by default.
func (b *RecordBuilder) Steps() *RecordBuilder { b.steps = true; return b }

// neighbours are the records before and after id in the list at base,
// as root-relative record paths, "" past either end. The list's query
// is the one the drawer opened over (the client names it, so it is
// parsed the way the list parses its own URL, never trusted). The read
// is the ids alone, at most EveryMatchCap of them, the bound an
// every-match selection reads under; a record past it, or not in the
// list at all, has no neighbours.
func (u *UI) neighbours(ctx context.Context, m *meta, base, id string) (prev, next string) {
	if appui.OverlayOriginFromContext(ctx) != base {
		return "", ""
	}
	q := appui.OverlayOriginQueryFromContext(ctx)
	if q == nil {
		q = url.Values{}
	}
	s := &listState{m: m, p: listParamsFor(""), q: q}
	lb := &ListBuilder{ui: u, entity: m.name, saved: true}
	if err := s.resolveColumns(lb); err != nil {
		return "", ""
	}
	if err := lb.openSaved(ctx, s); err != nil || s.savedGone {
		return "", ""
	}
	if err := lb.narrow(ctx, s); err != nil || s.filterBad {
		return "", ""
	}
	s.resolveSort(lb)
	where, err := s.predicate(lb)
	if err != nil {
		return "", ""
	}
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:   where,
		Filters: s.facetFilters(),
		Search:  s.search,
		Fields:  []string{m.pk},
		Sorts:   s.sorts,
		Limit:   EveryMatchCap,
	})
	if err != nil {
		slog.WarnContext(ctx, "entityui: record steps", "entity", m.name, "error", err)
		return "", ""
	}
	for i, row := range rows {
		if cell(rowValue(row, m.pk)) != id {
			continue
		}
		if i > 0 {
			prev = base + "/" + url.PathEscape(cell(rowValue(rows[i-1], m.pk)))
		}
		if i < len(rows)-1 {
			next = base + "/" + url.PathEscape(cell(rowValue(rows[i+1], m.pk)))
		}
		break
	}
	return prev, next
}
