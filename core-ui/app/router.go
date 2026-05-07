package app

import (
	"fmt"

	"github.com/gofastr/gofastr/core/render"
)

// Router maps paths to screens and layouts.
type Router struct {
	screens       map[string]*Screen // path → screen
	defaultLayout *Layout
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{
		screens: make(map[string]*Screen),
	}
}

// Screen registers a screen with an optional layout.
// If layout is nil, the screen will use the default layout (if set).
func (r *Router) Screen(screen *Screen, layout *Layout) {
	if layout != nil {
		screen.Layout = layout
	}
	r.screens[screen.Path] = screen
}

// DefaultLayout sets the default layout for screens without one.
func (r *Router) DefaultLayout(layout *Layout) {
	r.defaultLayout = layout
}

// Resolve finds the screen for a given path.
// Returns the screen and whether it was found.
func (r *Router) Resolve(path string) (*Screen, bool) {
	s, ok := r.screens[path]
	return s, ok
}

// Render renders a screen by path, applying its layout.
func (r *Router) Render(path string) (render.HTML, error) {
	screen, ok := r.screens[path]
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	content := screen.Render()
	layout := screen.Layout
	if layout == nil {
		layout = r.defaultLayout
	}
	return layout.Wrap(content), nil
}

// Paths returns all registered paths.
func (r *Router) Paths() []string {
	paths := make([]string, 0, len(r.screens))
	for p := range r.screens {
		paths = append(paths, p)
	}
	return paths
}

// Screens returns the internal screens map for direct access.
func (r *Router) Screens() map[string]*Screen {
	return r.screens
}
