// Package resource renders CRUD-backed list, detail, and form screens from Config.
package resource

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// DataSource is the CRUD read seam Config needs. *crud.CrudHandler satisfies it.
type DataSource interface {
	CountAll(context.Context, crud.ListOptions) (int, error)
	ListAll(context.Context, crud.ListOptions) ([]map[string]any, error)
	GetOne(context.Context, string, []string) (map[string]any, error)
}

// Field is one displayed entity field.
type Field struct {
	Key     string
	Label   string
	Type    string   // string,text,int,float,decimal,bool,enum,date,timestamp,uuid,relation,...
	Values  []string // enum: the allowed values (drives <option>s on the form)
	NoQuery bool     // shown in the grid, but the API refuses to filter or sort on it
}

// Relation resolves a foreign-key column to a related record's label.
type Relation struct {
	Crud    DataSource
	Display string
}

// Filter is one facet-filter dimension on the list screen: a column the
// user can narrow the list by. Type is "enum", "bool", or "relation" — it
// selects both the facet control (pills vs select) and how options are
// sourced (Values for enums, yes/no for bools, related rows for relations).
type Filter struct {
	Key    string
	Label  string
	Type   string   // "enum" | "bool" | "relation"
	Values []string // enum: the allowed values
}

// Transition is a status-change workflow action shown on a detail page — a
// button that PUTs {status: Status} to the entity, then refreshes (Mark paid).
type Transition struct {
	Label   string
	Status  string
	Variant string // "primary" | "secondary" | "danger" | "ghost" (default secondary)
	Stamp   string // optional date field stamped with today on transition
}

// RelatedList is a reverse relation surfaced on a detail page: the records of
// another entity that point back at this one via ForeignKey. Turns a detail
// page from a row editor into an account view (a customer + their invoices).
type RelatedList struct {
	Title      string // e.g. "Invoices"
	ForeignKey string // the FK column on the related entity, e.g. "customer_id"
	BasePath   string // the related entity's app route, e.g. "/app/invoices"
	Crud       DataSource
	Fields     []Field
	Relations  map[string]Relation // for resolving the related rows' own FKs
}

// Config drives the server-rendered list + detail + form screens for
// one entity.
type Config struct {
	Entity      string
	Title       string
	Singular    string
	BasePath    string // app route, e.g. "/app/customers"
	APIPath     string // auto-CRUD JSON endpoint, e.g. "/api/customers"
	Crud        DataSource
	Fields      []Field
	Search      string
	Filters     []Filter // facet filters rendered as a toolbar above the table
	PageSize    int
	Relations   map[string]Relation
	CanCreate   bool          // List shows "New"; a /new create form is mounted
	CanEdit     bool          // Detail shows Edit + Delete; a /{id}/edit form is mounted
	Heading     string        // overrides the list's title (the block's text:)
	EmptyText   string        // overrides the empty-state description (the block's empty_text:)
	Related     []RelatedList // reverse relations surfaced on the detail page
	Transitions []Transition  // status-transition workflow buttons on the detail page

	// ExtraActions are appended to the list page header's action cluster
	// (e.g. a data-fui-open trigger for a quick-add modal).
	ExtraActions []render.HTML

	// IslandPath, when set, renders the list's DataTable in island mode:
	// sort headers and pagination fire GET RPCs at this endpoint and the
	// runtime swaps just the table — no document navigation. Mount
	// TableHandler at the same path.
	IslandPath string

	// IslandPolicy gates the island endpoint. It MUST be the same policy
	// that gates the screen showing this list: the island serves the same
	// rows over a route the screen's policy never sees, so leaving it nil
	// on a role-gated screen publishes that screen's data to every signed-in
	// user. TableHandler enforces it before rendering.
	IslandPolicy appui.Policy
}

// WithIslandPolicy gates the island endpoint with the screen's own policy.
func (c Config) WithIslandPolicy(p appui.Policy) Config {
	c.IslandPolicy = p
	return c
}

// PublicIsland is the policy for a list on a screen anyone may view. It has
// to be stated rather than left nil: with no policy at all TableHandler
// requires sign-in, so an anonymous visitor's first sort click on a public
// list would come back 401.
func PublicIsland() appui.Policy {
	return appui.PolicyFunc(func(context.Context) appui.Decision {
		return appui.Decision{Kind: appui.DecisionAllow}
	})
}

// WithTransitions sets the detail-page status-transition workflow buttons.
func (c Config) WithTransitions(ts ...Transition) Config {
	c.Transitions = ts
	return c
}

