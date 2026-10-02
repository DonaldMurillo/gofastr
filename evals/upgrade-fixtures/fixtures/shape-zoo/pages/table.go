package pages

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A package-local alias: at the new version the compile error names
// tableConfig, not ui.DataTableConfig.
type tableConfig = ui.DataTableConfig

func table() render.HTML {
	return ui.DataTable(tableConfig{SortHrefPattern: "?sort=%s&dir=%s"}) // zoo:hit sorthref
}
