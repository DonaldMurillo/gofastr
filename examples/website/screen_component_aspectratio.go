package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type AspectRatioScreen struct{}

func (s *AspectRatioScreen) ScreenTitle() string        { return "AspectRatio" }
func (s *AspectRatioScreen) ScreenDescription() string  { return "Pure-CSS aspect-ratio wrapper for responsive content." }
func (s *AspectRatioScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AspectRatioScreen) Render() render.HTML {
	// Use a colored box inside each aspect ratio to show the proportions
	innerBox := func(text string) render.HTML {
		return html.Div(html.DivConfig{
			Class: "ui-box--surface",
			Attrs: map[string]string{"class": "demo-ar-box"},
		},
			render.Text(text),
		)
	}

	ratio1_1 := ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio1_1}, innerBox("1:1"))
	ratio4_3 := ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio4_3}, innerBox("4:3"))
	ratio16_9 := ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio16_9}, innerBox("16:9"))
	ratio21_9 := ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio21_9}, innerBox("21:9"))
	ratio3_4 := ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio3_4}, innerBox("3:4"))

	src := `ui.AspectRatioComponent(ui.AspectRatioConfig{
    Ratio: ui.AspectRatio16_9,
}, child)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("AspectRatio")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Pure-CSS aspect-ratio wrapper that prevents layout shift for images, videos, and embeds whose dimensions aren't known at SSR time. The child is absolutely positioned to fill the box.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Available ratios")),
		render.Tag("div", map[string]string{"class": "demo-ar-grid"},
			ratio1_1,
			ratio4_3,
			ratio16_9,
			ratio21_9,
			ratio3_4,
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Usage")),
		demoFrame(ratio16_9, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Ratios")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("AspectRatio1_1 — square")),
			render.Tag("li", nil, render.Text("AspectRatio4_3 — classic TV")),
			render.Tag("li", nil, render.Text("AspectRatio16_9 — widescreen (default for video)")),
			render.Tag("li", nil, render.Text("AspectRatio21_9 — ultrawide")),
			render.Tag("li", nil, render.Text("AspectRatio3_4 — portrait")),
			render.Tag("li", nil, render.Text("AspectRatio3_2 — classic photo")),
			render.Tag("li", nil, render.Text("AspectRatio2_3 — tall portrait")),
			render.Tag("li", nil, render.Text("AspectRatioAuto — no fixed ratio, child sizes naturally")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Notes")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Pure CSS — no JavaScript runtime module.")),
			render.Tag("li", nil, render.Text("Child is absolutely positioned (position: absolute; inset: 0).")),
			render.Tag("li", nil, render.Text("Prevents CLS (Cumulative Layout Shift) for lazy-loaded content.")),
			render.Tag("li", nil, render.Text("Works with images, videos, iframes, or any content.")),
		),
	)
}