func (c Config) pageSize() int {
	if c.PageSize > 0 {
		return c.PageSize
	}
	return 25
}

// sortable reports whether a column may be used in ORDER BY. Displayed and
// sortable are not the same set: a NoQuery column shows its (masked) value
// but the API rejects sorting on it.
func (c Config) sortable(k string) bool {
	for _, f := range c.Fields {
		if f.Key == k {
			return !f.NoQuery
		}
	}
	return false
}

func (c Config) hasField(k string) bool {
	for _, f := range c.Fields {
		if f.Key == k {
			return true
		}
	}
	return false
}

func (c Config) field(k string) (Field, bool) {
	for _, f := range c.Fields {
		if f.Key == k {
			return f, true
		}
	}
	return Field{}, false
}

// WithColumns returns a copy showing only the named fields, in the given order.
func (c Config) WithColumns(keys ...string) Config {
	fields := make([]Field, 0, len(keys))
	for _, k := range keys {
		if f, ok := c.field(k); ok {
			fields = append(fields, f)
		}
	}
	c.Fields = fields
	return c
}

// WithSearch sets the LIKE-search field. WithLimit sets the page size.
// WithCreate shows a "New" action linking to BasePath/new.
func (c Config) WithSearch(field string) Config { c.Search = field; return c }
func (c Config) WithLimit(n int) Config         { c.PageSize = n; return c }
func (c Config) WithCreate() Config             { c.CanCreate = true; return c }

// WithFilters sets the facet filters shown in the toolbar above the list.
func (c Config) WithFilters(fs ...Filter) Config { c.Filters = fs; return c }

// WithEdit shows Edit + Delete on the detail screen (a /{id}/edit form is mounted).
func (c Config) WithEdit() Config { c.CanEdit = true; return c }

// WithHeading overrides the list's title; WithEmpty overrides the empty-state text.
func (c Config) WithHeading(s string) Config { c.Heading = s; return c }
func (c Config) WithEmpty(s string) Config   { c.EmptyText = s; return c }

// WithActions appends extra page-header actions to the list screen.
func (c Config) WithActions(a ...render.HTML) Config {
	c.ExtraActions = append(c.ExtraActions, a...)
	return c
}

// WithIsland turns the list's table into an island: sort + pagination RPC
// against endpoint instead of navigating. Register TableHandler there.
func (c Config) WithIsland(endpoint string) Config {
	c.IslandPath = endpoint
	return c
}

// islandSignal names the client signal binding the island wrapper to its
// RPC responses — one per resource, derived from the base path.
func (c Config) islandSignal() string {
	seg := c.BasePath
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	return "table-" + seg
}

func (c Config) relationLabels(ctx context.Context) map[string]map[string]string {
	out := map[string]map[string]string{}
	for col, rel := range c.Relations {
		if rel.Crud == nil {
			continue
		}
		// WithReadHooks: the DISPLAY value becomes the visible label on the
		// grid and detail page, so it must show the mask. Only the id is used
		// for lookup, and a picker submits the id — so redacting the label
		// cannot write a masked value back.
		rows, err := rel.Crud.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{Limit: 1000})
		if err != nil {
			continue
		}
		m := map[string]string{}
		for _, r := range rows {
			id := cell(rowValue(r, "id"))
			if id == "" {
				continue
			}
			label := cell(rowValue(r, rel.Display))
			if label == "" {
				label = id
			}
			m[id] = label
		}
		out[col] = m
	}
	return out
}

// queryFilters builds the ParsedFilters for the current query: the LIKE
// search plus one equality per active facet. Applied to both the count and
// the page query, so a filtered result set paginates correctly.
func (c Config) queryFilters(q url.Values, search string) []filter.ParsedFilter {
	var filters []filter.ParsedFilter
	if search != "" && c.Search != "" {
		filters = append(filters, filter.ParsedFilter{Field: c.Search, Op: filter.OpLike, Value: search})
	}
	for _, ff := range c.Filters {
		if v := strings.TrimSpace(q.Get(ff.Key)); v != "" {
			filters = append(filters, filter.ParsedFilter{Field: ff.Key, Op: filter.OpEq, Value: v})
		}
	}
	return filters
}

