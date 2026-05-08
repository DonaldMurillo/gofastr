package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// ErrorBoundaryDemoScreen demonstrates the ErrorBoundary feature.
// It includes a component that deliberately panics to show the fallback UI.
type ErrorBoundaryDemoScreen struct{}

func (s *ErrorBoundaryDemoScreen) ScreenTitle() string        { return "Error Boundary" }
func (s *ErrorBoundaryDemoScreen) ScreenDescription() string  { return "Error boundary demo" }
func (s *ErrorBoundaryDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ErrorBoundaryDemoScreen) Render() render.HTML {
	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Error Boundary Demo")),
		elements.Paragraph(nil, render.Text("Error boundaries catch panics in component Render() and show a fallback UI.")),
		elements.Section(
			elements.Aria("label", "Safe component"),
			elements.Heading(2, nil, render.Text("Working Component")),
			elements.Paragraph(nil, render.Text("This component renders normally.")),
		),
		elements.Section(
			elements.Aria("label", "Broken component with error boundary"),
			elements.Heading(2, nil, render.Text("Panicking Component")),
			renderHTMLWithErrorBoundary(),
		),
		elements.Paragraph(nil, render.Text("The red box above is the default error boundary fallback. Components can implement ErrorBoundary for custom fallback UI.")),
	)
}

// brokenComponent deliberately panics to demonstrate error boundaries.
type brokenComponent struct{}

func (b *brokenComponent) Render() render.HTML {
	panic("deliberate panic for error boundary demo")
}

// renderHTMLWithErrorBoundary uses SafeRender to catch the panic.
func renderHTMLWithErrorBoundary() render.HTML {
	html, err := component.SafeRender(&brokenComponent{})
	if err != nil {
		return elements.Div(elements.Attrs{"class": "error-boundary-result"}, html)
	}
	return html
}
