package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// demoDatatableResponsiveStyle clamps the container-query demo wrapper
// to 360px so the cards layout is visible without resizing the viewport.
// Registered through the proper pipeline because strict CSP blocks
// inline style="" and <style> blocks injected via render.Tag.
var demoDatatableResponsiveStyle = registry.RegisterStyle("demo-datatable-responsive", func(_ style.Theme) string {
	return `.demo-datatable-responsive-probe {
  inline-size: 360px;
  min-inline-size: 0;
  max-inline-size: 100%;
  justify-self: start;
  border: 1px dashed var(--color-border, #E4E4E7);
  padding: var(--spacing-md, 8px);
}`
})

// DataTableDemoScreen serves /framework-ui/datatable. The live demo is
// an island with three controls: sort headers, pagination buttons, and
// a search input. All three fire RPCs to /islands/datatable-demo/state.
// The runtime swaps just the island; URL is kept in sync via
// data-fui-push-state. Refresh / share-link / browser-back round-trip
// through the URL → Load(ctx) reads ?sort=…&dir=…&p=…&q=… → SSR.
//
// See core-ui/ARCHITECTURE.md.
type DataTableDemoScreen struct {
	sortBy  string
	sortDir ui.SortDir
	page    int
	query   string
}

const (
	datatableDemoSignal   = "datatable-demo"
	datatableDemoEndpoint = "/islands/datatable-demo/state"
)

