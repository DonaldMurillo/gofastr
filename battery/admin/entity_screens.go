package admin

// Server-rendered screens for the entity CRUD admin. These register on the
// host's app.App, so they render with the full chrome + runtime.js and hydrate:
// the list is a DataTable island (paginate via RPC, no reload) and each row's
// Delete is a `data-fui-confirm` + `data-fui-rpc` button. Forms are plain SSR
// forms (browser follows the 303 to the host-rendered list); validation errors
// round-trip through a one-shot flash so the re-render is a full host page.
//
// No bespoke JavaScript: every interaction is a declarative data-fui-* primitive
// the runtime already understands.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// signalName is the per-entity island signal the list table binds to.
func signalName(ent *entity.Entity) string { return "admin-" + ent.GetTable() + "-rows" }

// ----- list screen ----------------------------------------------------------

type entityListScreen struct {
	component.ContextOnly
	b   *Battery
	ent *entity.Entity
}

func (s *entityListScreen) RenderCtx(ctx context.Context) render.HTML {
	base := s.b.entityBase(s.ent)
	q := appui.QueryFromContext(ctx)

	// "New" is a real anchor (not a button) so the runtime intercepts it for
	// SPA navigation to the form screen — no scripting.
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:   s.ent.GetName(),
		Actions: ui.Link(ui.LinkConfig{Href: base + "/new", Text: "New " + singular(s.ent.GetName()), Variant: ui.LinkAction}),
	})

	// Toolbar: search + sort controls in a dedicated area, OUTSIDE the island so
	// they survive sort/page RPC swaps. Both work on every viewport — crucially
	// the sort dropdown replaces the clickable column headers on mobile, where
	// the table collapses to cards and the headers disappear. Search is a GET
	// <form>, sort is anchors; both SPA-navigate (no reload) and the server does
	// the filtering/sorting — the admin never sorts or filters in JS.
	body := []render.HTML{header}
	var controls []render.HTML
	if searchField(s.ent) != "" {
		controls = append(controls, ui.SearchInput(ui.SearchInputConfig{
			Name:        "q",
			ID:          "admin-search-" + s.ent.GetTable(),
			Action:      base,
			Method:      "GET",
			Placeholder: "Search " + s.ent.GetName() + "…",
			ExtraAttrs:  map[string]string{"value": q.Get("q")},
		}))
	}
	controls = append(controls, s.b.sortControl(s.ent, q))
	body = append(body, render.Tag("div", map[string]string{"class": "admin-toolbar"}, controls...))

	// The DataTable lives inside its signal-bound wrapper; sort/pagination RPCs
	// swap this wrapper's innerHTML with the fragment from _rows.
	island := render.Tag("div", map[string]string{
		"data-fui-signal":      signalName(s.ent),
		"data-fui-signal-mode": "html",
	}, s.b.renderTable(ctx, s.ent, q))
	body = append(body, island)

	return s.b.shell(ui.Container(ui.ContainerConfig{Class: "admin-entity"}, body...))
}

