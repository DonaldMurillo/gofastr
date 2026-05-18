package main

import (
	"fmt"

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
	// Build a scrollable demo container with tall content and a sticky header
	// so users can see the element actually stick.
	scrollBox := func(height, stickyEdge string, offset ui.StickyOffset, children ...render.HTML) render.HTML {
		edge := ui.StickyTop
		if stickyEdge == "bottom" {
			edge = ui.StickyBottom
		}
		var inner []render.HTML
		inner = append(inner, ui.Sticky(ui.StickyConfig{
			Edge:   edge,
			Offset: offset,
		}, children...))
		// Tall filler lines so the container scrolls
		for i := 0; i < 20; i++ {
			inner = append(inner, html.Paragraph(html.TextConfig{},
				render.Text("Scroll down to see the sticky element stay in place...")))
		}
		return render.Tag("div", map[string]string{
			"class": "demo-sticky-scroll",
		}, inner...)
	}

	topDemo := scrollBox("400px", "top", ui.StickyOffsetNone,
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			html.Paragraph(html.TextConfig{}, render.Text("📌 I stick to the top! Keep scrolling...")),
		),
	)
	topSrc := `ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
    html.Div(html.DivConfig{Class: "ui-box--surface"},
        html.Paragraph(html.TextConfig{}, render.Text("📌 I stick to the top!")),
    ),
)`

	bottomDemo := scrollBox("400px", "bottom", ui.StickyOffsetNone,
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			html.Paragraph(html.TextConfig{}, render.Text("📌 I stick to the bottom! Keep scrolling...")),
		),
	)
	bottomSrc := `ui.Sticky(ui.StickyConfig{Edge: ui.StickyBottom},
    html.Div(html.DivConfig{Class: "ui-box--surface"},
        html.Paragraph(html.TextConfig{}, render.Text("📌 I stick to the bottom!")),
    ),
)`

	offsetDemo := scrollBox("400px", "top", ui.StickyOffsetMd,
		html.Div(html.DivConfig{Class: "ui-box--surface"},
			html.Paragraph(html.TextConfig{}, render.Text("📌 Sticky with medium offset from top")),
		),
	)
	offsetSrc := `ui.Sticky(ui.StickyConfig{
    Edge:   ui.StickyTop,
    Offset: ui.StickyOffsetMd,
}, ...)`

	// Real-world example: sticky toolbar above a long list
	toolbarDemo := render.Tag("div", map[string]string{"class": "demo-sticky-scroll"}, []render.HTML{
		ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
			html.Div(html.DivConfig{Class: "ui-box--surface"},
				html.Paragraph(html.TextConfig{},
					render.Text("🔧 Toolbar — stays visible while you scroll the items below")),
			),
		),
		render.Tag("ul", nil, func() []render.HTML {
			var items []render.HTML
			for i := 1; i <= 30; i++ {
				items = append(items, render.Tag("li", nil,
					render.Text(fmt.Sprintf("Item #%d — keep scrolling, the toolbar stays put", i))))
			}
			return items
		}()...),
	}...)
	toolbarSrc := `ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
    toolbarOrActions,
)
// ... long scrollable content below ...`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Sticky")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"position:sticky layout wrapper that pins content to a viewport edge on scroll. Uses theme tokens for z-index so sticky elements layer consistently with modals, drawers, and other surfaces. Scroll inside each box below to see it in action.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stick to top")),
		render.Tag("p", nil, render.Text("Scroll down inside the box — the bar stays pinned to the top edge.")),
		demoFrame(topDemo, topSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stick to bottom")),
		render.Tag("p", nil, render.Text("The bar pins to the bottom of the scroll container instead.")),
		demoFrame(bottomDemo, bottomSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With offset")),
		render.Tag("p", nil, render.Text("Offset presets (sm, md, lg, xl) add space between the stuck element and the edge.")),
		demoFrame(offsetDemo, offsetSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Real-world: sticky toolbar")),
		render.Tag("p", nil, render.Text("A common pattern — action toolbar stays visible over a long list.")),
		demoFrame(toolbarDemo, toolbarSrc),

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

