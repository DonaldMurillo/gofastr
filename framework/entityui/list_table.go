package entityui

import (
	"context"
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// table draws the rows as a DataTable. Sorting and paging are typed
// props — the active sort, the carried query, the param names — so the
// namespaced params (?due_sort=, ?due_page=) ride every link the table
// builds and a second list on the page keeps its own.
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
		// Long text wraps; every other cell holds one line.
		col.Wrap = f.Type == schema.Text
		cols = append(cols, col)
	}
	// The trash view keeps the actions column — its rows carry the
	// restore and purge forms — while drawing no record link.
	noLinks := b.noLinks || s.deletedView
	if (!noLinks && !b.top) || s.deletedView {
		cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end", Fit: true, Phone: ui.PhoneEnd})
	}

	uiRows := make([]ui.Row, 0, len(rows))
	for i, row := range rows {
		id := cell(rowValue(row, s.m.pk))
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
				cells[name] = ui.Link(ui.LinkConfig{
					Href:    s.recordHref(id),
					Text:    b.ui.plainText(ctx, s, labels, f, row, name),
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
		Columns:    cols,
		Rows:       uiRows,
		Responsive: ui.ResponsiveRows,
		Flush:      b.flush,
		SortBy:     s.sortField,
		SortDir:    ui.SortDir(sortDir(s.sortDesc)),
		Path:       s.path,
		Query:      s.carry(),
		SortParam:  s.p.sort,
		DirParam:   s.p.dir,
		Empty:      b.emptyState(ctx, s),
		Ctx:        ctx,
		// The heading names the table too, as a hidden caption (the
		// heading already shows): the scroll region is a named region,
		// and two lists on one page are told apart.
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
	if s.inlineDrawn {
		// The cells look like values; the line under the rows says they
		// edit where they stand.
		return render.Join(ui.DataTable(dt), ui.Muted(render.Text(i18nui.T(ctx, i18nui.KeyEntityInlineEditHint))))
	}
	return ui.DataTable(dt)
}

// inlineEditable is whether a cell of f may be edited in place: a plain
// scalar the record form edits with one control, never a system,
// locked, hidden or NoQuery column.
func inlineEditable(m *meta, f schema.Field) bool {
	if m.system(f) || m.locked(f) || f.Hidden || f.NoQuery {
		return false
	}
	switch f.Type {
	case schema.String, schema.Enum, schema.Bool, schema.Int, schema.Float, schema.Decimal, schema.Date:
		return true
	default:
		// Text, JSON, files, relations, timestamps and UUIDs need the
		// record form's larger controls.
		return false
	}
}

// inlineEditor wraps a cell in a ui.InlineEdit whose form PUTs the one
// field to the record's write route and returns to this URL. It draws
// nothing for a field it may not edit, or whose hooked value differs
// from the stored one (a mask).
func (b *ListBuilder) inlineEditor(ctx context.Context, s *listState, f schema.Field, row map[string]any, display render.HTML, id string, i int) render.HTML {
	if !inlineEditable(s.m, f) || display == "" {
		return ""
	}
	raw, ok := s.rawRows[id]
	if !ok || cell(rowValue(raw, f.Name)) != cell(rowValue(row, f.Name)) {
		return ""
	}
	fb := &formBuilder{b: &RecordBuilder{ui: b.ui, entity: s.m.name, id: id}, m: s.m, row: raw, compact: true}
	s.inlineDrawn = true
	label := s.m.label(ctx, f.Name)
	control := fb.typedInput(ctx, f, label, "", "eui-ie-"+listIDSafe(s.key, s.m.name)+"-"+strconv.Itoa(i)+"-"+f.Name)
	return ui.InlineEdit(ui.InlineEditConfig{
		Display: display,
		Label:   i18nui.TVars(ctx, i18nui.KeyEntityInlineEdit, map[string]string{"field": label, "title": s.rowTitle(ctx, row)}),
		Control: control,
		Action:  s.m.api + "/" + url.PathEscape(id),
		Return:  listHref(s.path, s.q),
		Saved:   i18nui.T(ctx, i18nui.KeyEntitySaved),
		Ctx:     ctx,
	})
}

// pagerQuery carries everything a page turn must not drop: the search,
// the facets, the filter, the view, the active sort — and every foreign
// param on the URL.
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

// phoneSlots places the shown columns on the two-line phone row: the
// link column is the title, the card badge (or the first enum) the
// meta at the end, the card subtitle (or the first other text column)
// under the title, and the first number (or the next column) under
// the meta. Every other column stays off the phone row; the record
// page shows it.
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
				return // no enum: the row keeps its end clear
			}
		}
	}
	pick(ui.PhoneMeta, card.badge, func(f schema.Field) bool { return f.Type == schema.Enum })
	// Long text is a note, not a line that tells two records apart.
	pick(ui.PhoneSubtitle, card.subtitle, func(f schema.Field) bool { return !numericField(f) && f.Type != schema.Text })
	pick(ui.PhoneDetail, "", numericField)
	return out
}