// renderTable fetches the current page via the CrudHandler and renders the
// DataTable (bare — the signal wrapper is added by the caller / is the existing
// DOM node on island swap). Errors render an inline notice instead of panicking.
func (b *Battery) renderTable(ctx context.Context, ent *entity.Entity, q url.Values) render.HTML {
	base := b.entityBase(ent)
	limit := b.cfg.EntityListLimit
	page := atoiDefault(q.Get("p"), 1)
	if page < 1 {
		page = 1
	}
	cols := listColumns(ent)

	// Sort state. Only honor a sort on a column we actually display (the
	// CrudHandler validates too, but a bad value would surface as an empty
	// table rather than silently). Direction defaults to ascending.
	sortCol := q.Get("sort")
	if sortCol != "" && !containsStr(cols, sortCol) {
		sortCol = ""
	}
	sortDir := SortDirOf(q.Get("dir"))
	search := strings.TrimSpace(q.Get("q"))

	// Build the CrudHandler query: sorting and search are SERVER-side. The
	// admin never sorts or filters rows in Go/JS — it just forwards intent.
	crudQ := url.Values{}
	crudQ.Set("page", strconv.Itoa(page))
	crudQ.Set("limit", strconv.Itoa(limit))
	if sortCol != "" {
		s := sortCol
		if sortDir == "desc" {
			s = "-" + sortCol // CrudHandler encodes descending as a leading "-"
		}
		crudQ.Set("sort", s)
	}
	if search != "" {
		if sf := searchField(ent); sf != "" {
			crudQ.Set(sf+"_like", search) // LIKE %search% on the entity's label field
		}
	}

	rows, total, err := b.listRows(ctx, ent, crudQ.Encode())
	if err != nil {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:        "Could not load " + ent.GetName(),
			Description:  "Check the server logs for details.",
			HeadingLevel: 2,
		})
	}

	ftypes := fieldTypeMap(ent)
	relLabels := b.relationLabelMaps(ctx, ent)

	columns := make([]ui.Column, 0, len(cols)+1)
	for _, c := range cols {
		columns = append(columns, ui.Column{Key: c, Header: prettyLabel(c), Sortable: true})
	}
	columns = append(columns, ui.Column{Key: "_actions", Header: "", Align: "end"})

	// viewState is the current list view (search + sort + page) preserved on
	// the per-row Delete RPC so the re-rendered table keeps the same view.
	viewState := url.Values{}
	if search != "" {
		viewState.Set("q", search)
	}
	if sortCol != "" {
		viewState.Set("sort", sortCol)
		viewState.Set("dir", sortDir)
	}
	if page > 1 {
		viewState.Set("p", strconv.Itoa(page))
	}

	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id := cellText(row["id"])
		cells := map[string]render.HTML{}
		for _, c := range cols {
			cells[c] = formatValue(c, ftypes[c], row[c], relLabels, false)
		}
		cells["_actions"] = b.rowActions(ent, id, viewState)
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}

	// Sort links carry the active search so sorting doesn't drop the filter;
	// clicking a header resets to page 1 (no p in the sort pattern).
	carrySearch := url.Values{}
	if search != "" {
		carrySearch.Set("q", search)
	}
	// Sort headers + pagination are plain links (NOT island RPCs) so a click
	// SPA-navigates and re-renders the whole screen — keeping the toolbar Sort
	// summary, the active-search chip, and the table all in one consistent
	// state. (Delete still island-swaps via the signal wrapper around this
	// table, so removing a row doesn't reload the page.)
	cfg := ui.DataTableConfig{
		Columns: columns,
		Rows:    uiRows,
		// No visible caption: the page header already names the collection, and
		// a repeated "PRODUCTS" band just adds noise. Column headers + the H1
		// provide the table's accessible context.
		Responsive:      ui.ResponsiveCards,
		SortBy:          sortCol,
		SortDir:         ui.SortDir(sortDir),
		SortHrefPattern: patternWith(carrySearch, "sort=%s&dir=%s"),
		Empty: ui.EmptyStateConfig{
			Title:        "Nothing here yet",
			Description:  "Create the first " + singular(ent.GetName()) + " with the New button.",
			HeadingLevel: 2,
		},
	}
	if totalPages := int(math.Ceil(float64(total) / float64(limit))); totalPages > 1 {
		// Pagination links carry search + sort so paging preserves both.
		carry := url.Values{}
		if search != "" {
			carry.Set("q", search)
		}
		if sortCol != "" {
			carry.Set("sort", sortCol)
			carry.Set("dir", sortDir)
		}
		cfg.Pagination = &pagination.Config{
			Total:       totalPages,
			Current:     page,
			HrefPattern: patternWith(carry, "p=%d"),
		}
	}
	table := ui.DataTable(cfg)

	// Always-visible result count, so the user knows the data size + page slice
	// even when there's only one page.
	parts := make([]render.HTML, 0, 3)
	if search != "" {
		// Active-search chip: the term + match count, with a real link back to the
		// unfiltered list — so "clear" always returns to the full view and the user
		// always sees what they searched for.
		parts = append(parts, render.Tag("div", map[string]string{"class": "admin-filter-row"},
			render.Tag("span", map[string]string{"class": "admin-filter"},
				render.Text(fmt.Sprintf("%d %s for ", total, plural(total, "result", "results"))),
				render.Tag("strong", nil, render.Text(search)),
				render.Tag("a", map[string]string{"class": "admin-filter__clear", "href": base, "aria-label": "Clear search"}, render.Text("✕")),
			),
		))
	}
	parts = append(parts, table, countFooter(ent, page, limit, total))
	return render.Join(parts...)
}

