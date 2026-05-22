package app

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Router maps paths to screens and layouts.
// Supports both exact paths ("/about") and dynamic patterns ("/products/:slug").
type Router struct {
	screens       map[string]*Screen // path → screen (exact matches)
	dynamic       []dynamicRoute     // pattern-based routes
	defaultLayout *Layout
}

// dynamicRoute holds a parsed route pattern with parameter extraction.
type dynamicRoute struct {
	segments   []string // e.g. ["products", ":slug"]
	ParamNames []string // e.g. ["slug"]
	screen     *Screen
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{
		screens: make(map[string]*Screen),
	}
}

// Screen registers a screen with an optional layout.
// If layout is nil, the screen will use the default layout (if set).
// Paths with ":param" segments are registered as dynamic routes.
func (r *Router) Screen(screen *Screen, layout *Layout) {
	if layout != nil {
		screen.Layout = layout
	}

	if strings.Contains(screen.Path, ":") {
		// Dynamic route — parse segments
		parts := strings.Split(strings.Trim(screen.Path, "/"), "/")
		var paramNames []string
		for _, p := range parts {
			if strings.HasPrefix(p, ":") {
				paramNames = append(paramNames, p[1:])
			}
		}
		r.dynamic = append(r.dynamic, dynamicRoute{
			segments:   parts,
			ParamNames: paramNames,
			screen:     screen,
		})
	} else {
		r.screens[screen.Path] = screen
	}
}

// DefaultLayout sets the default layout for screens without one.
func (r *Router) DefaultLayout(layout *Layout) {
	r.defaultLayout = layout
}

// Resolve finds the screen for a given path.
// Returns the screen, the extracted route params (for dynamic routes), and whether it was found.
// Params are returned separately to avoid mutating the shared screen instance.
func (r *Router) Resolve(path string) (*Screen, map[string]string, bool) {
	// Exact match first
	if s, ok := r.screens[path]; ok {
		return s, nil, true
	}

	// Try dynamic routes
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	for _, dr := range r.dynamic {
		if len(dr.segments) != len(pathParts) {
			continue
		}
		match := true
		params := make(map[string]string)
		for i, seg := range dr.segments {
			if strings.HasPrefix(seg, ":") {
				params[seg[1:]] = pathParts[i]
			} else if seg != pathParts[i] {
				match = false
				break
			}
		}
		if match {
			return dr.screen, params, true
		}
	}

	return nil, nil, false
}

// Render renders a screen by path, applying its layout.
func (r *Router) Render(path string) (render.HTML, error) {
	screen, params, ok := r.Resolve(path)
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Lock screen for concurrent-safe param mutation + render
	screen.mu.Lock()
	defer screen.mu.Unlock()

	// Inject route params if the component accepts them
	if len(params) > 0 {
		if ps, ok := screen.Component.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}
	screen.routeParams = params

	content := screen.Render()
	layout := screen.Layout
	if layout == nil {
		layout = r.defaultLayout
	}
	return layout.Wrap(content), nil
}

// Paths returns all registered paths (exact + dynamic patterns).
func (r *Router) Paths() []string {
	paths := make([]string, 0, len(r.screens)+len(r.dynamic))
	for p := range r.screens {
		paths = append(paths, p)
	}
	for _, dr := range r.dynamic {
		paths = append(paths, "/"+strings.Join(dr.segments, "/"))
	}
	return paths
}

// Screens returns the internal screens map for direct access.
func (r *Router) Screens() map[string]*Screen {
	return r.screens
}

// ScreenGroup registers all screens from a ScreenGroup into the router.
// Each screen in the group (and its sub-groups) is registered with the
// router, and the group's layout is applied.
//
// When the runtime navigates between siblings in the same group, only
// the inner content is swapped — the layout shell is DOM-stable.
func (r *Router) ScreenGroup(sg *ScreenGroup) {
	for _, screen := range sg.AllScreens() {
		r.Screen(screen, screen.Layout)
	}
}
