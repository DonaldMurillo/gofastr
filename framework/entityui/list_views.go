package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// viewKeyOf resolves which view this render opens on: the URL's, else
// the builder's .View, else the declared default, else All. A URL
// naming an unknown or hidden view shows All, never an error — it is
// someone's bookmark, not a configuration bug. A builder naming an
// unknown view is a configuration bug and fails the slot. A default
// view hidden from this caller by its Show falls back to All.
// allView is the reserved view key (entity.reservedDisplayKeys) that
// names All when the list would otherwise fall to a default view.
const allView = "all"

func viewKeyOf(ctx context.Context, m *meta, b *ListBuilder, q url.Values) (string, error) {
	if key := q.Get(param(b.key, "view")); key != "" {
		// The trash view is the screens' own, beside the declared ones.
		// It narrows nothing here — the read inverts its soft-delete
		// term — so it resolves before the unknown-view fallback.
		if key == deletedViewKey && b.deleted && m.e.Config.Scope.SoftDelete {
			return deletedViewKey, nil
		}
		if declaredAndViewable(ctx, m, key) {
			return key, nil
		}
		return "", nil
	}
	if b.view != "" {
		if !m.declaresView(b.view) {
			return "", fmt.Errorf("entityui: entity %q: View(%q) names no declared view", m.name, b.view)
		}
		if declaredAndViewable(ctx, m, b.view) {
			return b.view, nil
		}
	}
	for _, v := range m.d.Views {
		if v.Default && viewable(ctx, m, v.Key) {
			return v.Key, nil
		}
	}
	return "", nil
}

// viewable asks the view func's Show, when one is registered: a view
// whose filter depends on who is looking can also hide itself from
// callers it does not apply to. A view with no func, or a func with no
// Show, is shown to everyone.
func viewable(ctx context.Context, m *meta, key string) bool {
	vf, ok := m.ext.Views[key]
	if !ok || vf.Show == nil {
		return true
	}
	return vf.Show(asCaller(ctx))
}

// declaredAndViewable reports whether key is one of the entity's views
// and shown to this caller.
func declaredAndViewable(ctx context.Context, m *meta, key string) bool {
	return m.declaresView(key) && viewable(ctx, m, key)
}

// declaresView reports whether Display declares a view under key.
func (m *meta) declaresView(key string) bool {
	for _, v := range m.d.Views {
		if v.Key == key {
			return true
		}
	}
	return false
}

// viewPredicate resolves the active view's narrowing term: its declared
// Where parsed with the DSL, or its registered Filter func. A func's
// result passes filter.ValidatePredicate before use — every time, never
// trusting the app's own tree — and the resolved copy is what runs.
// A view with neither narrows nothing.
func viewPredicate(ctx context.Context, m *meta, key string) (*filter.Predicate, error) {
	if key == "" {
		return nil, nil
	}
	var view entity.ListView
	for _, v := range m.d.Views {
		if v.Key == key {
			view = v
			break
		}
	}
	if view.Where != "" {
		p, err := dsl.ParsePredicate(view.Where, m.e.GetFields())
		if err != nil {
			return nil, fmt.Errorf("entityui: entity %q view %q: %w", m.name, key, err)
		}
		return p, nil
	}
	vf, ok := m.ext.Views[key]
	if !ok || vf.Filter == nil {
		return nil, nil
	}
	p, err := vf.Filter(asCaller(ctx))
	if err != nil {
		return nil, fmt.Errorf("entityui: entity %q view %q filter: %w", m.name, key, err)
	}
	resolved, err := filter.ValidatePredicate(p, m.e.GetFields())
	if err != nil {
		return nil, fmt.Errorf("entityui: entity %q view %q filter: %w", m.name, key, err)
	}
	return resolved, nil
}

// viewSorts parses the view's declared Sort, the default order when the
// URL names none.
func viewSorts(m *meta, key string) ([]filter.ParsedSort, error) {
	if key == "" {
		return nil, nil
	}
	for _, v := range m.d.Views {
		if v.Key != key || v.Sort == "" {
			continue
		}
		sorts, err := dsl.ParseSort(v.Sort, m.e.GetFields())
		if err != nil {
			return nil, fmt.Errorf("entityui: entity %q view %q sort: %w", m.name, key, err)
		}
		return sorts, nil
	}
	return nil, nil
}

