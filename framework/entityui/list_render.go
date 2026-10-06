package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	fwpagination "github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// render draws the list. W1 implements it.
func (b *ListBuilder) render(ctx context.Context) (render.HTML, error) {
	m, err := b.ui.meta(b.entity)
	if err != nil {
		return "", err
	}
	// The host's catalog answers the chrome keys (T/TVars) too: the App
	// middleware already stashes it on the request ctx, and this bridge
	// keeps a render outside the middleware (a test, a build) translated.
	if m.tr != nil {
		ctx = i18nui.WithTranslator(ctx, m.tr)
	}
	// The read gate: the same posture the JSON list route enforces. A
	// screen for an entity whose GET /api/<entity> answers 401 or 403
	// must not render its rows (the leak resource.canRead closed).
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, m.plural(ctx)), nil
	}

	// The key namespaces this list's params; a page with two unkeyed
	// lists, or two sharing a key, is refused before anything draws.
	if b.key != "" && !entity.ValidKey(b.key) {
		return "", fmt.Errorf("entityui: entity %q: list key %q is not a key (lowercase ASCII slug)", m.name, b.key)
	}
	if !claimListKey(ctx, b.key) {
		return "", fmt.Errorf("entityui: entity %q: list key %q is already taken by another list on this page; two lists each need their own key", m.name, b.key)
	}

	// A replaced list body is the app's component, under the same read
	// gate and the same key rules. Build runs inside this render's
	// recover; SafeRenderCtx contains a panic in the component's render.
	if m.ext.List != nil {
		comp, err := m.ext.List(ListContext{Ctx: ctx, UI: b.ui, Entity: m.name})
		if err != nil {
			return "", err
		}
		if comp == nil {
			return "", fmt.Errorf("entityui: entity %q: replaced list returned no component", m.name)
		}
		out, err := component.SafeRenderCtx(ctx, comp)
		if err != nil {
			return "", err
		}
		return out, nil
	}

	s := &listState{m: m, key: b.key, p: listParamsFor(b.key), pins: b.where}
	s.q = appui.QueryFromContext(ctx)
	// The page's own path carries the sort, page and view links, so they
	// stay on the screen the list lives in; .Base overrides where RECORD
	// links hang off (the admin's entity page beside an app page's list).
	s.path = b.base
	if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
		s.path = r.URL.Path
	}
	s.base = b.base
	if s.base == "" {
		s.base = s.path
	}

	if err := s.resolveColumns(b); err != nil {
		return "", err
	}
	if err := b.narrow(ctx, s); err != nil {
		return "", err
	}
	s.as = b.as
	if s.as == "" {
		for _, v := range m.d.Views {
			if v.Key == s.view {
				s.as = v.As
			}
		}
	}
	switch s.as {
	case "", "table", "cards":
	default:
		return "", fmt.Errorf("entityui: entity %q: As(%q) must be \"table\" or \"cards\"", m.name, b.as)
	}

	s.resolveSort()
	s.resolvePage(b)

	where, err := s.predicate(b)
	if err != nil {
		return "", err
	}
	filters := s.facetFilters()
	read := crud.ListOptions{
		Where:   where,
		Filters: filters,
		Search:  s.search,
		Fields:  s.readFields(),
	}
	// The count only feeds pagination chrome; a refused count degrades to
	// unknown totals rather than a failed screen, and only a known total
	// may clamp the requested page.
	total, countErr := m.ch.CountAll(crud.WithReadHooks(ctx), read)
	known := countErr == nil
	if countErr != nil {
		slog.WarnContext(ctx, "entityui: count", "entity", m.name, "error", countErr)
	}
	page := clampPage(s.page, total, s.limit, known)
	listOpts := read
	listOpts.Sorts = s.sorts
	listOpts.Limit = s.limit
	listOpts.Offset = fwpagination.OffsetForPage(page, s.limit)
	// WithReadHooks: these rows are rendered to an end user.
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), listOpts)
	if err != nil {
		return ui.Callout(ui.CalloutConfig{
			Title:   i18nui.TVars(ctx, i18nui.KeyEntityLoadFailed, map[string]string{"entity": m.plural(ctx)}),
			Variant: ui.StatusDanger,
		}, render.Text(i18nui.T(ctx, i18nui.KeyEntitySlotFailedBody))), nil
	}

	lb := b.bulkFor(ctx, s)
	var body []render.HTML
	body = append(body, b.header(ctx, s, total, known))
	if tabs := viewTabs(ctx, s); tabs != "" {
		body = append(body, tabs)
	}
	if s.filterBad {
		body = append(body, filterWarning(ctx))
	}
	if chips := filterChips(ctx, s); chips != "" {
		body = append(body, chips)
	}
	if tb := b.toolbar(ctx, s); tb != "" {
		body = append(body, tb)
	}

	if lb != nil {
		if bar := b.bulkBar(ctx, s, lb, rows, total, known); bar != "" {
			body = append(body, bar)
		}
	}
	if s.as == "cards" {
		body = append(body, b.cards(ctx, s, rows, total, known, page))
	} else {
		body = append(body, b.table(ctx, s, lb, rows, total, known, page))
	}
	return render.Join(body...), nil
}

