package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
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

// Wrap renders the layout as the OUTERMOST shell: its content region is
// the page's single <main id="main-content">. If the layout is nil, Wrap
// returns the content unchanged.
func (l *Layout) Wrap(content render.HTML) render.HTML {
	return l.wrap(content, true)
}

// WrapNested renders the layout as an INNER shell — one composed inside
// another layout's <main> (e.g. a screen-group layout nested in the app's
// default layout). It contributes its sidebar + content region but NOT a
// <main> landmark, so the page keeps exactly one <main id="main-content">
// instead of emitting a duplicate (invalid id + a second landmark).
func (l *Layout) WrapNested(content render.HTML) render.HTML {
	return l.wrap(content, false)
}

func (l *Layout) wrap(content render.HTML, outermost bool) render.HTML {
	if l == nil {
		return content
	}

	var bodyChildren []render.HTML

	// Sidebar (optional).
	if l.Sidebar != nil {
		nav := html.Nav(html.NavConfig{Label: "Sidebar"}, l.Sidebar.Render())
		bodyChildren = append(bodyChildren, nav)
	}

	// Content region. Only the outermost layout owns the <main> landmark;
	// nested layouts emit a plain region so there's just one <main>.
	var contentRegion render.HTML
	if outermost {
		contentRegion = html.Main(html.MainConfig{}, content)
	} else {
		contentRegion = html.Div(html.DivConfig{Class: "layout-content"}, content)
	}
	bodyChildren = append(bodyChildren, contentRegion)

	// Layout body: sidebar + main.
	body := html.Div(html.DivConfig{Class: "layout-body"}, bodyChildren...)

	var wrapperChildren []render.HTML

	// Header (optional). Banner=true — the page-wide banner role lives
	// here; the component supplies inner content only.
	if l.Header != nil {
		header := html.Header(html.HeaderConfig{Banner: true}, l.Header.Render())
		wrapperChildren = append(wrapperChildren, header)
	}

	// Body.
	wrapperChildren = append(wrapperChildren, body)

	// Footer (optional). ContentInfo=true — page-wide footer role.
	if l.Footer != nil {
		footer := html.Footer(html.FooterConfig{ContentInfo: true}, l.Footer.Render())
		wrapperChildren = append(wrapperChildren, footer)
	}

	// Wrapper div.
	return html.Div(html.DivConfig{Class: "layout-" + l.Name}, wrapperChildren...)
}
