package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const allView = "all"

func viewKeyOf(ctx context.Context, m *meta, b *ListBuilder, q url.Values) (string, error) {
	if key := q.Get(param(b.key, "view")); key != "" {
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

func viewable(ctx context.Context, m *meta, key string) bool {
	vf, ok := m.ext.Views[key]
	if !ok || vf.Show == nil {
		return true
	}
	return vf.Show(asCaller(ctx))
}

func declaredAndViewable(ctx context.Context, m *meta, key string) bool {
	return m.declaresView(key) && viewable(ctx, m, key)
}

func (m *meta) declaresView(key string) bool {
	for _, v := range m.d.Views {
		if v.Key == key {
			return true
		}
	}
	return false
}

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

func (b *ListBuilder) viewTabs(ctx context.Context, s *listState, total int, known bool) render.HTML {
	builtIn := s.savedID == ""
	urlFilter := s.filterPred
	if !s.q.Has(s.p.filter) {
		urlFilter = nil
	}
	count := func(current bool, view, filt *filter.Predicate, deleted bool) string {
		switch {
		case !b.counts:
			return ""
		case current && known:
			return i18n.Number(ctx, total)
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
		Text:    i18nui.EntityViewAll.Text(ctx),
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
	if s.offeredTab {
		q := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page, s.p.saved)
		q.Set(s.p.view, deletedViewKey)
		current := builtIn && s.deletedView
		items = append(items, ui.TabNavItem{
			Text:    i18nui.EntityViewDeleted.Text(ctx),
			Href:    listHref(s.path, q),
			Current: current,
			Badge:   count(current, nil, urlFilter, true),
		})
	}
	end := b.viewTools(ctx, s)
	if len(items) < 2 && end == "" {
		return ""
	}
	return ui.TabNav(ui.TabNavConfig{
		Label: i18nui.EntityViews.Text(ctx),
		Items: items,
		End:   end,
	})
}

func (b *ListBuilder) tabCount(ctx context.Context, s *listState, view, filt *filter.Predicate, deleted bool) string {
	where, err := s.narrowed(b, view, filt)
	if err != nil {
		return ""
	}
	n, err := s.m.countRows(ctx, crud.ListOptions{
		Where:   where,
		Filters: s.facetFilters(),
		Search:  s.search,
		Deleted: deleted,
	})
	if err != nil {
		slog.WarnContext(ctx, "entityui: tab count", "entity", s.m.name, "error", err)
		return ""
	}
	return i18n.Number(ctx, n)
}

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
				"aria-label": i18nui.FilterChipRemove.Text(ctx, map[string]any{"label": text}),
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
				"aria-label": i18nui.FilterChipRemove.Text(ctx, map[string]any{"label": term}),
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
	s.clearFilter(q)
	chips = append(chips, ui.LinkButton(ui.LinkButtonConfig{
		Label:   i18nui.FilterClearAll.Text(ctx),
		Href:    listHref(s.path, q),
		Variant: ui.ButtonGhost,
		Size:    ui.ButtonSizeSmall,
	}))
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, chips...)
}

func topLevelTerms(p *filter.Predicate) []string {
	if p == nil {
		return nil
	}
	if len(p.Children) == 0 || p.Or {
		return []string{predicateText(p)}
	}
	out := make([]string, 0, len(p.Children))
	for i := range p.Children {
		out = append(out, termText(&p.Children[i]))
	}
	return out
}

func termText(p *filter.Predicate) string {
	if len(p.Children) > 0 {
		return "(" + predicateText(p) + ")"
	}
	return predicateText(p)
}

func predicateText(p *filter.Predicate) string {
	if p == nil {
		return ""
	}
	if len(p.Children) > 0 {
		parts := make([]string, 0, len(p.Children))
		for i := range p.Children {
			parts = append(parts, termText(&p.Children[i]))
		}
		joiner := " and "
		if p.Or {
			joiner = " or "
		}
		return strings.Join(parts, joiner)
	}
	quote := dsl.Quote
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
	op := map[filter.FilterOp]string{
		filter.OpEq: "=", filter.OpNe: "!=", filter.OpGt: ">",
		filter.OpLt: "<", filter.OpGte: ">=", filter.OpLte: "<=",
	}[p.Op]
	return p.Field + " " + op + " " + quote(p.Value)
}

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
	s.clearFilter(q)
	return listHref(s.path, q)
}
