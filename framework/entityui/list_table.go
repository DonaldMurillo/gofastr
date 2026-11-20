package entityui

import (
	"context"
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *ListBuilder) table(ctx context.Context, s *listState, lb *listBulk, rows []map[string]any, total int, known bool, page int) render.HTML {
	labels := b.ui.resolveRowLabels(ctx, s, rows, s.columns)
	linkCol := linkColumn(s)
	phone := phoneSlots(s, linkCol)
	cols := make([]ui.Column, 0, len(s.columns)+2)
	if lb != nil {
		cols = append(cols, ui.Column{Key: "_s", SelectAll: "ids", Fit: true, Phone: ui.PhoneLead})
	}
	for _, name := range s.columns {
		f, _ := s.m.field(name)
		col := ui.Column{Key: name, Header: s.m.label(ctx, name), Sortable: s.sortable(name) && !b.top, Phone: phone[name]}
		if numericField(f) {
			col.Align = "end"
		}
		col.Wrap = f.Type == schema.Text
		cols = append(cols, col)
	}
	noLinks := b.noLinks || s.deletedView
	if (!noLinks && !b.top) || s.deletedView {
		cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end", Fit: true, Phone: ui.PhoneEnd})
	}

	uiRows := make([]ui.Row, 0, len(rows))
	for i, row := range rows {
		id := s.m.rowID(row)
		cells := map[string]render.HTML{}
		if lb != nil {
			cells["_s"] = selectCell(ctx, s, lb, row, i)
		}
		editable := s.rawRows != nil && canUpdate(ctx, s.m, id)
		for _, name := range s.columns {
			f, _ := s.m.field(name)
			cells[name] = b.ui.cellHTML(ctx, s, labels, f, row, name)
			if editable && name != linkCol {
				if ed := b.inlineEditor(ctx, s, f, row, cells[name], id, i); ed != "" {
					cells[name] = ed
				}
			}
			if name == linkCol && !noLinks {
				text := b.ui.plainText(ctx, s, labels, f, row, name)
				if text == "" {
					text = id
				}
				cells[name] = ui.Link(ui.LinkConfig{
					Href:    s.recordHref(id),
					Text:    text,
					Variant: ui.LinkTitle,
				})
			}
		}
		if s.deletedView {
			cells["_a"] = b.deletedActions(ctx, s, row)
		} else if !noLinks && !b.top {
			cells["_a"] = b.rowActions(ctx, s, row, i)
		}
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}

	dt := ui.DataTableConfig{
		Columns:       cols,
		Rows:          uiRows,
		Responsive:    ui.ResponsiveRows,
		Flush:         b.flush,
		SortBy:        s.sortField,
		SortDir:       ui.SortDir(sortDir(s.sortDesc)),
		Path:          s.path,
		Query:         s.carry(),
		SortParam:     s.p.sort,
		DirParam:      s.p.dir,
		Empty:         b.emptyState(ctx, s),
		Ctx:           ctx,
		Caption:       b.title(ctx, s.m),
		CaptionHidden: true,
	}
	if known && pagesFor(total, s.limit) > 1 && !b.top {
		dt.Pagination = &ui.PaginationConfig{
			Pages:     pagesFor(total, s.limit),
			Page:      page,
			Path:      s.path,
			Query:     s.pagerQuery(),
			PageParam: s.p.page,
			Ctx:       ctx,
		}
	}
	if known && total > 0 && len(rows) > 0 && !b.top {
		//gofastr:allow(intwrap) page is clampPage'd to the total's page count before table runs (list_render.go), and s.limit is one of the offered page sizes under maxListLimit (list_query.go)
		from := (page-1)*s.limit + 1
		dt.Summary = i18nui.EntityRange.Text(ctx, map[string]any{
			"from":  from,
			"to":    from + len(rows) - 1,
			"total": total,
		})
		dt.FooterTools = b.perPageMenu(ctx, s, total)
	}
	if s.inlineDrawn {
		dt.Note = i18nui.EntityInlineEditHint.Text(ctx)
	}
	return ui.DataTable(dt)
}

