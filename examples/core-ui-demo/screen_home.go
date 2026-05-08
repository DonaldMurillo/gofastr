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

	return elements.Div(nil,
		hero.Render(),
		elements.Section(
			elements.Aria("label", "Interactive counter"),
			elements.Heading(2, nil, render.Text("Try It Live")),
			elements.Paragraph(nil, render.Text("Click the buttons — the Go counter compiles to JS that runs in your browser.")),
			counter.Render(),
		),
		elements.Section(
			elements.Aria("label", "Overlay demos"),
			elements.Heading(2, nil, render.Text("Overlays")),
			elements.Paragraph(nil, render.Text("Drawers, sheets, and dialogs — all powered by the runtime overlay manager.")),
			elements.Div(elements.Attrs{"class": "overlay-demo-buttons"},
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('drawer','/demo-drawer')"}, render.Text("Open Drawer")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('sheet','/demo-sheet')"}, render.Text("Open Sheet")),
				render.Tag("button", map[string]string{"class": "cta-button", "onclick": "G.openOverlay('dialog','/confirm-dialog')"}, render.Text("Open Dialog")),
			),
		),
		elements.Section(
			elements.Aria("label", "Featured products"),
			elements.Heading(2, nil, render.Text("Featured Products")),
			featuredProductCards(),
		),
	)
}
