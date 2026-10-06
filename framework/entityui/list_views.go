package entityui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
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
	return vf.Show(ctx)
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
	p, err := vf.Filter(ctx)
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

// viewTabs draws the strip above the list: All plus every declared view
// shown to this caller. Each tab is a link that swaps the view param and
// drops sort and page — a view carries its own default order, and every
// view starts on page one.
func viewTabs(ctx context.Context, s *listState) render.HTML {
	allQ := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page)
	if s.implicitView != "" {
		allQ.Set(s.p.view, allView)
	}
	items := []ui.TabNavItem{{
		Text:    i18nui.T(ctx, i18nui.KeyEntityViewAll),
		Href:    listHref(s.path, allQ),
		Current: s.view == "",
	}}
	for _, v := range s.m.d.Views {
		if !viewable(ctx, s.m, v.Key) {
			continue
		}
		items = append(items, ui.TabNavItem{
			Text: i18nui.ViewLabel(ctx, s.m.tr, s.m.name, v.Key, v.Label),
			Href: func() string {
				q := s.carry(s.p.view, s.p.sort, s.p.dir, s.p.page)
				q.Set(s.p.view, v.Key)
				return listHref(s.path, q)
			}(),
			Current: s.view == v.Key,
		})
	}
	if len(items) < 2 {
		// All alone is not a strip of tabs; it is the list's only shape.
		return ""
	}
	return ui.TabNav(ui.TabNavConfig{
		Label: i18nui.T(ctx, i18nui.KeyEntityViews),
		Items: items,
	})
}

// filterChips draws the active filter as one chip per top-level AND
// term, each a link to the same URL with that term removed. The query
// box that writes the text is P4; the chips are how a reader narrows
// what they typed.
func filterChips(ctx context.Context, s *listState) render.HTML {
	if s.filterPred == nil {
		return ""
	}
	terms := topLevelTerms(s.filterPred)
	if len(terms) == 0 {
		return ""
	}
	var chips []render.HTML
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
// the same URL with that filter. Removing the last term drops the param.
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
