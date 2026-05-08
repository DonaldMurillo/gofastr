package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// ErrorBoundaryDemoScreen demonstrates the ErrorBoundary feature.
type ErrorBoundaryDemoScreen struct{}

func (s *ErrorBoundaryDemoScreen) ScreenTitle() string        { return "Error Boundary" }
func (s *ErrorBoundaryDemoScreen) ScreenDescription() string  { return "Error boundary demo" }
func (s *ErrorBoundaryDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ErrorBoundaryDemoScreen) Render() render.HTML {
	return elements.Div(elements.DivConfig{},
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Error Boundary Demo")),
		elements.Paragraph(elements.TextConfig{}, render.Text("Error boundaries catch panics in component Render() and show a fallback UI.")),
		elements.Section(
			elements.SectionConfig{Label: "Safe component"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Working Component")),
			elements.Paragraph(elements.TextConfig{}, render.Text("This component renders normally.")),
		),
		elements.Section(
			elements.SectionConfig{Label: "Broken component with error boundary"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Panicking Component")),
			renderHTMLWithErrorBoundary(),
		),
		elements.Paragraph(elements.TextConfig{}, render.Text("The red box above is the default error boundary fallback. Components can implement ErrorBoundary for custom fallback UI.")),
	)
}

type brokenComponent struct{}

func (b *brokenComponent) Render() render.HTML {
	panic("deliberate panic for error boundary demo")
}

func renderHTMLWithErrorBoundary() render.HTML {
	html, err := component.SafeRender(&brokenComponent{})
	if err != nil {
		return elements.Div(elements.DivConfig{Class: "error-boundary-result"}, html)
	}
	return html
}