// viewTabs draws the strip above the list: All, every declared view
// shown to this caller, then the caller's saved views, with the
// save/delete view tools at its end. Each tab is a link: a declared one
// swaps the view param and drops sort, page and any open saved view —
// a view carries its own default order, and every view starts on page
// one; a saved one opens that view. With TabCounts each tab carries the
// count of what its link lists; the open tab reuses the page's total.
func (b *ListBuilder) viewTabs(ctx context.Context, s *listState, total int, known bool) render.HTML {
	builtIn := s.savedID == ""
	// A built-in tab's link keeps the URL's filter param and drops the
	// open saved view, and with it the saved view's filter.
	urlFilter := s.filterPred
	if !s.q.Has(s.p.filter) {
		urlFilter = nil
	}
	count := func(current bool, view, filt *filter.Predicate, deleted bool) string {
		switch {
		case !b.counts:
			return ""
		case current && known:
			return formatNumber(float64(total), 0)
		case current:
			return ""
		}
		return b.tabCount(ctx, s, view, filt, deleted)
	}
	allQ := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page, s.p.saved)
	if s.implicitView != "" {
		allQ.Set(s.p.view, allView)
	}
	allCurrent := builtIn && s.view == ""
	items := []ui.TabNavItem{{
		Text:    i18nui.T(ctx, i18nui.KeyEntityViewAll),
		Href:    listHref(s.path, allQ),
		Current: allCurrent,
		Badge:   count(allCurrent, nil, urlFilter, false),
	}}
	for _, v := range s.m.d.Views {
		if !viewable(ctx, s.m, v.Key) {
			continue
		}
		current := builtIn && s.view == v.Key
		item := ui.TabNavItem{
			Text: i18nui.ViewLabel(ctx, s.m.tr, s.m.name, v.Key, v.Label),
			Href: func() string {
				q := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page, s.p.saved)
				q.Set(s.p.view, v.Key)
				return listHref(s.path, q)
			}(),
			Current: current,
		}
		// A view whose predicate fails here failed the open list already
		// when it was the open one; another tab only goes bare.
		if pred, err := viewPredicate(ctx, s.m, v.Key); err == nil {
			item.Badge = count(current, pred, urlFilter, false)
		}
		items = append(items, item)
	}
	if savedViewsOn(ctx, s) {
		for _, v := range s.savedViews {
			q := s.carry(s.p.saved, s.p.filter, s.p.cols, s.p.page)
			q.Set(s.p.saved, v.ID)
			current := v.ID == s.savedID
			item := ui.TabNavItem{
				Text:    v.Name,
				Href:    listHref(s.path, q),
				Current: current,
			}
			// A saved view's link keeps the open view and swaps in its
			// own filter; one that no longer parses opens as All.
			var filt *filter.Predicate
			ok := true
			if v.Filter != "" {
				p, err := dsl.ParsePredicate(v.Filter, s.m.e.GetFields())
				filt, ok = p, err == nil
			}
			if ok {
				item.Badge = count(current, s.viewPred, filt, s.deletedView)
			}
			items = append(items, item)
		}
	}
	// The trash view rides after the others.
	if s.offeredTab {
		q := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page, s.p.saved)
		q.Set(s.p.view, deletedViewKey)
		current := builtIn && s.deletedView
		items = append(items, ui.TabNavItem{
			Text:    i18nui.T(ctx, i18nui.KeyEntityViewDeleted),
			Href:    listHref(s.path, q),
			Current: current,
			Badge:   count(current, nil, urlFilter, true),
		})
	}
	end := b.viewTools(ctx, s)
	if len(items) < 2 && end == "" {
		// All alone is not a strip of tabs; it is the list's only shape.
		return ""
	}
	return ui.TabNav(ui.TabNavConfig{
		Label: i18nui.T(ctx, i18nui.KeyEntityViews),
		Items: items,
		End:   end,
	})
}

// tabCount counts the rows one tab lists: its view and filter with the
// page's pins, search and facets, under the same read scope as the
// list. A refused count is a bare tab, never a failed strip.
func (b *ListBuilder) tabCount(ctx context.Context, s *listState, view, filt *filter.Predicate, deleted bool) string {
	where, err := s.narrowed(b, view, filt)
	if err != nil {
		return ""
	}
	n, err := s.m.ch.CountAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:   where,
		Filters: s.facetFilters(),
		Search:  s.search,
		Deleted: deleted,
	})
	if err != nil {
		slog.WarnContext(ctx, "entityui: tab count", "entity", s.m.name, "error", err)
		return ""
	}
	return formatNumber(float64(n), 0)
}

