package pages

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func forms() []render.HTML {
	return []render.HTML{
		ui.Form(ui.FormConfig{Action: "/save"}),
	}
}
