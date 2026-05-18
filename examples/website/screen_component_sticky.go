package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type StickyScreen struct{}

func (s *StickyScreen) ScreenTitle() string        { return "Sticky" }
func (s *StickyScreen) ScreenDescription() string  { return "position:sticky layout primitive with theme-token z-index." }
func (s *StickyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *StickyScreen) Render() render.HTML {
	topDemo := ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			render.Text("I stick to the top when you scroll past me"),
		),
	)
	topSrc := `ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
    html.Div(html.DivConfig{Class: "ui-box--surface"}, ...),
)`

	bottomDemo := ui.Sticky(ui.StickyConfig{Edge: ui.StickyBottom},
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			render.Text("I stick to the bottom"),
		),
	)
	bottomSrc := `ui.Sticky(ui.StickyConfig{Edge: ui.StickyBottom}, ...)`

	offsetDemo := ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop, Offset: ui.StickyOffsetMd},
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			render.Text("Sticky with medium offset from top"),
		),
	)
	offsetSrc := `ui.Sticky(ui.StickyConfig{
    Edge:   ui.StickyTop,
    Offset: ui.StickyOffsetMd,
}, ...)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Sticky")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"position:sticky layout wrapper that pins content to a viewport edge on scroll. Uses theme tokens for z-index so sticky elements layer consistently with modals, drawers, and other surfaces.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stick to top")),
		demoFrame(topDemo, topSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stick to bottom")),
		demoFrame(bottomDemo, bottomSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With offset")),
		render.Text("Offset presets: 0 (default), sm, md, lg, xl — mapped to spacing tokens."),
		demoFrame(offsetDemo, offsetSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Config options")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Edge — StickyTop | StickyBottom (default: top)")),
			render.Tag("li", nil, render.Text("Offset — 0 | sm | md | lg | xl (default: 0)")),
			render.Tag("li", nil, render.Text("ZIndexTier — any tier name from theme tokens (default: \"sticky\")")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Notes")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Pure CSS — no JavaScript runtime module.")),
			render.Tag("li", nil, render.Text("Z-index uses --z-index-{tier} token, defaulting to 100.")),
			render.Tag("li", nil, render.Text("A subtle bottom border appears via ::after pseudo-element when the element is stuck.")),
		),
	)
}
