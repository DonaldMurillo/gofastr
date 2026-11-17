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
// list the drawer opens from, in the order that list shows its rows. Only
// a drawer over this entity's list at Base draws them. Off by default; a
// nil list turns it off.
func (b *RecordBuilder) Steps(list *ListBuilder) *RecordBuilder { b.steps = list; return b }

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
	rows, err := m.listRows(ctx, crud.ListOptions{
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
