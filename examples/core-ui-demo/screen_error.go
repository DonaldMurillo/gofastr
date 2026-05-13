package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ErrorBoundaryDemoScreen demonstrates the ErrorBoundary feature.
type ErrorBoundaryDemoScreen struct{}

func (s *ErrorBoundaryDemoScreen) ScreenTitle() string        { return "Error Boundary" }
func (s *ErrorBoundaryDemoScreen) ScreenDescription() string  { return "Error boundary demo" }
func (s *ErrorBoundaryDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ErrorBoundaryDemoScreen) Render() render.HTML {
	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Error Boundary Demo")),
		html.Paragraph(html.TextConfig{}, render.Text("Error boundaries catch panics in component Render() and show a fallback UI.")),
		html.Section(
			html.SectionConfig{Label: "Safe component"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Working Component")),
			html.Paragraph(html.TextConfig{}, render.Text("This component renders normally.")),
		),
		html.Section(
			html.SectionConfig{Label: "Broken component with error boundary"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Panicking Component")),
			renderHTMLWithErrorBoundary(),
		),
		html.Paragraph(html.TextConfig{}, render.Text("The red box above is the default error boundary fallback. Components can implement ErrorBoundary for custom fallback UI.")),
	)
}

type brokenComponent struct{}

func (b *brokenComponent) Render() render.HTML {
	panic("deliberate panic for error boundary demo")
}

func renderHTMLWithErrorBoundary() render.HTML {
	rendered, err := component.SafeRender(&brokenComponent{})
	if err != nil {
		return html.Div(html.DivConfig{Class: "error-boundary-result"}, rendered)
	}
	return rendered
}
