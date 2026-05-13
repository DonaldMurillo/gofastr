package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HomeScreen is the landing page with hero, counter, and featured products.
type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Home" }
func (s *HomeScreen) ScreenDescription() string  { return "GoFastr Demo Homepage" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	hero := &HeroComponent{
		Title:    "Welcome to GoFastr",
		Subtitle: "Build fast, accessible web applications in Go.",
		CTAText:  "Browse Products",
		CTALink:  "/products",
	}

	counter := &CounterComponent{ID: "home-counter", Count: 0}

	return html.Div(html.DivConfig{},
		hero.Render(),
		html.Section(
			html.SectionConfig{Label: "Interactive counter"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Try It Live")),
			html.Paragraph(html.TextConfig{}, render.Text("Click the buttons — the Go counter compiles to JS that runs in your browser.")),
			counter.Render(),
		),
		html.Section(
			html.SectionConfig{Label: "Overlay demos"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Overlays")),
			html.Paragraph(html.TextConfig{}, render.Text("Drawers, sheets, and dialogs — all powered by the runtime overlay manager.")),
			html.Div(html.DivConfig{Class: "overlay-demo-buttons"},
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('drawer','/demo-drawer')"}, render.Text("Open Drawer")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('sheet','/demo-sheet')"}, render.Text("Open Sheet")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('dialog','/confirm-dialog')"}, render.Text("Open Dialog")),
			),
		),
		html.Section(
			html.SectionConfig{Label: "Featured products"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Featured Products")),
			featuredProductCards(),
		),
	)
}