// List renders the entity list screen.
func (c Config) List(ctx context.Context) render.HTML {
	q := appui.QueryFromContext(ctx)
	search := strings.TrimSpace(q.Get("q"))
	total, _ := c.Crud.CountAll(ctx, crud.ListOptions{Filters: c.queryFilters(q, search)})

	actionList := append([]render.HTML{}, c.ExtraActions...)
	if c.CanCreate {
		actionList = append(actionList, ui.LinkButton(ui.LinkButtonConfig{Label: "New " + c.Singular, Href: c.BasePath + "/new", Variant: ui.ButtonPrimary}))
	}
	var actions render.HTML
	switch len(actionList) {
	case 0:
	case 1:
		actions = actionList[0]
	default:
		actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, actionList...)
	}
	title := c.Title
	if c.Heading != "" {
		title = c.Heading
	}
	body := []render.HTML{ui.PageHeader(ui.PageHeaderConfig{Title: title, Subtitle: countLabel(total, c.Singular, c.Title), Actions: actions})}
	// When facets are configured, search folds into the one filter toolbar
	// (rendered below, once relation options are resolved) so the screen is a
	// single GET form. Otherwise keep the standalone search box unchanged.
	if len(c.Filters) == 0 && c.Search != "" {
		body = append(body, ui.SearchInput(ui.SearchInputConfig{
			Name: "q", ID: "search-" + c.Title, Action: c.BasePath, Method: "GET",
			Placeholder: "Search " + c.Title, ExtraAttrs: map[string]string{"value": search},
		}))
	}
	if len(c.Filters) > 0 {
		if tb := c.filterToolbar(q, search, c.relationLabels(ctx)); tb != "" {
			body = append(body, tb)
		}
	}
	table := c.table(ctx, total)
	if c.IslandPath != "" {
		// The island wrapper: sort/page RPC responses (the same Table HTML,
		// served by TableHandler) replace this element's innerHTML.
		table = render.Tag("div", map[string]string{
			"data-fui-signal":      c.islandSignal(),
			"data-fui-signal-mode": "html",
		}, table)
	}
	body = append(body, table)
	return render.Join(body...)
}

// Table renders the list's DataTable for ctx's query state. It is shared
// by List (initial SSR) and TableHandler (island RPC responses), so a
// sort/page swap returns exactly the HTML the initial render painted.
func (c Config) Table(ctx context.Context) render.HTML {
	return c.table(ctx, -1)
}

func (c Config) table(ctx context.Context, total int) render.HTML {
	q := appui.QueryFromContext(ctx)
	page := 1
	if n, err := strconv.Atoi(q.Get("p")); err == nil && n > 1 {
		page = n
	}
	limit := c.pageSize()
	search := strings.TrimSpace(q.Get("q"))
	filters := c.queryFilters(q, search)
	var sorts []filter.ParsedSort
	sortCol := q.Get("sort")
	// sortable(), not hasField(): a NoQuery column is displayed but the API
	// refuses ORDER BY on it, and this ParsedSort goes straight to ListAll
	// without passing through ParseSortValues. Rendering the header
	// unsortable stops the app emitting the link; this stops a hand-typed
	// or bookmarked ?sort= from reaching the query.
	if sortCol != "" && c.sortable(sortCol) {
		sorts = append(sorts, filter.ParsedSort{Field: sortCol, Desc: q.Get("dir") == "desc"})
	}

	if total < 0 {
		total, _ = c.Crud.CountAll(ctx, crud.ListOptions{Filters: filters})
	}
	// WithReadHooks: these rows are rendered to an end user.
	rows, err := c.Crud.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{Filters: filters, Sorts: sorts, Limit: limit, Offset: (page - 1) * limit})
	if err != nil {
		return ui.Callout(ui.CalloutConfig{Title: "Couldn't load " + c.Title, Variant: ui.StatusDanger}, render.Text("See server logs."))
	}

	rel := c.relationLabels(ctx)
	cols := make([]ui.Column, 0, len(c.Fields)+1)
	for _, f := range c.Fields {
		// A NoQuery column still shows its value, but ?sort= on it is a 400
		// that blanks the grid — so it renders unsortable.
		col := ui.Column{Key: f.Key, Header: f.Label, Sortable: !f.NoQuery}
		if numeric(f.Type) {
			col.Align = "end"
		}
		cols = append(cols, col)
	}
	cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end"})

	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id := cell(rowValue(row, "id"))
		cells := map[string]render.HTML{}
		for _, f := range c.Fields {
			cells[f.Key] = format(f, rowValue(row, f.Key), rel)
		}
		cells["_a"] = ui.Link(ui.LinkConfig{Href: c.BasePath + "/" + id, Text: "View", Variant: ui.LinkAction})
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}

	// carry preserves search + active facets across sort-header and pagination
	// links (which are <a> navigations, not the toolbar form) so those actions
	// never silently drop the current filter set.
	var carry strings.Builder
	if search != "" {
		carry.WriteString("q=" + url.QueryEscape(search) + "&")
	}
	for _, ff := range c.Filters {
		if v := strings.TrimSpace(q.Get(ff.Key)); v != "" {
			carry.WriteString(url.QueryEscape(ff.Key) + "=" + url.QueryEscape(v) + "&")
		}
	}
	dt := ui.DataTableConfig{
		Columns: cols, Rows: uiRows, Responsive: ui.ResponsiveCards,
		SortBy: sortCol, SortDir: ui.SortDir(q.Get("dir")),
		SortHrefPattern: "?" + carry.String() + "sort=%s&dir=%s",
		Empty:           ui.EmptyStateConfig{Title: "No " + c.Title + " yet", Description: emptyDescription(c.EmptyText), HeadingLevel: 2},
	}
	if c.IslandPath != "" {
		// Island mode: sort headers become data-fui-rpc buttons and the
		// pagination inherits the same signal/endpoint pair automatically.
		dt.IslandSignal = c.islandSignal()
		dt.IslandEndpoint = c.IslandPath
	}
	if pages := int(math.Ceil(float64(total) / float64(limit))); pages > 1 {
		dt.Pagination = &pagination.Config{Total: pages, Current: page, HrefPattern: "?" + carry.String() + "p=%d"}
	}
	return ui.DataTable(dt)
}

