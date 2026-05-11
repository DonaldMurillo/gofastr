package ui

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/patterns/pagination"
	"github.com/gofastr/gofastr/core/render"
)

func TestDataTableRequiresColumns(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{Rows: []Row{{}}})
	t.Fatal("expected panic on empty Columns")
}

func TestDataTableColumnRequiresKey(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{Columns: []Column{{Header: "x"}}})
	t.Fatal("expected panic on Column without Key")
}

func TestDataTableSortableEmptyHeaderPanics(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{
		Columns:         []Column{{Key: "x", Header: "", Sortable: true}},
		Rows:            []Row{{Cells: map[string]render.HTML{"x": render.Text("a")}}},
		SortHrefPattern: "?s=%s&d=%s",
	})
	t.Fatal("expected panic on sortable column with empty header")
}

func TestDataTableActionsColumnEmptyHeaderOK(t *testing.T) {
	// Empty Header is allowed on non-sortable columns (actions / icons).
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "actions", Header: "", Align: "end"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{
			"name":    render.Text("Alice"),
			"actions": render.Text("✎"),
		}}},
	}))
	if !strings.Contains(h, "Alice") {
		t.Errorf("expected row to render: %s", h)
	}
}

func TestDataTableSortableRequiresHrefPattern(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
	})
	t.Fatal("expected panic when sortable column lacks HrefPattern")
}

func TestDataTableEmptyStateRenders(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    nil,
	}))
	for _, want := range []string{
		"ui-data-table is-empty",
		"ui-empty-state",
		"No results",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableEmptyStateUsesProvidedConfig(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Empty: EmptyStateConfig{
			Title:       "No customers yet",
			Description: "Try inviting one.",
		},
	}))
	for _, want := range []string{"No customers yet", "Try inviting one."} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableRendersHeadersAndRows(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "email", Header: "Email"},
		},
		Rows: []Row{
			{Cells: map[string]render.HTML{
				"name":  render.Text("Alice"),
				"email": render.Text("a@x.com"),
			}},
			{Cells: map[string]render.HTML{
				"name":  render.Text("Bob"),
				"email": render.Text("b@x.com"),
			}},
		},
	}))
	// html.TH/TD add role= attributes; we just check the scope and
	// the cell text are present.
	for _, want := range []string{
		`scope="col"`, `>Name<`, `>Email<`,
		`>Alice<`, `>b@x.com<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableSortableColumnRendersClickableLink(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name", Sortable: true},
		},
		Rows: []Row{{Cells: map[string]render.HTML{"name": render.Text("Alice")}}},
		SortHrefPattern: "?sort=%s&dir=%s",
	}))
	if !strings.Contains(h, `ui-data-table__sort`) {
		t.Errorf("expected sort link class, got: %s", h)
	}
	if !strings.Contains(h, `href="?sort=name&amp;dir=asc"`) {
		t.Errorf("expected default-asc link, got: %s", h)
	}
	if !strings.Contains(h, `aria-sort="none"`) {
		t.Errorf("expected aria-sort=none on inactive sortable column, got: %s", h)
	}
}

func TestDataTableActiveSortFlipsDirectionAndShowsIndicator(t *testing.T) {
	hAsc := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows: []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		SortHrefPattern: "?sort=%s&dir=%s",
		SortBy:          "name",
		SortDir:         SortAsc,
	}))
	if !strings.Contains(hAsc, `aria-sort="ascending"`) {
		t.Errorf("expected aria-sort=ascending, got: %s", hAsc)
	}
	if !strings.Contains(hAsc, `href="?sort=name&amp;dir=desc"`) {
		t.Errorf("expected next-click flips to desc, got: %s", hAsc)
	}
	if !strings.Contains(hAsc, "↑") {
		t.Errorf("expected ascending indicator, got: %s", hAsc)
	}

	hDesc := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows: []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		SortHrefPattern: "?sort=%s&dir=%s",
		SortBy:          "name",
		SortDir:         SortDesc,
	}))
	if !strings.Contains(hDesc, `aria-sort="descending"`) {
		t.Errorf("expected aria-sort=descending, got: %s", hDesc)
	}
	if !strings.Contains(hDesc, `href="?sort=name&amp;dir=asc"`) {
		t.Errorf("expected next-click flips to asc, got: %s", hDesc)
	}
}

func TestDataTablePaginationFooterRenders(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Pagination: &pagination.Config{
			Total: 5, Current: 2, HrefPattern: "?p=%d",
		},
	}))
	if !strings.Contains(h, "ui-data-table__footer") {
		t.Errorf("expected pagination footer, got: %s", h)
	}
	if !strings.Contains(h, `<nav aria-label="Pagination">`) {
		t.Errorf("expected pagination nav, got: %s", h)
	}
}

func TestDataTableMissingCellRendersEmpty(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "email", Header: "Email"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{
			"name": render.Text("Alice"),
			// email intentionally missing
		}}},
	}))
	// Two cells should render even when the second is missing from
	// the Cells map — they collapse to empty content.
	if strings.Count(h, "<td") != 2 {
		t.Errorf("expected 2 <td even with missing cell, got %d in: %s",
			strings.Count(h, "<td"), h)
	}
}

func TestDataTableAlignmentClassesRender(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "amt", Header: "Amount", Align: "end"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{"amt": render.Text("$5")}}},
	}))
	if !strings.Contains(h, "is-align-end") {
		t.Errorf("expected is-align-end class, got: %s", h)
	}
}

func TestDataTableCaptionRenders(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Caption: "Recent customers",
	}))
	if !strings.Contains(h, "<caption") || !strings.Contains(h, "Recent customers") {
		t.Errorf("expected caption to render, got: %s", h)
	}
}
