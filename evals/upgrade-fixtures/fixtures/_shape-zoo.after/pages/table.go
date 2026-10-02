package pages

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type tableConfig = ui.DataTableConfig

func table() render.HTML {
	return ui.DataTable(tableConfig{})
}
