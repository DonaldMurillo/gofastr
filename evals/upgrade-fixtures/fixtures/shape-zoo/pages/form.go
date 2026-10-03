package pages

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const unsafeAction = "javascript:void(0)"

func forms() []render.HTML {
	return []render.HTML{
		ui.Form(ui.FormConfig{Action: unsafeAction}),          // zoo:hit action
		ui.Form(ui.FormConfig{Action: "JaVaScRiPt:alert(1)"}), // zoo:hit action
		ui.Form(ui.FormConfig{Action: "/save"}),
	}
}
