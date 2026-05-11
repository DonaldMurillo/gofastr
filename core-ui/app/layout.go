package app

import (
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// Layout defines shared chrome that wraps screens.
// A layout has slots for header, sidebar, footer, and content.
type Layout struct {
	// Name identifies the layout (used in CSS class).
	Name string
	// Header is an optional header component rendered with role="banner".
	Header component.Component
	// Sidebar is an optional sidebar component rendered as navigation.
	Sidebar component.Component
	// Footer is an optional footer component rendered with role="contentinfo".
	Footer component.Component
}

// NewLayout creates a named layout.
func NewLayout(name string) *Layout {
	return &Layout{Name: name}
}

// WithHeader sets the layout header and returns the layout for chaining.
func (l *Layout) WithHeader(c component.Component) *Layout {
	l.Header = c
	return l
}

// WithSidebar sets the layout sidebar and returns the layout for chaining.
func (l *Layout) WithSidebar(c component.Component) *Layout {
	l.Sidebar = c
	return l
}

// WithFooter sets the layout footer and returns the layout for chaining.
func (l *Layout) WithFooter(c component.Component) *Layout {
	l.Footer = c
	return l
}

// Wrap renders the layout wrapping the given content HTML.
// If the layout is nil, Wrap returns the content unchanged.
func (l *Layout) Wrap(content render.HTML) render.HTML {
	if l == nil {
		return content
	}

	var bodyChildren []render.HTML

	// Sidebar (optional).
	if l.Sidebar != nil {
		nav := html.Nav(html.NavConfig{Label: "Sidebar"}, l.Sidebar.Render())
		bodyChildren = append(bodyChildren, nav)
	}

	// Main content.
	mainContent := html.Main(html.MainConfig{}, content)
	bodyChildren = append(bodyChildren, mainContent)

	// Layout body: sidebar + main.
	body := html.Div(html.DivConfig{Class: "layout-body"}, bodyChildren...)

	var wrapperChildren []render.HTML

	// Header (optional).
	if l.Header != nil {
		header := html.Header(html.HeaderConfig{}, l.Header.Render())
		wrapperChildren = append(wrapperChildren, header)
	}

	// Body.
	wrapperChildren = append(wrapperChildren, body)

	// Footer (optional).
	if l.Footer != nil {
		footer := html.Footer(html.FooterConfig{}, l.Footer.Render())
		wrapperChildren = append(wrapperChildren, footer)
	}

	// Wrapper div.
	return html.Div(html.DivConfig{Class: "layout-" + l.Name}, wrapperChildren...)
}
