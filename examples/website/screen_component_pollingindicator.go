package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type PollingIndicatorScreen struct{}

func (s *PollingIndicatorScreen) ScreenTitle() string {
	return "PollingIndicator"
}
func (s *PollingIndicatorScreen) ScreenDescription() string {
	return "Pulsing dot + label that confirms a polling RPC or live-update pipeline is firing."
}
func (s *PollingIndicatorScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PollingIndicatorScreen) Render() render.HTML {
	live := ui.PollingIndicator(ui.PollingIndicatorConfig{})
	srcLive := `ui.PollingIndicator(ui.PollingIndicatorConfig{})`

	custom := ui.PollingIndicator(ui.PollingIndicatorConfig{Label: "Syncing"})
	srcCustom := `ui.PollingIndicator(ui.PollingIndicatorConfig{Label: "Syncing"})`

	paused := ui.PollingIndicator(ui.PollingIndicatorConfig{Label: "Paused", Paused: true})
	srcPaused := `ui.PollingIndicator(ui.PollingIndicatorConfig{Label: "Paused", Paused: true})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("PollingIndicator")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Tiny pulsing dot + label. Pair with data-fui-rpc-trigger=\"input\" patterns to give users feedback that a live-search or live-validate is actually running. Pure CSS — no runtime module needed.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Default (Live)")),
		demoFrame(live, srcLive),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Custom label")),
		demoFrame(custom, srcCustom),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Paused variant")),
		render.Tag("p", nil, render.Text(
			"Paused freezes the pulse and dims the dot. Use when the upstream poll is paused or completed.")),
		demoFrame(paused, srcPaused),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("role=\"status\" + aria-live=\"polite\" so label changes are announced.")),
			render.Tag("li", nil, render.Text("Pulse animation respects prefers-reduced-motion.")),
		),
	)
}
