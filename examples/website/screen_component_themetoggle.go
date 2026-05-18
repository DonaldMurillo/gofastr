package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ThemeToggleScreen struct{}

func (s *ThemeToggleScreen) ScreenTitle() string        { return "ThemeToggle" }
func (s *ThemeToggleScreen) ScreenDescription() string  { return "Dark/light mode toggle button with colorscheme.js integration." }
func (s *ThemeToggleScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ThemeToggleScreen) Render() render.HTML {
	iconDemo := ui.ThemeToggle(ui.ThemeToggleConfig{})
	iconSrc := `ui.ThemeToggle(ui.ThemeToggleConfig{})`

	labelDemo := ui.ThemeToggle(ui.ThemeToggleConfig{
		Variant:    ui.ThemeToggleLabel,
		LightLabel: "Light",
		DarkLabel:  "Dark",
	})
	labelSrc := `ui.ThemeToggle(ui.ThemeToggleConfig{
    Variant:    ui.ThemeToggleLabel,
    LightLabel: "Light",
    DarkLabel:  "Dark",
})`

	pillDemo := ui.ThemeToggle(ui.ThemeToggleConfig{
		Variant:    ui.ThemeTogglePill,
		LightLabel: "Light",
		AutoLabel:  "Auto",
		DarkLabel:  "Dark",
	})
	pillSrc := `ui.ThemeToggle(ui.ThemeToggleConfig{
    Variant:    ui.ThemeTogglePill,
    LightLabel: "Light",
    AutoLabel:  "Auto",
    DarkLabel:  "Dark",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("ThemeToggle")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Dark/light mode toggle that persists the user's choice to localStorage and applies it immediately via the colorscheme.js bootstrap. No page reload needed — all theme tokens update in place.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Icon variant (default)")),
		render.Text("Shows a sun icon in dark mode, moon icon in light mode. Click cycles: dark → light → auto."),
		demoFrame(iconDemo, iconSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Label variant")),
		render.Text("Shows the current mode as text."),
		demoFrame(labelDemo, labelSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Pill variant")),
		render.Text("Segmented control with Light / Auto / Dark options. The active option is highlighted."),
		demoFrame(pillDemo, pillSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("How it works")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("On click, calls window.__gofastr_colorScheme.set() from the colorscheme.js bootstrap.")),
			render.Tag("li", nil, render.Text("The scheme is persisted to localStorage[\"gofastr.colorScheme\"].")),
			render.Tag("li", nil, render.Text("colorscheme.js sets data-color-scheme=\"dark|light\" on <html>, which cascades all theme tokens.")),
			render.Tag("li", nil, render.Text("CSS handles showing the correct icon/label based on [data-color-scheme].")),
		),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Icon variant: aria-label=\"Toggle color scheme\".")),
			render.Tag("li", nil, render.Text("Pill variant: role=\"radiogroup\" with aria-label, options use role=\"radio\" + aria-pressed.")),
			render.Tag("li", nil, render.Text("Touch-target compliant (44×44 minimum).")),
		),
	)
}
