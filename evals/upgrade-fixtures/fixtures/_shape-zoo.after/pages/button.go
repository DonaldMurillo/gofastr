package pages

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func saveButton() render.HTML {
	return ui.Button(ui.ButtonConfig{Label: "Save", Disabled: true, ExtraAttrs: saveAttrs})
}

func labelledButton() render.HTML {
	return ui.Button(ui.ButtonConfig{Label: "Go", ExtraAttrs: html.Attrs{"aria-disabled": "false"}})
}
