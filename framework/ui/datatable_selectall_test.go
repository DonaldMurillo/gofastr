package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestEmptyTableDrawsNoSelectAll(t *testing.T) {
	cols := []Column{{Key: "pick", SelectAll: "ids", Fit: true}, {Key: "name", Header: "Name"}}
	empty := string(DataTable(DataTableConfig{Columns: cols}))
	if strings.Contains(empty, "data-hui-table-select-all") {
		t.Errorf("an empty table draws a select-all box:\n%s", empty)
	}
	full := string(DataTable(DataTableConfig{Columns: cols, Rows: []Row{{ID: "1", Cells: map[string]render.HTML{"name": "Ada"}}}}))
	if !strings.Contains(full, `data-hui-table-select-all="ids"`) {
		t.Errorf("a table with rows lost its select-all box:\n%s", full)
	}
}