func (s *DataTableDemoScreen) ScreenTitle() string        { return "DataTable" }
func (s *DataTableDemoScreen) ScreenDescription() string  { return "Composable list view with sort + pagination." }
func (s *DataTableDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

const demoPageSize = 4

func (s *DataTableDemoScreen) Load(ctx context.Context) error {
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
	return nil
}

type customer struct {
	Name    string
	Email   string
	Status  ui.StatusVariant
	Balance string
	balance float64 // for sort
}

var demoCustomers = []customer{
	{"Alice Johnson", "alice@example.com", ui.StatusSuccess, "$1,283.40", 1283.40},
	{"Bob Patel", "bob@example.com", ui.StatusWarning, "$0.00", 0.00},
	{"Caroline Park", "caroline@example.com", ui.StatusSuccess, "$472.10", 472.10},
	{"Diego Mendes", "diego@example.com", ui.StatusDanger, "$3,012.99", 3012.99},
	{"Eli Tan", "eli@example.com", ui.StatusInfo, "$58.50", 58.50},
	{"Fatima Khan", "fatima@example.com", ui.StatusSuccess, "$902.00", 902.00},
	{"George Brooks", "george@example.com", ui.StatusNeutral, "$1,180.25", 1180.25},
	{"Hae-jin Lee", "hae@example.com", ui.StatusSuccess, "$240.00", 240.00},
	{"Iris Cohen", "iris@example.com", ui.StatusSuccess, "$3,540.10", 3540.10},
	{"Jamal Reyes", "jamal@example.com", ui.StatusWarning, "$15.00", 15.00},
	{"Kira Sato", "kira@example.com", ui.StatusInfo, "$662.75", 662.75},
	{"Liam O'Connor", "liam@example.com", ui.StatusNeutral, "$84.00", 84.00},
}

// renderDataTableIsland produces the DataTable's island content for a
// given (sort, dir, page, query). Reused by both the initial SSR
// render and the RPC handler so the two responses are byte-for-byte
// identical for the same inputs.
func renderDataTableIsland(sortBy string, sortDir ui.SortDir, page int, query string) render.HTML {
	all := filterCustomers(demoCustomers, query)
	sortCustomers(all, sortBy, sortDir)

	totalPages := (len(all) + demoPageSize - 1) / demoPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * demoPageSize
	end := start + demoPageSize
	if end > len(all) {
		end = len(all)
	}
	visible := []customer{}
	if start < len(all) {
		visible = all[start:end]
	}

	rows := make([]ui.Row, 0, len(visible))
	for _, c := range visible {
		rows = append(rows, ui.Row{
			Cells: map[string]render.HTML{
				"name":    render.Text(c.Name),
				"email":   render.Text(c.Email),
				"status":  ui.StatusBadge(ui.StatusBadgeConfig{Label: capitalize(string(c.Status)), Variant: c.Status}),
				"balance": render.Text(c.Balance),
			},
		})
	}
	cols := []ui.Column{
		{Key: "name", Header: "Name", Sortable: true},
		{Key: "email", Header: "Email", Sortable: true},
		{Key: "status", Header: "Status"},
		{Key: "balance", Header: "Balance", Sortable: true, Align: "end"},
	}
	caption := "Customer accounts"
	if query != "" {
		caption += " · matching \"" + query + "\""
	}
	if sortBy != "" {
		caption += " · sorted by " + sortBy + " " + string(sortDir) + "ending"
	}
	caption += " · page " + itoa(page) + " of " + itoa(totalPages)

	empty := ui.EmptyStateConfig{}
	if query != "" && len(rows) == 0 {
		empty = ui.EmptyStateConfig{
			Title:       "No customers match \"" + query + "\"",
			Description: "Try a different search term or clear the filter.",
		}
	}

	return ui.DataTable(ui.DataTableConfig{
		Caption: caption,
		Columns: cols, Rows: rows,
		SortBy: sortBy, SortDir: sortDir,
		SortHrefPattern: sortHrefPattern(page, query),
		Pagination: &pagination.Config{
			Total: totalPages, Current: page,
			HrefPattern: pageHrefPattern(sortBy, string(sortDir), query),
		},
		Empty:          empty,
		IslandSignal:   datatableDemoSignal,
		IslandEndpoint: datatableDemoEndpoint,
	})
}

// DataTableIslandHandler serves /islands/datatable-demo/state for sort,
// page, and search RPCs. Reads ?sort=&dir=&p=&q= from the query and
// returns the rendered island HTML.
func DataTableIslandHandler(w http.ResponseWriter, r *http.Request) {
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
	// Build the canonical URL for X-Gofastr-Push-State so the URL bar
	// reflects the rendered state. The search input lives outside the
	// island so it preserves focus; we use the response header to
	// keep the URL in sync without a re-fetch.
	pushPath := canonicalPath(sortBy, string(sortDir), page, query)
	if pushPath != "" {
		w.Header().Set("X-Gofastr-Push-State", pushPath)
	}
	render.RespondHTML(w, renderDataTableIsland(sortBy, sortDir, page, query))
}

func (s *DataTableDemoScreen) Render() render.HTML {
	tableLive := renderDataTableIsland(s.sortBy, s.sortDir, s.page, s.query)

	// Search input. Sits OUTSIDE the signal-bound wrapper so it keeps
	// focus while the user types (the swap only touches the table).
	// The form fires its RPC on every input event with a 250ms debounce.
	// Hidden inputs carry the current sort so search doesn't drop it.
	// Page resets to 1 on a new query (no hidden ?p — the server
	// defaults to 1 when absent).
	searchInput := render.Tag("input", map[string]string{
		"type":          "search",
		"name":          "q",
		"value":         s.query,
		"placeholder":   "Search customers (name or email)…",
		"class":         "demo-search-input",
		"aria-label":    "Search customers",
		"autocomplete":  "off",
		"spellcheck":    "false",
	})
	hiddenSort := render.Tag("input", map[string]string{
		"type": "hidden", "name": "sort", "value": s.sortBy,
	})
	hiddenDir := render.Tag("input", map[string]string{
		"type": "hidden", "name": "dir", "value": string(s.sortDir),
	})
	searchForm := render.Tag("form", map[string]string{
		"class":                     "demo-search-form",
		"data-fui-rpc":              datatableDemoEndpoint,
		"data-fui-rpc-method":       "GET",
		"data-fui-rpc-signal":       datatableDemoSignal,
		"data-fui-rpc-trigger":      "input",
		"data-fui-rpc-debounce-ms":  "250",
		"role":                      "search",
	}, hiddenSort, hiddenDir, searchInput)

	// Wrap the rendered DataTable in the signal-bound container —
	// RPC responses replace this innerHTML.
	liveIsland := render.Tag("div",
		map[string]string{
			"data-fui-signal":      datatableDemoSignal,
			"data-fui-signal-mode": "html",
		},
		tableLive,
	)

	liveWithSearch := render.Tag("div",
		map[string]string{"class": "demo-stack demo-stack--sm"},
		searchForm,
		liveIsland,
	)

	emptyCols := []ui.Column{
		{Key: "name", Header: "Name"},
		{Key: "email", Header: "Email"},
		{Key: "status", Header: "Status"},
		{Key: "balance", Header: "Balance", Align: "end"},
	}
	emptyDemo := ui.DataTable(ui.DataTableConfig{
		Columns: emptyCols,
		Empty: ui.EmptyStateConfig{
			Title:       "No customers match your filter",
			Description: "Try widening the date range or clearing the search.",
		},
	})
	_ = emptyDemo

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui", Title: "DataTable",
			Subtitle: "Sortable, paginated list view composed from core-ui primitives + framework/ui's EmptyState. Pure server-rendered — click a sortable header or a page link and watch the URL update + the table re-render.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Live (island mode)",
			Description: "Type in the search box (debounced 250ms), click a sortable header, or click a pagination button. All three fire RPCs to /islands/datatable-demo/state and swap just this island — no full reload. URL stays in sync via X-Gofastr-Push-State response header. Refresh / share-link / back-button all round-trip through the URL.",
		}, liveWithSearch),

		ui.Callout(ui.CalloutConfig{Title: "Try it", Variant: ui.StatusInfo},
			render.Text("Click \"Email\" twice — the indicator goes ↑ then ↓ as direction flips. Click page 2 — the same sort persists."),
		),

		ui.Section(ui.SectionConfig{
			Heading:     "Empty state",
			Description: "When Rows is empty, the configured EmptyState renders inside the wrapper.",
		}, emptyDemo),

		ui.Section(ui.SectionConfig{
			Heading:     "Responsive (container queries → cards)",
			Description: "Responsive: ui.ResponsiveCards adds container-query rules that collapse rows into labeled cards when the table's container drops below 640px. The probe below is artificially clamped to 360px so the cards mode is visible without resizing the viewport — a wide table in a narrow sidebar gets the same treatment.",
		},
			demoDatatableResponsiveStyle.WrapHTML(render.Tag("div", map[string]string{
				"class":                              "demo-datatable-responsive-probe",
				"data-fui-datatable-responsive-demo": "true",
			},
				ui.DataTable(ui.DataTableConfig{
					Columns: []ui.Column{
						{Key: "name", Header: "Name"},
						{Key: "email", Header: "Email"},
						{Key: "plan", Header: "Plan"},
					},
					Rows: []ui.Row{
						{Cells: map[string]render.HTML{
							"name":  render.Text("Ada Lovelace"),
							"email": render.Text("ada@example.com"),
							"plan":  render.Text("Pro"),
						}},
						{Cells: map[string]render.HTML{
							"name":  render.Text("Linus Torvalds"),
							"email": render.Text("linus@example.com"),
							"plan":  render.Text("Enterprise"),
						}},
					},
					Responsive: ui.ResponsiveCards,
				}),
			)),
		),

		ui.Section(ui.SectionConfig{
			Heading: "Composition",
			Description: "DataTable wires html.Table + html.Caption + html.TH/TD + framework/ui.EmptyState + core-ui/patterns/pagination. Every ARIA role (rowgroup, columnheader, cell) comes from core-ui's html.",
		}),

		ui.Section(ui.SectionConfig{
			Heading: "How sort + page round-trip works",
			Description: "Each sort link's href is built via SortHrefPattern, which preserves the active page. Each page link's href is built via the Pagination's HrefPattern, which preserves the active sort. The screen reads ?sort, ?dir, and ?p in Load(ctx) via app.QueryFromContext, sets fields, and Render() builds the table from those fields.",
		}),
	)
}