func (b *ListBuilder) perPageMenu(ctx context.Context, s *listState, total int) render.HTML {
	sizes := s.pageSizes(b)
	if len(sizes) == 0 || total <= sizes[0] {
		return ""
	}
	items := make([]ui.MenuItem, len(sizes))
	for i, n := range sizes {
		q := s.carryWithSort(s.p.page, s.p.per)
		q.Set(s.p.per, strconv.Itoa(n))
		items[i] = ui.MenuItem{Label: i18n.Number(ctx, n), Href: listHref(s.path, q)}
	}
	label := i18nui.EntityPageSize.Text(ctx)
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true},
		ui.Muted(render.Text(label)),
		ui.Menu(ui.MenuConfig{
			ID:       "eui-" + listIDSafe(s.key, s.m.name) + "-per",
			Label:    i18n.Number(ctx, s.limit),
			Items:    items,
			Position: ui.MenuTopEnd,
		}),
	)
}

func inlineEditable(m *meta, f schema.Field) bool {
	if m.system(f) || m.locked(f) || f.Hidden || f.NoQuery {
		return false
	}
	switch f.Type {
	case schema.String, schema.Enum, schema.Bool, schema.Int, schema.Float, schema.Decimal, schema.Date:
		return true
	default:
		return false
	}
}

func (b *ListBuilder) inlineEditor(ctx context.Context, s *listState, f schema.Field, row map[string]any, display render.HTML, id string, i int) render.HTML {
	if !inlineEditable(s.m, f) || display == "" {
		return ""
	}
	raw, ok := s.rawRows[id]
	if !ok || cell(s.m.rowValue(raw, f.Name)) != cell(s.m.rowValue(row, f.Name)) {
		return ""
	}
	fb := &formBuilder{b: &RecordBuilder{ui: b.ui, entity: s.m.name, id: id}, m: s.m, row: raw, compact: true}
	s.inlineDrawn = true
	label := s.m.label(ctx, f.Name)
	control := fb.typedInput(ctx, f, label, "", "eui-ie-"+listIDSafe(s.key, s.m.name)+"-"+strconv.Itoa(i)+"-"+f.Name)
	return ui.InlineEdit(ui.InlineEditConfig{
		Display: display,
		Label:   i18nui.EntityInlineEdit.Text(ctx, map[string]any{"field": label, "title": s.rowTitle(ctx, row)}),
		Control: control,
		Action:  s.m.api + "/" + s.m.idPath(id),
		Return:  listHref(s.path, s.q),
		Saved:   i18nui.EntitySaved.Text(ctx),
		Ctx:     ctx,
	})
}

func (s *listState) pagerQuery() map[string][]string {
	q := s.carry(s.p.page)
	if s.sortField != "" {
		q.Set(s.p.sort, s.sortField)
		if s.sortDesc {
			q.Set(s.p.dir, "desc")
		} else {
			q.Set(s.p.dir, "asc")
		}
	}
	return q
}

func phoneSlots(s *listState, linkCol string) map[string]ui.PhoneSlot {
	out := map[string]ui.PhoneSlot{}
	if linkCol == "" {
		return out
	}
	out[linkCol] = ui.PhoneTitle
	card := cardFieldsOf(s)
	free := func(name string) bool { _, used := out[name]; return !used && s.shownColumn(name) }
	pick := func(slot ui.PhoneSlot, prefer string, ok func(schema.Field) bool) {
		if prefer != "" && free(prefer) {
			out[prefer] = slot
			return
		}
		for _, pass := range []bool{true, false} {
			for _, name := range s.columns {
				f, _ := s.m.field(name)
				if free(name) && (!pass || ok(f)) {
					out[name] = slot
					return
				}
			}
			if slot == ui.PhoneMeta {
				return
			}
		}
	}
	pick(ui.PhoneMeta, card.badge, func(f schema.Field) bool { return f.Type == schema.Enum })
	pick(ui.PhoneSubtitle, card.subtitle, func(f schema.Field) bool { return !numericField(f) && f.Type != schema.Text })
	pick(ui.PhoneDetail, "", numericField)
	return out
}

func linkColumn(s *listState) string {
	tf := s.m.titleField()
	if tf != "" && s.shownColumn(tf) {
		return tf
	}
	if len(s.columns) > 0 {
		return s.columns[0]
	}
	return ""
}

func (s *listState) shownColumn(name string) bool {
	for _, c := range s.columns {
		if c == name {
			return true
		}
	}
	return false
}

