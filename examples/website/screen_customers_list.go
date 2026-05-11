package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core-ui/patterns/pagination"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// CustomersListScreen is the CRUD demo's list page. It exercises every
// architecture pattern in one place:
//
//   - SSR initial render reading ?sort=&dir=&p=&q= via Load(ctx)
//   - In-page state changes (sort, page, filter) as island RPCs to
//     /islands/customers/state — no full reloads
//   - URL-as-source-of-truth via X-Gofastr-Push-State response header
//   - Cross-page links to /customers/new and /customers/<id> go
//     through the SPA partial-fetch + cache path (no hard refresh)
//   - Delete uses an island RPC to /islands/customers/delete with a
//     server-side confirmation step (no destructive POST without intent)
//   - After mutations the list island re-renders with the latest data.
type CustomersListScreen struct {
	sortBy  string
	sortDir ui.SortDir
	page    int
	query   string
	flash   string // optional success message rendered as a Notification
}

func (s *CustomersListScreen) ScreenTitle() string        { return "Customers" }
func (s *CustomersListScreen) ScreenDescription() string  { return "CRUD demo — list + create + edit + delete, all islands." }
func (s *CustomersListScreen) ScreenType() app.ScreenType { return app.ScreenPage }

const (
	customersIslandSignal   = "customers-list"
	customersIslandEndpoint = "/islands/customers/state"
	customersDeleteEndpoint = "/islands/customers/delete"
)

func (s *CustomersListScreen) Load(ctx context.Context) error {
	q := app.QueryFromContext(ctx)
	s.sortBy = q.Get("sort")
	s.sortDir = ui.SortDir(q.Get("dir"))
	if s.sortDir != ui.SortAsc && s.sortDir != ui.SortDesc {
		s.sortDir = ui.SortAsc
	}
	s.page = 1
	if p, err := strconv.Atoi(q.Get("p")); err == nil && p > 0 {
		s.page = p
	}
	s.query = q.Get("q")
	s.flash = q.Get("flash")
	return nil
}

func renderCustomersIsland(sortBy string, sortDir ui.SortDir, page int, query string) render.HTML {
	all := filterCustomersBy(customers.All(), query)
	sortCustomerRows(all, sortBy, sortDir)

	const pageSize = 5
	totalPages := (len(all) + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	visible := []Customer{}
	if start < len(all) {
		visible = all[start:end]
	}

	rows := make([]ui.Row, 0, len(visible))
	for _, c := range visible {
		rows = append(rows, ui.Row{
			Cells: map[string]render.HTML{
				"name":   render.Text(c.Name),
				"email":  render.Text(c.Email),
				"status": ui.StatusBadge(ui.StatusBadgeConfig{Label: capitalize(string(c.Status)), Variant: c.Status}),
				"actions": render.Join(
					html.Link(html.LinkConfig{
						Href:  "/customers/" + strconv.FormatInt(c.ID, 10),
						Text:  "Edit",
						Class: "ui-link",
					}),
					render.Text(" "),
					render.Tag("button", map[string]string{
						"type":                "button",
						"class":               "ui-button ui-button--danger ui-button--small",
						"data-fui-rpc":        customersDeleteEndpoint + "?id=" + strconv.FormatInt(c.ID, 10),
						"data-fui-rpc-method": "POST",
						"data-fui-rpc-signal": customersIslandSignal,
						"data-fui-confirm":    "Delete \"" + c.Name + "\"? This cannot be undone.",
					}, render.Text("Delete")),
				),
			},
		})
	}

	cols := []ui.Column{
		{Key: "name", Header: "Name", Sortable: true},
		{Key: "email", Header: "Email", Sortable: true},
		{Key: "status", Header: "Status"},
		{Key: "actions", Header: "", Align: "end"},
	}
	caption := "Customers"
	if query != "" {
		caption += " · matching \"" + query + "\""
	}
	caption += " · page " + strconv.Itoa(page) + " of " + strconv.Itoa(totalPages)

	empty := ui.EmptyStateConfig{}
	if len(rows) == 0 {
		if query != "" {
			empty = ui.EmptyStateConfig{
				Title:       "No customers match \"" + query + "\"",
				Description: "Try a different search term or clear the filter.",
			}
		} else {
			empty = ui.EmptyStateConfig{
				Title:       "No customers yet",
				Description: "Add the first one using the button above.",
				Action: html.Link(html.LinkConfig{
					Href: "/customers/new", Text: "Add customer", Class: "ui-button",
				}),
			}
		}
	}

	return ui.DataTable(ui.DataTableConfig{
		Caption: caption,
		Columns: cols, Rows: rows,
		SortBy: sortBy, SortDir: sortDir,
		SortHrefPattern: customersSortHrefPattern(page, query),
		Pagination: &pagination.Config{
			Total: totalPages, Current: page,
			HrefPattern: customersPageHrefPattern(sortBy, string(sortDir), query),
		},
		Empty:          empty,
		IslandSignal:   customersIslandSignal,
		IslandEndpoint: customersIslandEndpoint,
	})
}

// CustomersIslandHandler serves /islands/customers/state for sort,
// page, and search RPCs.
func CustomersIslandHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortBy := q.Get("sort")
	sortDir := ui.SortDir(q.Get("dir"))
	if sortDir != ui.SortAsc && sortDir != ui.SortDesc {
		sortDir = ui.SortAsc
	}
	page := 1
	if v, err := strconv.Atoi(q.Get("p")); err == nil && v > 0 {
		page = v
	}
	query := q.Get("q")
	pushPath := customersCanonicalPath(sortBy, string(sortDir), page, query, "")
	if pushPath != "" {
		w.Header().Set("X-Gofastr-Push-State", pushPath)
	}
	render.RespondHTML(w, renderCustomersIsland(sortBy, sortDir, page, query))
}