// filterCustomers returns the rows whose Name or Email contains q
// (case-insensitive). Empty q is a no-op — returns a copy of all rows
// to keep sortCustomers's in-place mutation contained to the demo.
func filterCustomers(rows []customer, q string) []customer {
	out := make([]customer, 0, len(rows))
	if q == "" {
		out = append(out, rows...)
		return out
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

// canonicalPath builds the URL for pushState that reflects the active
// (sort, dir, page, query). Empty params are omitted so the URL stays
// short.
func canonicalPath(sortBy, sortDir string, page int, query string) string {
	parts := []string{}
	if sortBy != "" {
		parts = append(parts, "sort="+sortBy)
		parts = append(parts, "dir="+sortDir)
	}
	if page > 1 {
		parts = append(parts, "p="+itoa(page))
	}
	if query != "" {
		parts = append(parts, "q="+url.QueryEscape(query))
	}
	if len(parts) == 0 {
		return "/framework-ui/datatable"
	}
	return "/framework-ui/datatable?" + strings.Join(parts, "&")
}

func sortCustomers(rows []customer, by string, dir ui.SortDir) {
	if by == "" {
		return
	}
	asc := dir != ui.SortDesc
	sort.SliceStable(rows, func(i, j int) bool {
		var less bool
		switch by {
		case "email":
			less = rows[i].Email < rows[j].Email
		case "balance":
			less = rows[i].balance < rows[j].balance
		default: // "name" or unknown
			less = rows[i].Name < rows[j].Name
		}
		if !asc {
			return !less
		}
		return less
	})
}

func sortHrefPattern(page int, query string) string {
	parts := []string{"sort=%s", "dir=%s"}
	if page > 1 {
		parts = append(parts, "p="+itoa(page))
	}
	if query != "" {
		parts = append(parts, "q="+url.QueryEscape(query))
	}
	return "?" + strings.Join(parts, "&")
}

func pageHrefPattern(sortBy, sortDir, query string) string {
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

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
