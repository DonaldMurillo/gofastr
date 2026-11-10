package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Every alignment a column can name meets every width rule it can
// name: the class map is keyed by the two together, and a pair with no
// entry drew a cell with no class at all. A centered Truncate column
// was neither centered nor cut, so one long value widened the table.
func TestDataTableEveryAlignAndWidthVariantIsDressed(t *testing.T) {
	widths := map[string]func(*Column){
		"":         func(*Column) {},
		"wrap":     func(c *Column) { c.Wrap = true },
		"fit":      func(c *Column) { c.Fit = true },
		"truncate": func(c *Column) { c.Truncate = true },
	}
	for _, align := range []string{"", "start", "center", "end"} {
		for width, set := range widths {
			col := Column{Key: "v", Header: "Value", Align: align}
			set(&col)
			h := string(DataTable(DataTableConfig{
				Columns: []Column{col},
				Rows:    []Row{{Cells: map[string]render.HTML{"v": "x"}}},
			}))
			th := h[strings.Index(h, "<th "):]
			th = th[:strings.Index(th, ">")]
			td := h[strings.Index(h, "<td "):]
			td = td[:strings.Index(td, ">")]
			if align == "center" || align == "end" {
				for name, tag := range map[string]string{"header": th, "cell": td} {
					if !strings.Contains(tag, "is-align-"+align) {
						t.Errorf("align %q width %q: the %s is not aligned: %s", align, width, name, tag)
					}
				}
			}
			if width != "" && !strings.Contains(td, "is-"+width) {
				t.Errorf("align %q width %q: the cell does not carry is-%s: %s", align, width, width, td)
			}
		}
	}
}
