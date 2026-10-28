package entityui

import (
	"context"
	"log/slog"
	"net/url"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Steps turns on the drawer's previous and next record over list, the
// list the drawer opens from. Drawn as an intercepted drawer over this
// entity's list (the builder's Base), the drawer's bar steps through
// list's rows in the order it shows them: its key, View, Where pins,
// and the view, saved view, search, filter, facets and sort its query
// names, read as the caller with the list's own read hooks. Each step
// renders in the same drawer. The full page, a drawer over any other
// page, and a list of another entity draw none. Off by default; a nil
// list turns it off.
func (b *RecordBuilder) Steps(list *ListBuilder) *RecordBuilder { b.steps = list; return b }

// neighbours are the records before and after id in list at base, as
// root-relative record paths, "" past either end. The list's query is
// the one the drawer opened over (the client names it, so it is parsed
// the way the list parses its own URL, never trusted). The read is the
// ids alone, at most EveryMatchCap of them, the bound an every-match
// selection reads under; a record past it, or not in the list at all,
// has no neighbours.
func (u *UI) neighbours(ctx context.Context, m *meta, list *ListBuilder, base, id string) (prev, next string) {
	if list.entity != m.name || appui.OverlayOriginFromContext(ctx) != base {
		return "", ""
	}
	if list.key != "" && !entity.ValidKey(list.key) {
		return "", ""
	}
	q := appui.OverlayOriginQueryFromContext(ctx)
	if q == nil {
		q = url.Values{}
	}
	s := &listState{m: m, key: list.key, p: listParamsFor(list.key), pins: list.where, q: q}
	lb := list
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
		Fields:  m.keys,
		Sorts:   s.sorts,
		Limit:   EveryMatchCap,
	})
	if err != nil {
		slog.WarnContext(ctx, "entityui: record steps", "entity", m.name, "error", err)
		return "", ""
	}
	for i, row := range rows {
		if m.rowID(row) != id {
			continue
		}
		if i > 0 {
			prev = under(base, m.idPath(m.rowID(rows[i-1])))
		}
		if i < len(rows)-1 {
			next = under(base, m.idPath(m.rowID(rows[i+1])))
		}
		break
	}
	return prev, next
}
