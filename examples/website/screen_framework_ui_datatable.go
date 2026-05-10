package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/pagination"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

type DataTableDemoScreen struct{}

func (s *DataTableDemoScreen) ScreenTitle() string        { return "DataTable" }
func (s *DataTableDemoScreen) ScreenDescription() string  { return "Composable list view with sort + pagination." }
func (s *DataTableDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

type customer struct {
	Name    string
	Email   string
	Status  ui.StatusVariant
	Balance string
}

var demoCustomers = []customer{
	{"Alice Johnson", "alice@example.com", ui.StatusSuccess, "$1,283.40"},
	{"Bob Patel", "bob@example.com", ui.StatusWarning, "$0.00"},
	{"Caroline Park", "caroline@example.com", ui.StatusSuccess, "$472.10"},
	{"Diego Mendes", "diego@example.com", ui.StatusDanger, "$3,012.99"},
	{"Eli Tan", "eli@example.com", ui.StatusInfo, "$58.50"},
	{"Fatima Khan", "fatima@example.com", ui.StatusSuccess, "$902.00"},
	{"George Brooks", "george@example.com", ui.StatusNeutral, "$1,180.25"},
	{"Hae-jin Lee", "hae@example.com", ui.StatusSuccess, "$240.00"},
}

func (s *DataTableDemoScreen) Render() render.HTML {
	rows := make([]ui.Row, 0, len(demoCustomers))
	for _, c := range demoCustomers {
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

	tableLive := ui.DataTable(ui.DataTableConfig{
		Caption: "Customer accounts — sorted by name",
		Columns: cols, Rows: rows,
		SortBy: "name", SortDir: ui.SortAsc,
		SortHrefPattern: "?sort=%s&dir=%s",
		Pagination: &pagination.Config{
			Total: 12, Current: 1, HrefPattern: "?p=%d",
		},
	})

	tableSortedDesc := ui.DataTable(ui.DataTableConfig{
		Caption: "Same data — sorted by balance descending",
		Columns: cols, Rows: rows,
		SortBy: "balance", SortDir: ui.SortDesc,
		SortHrefPattern: "?sort=%s&dir=%s",
	})

	// Empty-state columns drop Sortable so we don't need to pass a
	// SortHrefPattern alongside an empty Rows slice.
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
			Subtitle: "Sortable, paginated list view composed from core-ui primitives + framework/ui's EmptyState. Pure server-rendered.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Live",
			Description: "Sortable headers are real <a> links. Clicking flips the direction; clicking another sortable column starts at ascending. Sort state is reflected in the URL via SortHrefPattern.",
		}, tableLive),
		ui.Section(ui.SectionConfig{
			Heading:     "Different sort state",
			Description: "Setting SortBy: \"balance\", SortDir: ui.SortDesc renders the same rows with the descending indicator on the active column.",
		}, tableSortedDesc),
		ui.Section(ui.SectionConfig{
			Heading:     "Empty state",
			Description: "When Rows is empty, the configured EmptyState renders inside the wrapper.",
		}, emptyDemo),
		ui.Section(ui.SectionConfig{
			Heading: "Composition",
			Description: "DataTable wires elements.Table + elements.Caption + elements.TH/TD + framework/ui.EmptyState + core-ui/pagination. Every ARIA role (rowgroup, columnheader, cell) comes from core-ui's elements.",
		}),
	)
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
