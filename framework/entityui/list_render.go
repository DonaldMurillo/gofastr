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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	fwpagination "github.com/DonaldMurillo/gofastr/framework/pagination"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *ListBuilder) render(ctx context.Context) (render.HTML, error) {
	m, err := b.ui.meta(b.entity)
	if err != nil {
		return "", err
	}
	if m.tr != nil {
		ctx = i18n.WithTranslator(ctx, m.tr)
	}
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, b.title(ctx, m), m.plural(ctx), b.headingLevel()), nil
	}

	if b.key != "" && !entity.ValidKey(b.key) {
		return "", fmt.Errorf("entityui: entity %q: list key %q is not a key (lowercase ASCII slug)", m.name, b.key)
	}
	if !claimListKey(ctx, b.key) {
		return "", fmt.Errorf("entityui: entity %q: list key %q is already taken by another list on this page; two lists each need their own key", m.name, b.key)
	}

	if m.ext.List != nil {
		cctx := asCaller(ctx)
		comp, err := m.ext.List(ListContext{Ctx: cctx, UI: b.ui, Entity: m.name})
		if err != nil {
			return "", err
		}
		if comp == nil {
			return "", fmt.Errorf("entityui: entity %q: replaced list returned no component", m.name)
		}
		out, err := component.SafeRenderCtx(cctx, comp)
		if err != nil {
			return "", err
		}
		return out, nil
	}

	s, read, err := b.state(ctx, m)
	if err != nil {
		return "", err
	}
	total, countErr := m.countRows(ctx, read)
	known := countErr == nil
	if countErr != nil {
		slog.WarnContext(ctx, "entityui: count", "entity", m.name, "error", countErr)
	}
	page := clampPage(s.page, total, s.limit, known)
	listOpts := read
	listOpts.Sorts = s.sorts
	listOpts.Limit = s.limit
	listOpts.Offset = fwpagination.OffsetForPage(page, s.limit)
	rows, err := m.listRows(ctx, listOpts)
	if err != nil {
		return ui.Callout(ui.CalloutConfig{
			Title:   i18nui.EntityLoadFailed.Text(ctx, map[string]any{"entity": m.plural(ctx)}),
			Variant: ui.StatusDanger,
		}, render.Text(i18nui.EntitySlotFailedBody.Text(ctx))), nil
	}
	b.ui.pageTitles(ctx, s, rows)
	if len(rows) > 0 && !b.embedded && !b.noLinks && !s.deletedView {
		b.ui.checkRecordScreen(ctx, m.name, s.base, m.idPath(m.rowID(rows[0])))
	}
	if b.inlineEdit && !s.deletedView && !b.top && m.hasAPI {
		if raw, err := m.listRowsUnhooked(ctx, listOpts); err == nil {
			s.rawRows = make(map[string]map[string]any, len(raw))
			for _, r := range raw {
				s.rawRows[m.rowID(r)] = r
			}
		}
	}

	if b.embedded {
		return b.embeddedBody(ctx, s, rows, total, known, page), nil
	}
	lb := b.bulkFor(ctx, s)
	var body []render.HTML
	tabs := b.viewTabs(ctx, s, total, known)
	body = append(body, b.header(ctx, s, total, known, b.counts && tabs != ""))
	if tabs != "" {
		body = append(body, tabs)
	}
	if s.savedGone {
		body = append(body, savedGoneWarning(ctx))
	}
	if s.filterBad {
		body = append(body, filterWarning(ctx))
	}
	facets := b.facetControls(ctx, s)
	if tb := b.toolbar(ctx, s, facets); tb != "" {
		body = append(body, tb)
	}
	if chips := filterChips(ctx, s, facets); chips != "" {
		body = append(body, chips)
	}

	var bar render.HTML
	if lb != nil {
		bar = b.bulkBar(ctx, s, lb, rows, total, known)
	}
	switch {
	case s.as == "cards":
		if bar != "" {
			body = append(body, bar)
		}
		body = append(body, b.cards(ctx, s, rows, total, known, page))
	case bar != "":
		var copyURL string
		if bulkOn(m) && len(b.where) == 0 && s.savedID == "" && !s.deletedView {
			copyURL = m.api + "/_export.csv"
		}
		body = append(body, ui.Selection(ui.SelectionConfig{
			Bar:      bar,
			Body:     b.table(ctx, s, lb, rows, total, known, page),
			Floating: true,
			Form:     lb.form,
			Copy:     copyURL,
			Ctx:      ctx,
		}))
	default:
		body = append(body, b.table(ctx, s, lb, rows, total, known, page))
	}
	return render.Join(body...), nil
}