// TableHandler serves the island endpoint: it renders the same table HTML
// List paints, for the RPC's query string. The runtime writes the response
// into the island's data-fui-signal wrapper — no document navigation.
//
// It is a SECOND route onto the rows the screen shows, so it repeats every
// gate the screen and the JSON API apply: sign-in, the screen's own policy
// (IslandPolicy), and the entity's declared read permission. A gate that
// lives only on the screen route is not a gate.
func (c Config) TableHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The screen's policy decides on its own when there is one: it
		// knows whether the screen is public, sign-in-only, or role-gated.
		// With no policy configured we fall back to requiring sign-in,
		// which is what this handler has always done.
		if c.IslandPolicy == nil {
			if u, ok := handler.GetUser(r.Context()); !ok || u == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			switch d := c.IslandPolicy.Decide(r.Context()); d.Kind {
			case appui.DecisionAllow:
				// proceed
			case appui.DecisionBlock:
				status, msg := d.Status, d.Message
				if status == 0 {
					status = http.StatusForbidden
				}
				if msg == "" {
					msg = http.StatusText(status)
				}
				http.Error(w, msg, status)
				return
			default:
				// Redirect/RenderAlt are document-navigation outcomes with
				// no meaning for a fragment RPC. Refuse rather than serve
				// the rows the policy declined to show.
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
		}
		// The entity's RBAC — the same permission the JSON list route
		// enforces. Without this the island answers 200 for rows
		// GET /api/<entity> answers 403 for.
		if g, ok := c.Crud.(interface {
			CanRead(context.Context) bool
		}); ok && !g.CanRead(r.Context()) {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, string(c.Table(appui.WithRequest(r.Context(), r))))
	}
}