// filterChips draws what narrows the list: a chip per facet set
// ("Status: Paid") and per top-level AND term of the typed filter, each
// a link to the same URL without it, then "Clear all", which drops the
// facets and the filter (and an open saved view whose filter it is) but
// keeps the search, the view and the columns.
func filterChips(ctx context.Context, s *listState, facets []ui.Facet) render.HTML {
	var chips []render.HTML
	for _, f := range facets {
		if f.Value == "" {
			continue
		}
		label := f.Value
		for _, o := range f.Options {
			if o.Value == f.Value {
				label = o.Label
			}
		}
		text := f.Label + ": " + label
		chips = append(chips, ui.Tag(ui.TagConfig{
			Label: text,
			Href:  listHref(s.path, s.carry(f.Name, s.p.page)),
			ExtraAttrs: map[string]string{
				"aria-label": i18nui.TVars(ctx, i18nui.KeyFilterChipRemove, map[string]string{"label": text}),
			},
			Ctx: ctx,
		}))
	}
	var terms []string
	if s.filterPred != nil {
		terms = topLevelTerms(s.filterPred)
	}
	for i, term := range terms {
		chips = append(chips, ui.Tag(ui.TagConfig{
			Label: term,
			Href:  removeFilterHref(s, terms, i),
			ExtraAttrs: map[string]string{
				"aria-label": i18nui.TVars(ctx, i18nui.KeyFilterChipRemove, map[string]string{"label": term}),
			},
			Ctx: ctx,
		}))
	}
	if len(chips) == 0 {
		return ""
	}
	drop := []string{s.p.filter, s.p.page}
	for _, f := range facets {
		drop = append(drop, f.Name)
	}
	q := s.carry(drop...)
	if s.savedID != "" && !s.q.Has(s.p.filter) && s.filterText != "" {
		q.Del(s.p.saved)
	}
	chips = append(chips, ui.LinkButton(ui.LinkButtonConfig{
		Label:   i18nui.T(ctx, i18nui.KeyFilterClearAll),
		Href:    listHref(s.path, q),
		Variant: ui.ButtonGhost,
		Size:    ui.ButtonSizeSmall,
	}))
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, chips...)
}

// topLevelTerms are the filter's top-level AND terms as display text.
// A lone leaf is its own single term; an OR group is one term (it can
// only be removed whole).
func topLevelTerms(p *filter.Predicate) []string {
	if p == nil {
		return nil
	}
	if len(p.Children) == 0 {
		return []string{predicateText(p)}
	}
	out := make([]string, 0, len(p.Children))
	for i := range p.Children {
		out = append(out, predicateText(&p.Children[i]))
	}
	return out
}

// predicateText renders one term back as DSL text, the same spelling
// the ?filter= param accepts, so a rebuilt without-term filter is a
// value the parser round-trips.
func predicateText(p *filter.Predicate) string {
	if p == nil {
		return ""
	}
	if len(p.Children) > 0 {
		parts := make([]string, 0, len(p.Children))
		for i := range p.Children {
			parts = append(parts, predicateText(&p.Children[i]))
		}
		joiner := " and "
		if p.Or {
			joiner = " or "
		}
		return strings.Join(parts, joiner)
	}
	quote := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"` }
	switch p.Op {
	case filter.OpIn:
		vals := make([]string, 0, len(p.Values))
		for _, v := range p.Values {
			vals = append(vals, quote(v))
		}
		return p.Field + " in [" + strings.Join(vals, ", ") + "]"
	case filter.OpLike:
		return p.Field + " contains " + quote(p.Value)
	}
	// The DSL's symbolic operators, not the FilterOp tokens ("eq"),
	// so a rebuilt filter parses again.
	op := map[filter.FilterOp]string{
		filter.OpEq: "=", filter.OpNe: "!=", filter.OpGt: ">",
		filter.OpLt: "<", filter.OpGte: ">=", filter.OpLte: "<=",
	}[p.Op]
	return p.Field + " " + op + " " + quote(p.Value)
}

// removeFilterHref rebuilds the filter text without term i and returns
// the same URL with that filter. Removing the last term drops the
// param — or, when the text came from an open saved view, drops the
// saved param: leaving it would bring the view's filter straight back.
func removeFilterHref(s *listState, terms []string, i int) string {
	kept := make([]string, 0, len(terms)-1)
	for j, t := range terms {
		if j != i {
			kept = append(kept, t)
		}
	}
	q := s.carry(s.p.filter, s.p.page)
	if len(kept) > 0 {
		q.Set(s.p.filter, strings.Join(kept, " and "))
		return listHref(s.path, q)
	}
	if s.savedID != "" && !s.q.Has(s.p.filter) {
		q.Del(s.p.saved)
	}
	return listHref(s.path, q)
}

// filterWarning is the callout a bad ?filter= earns: the text could not
// be applied, and the list below is unfiltered. The error itself is
// logged, not shown — it carries the raw input.
func filterWarning(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.T(ctx, i18nui.KeyEntityFilterInvalidTitle),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.T(ctx, i18nui.KeyEntityFilterInvalidBody)))
}
