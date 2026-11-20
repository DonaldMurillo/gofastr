package app

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ScreenGroup defines a shared layout that wraps every child screen.
type ScreenGroup struct {
	prefix     string
	layout     *Layout
	policies   []Policy
	screens    []*Screen
	children   []*ScreenGroup // nested sub-groups
	parent     *ScreenGroup
	parentApp  *Router
	standalone bool // when true, the host's default layout does NOT wrap this group

	fills map[*Outlet]fillDecl

	resolvers []resolverDecl

	requires []AnyKey

	loading *Loading
}

// StaticComponent wraps raw HTML as a component.Component.
type StaticComponent struct {
	HTML render.HTML
}

// Render returns the pre-rendered HTML.
func (s *StaticComponent) Render() render.HTML { return s.HTML }

// NewStaticComponent creates a component from raw HTML.
func NewStaticComponent(h render.HTML) *StaticComponent {
	return &StaticComponent{HTML: h}
}

type contextComponentFunc func(ctx context.Context) render.HTML

func (f contextComponentFunc) Render() render.HTML                     { return f(context.Background()) }
func (f contextComponentFunc) RenderCtx(c context.Context) render.HTML { return f(c) }

// NewContextComponent creates a component whose output depends on the request context (auth-aware
// chrome, tenant-scoped headers, …).
func NewContextComponent(fn func(ctx context.Context) render.HTML) component.Component {
	return contextComponentFunc(fn)
}

// NewScreenGroup creates a screen group with the given prefix and layout.
func NewScreenGroup(prefix string, layout *Layout, policies ...Policy) *ScreenGroup {
	return &ScreenGroup{
		prefix:   normalizeGroupPrefix(prefix),
		layout:   layout,
		policies: policies,
	}
}

// Standalone marks this group as a self-contained shell: its own layout is the outermost wrapper
// and the host App's default layout (SetDefaultLayout) does NOT wrap it.
func (g *ScreenGroup) Standalone() *ScreenGroup {
	g.standalone = true
	return g
}

// WithPolicy appends a Policy to the group's chain.
func (g *ScreenGroup) WithPolicy(p Policy) *ScreenGroup {
	if p == nil {
		return g
	}
	g.policies = append(g.policies, p)
	return g
}

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

// Screen registers a screen within this group.
func (g *ScreenGroup) Screen(screen *Screen, layout *Layout) {
	screen.Path = g.resolvePath(screen.Path)

	if layout != nil {
		screen.Layout = layout
	} else if screen.Layout == nil {
		screen.Layout = g.layout
	}

	screen.group = g

	g.screens = append(g.screens, screen)
}

// SubGroup creates a nested screen group.
func (g *ScreenGroup) SubGroup(prefix string, layout *Layout, policies ...Policy) *ScreenGroup {
	childPrefix := normalizeGroupPrefix(path.Join(g.prefix, prefix))
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

func (g *ScreenGroup) resolvePath(path string) string {
	if startsWithSlash(path) {
		prefix := normalizeRoutePath(g.prefix)
		normalized := normalizeRoutePath(path)
		if normalized != strings.TrimSuffix(prefix, "/") && !strings.HasPrefix(normalized, prefix) {
			panic(fmt.Sprintf("app: screen path %q is outside group prefix %q", path, g.prefix))
		}
		return path
	}
	return g.prefix + path
}

// Screens returns all screens registered directly in this group (not including sub-groups).
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

// RenderLayout wraps content in the group's layout.
func (g *ScreenGroup) RenderLayout(content render.HTML) render.HTML {
	return g.RenderLayoutCtx(context.Background(), content)
}

// RenderLayoutCtx is RenderLayout with the request context threaded into the group layout's chrome.
func (g *ScreenGroup) RenderLayoutCtx(ctx context.Context, content render.HTML) render.HTML {
	if g.layout == nil {
		return content
	}
	wrapped := g.layout.WrapCtx(ctx, content)
	return html.Div(html.DivConfig{
		Class:      "cui-screen-group",
		ExtraAttrs: map[string]string{"data-cui-screen-group": g.prefix},
	}, wrapped)
}

var _ component.Component = (*StaticComponent)(nil)

func groupChainContainsLayout(innermost *ScreenGroup, layout *Layout) bool {
	for g := innermost; g != nil; g = g.parent {
		if g.layout == layout {
			return true
		}
	}
	return false
}

func groupChainIsStandalone(innermost *ScreenGroup) bool {
	for g := innermost; g != nil; g = g.parent {
		if g.standalone {
			return true
		}
	}
	return false
}