// filterToolbar builds the facet + search toolbar shown above the list. Enum
// facets render as pills when they hold a few short choices and as a select
// otherwise; bools render as Yes/No pills; relations render as a select whose
// options are the related records' display labels. Returns nil when there is
// nothing to render (e.g. only an empty relation facet and no search).
func (c Config) filterToolbar(q url.Values, search string, rel map[string]map[string]string) render.HTML {
	facets := make([]ui.Facet, 0, len(c.Filters))
	for _, ff := range c.Filters {
		facet := ui.Facet{Name: ff.Key, Label: ff.Label, Value: q.Get(ff.Key)}
		switch ff.Type {
		case "bool", "boolean":
			facet.Options = []ui.FacetOption{{Label: "Yes", Value: "true"}, {Label: "No", Value: "false"}}
			facet.Kind = ui.FacetPills
		case "relation":
			facet.Options = relationFacetOptions(rel[ff.Key])
			facet.Kind = ui.FacetSelect
		default: // enum
			short := len(ff.Values) > 0 && len(ff.Values) <= 4
			opts := make([]ui.FacetOption, 0, len(ff.Values))
			for _, v := range ff.Values {
				label := title(v)
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
	cfg := ui.FilterToolbarConfig{Action: c.BasePath, Facets: facets}
	if c.Search != "" {
		cfg.Search = &ui.FilterSearch{Name: "q", Value: search, Placeholder: "Search " + c.Title, Label: "Search " + c.Title}
	}
	if len(cfg.Facets) == 0 && cfg.Search == nil {
		return ""
	}
	return ui.FilterToolbar(cfg)
}

// relationFacetOptions turns a relation's id→label map into select options,
// ordered by label for a stable, glanceable dropdown.
func relationFacetOptions(m map[string]string) []ui.FacetOption {
	opts := make([]ui.FacetOption, 0, len(m))
	for id, label := range m {
		opts = append(opts, ui.FacetOption{Label: label, Value: id})
	}
	sort.Slice(opts, func(i, j int) bool {
		if opts[i].Label == opts[j].Label {
			return opts[i].Value < opts[j].Value
		}
		return opts[i].Label < opts[j].Label
	})
	return opts
}

// Detail renders the single-record detail screen.
func (c Config) Detail(ctx context.Context, id string) render.HTML {
	// WithReadHooks: this row is rendered to an end user, so it must show
	// whatever an AfterGet redaction shows, not the stored value.
	row, err := c.Crud.GetOne(crud.WithReadHooks(ctx), id, nil)
	if err != nil || row == nil {
		return ui.EmptyState(ui.EmptyStateConfig{Title: "Not found", Description: "This " + c.Singular + " does not exist.", HeadingLevel: 1})
	}
	rel := c.relationLabels(ctx)
	title := cell(rowValue(row, "name"))
	if title == "" {
		title = cell(rowValue(row, "title"))
	}
	if title == "" {
		title = c.Singular
	}
	items := make([]ui.DetailItem, 0, len(c.Fields))
	for _, f := range c.Fields {
		items = append(items, ui.DetailItem{Label: f.Label, Value: format(f, rowValue(row, f.Key), rel)})
	}
	actions := []render.HTML{}
	for _, t := range c.Transitions {
		body := "{\"status\":\"" + t.Status + "\""
		if t.Stamp != "" {
			body += ",\"" + t.Stamp + "\":\"" + today() + "\""
		}
		body += "}"
		actions = append(actions, ui.Button(ui.ButtonConfig{Label: t.Label, Variant: buttonVariant(t.Variant), ExtraAttrs: interactive.Put(c.APIPath + "/" + id).
			WithBody(body).
			OnSuccess(interactive.Navigate(c.BasePath + "/" + id)).Attrs()}))
	}
	if c.CanEdit {
		actions = append(actions,
			ui.LinkButton(ui.LinkButtonConfig{Label: "Edit", Href: c.BasePath + "/" + id + "/edit", Variant: ui.ButtonSecondary}),
			ui.Button(ui.ButtonConfig{Label: "Delete", Variant: ui.ButtonDanger, ExtraAttrs: interactive.Delete(c.APIPath + "/" + id).
				WithConfirm("Delete this " + c.Singular + "? This cannot be undone.").
				OnSuccess(interactive.Navigate(c.BasePath)).Attrs()}),
		)
	}
	actions = append(actions, ui.Link(ui.LinkConfig{Href: c.BasePath, Text: "← Back", Variant: ui.LinkMuted}))
	body := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{Title: title, Actions: ui.Cluster(ui.ClusterConfig{}, actions...)}),
		ui.DetailList(ui.DetailListConfig{Items: items}),
	}
	for _, rl := range c.Related {
		body = append(body, c.relatedList(ctx, rl, id))
	}
	return render.Join(body...)
}

// relatedList renders one reverse-relation section: the related entity's rows
// where ForeignKey == this record's id, as a compact table under a heading.
func (c Config) relatedList(ctx context.Context, rl RelatedList, id string) render.HTML {
	// WithReadHooks: these rows are rendered to an end user, same as List and
	// Detail. relatedRelationLabels and the dashboard aggregates below stay
	// raw on purpose — they resolve ids and compute over stored values.
	rows, err := rl.Crud.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Filters: []filter.ParsedFilter{{Field: rl.ForeignKey, Op: filter.OpEq, Value: id}},
		Limit:   10,
	})
	head := ui.PageHeader(ui.PageHeaderConfig{Title: rl.Title, Subtitle: countLabel(len(rows), strings.TrimSuffix(rl.Title, "s"), rl.Title)})
	if err != nil {
		return render.Join(head, ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger, Title: "Couldn't load " + rl.Title}, render.Text("See server logs.")))
	}
	if len(rows) == 0 {
		return render.Join(head, ui.EmptyState(ui.EmptyStateConfig{Title: "No " + strings.ToLower(rl.Title) + " yet", Description: "They will appear here once added.", HeadingLevel: 2}))
	}
	relLabels := relatedRelationLabels(ctx, rl.Relations)
	cols := make([]ui.Column, 0, len(rl.Fields)+1)
	for _, f := range rl.Fields {
		col := ui.Column{Key: f.Key, Header: f.Label}
		if numeric(f.Type) {
			col.Align = "end"
		}
		cols = append(cols, col)
	}
	if rl.BasePath != "" {
		cols = append(cols, ui.Column{Key: "_a", Header: "", Align: "end"})
	}
	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		rid := cell(rowValue(row, "id"))
		cells := map[string]render.HTML{}
		for _, f := range rl.Fields {
			cells[f.Key] = format(f, rowValue(row, f.Key), relLabels)
		}
		if rl.BasePath != "" {
			cells["_a"] = ui.Link(ui.LinkConfig{Href: rl.BasePath + "/" + rid, Text: "View", Variant: ui.LinkAction})
		}
		uiRows = append(uiRows, ui.Row{ID: rid, Cells: cells})
	}
	return render.Join(head, ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: uiRows, Responsive: ui.ResponsiveCards}))
}