// countFooter renders the "Showing X–Y of N" line below the table.
func countFooter(ent *entity.Entity, page, limit, total int) render.HTML {
	var text string
	switch {
	case total == 0:
		text = "No " + ent.GetName()
	default:
		start := (page-1)*limit + 1
		end := page * limit
		if end > total {
			end = total
		}
		if total <= limit {
			text = fmt.Sprintf("%d %s", total, plural(total, singular(ent.GetName()), ent.GetName()))
		} else {
			text = fmt.Sprintf("Showing %d–%d of %d", start, end, total)
		}
	}
	return render.Tag("div", map[string]string{"class": "admin-listfoot"},
		render.Tag("span", map[string]string{"class": "admin-count"}, render.Text(text)))
}

// prettyLabel humanises a column name for display: "in_stock" → "In stock",
// "supplier_id" → "Supplier".
func prettyLabel(name string) string {
	s := strings.TrimSuffix(name, "_id")
	s = strings.ReplaceAll(s, "_", " ")
	return titleCase(s)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// sortControl renders the toolbar sort dropdown — a native <details> menu (no
// JS) of the sortable columns. On mobile, where the DataTable collapses to
// cards and the clickable column headers vanish, this is the way to sort.
// Each option SPA-navigates to the list with the chosen sort, preserving the
// active search. Clicking the current sort field flips its direction.
func (b *Battery) sortControl(ent *entity.Entity, q url.Values) render.HTML {
	base := b.entityBase(ent)
	cols := listColumns(ent)
	curSort := q.Get("sort")
	if curSort != "" && !containsStr(cols, curSort) {
		curSort = ""
	}
	curDir := SortDirOf(q.Get("dir"))
	search := strings.TrimSpace(q.Get("q"))

	summary := "Sort"
	if curSort != "" {
		arrow := "↑"
		if curDir == "desc" {
			arrow = "↓"
		}
		summary = "Sort: " + prettyLabel(curSort) + " " + arrow
	}

	opts := make([]render.HTML, 0, len(cols))
	for _, c := range cols {
		dir := "asc"
		label := prettyLabel(c)
		active := c == curSort
		if active {
			// Clicking the active field flips direction; the arrow shows current.
			if curDir == "asc" {
				dir, label = "desc", label+" ↑"
			} else {
				dir, label = "asc", label+" ↓"
			}
		}
		carry := url.Values{}
		if search != "" {
			carry.Set("q", search)
		}
		carry.Set("sort", c)
		carry.Set("dir", dir)
		attrs := map[string]string{"class": "admin-sort__opt", "href": base + "?" + carry.Encode()}
		if active {
			attrs["aria-current"] = "true"
		}
		opts = append(opts, render.Tag("a", attrs, render.Text(label)))
	}

	return render.Tag("details", map[string]string{"class": "admin-sort", "data-fui-disclosure": ""},
		render.Tag("summary", map[string]string{"class": "admin-sort__summary"}, render.Text(summary)),
		render.Tag("div", map[string]string{"class": "admin-sort__menu"}, opts...),
	)
}

// SortDirOf normalizes a direction query value to "asc" (default) or "desc".
func SortDirOf(v string) string {
	if v == "desc" {
		return "desc"
	}
	return "asc"
}

// patternWith builds a query-string fmt pattern that preserves the carry
// params (URL-escaped, so no stray % breaks the fmt verb) and appends tail
// (which holds the fmt verbs the DataTable/pagination fills in).
func patternWith(carry url.Values, tail string) string {
	enc := carry.Encode()
	if enc == "" {
		return "?" + tail
	}
	return "?" + enc + "&" + tail
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// searchField picks the column a quick-search box filters on: a conventional
// label field if present (name/title/label/email/slug), else the first
// text-like, non-hidden, non-id column. Returns "" when nothing is searchable
// (the search box is then omitted).
func searchField(ent *entity.Entity) string {
	byName := map[string]schema.Field{}
	for _, f := range ent.GetFields() {
		byName[f.Name] = f
	}
	for _, pref := range []string{"name", "title", "label", "email", "slug"} {
		if f, ok := byName[pref]; ok && isTextLike(f) {
			return pref
		}
	}
	for _, f := range ent.GetFields() {
		if f.Hidden || f.Name == "id" || isTimestampCol(f.Name) {
			continue
		}
		if isTextLike(f) {
			return f.Name
		}
	}
	return ""
}

func isTextLike(f schema.Field) bool {
	return f.Type == schema.String || f.Type == schema.Text
}

// fieldTypeMap maps a column name to its schema type for cell formatting.
func fieldTypeMap(ent *entity.Entity) map[string]schema.FieldType {
	m := make(map[string]schema.FieldType, len(ent.GetFields())+1)
	for _, f := range ent.GetFields() {
		m[f.Name] = f.Type
	}
	return m
}

// relationLabelMaps loads, per BelongsTo FK column, an id→label map so list
// rows and detail screens show the related record's name instead of a raw FK
// uuid. Loaded owner/tenant-scoped (you only see records you can see).
func (b *Battery) relationLabelMaps(ctx context.Context, ent *entity.Entity) map[string]map[string]string {
	rels := relationFields(ent)
	if len(rels) == 0 || b.registry == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(rels))
	for fkField, targetName := range rels {
		target, err := b.registry.Get(targetName)
		if err != nil {
			continue
		}
		q := url.Values{}
		q.Set("page", "1")
		q.Set("limit", strconv.Itoa(relationOptionLimit))
		rows, _, err := b.listRows(ctx, target, q.Encode())
		if err != nil {
			continue
		}
		labelField := searchField(target)
		m := make(map[string]string, len(rows))
		for _, row := range rows {
			id := cellText(row["id"])
			if labelField != "" {
				if lv := cellText(row[labelField]); lv != "" {
					m[id] = lv
				}
			}
		}
		out[fkField] = m
	}
	return out
}

// formatValue renders a single field value with type-aware presentation: ids
// as quiet monospace, booleans as a glanceable pill, BelongsTo FKs as the
// related record's label, and long free text truncated (list only). detail=true
// shows full values (no truncation) for the show screen.
func formatValue(col string, ft schema.FieldType, raw any, relLabels map[string]map[string]string, detail bool) render.HTML {
	val := cellText(raw)

	if col == "id" || ft == schema.UUID {
		if val == "" {
			return render.Tag("span", map[string]string{"class": "admin-muted"}, render.Text("—"))
		}
		return render.Tag("span", map[string]string{"class": "admin-id", "title": val}, render.Text(val))
	}
	if labels, ok := relLabels[col]; ok {
		if val == "" {
			return render.Tag("span", map[string]string{"class": "admin-muted"}, render.Text("—"))
		}
		if label, ok := labels[val]; ok {
			return render.Text(label)
		}
		// Referenced row not visible/loaded — show the id, quietly.
		return render.Tag("span", map[string]string{"class": "admin-id", "title": val}, render.Text(val))
	}
	if ft == schema.Bool {
		on := truthy(val)
		text := "No"
		if on {
			text = "Yes"
		}
		attrs := map[string]string{"class": "admin-bool"}
		if on {
			attrs["data-on"] = "true"
		}
		return render.Tag("span", attrs, render.Text(text))
	}
	if val == "" {
		return render.Tag("span", map[string]string{"class": "admin-muted"}, render.Text("—"))
	}

	switch ft {
	case schema.Image:
		// Thumbnail in the list, larger preview in detail. src is the stored
		// URL/path; alt is the column for a non-empty accessible name.
		cls := "admin-thumb"
		if detail {
			cls += " admin-thumb--lg"
		}
		return render.VoidTag("img", map[string]string{"class": cls, "src": val, "alt": col, "loading": "lazy"})
	case schema.File:
		name := val
		if i := strings.LastIndexAny(val, "/\\"); i >= 0 && i < len(val)-1 {
			name = val[i+1:]
		}
		return render.Tag("a", map[string]string{"class": "admin-file", "href": val, "download": "", "rel": "noopener"}, render.Text(name))
	case schema.JSON:
		if detail {
			return render.Tag("pre", map[string]string{"class": "admin-json"},
				render.Tag("code", nil, render.Text(prettyJSON(val))))
		}
		return render.Tag("span", map[string]string{"class": "admin-truncate admin-mono"}, render.Text(val))
	case schema.Text:
		// Long text: preserve line breaks in detail; truncate to one line in the list.
		if detail {
			return render.Tag("div", map[string]string{"class": "admin-prose"}, render.Text(val))
		}
		return render.Tag("span", map[string]string{"class": "admin-truncate", "title": val}, render.Text(val))
	case schema.Date:
		// Date columns round-trip through the DB as datetimes; show only the date.
		if i := strings.IndexByte(val, 'T'); i > 0 {
			val = val[:i]
		}
		return render.Tag("span", map[string]string{"class": "admin-mono"}, render.Text(val))
	case schema.Timestamp:
		// "2026-01-15T09:30:00Z" → "2026-01-15 09:30" — drop seconds + zone noise.
		v := strings.Replace(val, "T", " ", 1)
		if i := strings.IndexAny(v, ".Z+"); i > 11 {
			v = v[:i]
		}
		if len(v) >= 16 {
			v = v[:16]
		}
		return render.Tag("span", map[string]string{"class": "admin-mono"}, render.Text(v))
	case schema.String:
		if !detail {
			return render.Tag("span", map[string]string{"class": "admin-truncate", "title": val}, render.Text(val))
		}
	}
	return render.Text(val)
}

// prettyJSON indents a JSON string for the detail view; returns the input
// unchanged if it isn't valid JSON.
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// relationFields maps each BelongsTo FK column → target entity name. Both
// explicitly declared relations and those auto-derived from a relation-typed
// field live in Config.Relations, so this covers both.
func relationFields(ent *entity.Entity) map[string]string {
	out := map[string]string{}
	for _, r := range ent.Config.Relations {
		if r.Type == entity.RelManyToOne && r.ForeignKey != "" {
			out[r.ForeignKey] = r.Entity
		}
	}
	return out
}

// relationOptions loads, for every BelongsTo FK column on ent, the related
// records to offer as <select> options — fetched through the target entity's
// CrudHandler so the same owner/tenant scope applies (you can only link to
// records you can see). The current value (selected) is pre-marked. A target
// that isn't registered or fails to load is skipped, so its field falls back
// to a plain input rather than breaking the form.
func (b *Battery) relationOptions(ctx context.Context, ent *entity.Entity, selected map[string]string) map[string][]ui.SelectOption {
	rels := relationFields(ent)
	if len(rels) == 0 || b.registry == nil {
		return nil
	}
	out := make(map[string][]ui.SelectOption, len(rels))
	for fkField, targetName := range rels {
		target, err := b.registry.Get(targetName)
		if err != nil {
			continue
		}
		q := url.Values{}
		q.Set("page", "1")
		q.Set("limit", strconv.Itoa(relationOptionLimit))
		rows, _, err := b.listRows(ctx, target, q.Encode())
		if err != nil {
			continue
		}
		labelField := searchField(target)
		cur := selected[fkField]
		opts := make([]ui.SelectOption, 0, len(rows))
		for _, row := range rows {
			rid := cellText(row["id"])
			label := rid
			if labelField != "" {
				if lv := cellText(row[labelField]); lv != "" {
					label = lv
				}
			}
			opts = append(opts, ui.SelectOption{Value: rid, Text: label, Selected: rid == cur})
		}
		out[fkField] = opts
	}
	return out
}

// relationOptionLimit caps how many related records a picker lists. Matches the
// CrudHandler's max page size; entities with more than this need a searchable
// picker (future work) rather than a plain <select>.
const relationOptionLimit = 100

// rowActions renders the per-row Edit link + Delete confirm button. Delete is a
// data-fui-confirm + data-fui-rpc DELETE bound to the list's island signal: the
// handler returns the refreshed table fragment, which the runtime swaps in
// place. (Navigating back to the same list path would hit the SPA cache and
// show stale rows — the signal swap is the correct island update.) No JS.
func (b *Battery) rowActions(ent *entity.Entity, id string, viewState url.Values) render.HTML {
	base := b.entityBase(ent)
	rpc := base + "/_delete/" + url.PathEscape(id)
	if enc := viewState.Encode(); enc != "" {
		rpc += "?" + enc // re-render the same view (search/sort/page) after delete
	}
	view := ui.Link(ui.LinkConfig{Href: base + "/view/" + url.PathEscape(id), Text: "View", Variant: ui.LinkAction})
	edit := ui.Link(ui.LinkConfig{Href: base + "/edit/" + url.PathEscape(id), Text: "Edit", Variant: ui.LinkAction})
	del := ui.Button(ui.ButtonConfig{
		Label:   "Delete",
		Variant: ui.ButtonDanger,
		Size:    ui.ButtonSizeSmall,
		ExtraAttrs: html.Attrs{
			"data-fui-confirm":    "Delete this " + singular(ent.GetName()) + "?",
			"data-fui-rpc":        rpc,
			"data-fui-rpc-method": "DELETE",
			"data-fui-rpc-signal": signalName(ent),
		},
	})
	return render.Tag("div", map[string]string{"class": "admin-row-actions"}, view, edit, del)
}

// ----- form screen ----------------------------------------------------------

type entityFormScreen struct {
	component.ContextOnly
	b    *Battery
	ent  *entity.Entity
	edit bool

	// per-request state (shallow-copied template → safe to mutate in Load)
	id        string
	values    map[string]string
	fieldErrs map[string]string
	general   string
	loadErr   bool
	// relOpts holds, per BelongsTo FK column, the related records to offer as
	// <select> options (loaded owner/tenant-scoped in Load).
	relOpts map[string][]ui.SelectOption
}

func (s *entityFormScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *entityFormScreen) Load(ctx context.Context) error {
	q := appui.QueryFromContext(ctx)
	values := map[string]string{}

	if s.edit {
		row, err := s.b.getRow(ctx, s.ent, s.id)
		if err != nil {
			s.loadErr = true
			return nil
		}
		for _, f := range editableFields(s.ent) {
			values[f.Name] = cellText(row[f.Name])
		}
	}

	// A failed submit redirected here with a one-shot flash token: overlay the
	// submitted values + field errors so the user keeps their input + context.
	if fl := s.b.flash.pop(q.Get("e")); fl != nil {
		for k, v := range fl.values {
			values[k] = v
		}
		s.fieldErrs = fl.fieldErrs
		s.general = fl.general
	}
	s.values = values
	// Load relationship-picker options (BelongsTo FK columns) scoped to the
	// caller, marking the current value selected.
	s.relOpts = s.b.relationOptions(ctx, s.ent, values)
	return nil
}

func (s *entityFormScreen) RenderCtx(ctx context.Context) render.HTML {
	base := s.b.entityBase(s.ent)
	if s.loadErr {
		return s.b.shell(ui.Container(ui.ContainerConfig{Class: "admin-entity"},
			ui.PageHeader(ui.PageHeaderConfig{Title: "Not found"}),
			ui.EmptyState(ui.EmptyStateConfig{
				Title:        "Record not found",
				Description:  "It may have been deleted, or you may not have access.",
				Action:       ui.Link(ui.LinkConfig{Href: base, Text: "Back to list", Variant: ui.LinkAction}),
				HeadingLevel: 2,
			}),
		))
	}

	action := base + "/_create"
	title := "New " + singular(s.ent.GetName())
	if s.edit {
		action = base + "/_update/" + url.PathEscape(s.id)
		title = "Edit " + singular(s.ent.GetName())
	}

	errs := ui.FieldErrors(s.fieldErrs)
	fields := make([]render.HTML, 0, len(editableFields(s.ent)))
	for _, f := range editableFields(s.ent) {
		fields = append(fields, s.field(f, errs))
	}

	form := ui.Form(ui.FormConfig{
		Action:      action,
		Method:      "POST",
		Ctx:         ctx, // auto-stamps the hidden _csrf input
		Errors:      errs,
		Summary:     s.general,
		SubmitLabel: "Save",
	}, fields...)

	return s.b.shell(ui.Container(ui.ContainerConfig{Class: "admin-entity"},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:   title,
			Actions: ui.Link(ui.LinkConfig{Href: base, Text: "Cancel", Variant: ui.LinkMuted}),
		}),
		form,
	))
}

