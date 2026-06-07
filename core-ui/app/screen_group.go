package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ScreenGroup defines a shared layout that wraps every child screen.
// Screen groups nest: navigating between siblings inside the same group
// swaps only the inner content region, not the layout shell.
//
// Usage:
//
//	sidebar := app.NewStaticComponent(sidebarHTML)
//	group := app.NewScreenGroup("/settings", app.NewLayout("settings").WithSidebar(sidebar))
//	group.Screen(screen1, nil)
//	group.Screen(screen2, nil)
//	appRouter.ScreenGroup(group)
type ScreenGroup struct {
	prefix    string
	layout    *Layout
	policies  []Policy
	screens   []*Screen
	children  []*ScreenGroup // nested sub-groups
	parent    *ScreenGroup
	parentApp *Router
}

// StaticComponent wraps raw HTML as a component.Component. Useful for
// layout regions that don't change per-request (sidebars, headers).
type StaticComponent struct {
	HTML render.HTML
}

// Render returns the pre-rendered HTML.
func (s *StaticComponent) Render() render.HTML { return s.HTML }

// NewStaticComponent creates a component from raw HTML.
func NewStaticComponent(h render.HTML) *StaticComponent {
	return &StaticComponent{HTML: h}
}

// NewScreenGroup creates a screen group with the given prefix and layout.
// The prefix is the URL path prefix shared by all screens in the group.
// The layout wraps every child screen's content.
//
// Optional policies attach a chain of access policies to every screen
// in the group (and sub-groups). The chain is evaluated outermost
// group → innermost group → screen at request time; the first non-
// Allow Decision wins. Use this to gate entire route prefixes:
//
//	dash := app.NewScreenGroup("/dashboard", dashLayout, auth.SessionPolicy())
//	dash.Screen(app.NewScreen("/", &HomeScreen{}), nil)            // inherits SessionPolicy
//	dash.Screen(app.NewScreen("/billing", &BillingScreen{}).
//	    WithPolicy(auth.RolePolicy("admin")), nil)                  // SessionPolicy + RolePolicy
func NewScreenGroup(prefix string, layout *Layout, policies ...Policy) *ScreenGroup {
	return &ScreenGroup{
		prefix:   normalizeGroupPrefix(prefix),
		layout:   layout,
		policies: policies,
	}
}

// WithPolicy appends a Policy to the group's chain. Returns the group
// for chaining. Chain is evaluated before any per-screen policies.
func (g *ScreenGroup) WithPolicy(p Policy) *ScreenGroup {
	if p == nil {
		return g
	}
	g.policies = append(g.policies, p)
	return g
}

// normalizeGroupPrefix ensures the prefix starts with / and ends with /
// (because it's a directory-like prefix for child screens).
func normalizeGroupPrefix(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	if !startsWithSlash(p) {
		p = "/" + p
	}
	if p[len(p)-1] != '/' {
		p = p + "/"
	}
	return p
}

func startsWithSlash(s string) bool {
	return len(s) > 0 && s[0] == '/'
}

// Prefix returns the URL prefix for this group.
func (g *ScreenGroup) Prefix() string {
	return g.prefix
}

// Layout returns the layout for this group.
func (g *ScreenGroup) Layout() *Layout {
	return g.layout
}

// Screen registers a screen within this group. The screen's path is
// resolved relative to the group's prefix. If the screen has no layout,
// the group's layout is applied.
//
// The screen path can be:
//   - Absolute ("/users") — used as-is, but must start with the group prefix
//   - Relative ("users") — prefixed with the group's prefix
func (g *ScreenGroup) Screen(screen *Screen, layout *Layout) {
	// Resolve path relative to group prefix
	screen.Path = g.resolvePath(screen.Path)

	// Apply layout: explicit > screen's own > group's layout
	if layout != nil {
		screen.Layout = layout
	} else if screen.Layout == nil {
		screen.Layout = g.layout
	}

	// Remember the innermost group so the renderer can compose all
	// parent group layouts at render time (with proper
	// data-fui-screen-group markers per level).
	screen.group = g

	g.screens = append(g.screens, screen)
}

// SubGroup creates a nested screen group. The child group's prefix is
// resolved relative to this group's prefix. The child inherits this
// group's layout unless it declares its own.
//
// Optional policies are appended to the parent's policy chain at
// resolution time — they don't replace inherited policies. A child's
// screens are gated by parent.policies + child.policies + screen.Policies
// in that order.
func (g *ScreenGroup) SubGroup(prefix string, layout *Layout, policies ...Policy) *ScreenGroup {
	// Always resolve relative to parent — strip leading slash
	for len(prefix) > 0 && prefix[0] == '/' {
		prefix = prefix[1:]
	}
	// Parent prefix already ends with /, so just concatenate
	childPrefix := g.prefix + prefix + "/"
	child := &ScreenGroup{
		prefix:   childPrefix,
		layout:   layout,
		policies: policies,
		parent:   g,
	}
	if child.layout == nil {
		child.layout = g.layout
	}
	g.children = append(g.children, child)
	return child
}