// relatedRelationLabels resolves the FK columns of a related entity's rows to
// display names (so an invoice row under a customer still shows plan names etc.).
func relatedRelationLabels(ctx context.Context, rels map[string]Relation) map[string]map[string]string {
	out := map[string]map[string]string{}
	for col, rel := range rels {
		if rel.Crud == nil {
			continue
		}
		// WithReadHooks: the DISPLAY value becomes the visible label on the
		// grid and detail page, so it must show the mask. Only the id is used
		// for lookup, and a picker submits the id — so redacting the label
		// cannot write a masked value back.
		rows, err := rel.Crud.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{Limit: 1000})
		if err != nil {
			continue
		}
		m := map[string]string{}
		for _, r := range rows {
			rid := cell(rowValue(r, "id"))
			if rid == "" {
				continue
			}
			label := cell(rowValue(r, rel.Display))
			if label == "" {
				label = rid
			}
			m[rid] = label
		}
		out[col] = m
	}
	return out
}

// Form renders the create (id == "") or edit (id != "") form for one record.
// It submits as an island: data-fui-rpc posts/puts JSON to the entity's
// auto-CRUD endpoint, then SPA-navigates back to the list/detail on success.
func (c Config) Form(ctx context.Context, id string) render.HTML {
	edit := id != ""
	var row map[string]any
	if edit {
		// Deliberately NOT WithReadHooks: this is an EDIT form and its inputs
		// round-trip back on submit, so prefilling from a redacted read would
		// write the mask over the stored value. Display surfaces (List,
		// Detail) opt in; an editor has to see what it edits.
		r, err := c.Crud.GetOne(ctx, id, nil)
		if err != nil || r == nil {
			return ui.EmptyState(ui.EmptyStateConfig{Title: "Not found", Description: "This " + c.Singular + " does not exist.", HeadingLevel: 1})
		}
		row = r
	}
	rel := c.relationLabels(ctx)

	title, submit := "New "+c.Singular, "Create "+c.Singular
	rpc, back := c.APIPath, c.BasePath
	var action interactive.Action
	if edit {
		title, submit = "Edit "+c.Singular, "Save changes"
		rpc, back = c.APIPath+"/"+id, c.BasePath+"/"+id
		action = interactive.Put(rpc)
	} else {
		action = interactive.Post(rpc)
	}
	attrs := action.OnSuccess(interactive.Navigate(back)).Attrs()

	fields := make([]render.HTML, 0, len(c.Fields))
	for _, f := range c.Fields {
		cur := ""
		if edit {
			cur = cell(rowValue(row, f.Key))
		}
		fields = append(fields, ui.FormField(ui.FormFieldConfig{
			Label: f.Label, For: "f-" + f.Key, Input: c.formInput(ctx, f, cur, rel),
		}))
	}
	form := ui.Form(ui.FormConfig{Action: rpc, Method: "POST", SubmitLabel: submit, ExtraAttrs: attrs, Ctx: ctx}, fields...)
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: title, Actions: ui.Link(ui.LinkConfig{Href: back, Text: "← Cancel", Variant: ui.LinkMuted})}),
		form,
	)
}