// field renders the right control for a schema field, wired for error display.
func (s *entityFormScreen) field(f schema.Field, errs ui.FieldErrors) render.HTML {
	val := s.values[f.Name]
	id := "f_" + f.Name

	// BelongsTo FK columns render as a relationship picker (<select> of related
	// records) instead of a raw FK text box. Options were loaded in Load.
	if opts, ok := s.relOpts[f.Name]; ok {
		ph := ""
		if !f.Required {
			ph = "— none —"
		}
		return ui.Select(ui.SelectConfig{
			Name: f.Name, Label: prettyLabel(f.Name), ID: id, Options: opts,
			Placeholder: ph, Required: f.Required, Error: errs[f.Name],
		})
	}

	switch f.Type {
	case schema.Bool:
		return ui.Checkbox(ui.ToggleConfig{
			Name: f.Name, Label: prettyLabel(f.Name), ID: id, Value: "on",
			Checked: truthy(val), Error: errs[f.Name],
		})
	case schema.Enum:
		opts := make([]ui.SelectOption, 0, len(f.Values))
		for _, o := range f.Values {
			opts = append(opts, ui.SelectOption{Value: o, Text: o, Selected: val == o})
		}
		ph := ""
		if !f.Required {
			ph = "—"
		}
		return ui.Select(ui.SelectConfig{
			Name: f.Name, Label: prettyLabel(f.Name), ID: id, Options: opts,
			Placeholder: ph, Required: f.Required, Error: errs[f.Name],
		})
	case schema.Text, schema.JSON:
		return ui.TextArea(ui.TextAreaConfig{
			Name: f.Name, Label: prettyLabel(f.Name), ID: id, Value: val, Rows: 4,
			Required: f.Required, Error: errs[f.Name],
		})
	default:
		return ui.FormFieldFor(errs, f.Name, ui.FormFieldConfig{
			Label: prettyLabel(f.Name), For: id, Required: f.Required,
			Input: html.Input(html.InputConfig{
				Type: inputType(f.Type), Name: f.Name, ID: id, Value: val,
			}),
		})
	}
}