func (b *ListBuilder) narrow(ctx context.Context, s *listState) error {
	m := s.m
	viewKey, err := viewKeyOf(ctx, m, b, s.q)
	if err != nil {
		return err
	}
	s.view = viewKey
	s.implicitView, _ = viewKeyOf(ctx, m, b, url.Values{})
	if s.viewPred, err = viewPredicate(ctx, m, viewKey); err != nil {
		return err
	}
	if s.viewSorts, err = viewSorts(m, viewKey); err != nil {
		return err
	}
	if len(m.e.Config.SearchFields) > 0 {
		s.search = strings.TrimSpace(s.q.Get(s.p.q))
	}
	text := strings.TrimSpace(s.q.Get(s.p.filter))
	if !s.q.Has(s.p.filter) && s.savedFilter != "" {
		text = s.savedFilter
	}
	if s.q.Has(s.p.rowF) {
		text = composeFilter(rowTerms(m, s.q[s.p.rowF], s.q[s.p.rowO], s.q[s.p.rowV]), strings.TrimSpace(s.q.Get(s.p.filter)))
	}
	if text != "" {
		s.filterText = text
		p, err := dsl.ParsePredicate(text, m.e.GetFields())
		if err != nil {
			slog.InfoContext(ctx, "entityui: filter not applied", "entity", m.name, "error", err)
			s.filterBad = true
		} else {
			s.filterPred = p
		}
	}
	return nil
}

func (b *ListBuilder) header(ctx context.Context, s *listState, total int, known, tabsCounted bool) render.HTML {
	m := s.m
	title := b.title(ctx, m)
	desc := m.description(ctx)
	subtitle := ""
	if known && (desc == "" || !tabsCounted) {
		if total == 1 {
			subtitle = i18nui.EntityCountOne.Text(ctx, map[string]any{"entity": m.noun(ctx, false)})
		} else {
			subtitle = i18nui.EntityCount.Text(ctx, map[string]any{
				"count":  total,
				"entity": m.noun(ctx, true),
			})
		}
	} else {
		subtitle = desc
	}
	var actions []render.HTML
	actions = append(actions, b.actions...)
	if b.bulk && bulkOn(m) && len(b.where) == 0 && s.savedID == "" && !s.deletedView {
		actions = append(actions, exportLink(ctx, s))
	}
	if !s.deletedView && b.mayCreate() && s.createPath != "" && canCreate(ctx, m) {
		actions = append(actions, ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.EntityNew.Text(ctx, map[string]any{"entity": m.singular(ctx)}),
			Href:    s.createHref(),
			Variant: ui.ButtonPrimary,
			Icon:    "plus",
		}))
	}
	return ui.PageHeader(ui.PageHeaderConfig{
		Title:        title,
		Subtitle:     subtitle,
		Actions:      actionCluster(actions),
		HeadingLevel: b.headingLevel(),
	})
}