func (u *UI) resolveRowLabels(ctx context.Context, s *listState, rows []map[string]any, cols []string) labelResolver {
	out := labelResolver{}
	for _, name := range cols {
		f, ok := s.m.field(name)
		if !ok || f.Type != schema.Relation {
			continue
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if v := cell(s.m.rowValue(row, name)); v != "" {
				ids = append(ids, v)
			}
		}
		out[name] = u.resolveLabels(ctx, s.m, name, ids)
		if base, ok := u.relatedBase(ctx, s.m, name); ok {
			if s.relBase == nil {
				s.relBase = map[string]string{}
			}
			s.relBase[name] = base
		}
	}
	return out
}

func (u *UI) cellHTML(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any, name string) render.HTML {
	m := s.m
	if f.Type == schema.Relation && labels.refused(name) {
		return muted()
	}
	if kind, ok := u.kindCell(m, f); ok {
		return contain(ctx, m.name, "cell "+f.Name, func() (render.HTML, error) {
			return kind.Cell(CellContext{Ctx: asCaller(ctx), Entity: m.name, Field: f, Value: m.rowValue(row, name), Row: row}), nil
		})
	}
	raw := m.rowValue(row, name)
	val := cell(raw)
	if f.Type == schema.Relation {
		if val == "" {
			return muted()
		}
		if t, ok := labels.title(name, val); ok {
			if base, ok := s.relBase[name]; ok {
				return ui.Tag(ui.TagConfig{Ctx: ctx, Label: t, Href: under(base, url.PathEscape(val))})
			}
			return render.Text(t)
		}
		return render.Text(val)
	}
	text := m.fieldText(ctx, f, raw)
	if text == "" {
		return muted()
	}
	switch f.Type {
	case schema.Bool:
		variant := ui.StatusNeutral
		if truthy(val) {
			variant = ui.StatusSuccess
		}
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: text, Variant: variant})
	case schema.Enum:
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: text, Variant: enumVariant(val), Dot: true})
	case schema.Image, schema.File:
		if t := u.fileValue(f, m.label(ctx, name), val, ui.ThumbnailSM); t != "" {
			return t
		}
		return muted()
	default:
	}
	return render.Text(text)
}

func (u *UI) plainText(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any, name string) string {
	m := s.m
	if f.Type == schema.Relation && labels.refused(name) {
		return ""
	}
	if kind, ok := u.kindOf(m, f); ok && kind.Text != nil {
		return kindText(ctx, m.name, f.Name, func() string {
			return kind.Text(CellContext{Ctx: asCaller(ctx), Entity: m.name, Field: f, Value: m.rowValue(row, name), Row: row})
		})
	}
	raw := m.rowValue(row, name)
	if f.Type == schema.Relation {
		val := cell(raw)
		if val == "" {
			return ""
		}
		if t, ok := labels.title(name, val); ok {
			return t
		}
		return val
	}
	return m.fieldText(ctx, f, raw)
}

// fieldText is a non-relation value as plain text: localized yes/no, the
// enum label, a formatted number or date, or the raw text; "" when empty.
// List cells, exports and record views decorate this one mapping.
func (m *meta) fieldText(ctx context.Context, f schema.Field, raw any) string {
	val := cell(raw)
	if val == "" {
		return ""
	}
	switch f.Type {
	case schema.Bool:
		if truthy(val) {
			return i18nui.EntityYes.Text(ctx)
		}
		return i18nui.EntityNo.Text(ctx)
	case schema.Enum:
		return m.valueLabel(ctx, f.Name, val)
	case schema.Decimal, schema.Float:
		return decimal(ctx, f, val)
	case schema.Int:
		return integer(ctx, val)
	case schema.Date:
		return formatDate(ctx, raw, false)
	case schema.Timestamp:
		return formatDate(ctx, raw, true)
	default:
	}
	return val
}

func (u *UI) kindCell(m *meta, f schema.Field) (Kind, bool) {
	k, ok := u.kindOf(m, f)
	if !ok || k.Cell == nil {
		return Kind{}, false
	}
	return k, true
}

func (u *UI) kindOf(m *meta, f schema.Field) (Kind, bool) {
	in := m.hint(f.Name).Input
	if in == "" {
		return Kind{}, false
	}
	k, ok := u.ext.Kinds[in]
	if !ok && isBuiltinKind(in) {
		k, ok = builtinKind(in), true
	}
	return k, ok
}

func numericField(f schema.Field) bool {
	switch f.Type {
	case schema.Int, schema.Float, schema.Decimal:
		return true
	default:
		return false
	}
}

