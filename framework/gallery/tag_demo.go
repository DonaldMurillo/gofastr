package gallery

import (
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// TagDemo renders the gallery's tag strip, optionally omitting its dismissed tag.
func TagDemo(dismissed bool) render.HTML {
	tags := []render.HTML{
		ui.Tag(ui.TagConfig{Label: "neutral"}),
		ui.Tag(ui.TagConfig{Label: "success", Variant: ui.StatusSuccess}),
		ui.Tag(ui.TagConfig{Label: "warning", Variant: ui.StatusWarning}),
		ui.Tag(ui.TagConfig{Label: "danger", Variant: ui.StatusDanger}),
		ui.Tag(ui.TagConfig{Label: "info", Variant: ui.StatusInfo}),
	}
	if !dismissed {
		tags = append(tags, ui.Tag(ui.TagConfig{Label: "beta", Dismiss: "/components/tag?dismissed=beta",
			DismissLabel: "Remove beta", DismissMethod: "POST",
			Island: headless.Island{Endpoint: "/components/tag", Signal: "tag-demo"}}))
	}
	return interactive.BindHTML(row(tags...), "tag-demo")
}