func (b *ListBuilder) embeddedBody(ctx context.Context, s *listState, rows []map[string]any, total int, known bool, page int) render.HTML {
	m := s.m
	title := b.title(ctx, m)
	var count render.HTML
	if known {
		count = ui.Muted(render.Text(i18n.Number(ctx, total)))
	}
	actions := slices.Clone(b.actions)
	if !s.deletedView && b.mayCreate() && s.createPath != "" && canCreate(ctx, m) {
		actions = append(actions, ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.EntityAdd.Text(ctx, map[string]any{"entity": m.noun(ctx, false)}),
			Href:    s.createHref(),
			Variant: ui.ButtonSecondary,
			Size:    ui.ButtonSizeSmall,
			Icon:    "plus",
		}))
	}
	head := ui.PageHeader(ui.PageHeaderConfig{
		Title:        title,
		Badge:        count,
		Actions:      actionCluster(actions),
		HeadingLevel: b.headingLevel(),
		Compact:      true,
	})
	switch {
	case known && total == 0:
		return render.Join(head, ui.EmptyState(b.emptyState(ctx, s)))
	case s.as == "cards":
		return render.Join(head, b.cards(ctx, s, rows, total, known, page))
	}
	return render.Join(head, b.table(ctx, s, nil, rows, total, known, page))
}

func actionCluster(actions []render.HTML) render.HTML {
	switch len(actions) {
	case 0:
		return ""
	case 1:
		return actions[0]
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, actions...)
}