// CustomersDeleteHandler serves /islands/customers/delete. Deletes the
// customer by ID and re-renders the list island in the same response
// so the row disappears immediately.
func CustomersDeleteHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	_ = customers.Delete(id)
	// Re-render with current sort/page/q from the referrer's query so
	// the visible state is consistent after the delete. The delete
	// button doesn't carry sort/page/q itself; we read them from the
	// Referer URL when present, otherwise default.
	sortBy := ""
	sortDir := ui.SortAsc
	page := 1
	query := ""
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil {
			qq := u.Query()
			sortBy = qq.Get("sort")
			if d := ui.SortDir(qq.Get("dir")); d == ui.SortDesc {
				sortDir = ui.SortDesc
			}
			if p, err := strconv.Atoi(qq.Get("p")); err == nil && p > 0 {
				page = p
			}
			query = qq.Get("q")
		}
	}
	render.RespondHTML(w, renderCustomersIsland(sortBy, sortDir, page, query))
}

func (s *CustomersListScreen) Render() render.HTML {
	tableLive := renderCustomersIsland(s.sortBy, s.sortDir, s.page, s.query)

	// Search input — see screen_framework_ui_datatable.go for the
	// same pattern; this one targets the customers endpoint.
	searchInput := render.Tag("input", map[string]string{
		"type": "search", "name": "q", "value": s.query,
		"placeholder":  "Search customers (name or email)…",
		"class":        "demo-search-input",
		"aria-label":   "Search customers",
		"autocomplete": "off",
		"spellcheck":   "false",
	})
	searchForm := render.Tag("form", map[string]string{
		"class":                    "demo-search-form",
		"data-fui-rpc":             customersIslandEndpoint,
		"data-fui-rpc-method":      "GET",
		"data-fui-rpc-signal":      customersIslandSignal,
		"data-fui-rpc-trigger":     "input",
		"data-fui-rpc-debounce-ms": "250",
		"role":                     "search",
	},
		render.Tag("input", map[string]string{
			"type": "hidden", "name": "sort", "value": s.sortBy,
		}),
		render.Tag("input", map[string]string{
			"type": "hidden", "name": "dir", "value": string(s.sortDir),
		}),
		searchInput,
	)

	liveIsland := render.Tag("div",
		map[string]string{
			"data-fui-signal":      customersIslandSignal,
			"data-fui-signal-mode": "html",
		},
		tableLive,
	)

	header := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow:  "CRUD demo",
		Title:    "Customers",
		Subtitle: "Sort, paginate, search, create, edit, delete — every interaction is an island RPC. No hard refreshes.",
		Actions: html.Link(html.LinkConfig{
			Href: "/customers/new", Text: "New customer", Class: "ui-button",
		}),
	})

	body := []render.HTML{header}

	// Flash notification (after a successful create/edit/delete).
	if s.flash != "" {
		body = append(body,
			ui.Notification(ui.NotificationConfig{
				Title:   s.flash,
				Variant: ui.StatusSuccess,
				DismissHref: "/customers" + customersCanonicalPath(
					s.sortBy, string(s.sortDir), s.page, s.query, ""),
			}))
	}

	body = append(body, render.Tag("div",
		map[string]string{"class": "demo-stack demo-stack--sm"},
		searchForm, liveIsland,
	))

	return render.Tag("main", nil, body...)
}