// ----- detail (read-only show) screen ---------------------------------------

type entityDetailScreen struct {
	component.ContextOnly
	b   *Battery
	ent *entity.Entity

	id        string
	row       map[string]any
	relLabels map[string]map[string]string
	loadErr   bool
}

func (s *entityDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *entityDetailScreen) Load(ctx context.Context) error {
	// getRow is owner/tenant-scoped via ctx, so a record the caller can't see
	// surfaces as not-found rather than leaking.
	row, err := s.b.getRow(ctx, s.ent, s.id)
	if err != nil {
		s.loadErr = true
		return nil
	}
	s.row = row
	s.relLabels = s.b.relationLabelMaps(ctx, s.ent)
	return nil
}

func (s *entityDetailScreen) RenderCtx(_ context.Context) render.HTML {
	base := s.b.entityBase(s.ent)
	if s.loadErr || s.row == nil {
		return s.b.shell(ui.Container(ui.ContainerConfig{Class: "admin-entity"},
			ui.PageHeader(ui.PageHeaderConfig{Title: "Not found"}),
			ui.EmptyState(ui.EmptyStateConfig{
				Title:        "Record not found",
				Description:  "It may have been deleted, or you may not have access.",
				Action:       ui.Link(ui.LinkConfig{Href: base, Text: "Back to list", Variant: ui.LinkAction}),
				HeadingLevel: 2,
			}),
		))
	}

	// A definition list of every readable column (id first, timestamps last
	// because GetFields keeps them in declaration order). Read-only: no form.
	// Same type-aware formatting as the list (ids mono, booleans as pills,
	// relations as their label).
	items := make([]render.HTML, 0, 2*(len(s.ent.GetFields())+1))
	items = append(items, detailRow("id", formatValue("id", schema.UUID, s.row["id"], s.relLabels, true)))
	for _, f := range s.ent.GetFields() {
		if f.Hidden || f.Name == "id" {
			continue
		}
		items = append(items, detailRow(prettyLabel(f.Name), formatValue(f.Name, f.Type, s.row[f.Name], s.relLabels, true)))
	}

	actions := render.Tag("div", map[string]string{"class": "admin-actions"},
		ui.Link(ui.LinkConfig{Href: base, Text: "← Back", Variant: ui.LinkMuted}),
		ui.Link(ui.LinkConfig{Href: base + "/edit/" + url.PathEscape(s.id), Text: "Edit", Variant: ui.LinkAction}),
	)
	return s.b.shell(ui.Container(ui.ContainerConfig{Class: "admin-entity"},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:   singular(s.ent.GetName()) + " detail",
			Actions: actions,
		}),
		render.Tag("dl", map[string]string{"class": "admin-detail"}, items...),
	))
}

