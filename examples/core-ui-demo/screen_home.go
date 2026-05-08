package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
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

	return elements.Div(elements.DivConfig{},
		hero.Render(),
		elements.Section(
			elements.SectionConfig{Label: "Interactive counter"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Try It Live")),
			elements.Paragraph(elements.TextConfig{}, render.Text("Click the buttons — the Go counter compiles to JS that runs in your browser.")),
			counter.Render(),
		),
		elements.Section(
			elements.SectionConfig{Label: "Overlay demos"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Overlays")),
			elements.Paragraph(elements.TextConfig{}, render.Text("Drawers, sheets, and dialogs — all powered by the runtime overlay manager.")),
			elements.Div(elements.DivConfig{Class: "overlay-demo-buttons"},
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('drawer','/demo-drawer')"}, render.Text("Open Drawer")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('sheet','/demo-sheet')"}, render.Text("Open Sheet")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('dialog','/confirm-dialog')"}, render.Text("Open Dialog")),
			),
		),
		elements.Section(
			elements.SectionConfig{Label: "Featured products"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Featured Products")),
			featuredProductCards(),
		),
	)
}
