package entityui

import (
	"context"
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
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
	cols := make([]ui.Column, 0, len(s.columns)+2)
	if lb != nil {
		cols = append(cols, ui.Column{Key: "_s", Header: ""})
	}
	for _, name := range s.columns {
		f, _ := s.m.field(name)
		col := ui.Column{Key: name, Header: s.m.label(ctx, name), Sortable: s.sortable(name)}
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
	if !noLinks || s.deletedView {
		cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end"})
	}

	uiRows := make([]ui.Row, 0, len(rows))
	for i, row := range rows {
		id := cell(rowValue(row, s.m.pk))
		cells := map[string]render.HTML{}
		if lb != nil {
			cells["_s"] = selectCell(ctx, s, lb, row, i)
		}
		for _, name := range s.columns {
			f, _ := s.m.field(name)
			cells[name] = b.ui.cellHTML(ctx, s, labels, f, row, name)
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
		} else if !noLinks {
			cells["_a"] = b.rowActions(ctx, s, row, i)
		}
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}

	dt := ui.DataTableConfig{
		Columns:    cols,
		Rows:       uiRows,
		Responsive: ui.ResponsiveScroll,
		SortBy:     s.sortField,
		SortDir:    ui.SortDir(sortDir(s.sortDesc)),
		Path:       s.path,
		Query:      s.carry(),
		SortParam:  s.p.sort,
		DirParam:   s.p.dir,
		Empty:      b.emptyState(ctx, s),
		Ctx:        ctx,
	}
	if known && pagesFor(total, s.limit) > 1 {
		dt.Pagination = &ui.PaginationConfig{
			Pages:     pagesFor(total, s.limit),
			Page:      page,
			Path:      s.path,
			Query:     s.pagerQuery(),
			PageParam: s.p.page,
			Ctx:       ctx,
		}
	}
	return ui.DataTable(dt)
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
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: m.valueLabel(ctx, name, val), Variant: enumVariant(val)})
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
	case schema.Image:
		// A URL the image policy refuses draws the empty mark, never
		// the stored text.
		if t := ui.Thumbnail(ui.ThumbnailConfig{Src: val, Alt: m.label(ctx, name), Size: ui.ThumbnailSM}); t != "" {
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
	title := m.recordTitle(ctx, row)

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
		del := interactive.Delete(m.api + "/" + url.PathEscape(id)).
			WithConfirmDialog(deleteConfirm(ctx, m.noun(ctx, false))).
			OnSuccess(interactive.Navigate(listHref(s.path, s.q))).
			// A refusal (a row other records still reference) toasts
			// the server's message instead of ending in silence.
			OnErrorToast(i18nui.TVars(ctx, i18nui.KeyEntityDeleteFailed, map[string]string{"entity": m.singular(ctx)}))
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
// below the list's own.
func (b *ListBuilder) emptyState(ctx context.Context, s *listState) ui.EmptyStateConfig {
	var title, desc string
	if s.deletedView {
		// The trash view's own empty state: nothing is deleted, and
		// there is no New to offer from inside it.
		title = i18nui.TVars(ctx, i18nui.KeyEntityDeletedEmpty, map[string]string{"entity": s.m.noun(ctx, true)})
		desc = i18nui.T(ctx, i18nui.KeyEntityDeletedEmptyBody)
		return ui.EmptyStateConfig{Title: title, Description: desc, HeadingLevel: b.headingLevel() + 1}
	}
	desc = b.empty
	if desc == "" {
		desc = i18nui.T(ctx, i18nui.KeyEntityEmptyBody)
	}
	cfg := ui.EmptyStateConfig{
		Title:        i18nui.TVars(ctx, i18nui.KeyEntityEmpty, map[string]string{"entity": s.m.noun(ctx, true)}),
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