// resolvePath resolves a path relative to the group's prefix.
func (g *ScreenGroup) resolvePath(path string) string {
	if startsWithSlash(path) {
		// Absolute path — validate it starts with group prefix
		return path
	}
	// Relative path — prepend group prefix
	return g.prefix + path
}

// Screens returns all screens registered directly in this group
// (not including sub-groups).
func (g *ScreenGroup) Screens() []*Screen {
	return g.screens
}

// AllScreens returns all screens in this group and its descendants.
func (g *ScreenGroup) AllScreens() []*Screen {
	var all []*Screen
	all = append(all, g.screens...)
	for _, child := range g.children {
		all = append(all, child.AllScreens()...)
	}
	return all
}

// RenderLayout wraps content in the group's layout. If the group has
// no layout, returns content unchanged.
//
// The rendered wrapper carries a data-fui-screen-group attribute so
// the runtime knows this is a layout boundary that should be preserved
// during sibling-screen navigation.
func (g *ScreenGroup) RenderLayout(content render.HTML) render.HTML {
	return g.RenderLayoutCtx(context.Background(), content)
}

// RenderLayoutCtx is RenderLayout with the request context threaded into the
// group layout's chrome.
func (g *ScreenGroup) RenderLayoutCtx(ctx context.Context, content render.HTML) render.HTML {
	if g.layout == nil {
		return content
	}
	wrapped := g.layout.WrapCtx(ctx, content)
	// Wrap in a group marker div so the runtime can identify the boundary.
	// The data-fui-screen-group attribute enables DOM-stable sibling nav.
	return html.Div(html.DivConfig{
		Class: "fui-screen-group",
		ExtraAttrs: map[string]string{"data-fui-screen-group": g.prefix},
	}, wrapped)
}

// ComposeLayouts walks from the innermost group to the outermost,
// wrapping content in each group's layout. Outer groups wrap inner
// groups. The innermost content (the screen) is wrapped first by its
// immediate group, then by each parent group going outward.
func ComposeLayouts(innermost *ScreenGroup, content render.HTML) render.HTML {
	return composeLayoutsWithOverride(innermost, nil, content, false)
}

// composeLayoutsWithOverride is the workhorse. When override is
// non-nil and differs from the innermost group's own layout, the
// innermost wrap uses the override instead of innermost.layout — but
// the innermost group's data-fui-screen-group marker is still emitted
// so sibling-screen navigation inside the group still preserves the
// (overridden) layout shell. Parent groups in the chain compose
// normally with their own layouts.
// nestInner, when true, makes every group layer emit its content region
// WITHOUT a <main> landmark — because an outer layout (the app default)
// will be wrapped around the whole composition and provides the single
// <main>. Without this, a grouped screen rendered inside the default
// layout ends up with two <main id="main-content"> (invalid + a duplicate
// landmark). When false (SSG / no outer default), behavior is unchanged:
// each group layer wraps with its own <main>.
func composeLayoutsWithOverride(innermost *ScreenGroup, override *Layout, content render.HTML, nestInner bool) render.HTML {
	return composeLayoutsWithOverrideCtx(context.Background(), innermost, override, content, nestInner)
}

// composeLayoutsWithOverrideCtx is composeLayoutsWithOverride with the request
// context threaded through every group layout's chrome, so context-aware
// sidebars/headers/footers in a group chain render with the live context.
func composeLayoutsWithOverrideCtx(ctx context.Context, innermost *ScreenGroup, override *Layout, content render.HTML, nestInner bool) render.HTML {
	var chain []*ScreenGroup
	for g := innermost; g != nil; g = g.parent {
		chain = append(chain, g)
	}
	wrapLayout := func(l *Layout, c render.HTML) render.HTML {
		if nestInner {
			return l.WrapNestedCtx(ctx, c)
		}
		return l.WrapCtx(ctx, c)
	}
	out := content
	for i, g := range chain {
		if i == 0 && override != nil && override != g.layout {
			wrapped := wrapLayout(override, out)
			out = html.Div(html.DivConfig{
				Class: "fui-screen-group",
				ExtraAttrs: map[string]string{"data-fui-screen-group": g.prefix},
			}, wrapped)
			continue
		}
		if nestInner && g.layout != nil {
			out = html.Div(html.DivConfig{
				Class:      "fui-screen-group",
				ExtraAttrs: map[string]string{"data-fui-screen-group": g.prefix},
			}, g.layout.WrapNestedCtx(ctx, out))
			continue
		}
		out = g.RenderLayoutCtx(ctx, out)
	}
	return out
}

// Ensure StaticComponent satisfies component.Component.
var _ component.Component = (*StaticComponent)(nil)

// groupChainContainsLayout reports whether any group in the chain
// (innermost → outermost) already uses layout. Used by the renderer
// to avoid wrapping the default layout TWICE when an app declares
// the same layout both at the App level and on a ScreenGroup.
func groupChainContainsLayout(innermost *ScreenGroup, layout *Layout) bool {
	for g := innermost; g != nil; g = g.parent {
		if g.layout == layout {
			return true
		}
	}
	return false
}
