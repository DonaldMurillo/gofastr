package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SkipLinkScreen struct{}

func (s *SkipLinkScreen) ScreenTitle() string        { return "SkipLink" }
func (s *SkipLinkScreen) ScreenDescription() string  { return "WCAG 2.4.1 skip-navigation link for keyboard users." }
func (s *SkipLinkScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SkipLinkScreen) Render() render.HTML {
	demo := ui.SkipLink(ui.SkipLinkConfig{})
	src := `ui.SkipLink(ui.SkipLinkConfig{})`

	demoCustom := ui.SkipLink(ui.SkipLinkConfig{
		Target: "main-content",
		Text:   "Skip to content",
	})
	srcCustom := `ui.SkipLink(ui.SkipLinkConfig{
    Target: "main-content",
    Text:   "Skip to content",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("SkipLink")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Visually-hidden link that becomes visible on keyboard focus, letting keyboard users jump past repetitive navigation to the main content area. Required for WCAG 2.1 Level A (criterion 2.4.1 \"Bypass Blocks\"). Place as the first element inside <body>.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Default")),
		render.Text("Tab into the page — the link appears in the top-left corner."),
		demoFrame(demo, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Custom target & text")),
		demoFrame(demoCustom, srcCustom),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("How it works")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Positioned off-screen (left: -9999px) by default.")),
			render.Tag("li", nil, render.Text("On :focus, moves to a fixed position in the top-left corner with high z-index.")),
			render.Tag("li", nil, render.Text("Clicking navigates to the target element via a fragment link (#main-content).")),
			render.Tag("li", nil, render.Text("Pure SSR — no JavaScript runtime module needed.")),
		),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Satisfies WCAG 2.4.1 Bypass Blocks (Level A).")),
			render.Tag("li", nil, render.Text("Visible on focus only — no visual clutter for mouse users.")),
			render.Tag("li", nil, render.Text("Uses theme colors for the focused state.")),
		),
	)
}
