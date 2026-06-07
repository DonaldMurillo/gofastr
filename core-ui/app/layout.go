package app

import (
	"context"

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
// returns the content unchanged. Chrome renders with a background context;
// use WrapCtx to give context-aware chrome the request context.
func (l *Layout) Wrap(content render.HTML) render.HTML {
	return l.wrap(context.Background(), content, true)
}

// WrapCtx is Wrap with an explicit context, threaded into the chrome
// (header/sidebar/footer) so a ContextComponent in any slot renders with the
// live request context (auth-aware nav, current tenant, etc.).
func (l *Layout) WrapCtx(ctx context.Context, content render.HTML) render.HTML {
	return l.wrap(ctx, content, true)
}

// WrapNested renders the layout as an INNER shell — one composed inside
// another layout's <main> (e.g. a screen-group layout nested in the app's
// default layout). It contributes its sidebar + content region but NOT a
// <main> landmark, so the page keeps exactly one <main id="main-content">
// instead of emitting a duplicate (invalid id + a second landmark).
func (l *Layout) WrapNested(content render.HTML) render.HTML {
	return l.wrap(context.Background(), content, false)
}

// WrapNestedCtx is WrapNested with an explicit context threaded into chrome.
func (l *Layout) WrapNestedCtx(ctx context.Context, content render.HTML) render.HTML {
	return l.wrap(ctx, content, false)
}

func (l *Layout) wrap(ctx context.Context, content render.HTML, outermost bool) render.HTML {
	if l == nil {
		return content
	}

	var bodyChildren []render.HTML

	// Sidebar (optional). Rendered ctx-aware so context-aware chrome works;
	// SafeRenderCtx falls back to Render() for plain components and recovers
	// panics (an errored slot renders empty rather than killing the page).
	if l.Sidebar != nil {
		inner, _ := component.SafeRenderCtx(ctx, l.Sidebar)
		nav := html.Nav(html.NavConfig{Label: "Sidebar"}, inner)
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
		inner, _ := component.SafeRenderCtx(ctx, l.Header)
		header := html.Header(html.HeaderConfig{Banner: true}, inner)
		wrapperChildren = append(wrapperChildren, header)
	}

	// Body.
	wrapperChildren = append(wrapperChildren, body)

	// Footer (optional). ContentInfo=true — page-wide footer role.
	if l.Footer != nil {
		inner, _ := component.SafeRenderCtx(ctx, l.Footer)
		footer := html.Footer(html.FooterConfig{ContentInfo: true}, inner)
		wrapperChildren = append(wrapperChildren, footer)
	}

	// Wrapper div.
	return html.Div(html.DivConfig{Class: "layout-" + l.Name}, wrapperChildren...)
}
