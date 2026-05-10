package main

import (
	"context"
	"sort"
	"strconv"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/pagination"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// DataTableDemoScreen serves /framework-ui/datatable. It now reads the
// URL query (sort, dir, p) via app.QueryFromContext, sorts and
// paginates the in-memory demo data, and re-renders. Clicking a
// sortable header or pagination link round-trips through the server
// and produces a different table — proving the link-driven sort and
// pagination story actually works end-to-end.
type DataTableDemoScreen struct {
	sortBy  string
	sortDir ui.SortDir
	page    int
}

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

func (s *DataTableDemoScreen) Render() render.HTML {
	// Snapshot the demo data, sort, paginate.
	all := make([]customer, len(demoCustomers))
	copy(all, demoCustomers)
	sortCustomers(all, s.sortBy, s.sortDir)

	totalPages := (len(all) + demoPageSize - 1) / demoPageSize
	page := s.page
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
	visible := all[start:end]

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
	if s.sortBy != "" {
		caption += " — sorted by " + s.sortBy + " " + string(s.sortDir) + "ending"
	}
	caption += " · page " + itoa(page) + " of " + itoa(totalPages)

	tableLive := ui.DataTable(ui.DataTableConfig{
		Caption: caption,
		Columns: cols, Rows: rows,
		SortBy: s.sortBy, SortDir: s.sortDir,
		// Sort links must keep the current page; pagination links
		// must keep the current sort. Build patterns that preserve
		// the OTHER axis.
		SortHrefPattern: sortHrefPattern(page),
		Pagination: &pagination.Config{
			Total: totalPages, Current: page,
			HrefPattern: pageHrefPattern(s.sortBy, string(s.sortDir)),
		},
	})

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

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui", Title: "DataTable",
			Subtitle: "Sortable, paginated list view composed from core-ui primitives + framework/ui's EmptyState. Pure server-rendered — click a sortable header or a page link and watch the URL update + the table re-render.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Live",
			Description: "Click a sortable header (Name, Email, Balance). The URL updates to ?sort=…&dir=…, the server re-sorts, and the table re-renders. Click a page link to flip pages — sort state is preserved across clicks.",
		}, tableLive),

		ui.Callout(ui.CalloutConfig{Title: "Try it", Variant: ui.StatusInfo},
			render.Text("Click \"Email\" twice — the indicator goes ↑ then ↓ as direction flips. Click page 2 — the same sort persists."),
		),

		ui.Section(ui.SectionConfig{
			Heading:     "Empty state",
			Description: "When Rows is empty, the configured EmptyState renders inside the wrapper.",
		}, emptyDemo),

		ui.Section(ui.SectionConfig{
			Heading: "Composition",
			Description: "DataTable wires elements.Table + elements.Caption + elements.TH/TD + framework/ui.EmptyState + core-ui/pagination. Every ARIA role (rowgroup, columnheader, cell) comes from core-ui's elements.",
		}),

		ui.Section(ui.SectionConfig{
			Heading: "How sort + page round-trip works",
			Description: "Each sort link's href is built via SortHrefPattern, which preserves the active page. Each page link's href is built via the Pagination's HrefPattern, which preserves the active sort. The screen reads ?sort, ?dir, and ?p in Load(ctx) via app.QueryFromContext, sets fields, and Render() builds the table from those fields.",
		}),
	)
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

func sortHrefPattern(page int) string {
	if page <= 1 {
		return "?sort=%s&dir=%s"
	}
	return "?sort=%s&dir=%s&p=" + itoa(page)
}

func pageHrefPattern(sortBy, sortDir string) string {
	if sortBy == "" {
		return "?p=%d"
	}
	return "?sort=" + sortBy + "&dir=" + sortDir + "&p=%d"
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