// linkColumn is the column whose cell links to the record: the title
// field when it is shown, else the first column.
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

// shownColumn reports whether name is one of the shown columns.
func (s *listState) shownColumn(name string) bool {
	for _, c := range s.columns {
		if c == name {
			return true
		}
	}
	return false
}

// resolveRowLabels resolves the relation columns' titles for one page's
// rows, one IN read per relation.
func (u *UI) resolveRowLabels(ctx context.Context, s *listState, rows []map[string]any, cols []string) labelResolver {
	out := labelResolver{}
	for _, name := range cols {
		f, ok := s.m.field(name)
		if !ok || f.Type != schema.Relation {
			continue
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if v := cell(rowValue(row, name)); v != "" {
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

// cellHTML draws one field's value: the badges, dates and decimals the
// kit shapes, the related record's title, or a registered kind's Cell.
func (u *UI) cellHTML(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any, name string) render.HTML {
	m := s.m
	// A relation the caller may not read is muted before any kind sees
	// its foreign key.
	if f.Type == schema.Relation && labels.refused(name) {
		return muted()
	}
	if kind, ok := u.kindCell(m, f); ok {
		// An app's Cell runs behind a recover: a panicking kind degrades
		// to a muted value, never a failed list.
		return contain(ctx, m.name, "cell "+f.Name, func() (render.HTML, error) {
			return kind.Cell(CellContext{Ctx: asCaller(ctx), Entity: m.name, Field: f, Value: rowValue(row, name), Row: row}), nil
		})
	}
	val := cell(rowValue(row, name))
	if f.Type == schema.Relation {
		// nil labels: the caller may not read the related entity. Muted,
		// never the raw foreign key — a bare id where a name belongs is
		// useless to a reader and discloses an internal id.
		if val == "" || labels.refused(name) {
			return muted()
		}
		if t, ok := labels.title(name, val); ok {
			// A title the caller read links to its record where the UI
			// knows the related entity's screens.
			if base, ok := s.relBase[name]; ok {
				return ui.Tag(ui.TagConfig{Label: t, Href: base + "/" + url.PathEscape(val)})
			}
			return render.Text(t)
		}
		// The id names a record of an entity the caller may read, but the
		// read did not return it (filtered out, or past the facet cap).
		// Printing the id is a legibility limit, not a disclosure.
		return render.Text(val)
	}
	switch f.Type {
	case schema.Bool:
		if truthy(val) {
			return ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyEntityYes), Variant: ui.StatusSuccess})
		}
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyEntityNo), Variant: ui.StatusNeutral})
	case schema.Enum:
		if val == "" {
			return muted()
		}
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: m.valueLabel(ctx, name, val), Variant: enumVariant(val), Dot: true})
	default:
		// Textual shapes draw below the empty check.
	}
	if val == "" {
		return muted()
	}
	switch f.Type {
	case schema.Decimal, schema.Float:
		return render.Text(decimal(val))
	case schema.Date:
		return render.Text(formatDate(rowValue(row, name), dateLayout))
	case schema.Timestamp:
		return render.Text(formatDate(rowValue(row, name), timestampLayout))
	case schema.Image, schema.File:
		// A value with no safe address draws the empty mark, never the
		// stored text.
		if t := u.fileValue(f, m.label(ctx, name), val, ui.ThumbnailSM); t != "" {
			return t
		}
		return muted()
	default:
		// Everything else prints its value as text.
	}
	return render.Text(val)
}