// formInput builds the typed control for one field, prefilled with cur. Enums
// and relations render their options server-side; relations resolve to the same
// human label the list/detail show.
func (c Config) formInput(ctx context.Context, f Field, cur string, rel map[string]map[string]string) render.HTML {
	id := "f-" + f.Key
	if labels, ok := rel[f.Key]; ok {
		opts := []html.SelectOption{{Value: "", Text: "— Select —"}}
		for val, label := range labels {
			opts = append(opts, html.SelectOption{Value: val, Text: label, Selected: val == cur})
		}
		return html.Select(html.SelectConfig{Name: f.Key, ID: id, Options: opts})
	}
	switch f.Type {
	case "enum":
		opts := []html.SelectOption{{Value: "", Text: "— Select —"}}
		for _, v := range f.Values {
			opts = append(opts, html.SelectOption{Value: v, Text: title(v), Selected: v == cur})
		}
		return html.Select(html.SelectConfig{Name: f.Key, ID: id, Options: opts})
	case "text":
		return html.TextArea(html.TextAreaConfig{Name: f.Key, ID: id, Content: cur, Rows: 4})
	case "bool", "boolean":
		attrs := html.Attrs{}
		if truthy(cur) {
			attrs["checked"] = "checked"
		}
		return html.Input(html.InputConfig{Type: "checkbox", Name: f.Key, ID: id, ExtraAttrs: attrs})
	default:
		return html.Input(html.InputConfig{Type: inputType(f.Type), Name: f.Key, ID: id, Value: cur})
	}
}

// inputType maps a field type to an <input type=...>.
func inputType(t string) string {
	switch t {
	case "int", "integer", "float", "decimal":
		return "number"
	case "date":
		return "date"
	case "timestamp", "datetime":
		return "datetime-local"
	case "email":
		return "email"
	default:
		return "text"
	}
}

func today() string {
	return time.Now().Format("2006-01-02")
}

func buttonVariant(v string) ui.ButtonVariant {
	switch v {
	case "primary":
		return ui.ButtonPrimary
	case "danger":
		return ui.ButtonDanger
	case "ghost":
		return ui.ButtonGhost
	default:
		return ui.ButtonSecondary
	}
}

// ----- formatting helpers ---------------------------------------------------

func cell(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

// rowValue reads a row value by snake_case key, falling back to the camelCase
// form the JSON API serializes (requires_prescription → requiresPrescription).
func rowValue(row map[string]any, key string) any {
	if v, ok := row[key]; ok {
		return v
	}
	return row[camel(key)]
}

func camel(s string) string {
	var b strings.Builder
	up := false
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up {
			if r >= 'a' && r <= 'z' {
				r = r - 32
			}
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func muted() render.HTML {
	return ui.EmptyValue()
}

func numeric(t string) bool {
	switch t {
	case "int", "integer", "float", "decimal":
		return true
	}
	return false
}

func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on", "t":
		return true
	}
	return false
}

func format(f Field, raw any, rel map[string]map[string]string) render.HTML {
	val := cell(raw)
	if labels, ok := rel[f.Key]; ok {
		if val == "" {
			return muted()
		}
		if l, ok := labels[val]; ok {
			return render.Text(l)
		}
		return render.Text(val)
	}
	switch f.Type {
	case "bool", "boolean":
		if truthy(val) {
			return ui.StatusBadge(ui.StatusBadgeConfig{Label: "Yes", Variant: ui.StatusSuccess})
		}
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: "No", Variant: ui.StatusNeutral})
	}
	if val == "" {
		return muted()
	}
	switch f.Type {
	case "enum":
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: title(val), Variant: enumVariant(val)})
	case "decimal", "float":
		return render.Text(money(val))
	case "date":
		return render.Text(formatDate(raw, val, "Jan 2, 2006"))
	case "timestamp", "datetime":
		return render.Text(formatDate(raw, val, "Jan 2, 2006 3:04 PM"))
	}
	return render.Text(val)
}

// formatDate renders a date/timestamp cleanly. DB drivers hand dates back as
// time.Time (whose default String() is the noisy "2006-01-02 15:04:05 -0700
// MST"), so format those directly; fall back to parsing common string layouts,
// then to trimming the time portion off an ISO-ish string.
func formatDate(raw any, val, layout string) string {
	if t, ok := raw.(time.Time); ok {
		if t.IsZero() {
			return "—"
		}
		return t.Format(layout)
	}
	for _, l := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(l, val); err == nil {
			return parsed.Format(layout)
		}
	}
	if i := strings.IndexByte(val, 'T'); i > 0 {
		return val[:i]
	}
	if i := strings.IndexByte(val, ' '); i > 0 {
		return val[:i]
	}
	return val
}

func title(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "_", " "), "-", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func enumVariant(v string) ui.StatusVariant {
	switch strings.ToLower(v) {
	case "active", "paid", "succeeded", "completed", "open":
		return ui.StatusSuccess
	case "past_due", "pending", "trialing", "draft":
		return ui.StatusWarning
	case "canceled", "cancelled", "void", "failed", "refunded", "inactive":
		return ui.StatusNeutral
	}
	return ui.StatusInfo
}