func sortDir(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

func (b *ListBuilder) rowActions(ctx context.Context, s *listState, row map[string]any, i int) render.HTML {
	m := s.m
	id := m.rowID(row)
	href := s.recordHref(id)
	title := s.rowTitle(ctx, row)

	spanID := "eui-url-" + listIDSafe(s.key, m.name) + "-" + strconv.Itoa(i)
	urlSpan := html.Span(html.TextConfig{
		Class:      "fui-visually-hidden",
		ExtraAttrs: html.Attrs{"aria-hidden": "true", "id": spanID, "data-cui-internal": ""},
	}, render.Text(href))

	items := []ui.MenuItem{
		{Label: i18nui.EntityView.Text(ctx), Href: href},
		{Label: i18nui.EntityCopyLink.Text(ctx), Copy: &ui.MenuCopy{Target: spanID, Toast: i18nui.CopyCopied.Text(ctx)}},
	}
	if b.dup && s.createPath != "" && !m.d.NoDuplicate && canCreate(ctx, m) {
		items = append(items, ui.MenuItem{
			Label: i18nui.EntityDuplicate.Text(ctx),
			Href:  s.createPath + "?duplicate=" + url.QueryEscape(id),
		})
	}
	if b.delete && canDelete(ctx, m, id) {
		del := deleteAction(ctx, m, id, listHref(s.path, s.q), b.undo)
		items = append(items, ui.MenuItem{Separator: true}, ui.MenuItem{
			Label:  i18nui.EntityDelete.Text(ctx),
			Danger: true,
			Do:     &del,
		})
	}
	return render.Join(urlSpan, ui.Menu(ui.MenuConfig{
		ID:       "eui-" + listIDSafe(s.key, m.name) + "-actions-" + strconv.Itoa(i),
		Label:    i18nui.EntityRowActions.Text(ctx, map[string]any{"title": title}),
		IconOnly: true,
		Items:    items,
		Position: ui.MenuBottomEnd,
	}))
}

func listIDSafe(key, entity string) string {
	if key != "" {
		return key
	}
	return entity
}

func (b *ListBuilder) emptyState(ctx context.Context, s *listState) ui.EmptyStateConfig {
	var title, desc string
	noun := map[string]any{"entity": s.m.noun(ctx, true)}
	if b.embedded {
		return ui.EmptyStateConfig{
			Title:        i18nui.EntityEmpty.Text(ctx, noun),
			HeadingLevel: b.headingLevel() + 1,
			Compact:      true,
		}
	}
	if s.deletedView {
		title = i18nui.EntityDeletedEmpty.Text(ctx, noun)
		desc = i18nui.EntityDeletedEmptyBody.Text(ctx)
		return ui.EmptyStateConfig{Title: title, Description: desc, HeadingLevel: b.headingLevel() + 1}
	}
	if s.searchedOrFiltered() {
		return ui.EmptyStateConfig{
			Title:        i18nui.EntityNoMatch.Text(ctx, noun),
			Description:  i18nui.EntityNoMatchBody.Text(ctx),
			HeadingLevel: b.headingLevel() + 1,
			Action: ui.LinkButton(ui.LinkButtonConfig{
				Label:   i18nui.EntityClearSearch.Text(ctx),
				Href:    s.clearSearchHref(),
				Variant: ui.ButtonSecondary,
			}),
		}
	}
	if s.viewPred != nil {
		return ui.EmptyStateConfig{
			Title:        i18nui.EntityViewEmpty.Text(ctx, noun),
			Description:  i18nui.EntityViewEmptyBody.Text(ctx),
			HeadingLevel: b.headingLevel() + 1,
		}
	}
	desc = b.empty
	if desc == "" {
		desc = i18nui.EntityEmptyBody.Text(ctx)
	}
	cfg := ui.EmptyStateConfig{
		Title:        i18nui.EntityEmpty.Text(ctx, noun),
		Description:  desc,
		HeadingLevel: b.headingLevel() + 1,
	}
	if b.mayCreate() && s.createPath != "" && canCreate(ctx, s.m) {
		cfg.Action = ui.LinkButton(ui.LinkButtonConfig{
			Label: i18nui.EntityNew.Text(ctx, map[string]any{"entity": s.m.singular(ctx)}),
			Href:  s.createHref(),
		})
	}
	return cfg
}