// detailRow renders a <dt>/<dd> pair from a pre-rendered (escaped) value.
func detailRow(label string, value render.HTML) render.HTML {
	return render.Join(
		render.Tag("dt", map[string]string{"class": "admin-detail__label"}, render.Text(label)),
		render.Tag("dd", map[string]string{"class": "admin-detail__value"}, value),
	)
}

// ----- helpers --------------------------------------------------------------

func inputType(t schema.FieldType) string {
	switch t {
	case schema.Int, schema.Float, schema.Decimal:
		return "number"
	case schema.Date:
		return "date"
	default: // String, UUID, Timestamp, Image, File, Relation
		return "text"
	}
}

func truthy(s string) bool { return s == "on" || s == "true" || s == "1" }

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// cellText formats a JSON-decoded value for display / input population.
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// listQueryString reflects the active view (search + sort + page) for
// X-Gofastr-Push-State, so refresh / share / back reproduce the same list.
func listQueryString(q url.Values) string {
	out := url.Values{}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		out.Set("q", v)
	}
	if v := q.Get("sort"); v != "" {
		out.Set("sort", v)
		out.Set("dir", SortDirOf(q.Get("dir")))
	}
	if p := q.Get("p"); p != "" && p != "1" {
		out.Set("p", p)
	}
	if enc := out.Encode(); enc != "" {
		return "?" + enc
	}
	return ""
}