func money(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f)
	cents := int64(math.Round((f - float64(whole)) * 100))
	if cents == 100 {
		whole++
		cents = 0
	}
	ws := strconv.FormatInt(whole, 10)
	var grp []string
	for len(ws) > 3 {
		grp = append([]string{ws[len(ws)-3:]}, grp...)
		ws = ws[:len(ws)-3]
	}
	grp = append([]string{ws}, grp...)
	out := "$" + strings.Join(grp, ",") + fmt.Sprintf(".%02d", cents)
	if neg {
		out = "-" + out
	}
	return out
}

func countLabel(total int, singular, title string) string {
	if total == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", total, strings.ToLower(title))
}

func emptyDescription(custom string) string {
	if custom != "" {
		return custom
	}
	return "They will appear here once created."
}

// ----- dashboard data binding (stat_card / charts with source) --------------

// Registry holds the resource configs used by generated screens and dashboard blocks.
type Registry map[string]Config

// StatValue computes a single metric over an entity for a stat_card:
// agg "count" (optionally filtered "field=value") or "sum" of a numeric field.
func (r Registry) StatValue(ctx context.Context, entity, agg, field, filterStr, format string) string {
	c, ok := r[entity]
	if !ok || c.Crud == nil {
		return "—"
	}
	var filters []filter.ParsedFilter
	if filterStr != "" {
		if i := strings.IndexByte(filterStr, '='); i > 0 {
			filters = append(filters, filter.ParsedFilter{Field: filterStr[:i], Op: filter.OpEq, Value: filterStr[i+1:]})
		}
	}
	if agg == "sum" {
		rows, err := c.Crud.ListAll(ctx, crud.ListOptions{Filters: filters, Limit: 100000})
		if err != nil {
			return "—"
		}
		var total float64
		for _, r := range rows {
			f, _ := strconv.ParseFloat(cell(rowValue(r, field)), 64)
			total += f
		}
		if format == "money" {
			return money(strconv.FormatFloat(total, 'f', 2, 64))
		}
		return formatNumber(total)
	}
	n, err := c.Crud.CountAll(ctx, crud.ListOptions{Filters: filters})
	if err != nil {
		return "—"
	}
	return strconv.Itoa(n)
}

type kvPair struct {
	k string
	v int
}

func (r Registry) groupCounts(ctx context.Context, entity, groupBy string) []kvPair {
	c, ok := r[entity]
	if !ok || c.Crud == nil {
		return nil
	}
	rows, err := c.Crud.ListAll(ctx, crud.ListOptions{Limit: 100000})
	if err != nil {
		return nil
	}
	order := []string{}
	m := map[string]int{}
	for _, r := range rows {
		key := cell(rowValue(r, groupBy))
		if key == "" {
			key = "—"
		}
		if _, seen := m[key]; !seen {
			order = append(order, key)
		}
		m[key]++
	}
	out := make([]kvPair, 0, len(order))
	for _, k := range order {
		out = append(out, kvPair{k, m[k]})
	}
	return out
}

func (r Registry) GroupBars(ctx context.Context, entity, groupBy string) []ui.BarChartBar {
	counts := r.groupCounts(ctx, entity, groupBy)
	bars := make([]ui.BarChartBar, 0, len(counts))
	for _, kv := range counts {
		bars = append(bars, ui.BarChartBar{Label: title(kv.k), Value: float64(kv.v)})
	}
	return bars
}

func (r Registry) GroupSlices(ctx context.Context, entity, groupBy string) []ui.PieSlice {
	counts := r.groupCounts(ctx, entity, groupBy)
	slices := make([]ui.PieSlice, 0, len(counts))
	for _, kv := range counts {
		slices = append(slices, ui.PieSlice{Label: title(kv.k), Value: float64(kv.v)})
	}
	return slices
}

// LineChart renders a single-series line chart over the grouped
// counts. Fewer than two groups renders ui.LineChart's calm empty state.
func (r Registry) LineChart(ctx context.Context, entity, groupBy string) render.HTML {
	counts := r.groupCounts(ctx, entity, groupBy)
	labels := make([]string, 0, len(counts))
	values := make([]float64, 0, len(counts))
	for _, kv := range counts {
		labels = append(labels, title(kv.k))
		values = append(values, float64(kv.v))
	}
	return ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{{Name: title(groupBy), Values: values}},
		Labels: labels,
	})
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}
