package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TimelineScreen struct{}

func (s *TimelineScreen) ScreenTitle() string { return "Timeline" }
func (s *TimelineScreen) ScreenDescription() string {
	return "Vertical event list on a rail."
}
func (s *TimelineScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TimelineScreen) Render() render.HTML {
	demo := ui.Timeline(ui.TimelineConfig{
		Events: []ui.TimelineEvent{
			{Title: "Order placed", Meta: "Mon 09:14", Variant: ui.TimelineInfo,
				Body: html.Paragraph(html.TextConfig{},
					render.Text("Customer placed order #4821 with 3 items. Total $148.20."))},
			{Title: "Payment captured", Meta: "Mon 09:14", Variant: ui.TimelineSuccess,
				Body: html.Paragraph(html.TextConfig{},
					render.Text("Stripe charge ch_3MZ… authorized and captured in the same request."))},
			{Title: "Address flagged for review", Meta: "Mon 09:18", Variant: ui.TimelineWarn,
				Body: html.Paragraph(html.TextConfig{},
					render.Text("AVS partial match. Manual review queued."))},
			{Title: "Shipped via UPS", Meta: "Tue 14:30", Variant: ui.TimelineSuccess,
				Body: html.Paragraph(html.TextConfig{},
					render.Text("Tracking 1Z999… ETA Thursday."))},
			{Title: "Delivered", Meta: "Thu 11:02", Variant: ui.TimelineSuccess},
		},
	})

	src := `ui.Timeline(ui.TimelineConfig{
    Events: []ui.TimelineEvent{
        {Title: "Order placed",      Meta: "Mon 09:14", Variant: ui.TimelineInfo,    Body: …},
        {Title: "Payment captured",  Meta: "Mon 09:14", Variant: ui.TimelineSuccess, Body: …},
        {Title: "Address flagged",   Meta: "Mon 09:18", Variant: ui.TimelineWarn,    Body: …},
        {Title: "Shipped via UPS",   Meta: "Tue 14:30", Variant: ui.TimelineSuccess, Body: …},
        {Title: "Delivered",         Meta: "Thu 11:02", Variant: ui.TimelineSuccess},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Timeline")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Vertical event list — order history, audit logs, deployment trail. Renders as a semantic <ol> with the rail + dot drawn via CSS pseudo-elements so screen readers hear a clean ordered list, not visual chrome.")),
		demoFrame(demo, src),
	)
}