// plainText is a value's text form, the same shapes cellHTML draws
// without the markup: a link label, a card title, a chip.
func (u *UI) plainText(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any, name string) string {
	m := s.m
	if f.Type == schema.Relation && labels.refused(name) {
		return ""
	}
	if kind, ok := u.kindCell(m, f); ok {
		return string(contain(ctx, m.name, "cell "+f.Name, func() (render.HTML, error) {
			return kind.Cell(CellContext{Ctx: asCaller(ctx), Entity: m.name, Field: f, Value: rowValue(row, name), Row: row}), nil
		}))
	}
	raw := rowValue(row, name)
	val := cell(raw)
	if f.Type == schema.Relation {
		if val == "" || labels.refused(name) {
			return ""
		}
		if t, ok := labels.title(name, val); ok {
			return t
		}
		return val
	}
	switch f.Type {
	case schema.Enum:
		if val == "" {
			return ""
		}
		return m.valueLabel(ctx, name, val)
	case schema.Decimal, schema.Float:
		if val == "" {
			return ""
		}
		return decimal(val)
	case schema.Date:
		return formatDate(raw, dateLayout)
	case schema.Timestamp:
		return formatDate(raw, timestampLayout)
	default:
		// Everything else prints its value as text.
	}
	return val
}

// kindCell reports the kind that draws this field's cells, when
// Display.Fields names one and the kind has a Cell: an app kind, else a
// built-in one. Of the built-ins only money draws its own cell.
func (u *UI) kindCell(m *meta, f schema.Field) (Kind, bool) {
	in := m.hint(f.Name).Input
	if in == "" {
		return Kind{}, false
	}
	k, ok := u.ext.Kinds[in]
	if !ok && isBuiltinKind(in) {
		k, ok = builtinKind(in), true
	}
	if !ok || k.Cell == nil {
		return Kind{}, false
	}
	return k, true
}

// numericField reports whether a column's values right-align.
func numericField(f schema.Field) bool {
	switch f.Type {
	case schema.Int, schema.Float, schema.Decimal:
		return true
	default:
		// Every other type reads as text and aligns to the start.
		return false
	}
}