// narrow resolves the request's narrowing terms onto s: the view (its
// predicate and sorts), the search and the filter text. The list render
// and a bulk "every match" selection both run it, so the selection is
// the rows the screen drew.
func (b *ListBuilder) narrow(ctx context.Context, s *listState) error {
	m := s.m
	viewKey, err := viewKeyOf(ctx, m, b, s.q)
	if err != nil {
		return err
	}
	s.view = viewKey
	// The view this list shows with no ?view= at all (a builder's View or
	// a Default). When there is one, reaching All takes ?view=all.
	s.implicitView, _ = viewKeyOf(ctx, m, b, url.Values{})
	if s.viewPred, err = viewPredicate(ctx, m, viewKey); err != nil {
		return err
	}
	if s.viewSorts, err = viewSorts(m, viewKey); err != nil {
		return err
	}
	// Search, only over fields the entity declares for it.
	if len(m.e.Config.SearchFields) > 0 {
		s.search = strings.TrimSpace(s.q.Get(s.p.q))
	}
	// The filter text: a parse failure is a warning and an unfiltered
	// list, never a failed screen — the reader typed it, not the app.
	if text := strings.TrimSpace(s.q.Get(s.p.filter)); text != "" {
		s.filterText = text
		p, err := dsl.ParsePredicate(text, m.e.GetFields())
		if err != nil {
			// The error quotes the input; it goes to the log, not the page.
			slog.InfoContext(ctx, "entityui: filter not applied", "entity", m.name, "error", err)
			s.filterBad = true
		} else {
			s.filterPred = p
		}
	}
	return nil
}

// header draws the list's page header: the plural (or the builder's
// heading), the description, a count subtitle and the actions.
func (b *ListBuilder) header(ctx context.Context, s *listState, total int, known bool) render.HTML {
	m := s.m
	title := b.heading
	if title == "" {
		title = m.plural(ctx)
	}
	subtitle := ""
	if known {
		if total == 1 {
			subtitle = i18nui.TVars(ctx, i18nui.KeyEntityCountOne, map[string]string{"entity": m.noun(ctx, false)})
		} else {
			subtitle = i18nui.TVars(ctx, i18nui.KeyEntityCount, map[string]string{
				"count":  formatNumber(float64(total), 0),
				"entity": m.noun(ctx, true),
			})
		}
	} else if desc := m.description(ctx); desc != "" {
		subtitle = desc
	}
	var actions []render.HTML
	actions = append(actions, b.actions...)
	// Export is a read: it rides with bulk, not with the caller's
	// write actions.
	// A Where pin is not in the query the export route reads, so a pinned
	// list draws none rather than exporting past its pins.
	if b.bulk && bulkOn(m) && len(b.where) == 0 {
		actions = append(actions, exportLink(ctx, s))
	}
	if b.mayCreate() && canCreate(ctx, m) {
		actions = append(actions, ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.TVars(ctx, i18nui.KeyEntityNew, map[string]string{"entity": m.singular(ctx)}),
			Href:    s.createHref(),
			Variant: ui.ButtonPrimary,
		}))
	}
	return ui.PageHeader(ui.PageHeaderConfig{
		Title:        title,
		Subtitle:     subtitle,
		Actions:      actionCluster(actions),
		HeadingLevel: b.headingLevel(),
	})
}

// actionCluster lays out zero or more actions without wrapping markup
// of its own: one action renders bare, several cluster.
func actionCluster(actions []render.HTML) render.HTML {
	switch len(actions) {
	case 0:
		return ""
	case 1:
		return actions[0]
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, actions...)
}

