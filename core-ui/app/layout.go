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
	// Container, when true, constrains the whole layout (header + content +
	// footer) to a centered max-width column with comfortable gutters and
	// vertical rhythm, the calm editorial shape for marketing/content pages.
	// Width is themeable via --ui-layout-container-width. Off by default.
	Container bool
	// StickyHeader, when true, makes the layout's own <header> wrapper stick
	// to the top of the viewport for the whole page. A header component
	// cannot do this itself: the layout renders it inside the wrapper, and a
	// sticky element only travels inside its parent's box, so its own
	// position:sticky lasts exactly the header's height. Background is
	// themeable via --ui-layout-header-bg, stacking via the --z-sticky theme
	// layer. Off by default.
	StickyHeader bool
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

// WithContainer constrains the layout to a centered max-width column
// (header + content + footer share one measure) and returns the layout for
// chaining. The calm editorial shape for marketing/content pages.
func (l *Layout) WithContainer() *Layout {
	l.Container = true
	return l
}

// WithStickyHeader makes the layout's own <header> wrapper stick to the top
// of the viewport for the whole page and returns the layout for chaining.
// Use it instead of giving the header component position: sticky — a sticky
// element only travels inside its parent's box, and the component's parent
// is the wrapper, whose height is the header itself.
func (l *Layout) WithStickyHeader() *Layout {
	l.StickyHeader = true
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

// WrapNested renders the layout as an INNER shell, one composed inside
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
	return l.wrapLayer(ctx, content, outermost, l.selfKey())
}

// selfKey is the layer key a layout carries when it is wrapped directly
// (Wrap/WrapCtx/WrapNested*) rather than through a resolved chain, the
// plain-layer form of LayoutLayer.Key.
func (l *Layout) selfKey() string {
	if l == nil || l.Name == "" {
		return ""
	}
	return "l:" + l.Name
}

// wrapLayer renders one level of a layout chain. outermost decides <main>
// ownership (the page has exactly one <main id="main-content">, owned by
// layer 0). key is the layer identity emitted as data-fui-layout-key on
// the wrapper and data-fui-layout-slot on the content cell, the markers
// the runtime walks to find the deepest layer shared with a navigation
// target and to address the swap target without structural heuristics.
func (l *Layout) wrapLayer(ctx context.Context, content render.HTML, outermost bool, key string) render.HTML {
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
	// nested layouts emit a plain region so there's just one <main>. Either
	// way the cell carries data-fui-layout-slot so the runtime swaps a
	// layer's content by key instead of guessing at structure.
	var slotAttrs html.Attrs
	if key != "" {
		slotAttrs = html.Attrs{"data-fui-layout-slot": key}
	}
	var contentRegion render.HTML
	if outermost {
		contentRegion = html.Main(html.MainConfig{ExtraAttrs: slotAttrs}, content)
	} else {
		// tabindex="-1" mirrors html.Main: after a layer swap the runtime
		// moves focus onto the fresh cell so keyboard users are not
		// stranded on a detached node.
		if slotAttrs != nil {
			slotAttrs["tabindex"] = "-1"
		}
		contentRegion = html.Div(html.DivConfig{Class: "layout-content", ExtraAttrs: slotAttrs}, content)
	}
	bodyChildren = append(bodyChildren, contentRegion)

	// Layout body: sidebar + main.
	body := html.Div(html.DivConfig{Class: "layout-body"}, bodyChildren...)

	var wrapperChildren []render.HTML

	// Header (optional). Banner=true, the page-wide banner role lives
	// here; the component supplies inner content only.
	if l.Header != nil {
		inner, _ := component.SafeRenderCtx(ctx, l.Header)
		header := html.Header(html.HeaderConfig{Banner: true}, inner)
		wrapperChildren = append(wrapperChildren, header)
	}

	// Body.
	wrapperChildren = append(wrapperChildren, body)

	// Footer (optional). ContentInfo=true, page-wide footer role.
	if l.Footer != nil {
		inner, _ := component.SafeRenderCtx(ctx, l.Footer)
		footer := html.Footer(html.FooterConfig{ContentInfo: true}, inner)
		wrapperChildren = append(wrapperChildren, footer)
	}

	// Wrapper div. Modifier classes let the shared layout CSS (app.LayoutBaseCSS)
	// style the shape generically, `layout--contained` for the centered column,
	// `layout--has-sidebar` for the padded sidebar shell, so consumers never
	// hand-roll per-layout-name CSS.
	cls := "layout-" + l.Name
	if l.Container {
		cls += " layout--contained"
	}
	if l.Sidebar != nil {
		cls += " layout--has-sidebar"
	}
	if l.StickyHeader {
		cls += " layout--sticky-header"
	}
	// Every layer is marked: data-fui-layout names the shell (CSS/debug
	// contract), data-fui-layout-key is the identity the runtime compares
	// to find the deepest layer shared with a navigation target. Nested
	// layers used to be unmarked, which forced the runtime onto class-name
	// heuristics and a screen-group precedence hack.
	attrs := html.Attrs{}
	if l.Name != "" {
		attrs["data-fui-layout"] = l.Name
	}
	if key != "" {
		attrs["data-fui-layout-key"] = key
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	return html.Div(html.DivConfig{Class: cls, ExtraAttrs: attrs}, wrapperChildren...)
}