func sortDir(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

// rowActions draws the row menu, one icon-only trigger named for the
// record: Open, Copy link, Duplicate where turned on and, where turned
// on and the entity mounts write routes, Delete as an RPC that confirms,
// re-fetches the page on success and toasts a refusal.
func (b *ListBuilder) rowActions(ctx context.Context, s *listState, row map[string]any, i int) render.HTML {
	m := s.m
	id := cell(rowValue(row, m.pk))
	href := s.recordHref(id)
	title := s.rowTitle(ctx, row)

	// Copy link targets a hidden span holding the URL: the copy module
	// copies an element's text, so the page carries the link as text.
	spanID := "eui-url-" + listIDSafe(s.key, m.name) + "-" + strconv.Itoa(i)
	urlSpan := html.Span(html.TextConfig{
		Class:      "fui-visually-hidden",
		ExtraAttrs: html.Attrs{"aria-hidden": "true", "id": spanID, "data-cui-internal": ""},
	}, render.Text(href))

	items := []ui.MenuItem{
		{Label: i18nui.T(ctx, i18nui.KeyEntityView), Href: href},
		{Label: i18nui.T(ctx, i18nui.KeyEntityCopyLink), Copy: &ui.MenuCopy{Target: spanID, Toast: i18nui.T(ctx, i18nui.KeyCopyCopied)}},
	}
	if b.dup && !m.d.NoDuplicate && canCreate(ctx, m) {
		items = append(items, ui.MenuItem{
			Label: i18nui.T(ctx, i18nui.KeyEntityDuplicate),
			Href:  s.base + "/create?duplicate=" + url.QueryEscape(id),
		})
	}
	if b.delete && canDelete(ctx, m, id) {
		del := deleteAction(ctx, m, id, listHref(s.path, s.q), b.undo)
		items = append(items, ui.MenuItem{Separator: true}, ui.MenuItem{
			Label:  i18nui.T(ctx, i18nui.KeyEntityDelete),
			Danger: true,
			Do:     &del,
		})
	}
	return render.Join(urlSpan, ui.Menu(ui.MenuConfig{
		Label:    i18nui.TVars(ctx, i18nui.KeyEntityRowActions, map[string]string{"title": title}),
		IconOnly: true,
		Items:    items,
		Position: ui.MenuBottomEnd,
	}))
}

// listIDSafe is a fragment safe for an id attribute.
func listIDSafe(key, entity string) string {
	if key != "" {
		return key
	}
	return entity
}

// emptyState is the list's empty state: New where the caller may create,
// the builder's text over the default description, one heading level
// below the list's own. A list narrowed to nothing says so instead: a
// search, filter or facet offers to clear them, and a view says the
// others may hold rows. Neither offers New, since the entity is not
// empty.
func (b *ListBuilder) emptyState(ctx context.Context, s *listState) ui.EmptyStateConfig {
	var title, desc string
	noun := map[string]string{"entity": s.m.noun(ctx, true)}
	if b.embedded {
		// The section's header already offers Add.
		return ui.EmptyStateConfig{
			Title:        i18nui.TVars(ctx, i18nui.KeyEntityEmpty, noun),
			HeadingLevel: b.headingLevel() + 1,
			Compact:      true,
		}
	}
	if s.deletedView {
		// The trash view's own empty state: nothing is deleted, and
		// there is no New to offer from inside it.
		title = i18nui.TVars(ctx, i18nui.KeyEntityDeletedEmpty, noun)
		desc = i18nui.T(ctx, i18nui.KeyEntityDeletedEmptyBody)
		return ui.EmptyStateConfig{Title: title, Description: desc, HeadingLevel: b.headingLevel() + 1}
	}
	if s.searchedOrFiltered() {
		return ui.EmptyStateConfig{
			Title:        i18nui.TVars(ctx, i18nui.KeyEntityNoMatch, noun),
			Description:  i18nui.T(ctx, i18nui.KeyEntityNoMatchBody),
			HeadingLevel: b.headingLevel() + 1,
			Action: ui.LinkButton(ui.LinkButtonConfig{
				Label:   i18nui.T(ctx, i18nui.KeyEntityClearSearch),
				Href:    s.clearSearchHref(),
				Variant: ui.ButtonSecondary,
			}),
		}
	}
	if s.viewPred != nil {
		return ui.EmptyStateConfig{
			Title:        i18nui.TVars(ctx, i18nui.KeyEntityViewEmpty, noun),
			Description:  i18nui.T(ctx, i18nui.KeyEntityViewEmptyBody),
			HeadingLevel: b.headingLevel() + 1,
		}
	}
	desc = b.empty
	if desc == "" {
		desc = i18nui.T(ctx, i18nui.KeyEntityEmptyBody)
	}
	cfg := ui.EmptyStateConfig{
		Title:        i18nui.TVars(ctx, i18nui.KeyEntityEmpty, noun),
		Description:  desc,
		HeadingLevel: b.headingLevel() + 1,
	}
	if b.mayCreate() && canCreate(ctx, s.m) {
		cfg.Action = ui.LinkButton(ui.LinkButtonConfig{
			Label: i18nui.TVars(ctx, i18nui.KeyEntityNew, map[string]string{"entity": s.m.singular(ctx)}),
			Href:  s.createHref(),
		})
	}
	return cfg
}