// toolbar draws the one GET form: the search box and the facets
// together, so a submission carries both. Hidden inputs round-trip the
// request state the form does not own — this list's view, filter and
// sort, and every other param on the URL — so applying a search does not
// silently reset the rest of the page.
func (b *ListBuilder) toolbar(ctx context.Context, s *listState) render.HTML {
	m := s.m
	facets := make([]ui.Facet, 0, len(m.d.Facets))
	for _, name := range s.facets() {
		f, ok := m.field(name)
		if !ok {
			continue
		}
		p := s.facetParam(name)
		// The label sits above the control, so the clear choice is a
		// plain "All", never "All Customer".
		facet := ui.Facet{Name: p, Label: m.label(ctx, name), Value: strings.TrimSpace(s.q.Get(p)),
			AllLabel: i18nui.T(ctx, i18nui.KeyFilterAllPlain)}
		switch f.Type {
		case schema.Bool:
			facet.Options = []ui.FacetOption{
				{Label: i18nui.T(ctx, i18nui.KeyEntityYes), Value: "true"},
				{Label: i18nui.T(ctx, i18nui.KeyEntityNo), Value: "false"},
			}
			facet.Kind = ui.FacetPills
		case schema.Relation:
			opts := b.ui.relationFacetOptions(ctx, m, name)
			if len(opts) == 0 {
				// A refused relation shows no options; a facet with
				// nothing to offer is not drawn.
				continue
			}
			facet.Options = opts
			facet.Kind = ui.FacetSelect
		default: // Enum
			short := len(f.Values) > 0 && len(f.Values) <= 4
			opts := make([]ui.FacetOption, 0, len(f.Values))
			for _, v := range f.Values {
				label := m.valueLabel(ctx, name, v)
				if len(label) > 14 {
					short = false
				}
				opts = append(opts, ui.FacetOption{Label: label, Value: v})
			}
			facet.Options = opts
			if short {
				facet.Kind = ui.FacetPills
			} else {
				facet.Kind = ui.FacetSelect
			}
		}
		if len(facet.Options) == 0 {
			continue
		}
		facets = append(facets, facet)
	}
	var search *ui.FilterSearch
	if len(m.e.Config.SearchFields) > 0 {
		placeholder := i18nui.TVars(ctx, i18nui.KeyEntitySearch, map[string]string{"entity": m.plural(ctx)})
		search = &ui.FilterSearch{Name: s.p.q, Value: s.search, Placeholder: placeholder, Label: placeholder}
	}
	if len(facets) == 0 && search == nil {
		return ""
	}
	var hidden []ui.HiddenField
	for _, k := range slices.Sorted(maps.Keys(s.q)) {
		if k == s.p.q || k == s.p.page || s.ownsFacetParam(k) {
			continue
		}
		if vs := s.q[k]; len(vs) > 0 {
			hidden = append(hidden, ui.HiddenField{Name: k, Value: vs[0]})
		}
	}
	// Reset returns to the bare action and would drop every param the
	// form does not carry, so it is hidden while foreign state rides the
	// URL; the chips and the controls still clear what they own.
	hideReset := false
	for k := range s.q {
		if !s.ownsParam(k) {
			hideReset = true
			break
		}
	}
	return ui.FilterToolbar(ui.FilterToolbarConfig{
		Action:    s.path,
		Facets:    facets,
		Search:    search,
		Hidden:    hidden,
		HideReset: hideReset,
		Ctx:       ctx,
	})
}

// ownsFacetParam reports whether name is one of this list's facet
// params — controls the toolbar owns.
func (s *listState) ownsFacetParam(name string) bool {
	for _, f := range s.facets() {
		if name == s.facetParam(f) {
			return true
		}
	}
	return false
}

// headingLevel is the list heading's level: the builder's, or 1 when
// unset. Out of range panics: falling back to 1 would print a second
// <h1> the field exists to prevent.
func (b *ListBuilder) headingLevel() int {
	switch {
	case b.level == 0:
		return 1
	case b.level >= 1 && b.level <= 5:
		return b.level
	}
	panic(fmt.Sprintf("entityui: %s: Heading level %d is out of range — 1 to 5 (0 means 1); the empty state takes the level below", b.entity, b.level))
}

// recordHref is one record's link: <base>/<id>, the id path-escaped.
func (s *listState) recordHref(id string) string {
	return s.base + "/" + url.PathEscape(id)
}