// -----------------------------------------------------------------------------
// helpers (kept in this file so the CRUD demo is self-contained — the
// DataTable demo has its own equivalents with the /framework-ui prefix).
// -----------------------------------------------------------------------------

func filterCustomersBy(rows []Customer, q string) []Customer {
	out := make([]Customer, 0, len(rows))
	if q == "" {
		return append(out, rows...)
	}
	needle := strings.ToLower(q)
	for _, c := range rows {
		if strings.Contains(strings.ToLower(c.Name), needle) ||
			strings.Contains(strings.ToLower(c.Email), needle) {
			out = append(out, c)
		}
	}
	return out
}

func sortCustomerRows(rows []Customer, by string, dir ui.SortDir) {
	if by == "" {
		return
	}
	asc := dir != ui.SortDesc
	sort.SliceStable(rows, func(i, j int) bool {
		var less bool
		switch by {
		case "email":
			less = rows[i].Email < rows[j].Email
		default:
			less = rows[i].Name < rows[j].Name
		}
		if !asc {
			return !less
		}
		return less
	})
}

func customersSortHrefPattern(page int, query string) string {
	parts := []string{"sort=%s", "dir=%s"}
	if page > 1 {
		parts = append(parts, "p="+strconv.Itoa(page))
	}
	if query != "" {
		parts = append(parts, "q="+url.QueryEscape(query))
	}
	return "?" + strings.Join(parts, "&")
}

func customersPageHrefPattern(sortBy, sortDir, query string) string {
	parts := []string{}
	if sortBy != "" {
		parts = append(parts, "sort="+sortBy)
		parts = append(parts, "dir="+sortDir)
	}
	parts = append(parts, "p=%d")
	if query != "" {
		parts = append(parts, "q="+url.QueryEscape(query))
	}
	return "?" + strings.Join(parts, "&")
}

// customersCanonicalPath builds the URL for X-Gofastr-Push-State that
// reflects the active state. The optional flash param surfaces a
// success notification on the next render.
func customersCanonicalPath(sortBy, sortDir string, page int, query, flash string) string {
	parts := []string{}
	if sortBy != "" {
		parts = append(parts, "sort="+sortBy)
		parts = append(parts, "dir="+sortDir)
	}
	if page > 1 {
		parts = append(parts, "p="+strconv.Itoa(page))
	}
	if query != "" {
		parts = append(parts, "q="+url.QueryEscape(query))
	}
	if flash != "" {
		parts = append(parts, "flash="+url.QueryEscape(flash))
	}
	if len(parts) == 0 {
		return "/customers"
	}
	return "/customers?" + strings.Join(parts, "&")
}