func (b *ListBuilder) facetControls(ctx context.Context, s *listState) []ui.Facet {
	m := s.m
	facets := make([]ui.Facet, 0, len(m.d.Facets))
	for _, name := range s.facets() {
		f, ok := m.field(name)
		if !ok {
			continue
		}
		p := s.facetParam(name)
		facet := ui.Facet{Name: p, Label: m.label(ctx, name), Value: strings.TrimSpace(s.q.Get(p)),
			AllLabel: i18nui.FilterAllPlain.Text(ctx)}
		switch f.Type {
		case schema.Bool:
			facet.Options = []ui.FacetOption{
				{Label: i18nui.EntityYes.Text(ctx), Value: "true"},
				{Label: i18nui.EntityNo.Text(ctx), Value: "false"},
			}
			facet.Kind = ui.FacetPills
		case schema.Relation:
			facet.Options = b.ui.relationFacetOptions(ctx, m, name)
			facet.Kind = ui.FacetSelect
		default:
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
	return facets
}

func (b *ListBuilder) toolbar(ctx context.Context, s *listState, facets []ui.Facet) render.HTML {
	m := s.m
	var search *ui.FilterSearch
	if len(m.e.Config.SearchFields) > 0 {
		placeholder := i18nui.EntitySearch.Text(ctx, map[string]any{"entity": m.plural(ctx)})
		search = &ui.FilterSearch{Name: s.p.q, Value: s.search, Placeholder: placeholder, Label: placeholder}
		if !b.embedded && !b.top && s.key == "" {
			search.Shortcut = "/"
		}
	}
	var extra []render.HTML
	applied := 0
	if qf := b.queryField(ctx, s); qf != "" {
		extra = append(extra, qf)
		if s.filterText != "" {
			applied = 1
		}
	}
	var tools []render.HTML
	if menu := b.columnsMenu(ctx, s); menu != "" {
		tools = append(tools, menu)
	}
	if sw := b.layoutSwitchControl(ctx, s); sw != "" {
		tools = append(tools, sw)
	}
	if len(facets) == 0 && search == nil && len(extra) == 0 {
		return actionCluster(tools)
	}
	var hidden []ui.HiddenField
	for _, k := range slices.Sorted(maps.Keys(s.q)) {
		if k == s.p.q || k == s.p.page || s.ownsFacetParam(k) || (len(extra) > 0 && (k == s.p.filter || k == s.p.rowF || k == s.p.rowO || k == s.p.rowV)) {
			continue
		}
		for _, v := range s.q[k] {
			hidden = append(hidden, ui.HiddenField{Name: k, Value: v})
		}
	}
	hideReset := false
	for k := range s.q {
		if !s.ownsParam(k) {
			hideReset = true
			break
		}
	}
	return ui.FilterToolbar(ui.FilterToolbarConfig{
		ID:        "eui-" + listIDSafe(s.key, m.name) + "-toolbar",
		Action:    s.path,
		Dropdown:  true,
		Facets:    facets,
		Search:    search,
		Extra:     extra,
		Applied:   applied,
		Tools:     tools,
		Hidden:    hidden,
		HideReset: hideReset,
		Ctx:       ctx,
	})
}

func (b *ListBuilder) layoutSwitchControl(ctx context.Context, s *listState) render.HTML {
	if !b.layoutSwitch {
		return ""
	}
	href := func(as string) string {
		q := s.carryWithSort(s.p.as)
		q.Set(s.p.as, as)
		return listHref(s.path, q)
	}
	cards := s.as == "cards"
	return ui.SegmentedLinks(ui.SegmentedLinksConfig{
		Label: i18nui.EntityLayout.Text(ctx),
		Items: []ui.SegmentLink{
			{Text: i18nui.EntityLayoutTable.Text(ctx), Icon: "list", Href: href("table"), Current: !cards},
			{Text: i18nui.EntityLayoutCards.Text(ctx), Icon: "grid", Href: href("cards"), Current: cards},
		},
		Ctx: ctx,
	})
}

func (s *listState) ownsFacetParam(name string) bool {
	for _, f := range s.facets() {
		if name == s.facetParam(f) {
			return true
		}
	}
	return false
}

func (b *ListBuilder) title(ctx context.Context, m *meta) string {
	if b.heading != "" {
		return b.heading
	}
	return m.plural(ctx)
}

func (b *ListBuilder) headingLevel() int {
	switch {
	case b.level == 0:
		return 1
	case b.level >= 1 && b.level <= 5:
		return b.level
	}
	panic(fmt.Sprintf("entityui: %s: Heading level %d is out of range — 1 to 5 (0 means 1); the empty state takes the level below", b.entity, b.level))
}

func (s *listState) recordHref(id string) string {
	return under(s.base, s.m.idPath(id))
}

func (b *ListBuilder) state(ctx context.Context, m *meta) (*listState, crud.ListOptions, error) {
	s := &listState{m: m, key: b.key, p: listParamsFor(b.key), pins: b.where}
	s.q = appui.QueryFromContext(ctx)
	s.path = b.base
	if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
		s.path = r.URL.Path
		s.filterBad = s.ownPairUndecodable(r.URL.RawQuery)
	}
	s.base = b.base
	if s.base == "" {
		s.base = s.path
	}

	s.createPath = b.ui.createScreenPath(m.e, s.base)

	if err := s.resolveColumns(b); err != nil {
		return nil, crud.ListOptions{}, err
	}
	if b.colsMenu {
		s.applyColsParam()
	}
	if err := b.openSaved(ctx, s); err != nil {
		return nil, crud.ListOptions{}, err
	}
	if err := b.narrow(ctx, s); err != nil {
		return nil, crud.ListOptions{}, err
	}
	s.offeredTab = b.deleted && m.e.Config.Scope.SoftDelete
	s.deletedView = s.offeredTab && s.view == deletedViewKey
	s.as = b.as
	if s.as == "" {
		for _, v := range m.d.Views {
			if v.Key == s.view {
				s.as = v.As
			}
		}
	}
	if b.layoutSwitch {
		if v := s.q.Get(s.p.as); v == "table" || v == "cards" {
			s.as = v
		}
	}
	switch s.as {
	case "", "table", "cards":
	default:
		return nil, crud.ListOptions{}, fmt.Errorf("entityui: entity %q: As(%q) must be \"table\" or \"cards\"", m.name, b.as)
	}

	s.resolveSort(b)
	s.resolvePage(b)

	where, err := s.predicate(b)
	if err != nil {
		return nil, crud.ListOptions{}, err
	}
	filters := s.facetFilters()
	read := crud.ListOptions{
		Where:   where,
		Filters: filters,
		Search:  s.search,
		Fields:  s.readFields(),
		Deleted: s.deletedView,
	}
	return s, read, nil
}
