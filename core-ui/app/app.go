package app

import (
	"context"
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
)

// App is the root of the UI hierarchy. It holds the DI container,
// theme, router, and global configuration.
type App struct {
	// Name is the application name, used in the page title.
	Name string
	// Container is the dependency injection container.
	Container *Container
	// Router maps paths to screens and layouts.
	Router *Router
	// Theme holds optional theme configuration (can be nil).
	Theme *style.Theme
}

// NewApp creates a new application with the given name.
func NewApp(name string) *App {
	return &App{
		Name:      name,
		Container: NewContainer(),
		Router:    NewRouter(),
	}
}

// WithTheme sets the application theme and returns the app for chaining.
func (a *App) WithTheme(theme style.Theme) *App {
	a.Theme = &theme
	return a
}

// Provide registers a service in the DI container.
func (a *App) Provide(constructor any) error {
	return a.Container.Provide(constructor)
}

// Inject fills struct fields tagged with `inject:""`.
func (a *App) Inject(target any) error {
	return a.Container.Inject(target)
}

// RegisterScreen adds a screen to the app's router.
func (a *App) RegisterScreen(screen *Screen, layout *Layout) {
	a.Router.Screen(screen, layout)
}

// Register adds a screen to the app by reading metadata from the component
// if it implements ScreenSpec. This is the preferred registration API — the
// component declares its own title, description, and type.
//
//	application.Register("/", &HomeScreen{})  // HomeScreen implements ScreenSpec
//
// If the component does not implement ScreenSpec, it defaults to ScreenPage
// with empty title/description (use RegisterScreen with builder for that case).
func (a *App) Register(path string, comp component.Component, layout *Layout) {
	screen := &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenPage,
		Component: comp,
	}

	// Read metadata from ScreenSpec if implemented
	if spec, ok := comp.(ScreenSpec); ok {
		screen.Title = spec.ScreenTitle()
		screen.Description = spec.ScreenDescription()
		screen.Type = spec.ScreenType()
	}

	a.Router.Screen(screen, layout)
}

// RouteEntry describes a registered route for consumption by the DevServer.
type RouteEntry struct {
	Path        string
	Title       string
	Description string
}

// Routes returns all registered screen paths as RouteEntry slices.
func (a *App) Routes() []RouteEntry {
	var entries []RouteEntry
	for _, path := range a.Router.Paths() {
		screen, _, ok := a.Router.Resolve(path)
		if !ok {
			continue
		}
		entries = append(entries, RouteEntry{
			Path:        screen.Path,
			Title:       screen.Title,
			Description: screen.Description,
		})
	}
	return entries
}

// SetDefaultLayout sets the default layout.
func (a *App) SetDefaultLayout(layout *Layout) {
	a.Router.DefaultLayout(layout)
}

// RenderScreen renders a screen by path, applying layout and theme CSS.
func (a *App) RenderScreen(path string) (render.HTML, error) {
	return a.Router.Render(path)
}

// RenderPage is the no-context shortcut for RenderPageContext. Use the
// context-aware variant when serving HTTP requests so screen Load methods
// see request cancellation.
func (a *App) RenderPage(path string) (render.HTML, error) {
	return a.RenderPageContext(context.Background(), path)
}

// RenderPageContext renders a full HTML page (<!DOCTYPE html><html>...) for a
// screen path. It resolves the route, locks the screen for concurrent param
// safety, injects route params, runs DI, calls the screen's Load(ctx) hook
// if present, and finally renders. The page includes:
//   - DOCTYPE and html declaration
//   - Head with charset, viewport, title, and theme CSS custom properties
//   - Body with skip link (ADA-compliant) and the rendered screen with layout
func (a *App) RenderPageContext(ctx context.Context, path string) (render.HTML, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Lock screen for concurrent-safe param mutation + render
	screen.mu.Lock()
	defer screen.mu.Unlock()

	// Inject route params into ParamSetter components
	if len(params) > 0 {
		if ps, ok := screen.Component.(ParamSetter); ok {
			ps.SetParams(params)
		}
		screen.routeParams = params
	}

	// Inject DI services into screen fields tagged `inject:""`
	if err := a.Inject(screen.Component); err != nil {
		return "", fmt.Errorf("app: DI injection failed for %q: %w", path, err)
	}

	// Run the screen's Load hook if present. Loaders run AFTER DI so they can
	// use injected services, and BEFORE render so they can populate fields.
	if loader, ok := screen.Component.(ScreenLoader); ok {
		if err := loader.Load(ctx); err != nil {
			return "", fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}

	layout := screen.Layout
	if layout == nil {
		layout = a.Router.defaultLayout
	}

	// Render the component directly for ScreenPage when a layout is present —
	// the layout provides the <main> wrapper. For other screen types (drawer,
	// sheet, dialog), always use screen.Render() which adds proper ARIA wrapping
	// and skip the layout entirely since they are overlays.
	var content render.HTML
	var wrapped render.HTML
	if screen.Type == ScreenPage {
		if layout != nil {
			var renderErr error
			content, renderErr = component.SafeRender(screen.Component)
			if renderErr != nil {
				return "", fmt.Errorf("app: component render error for %q: %w", path, renderErr)
			}
		} else {
			content = screen.Render()
		}
		if layout != nil {
			wrapped = layout.Wrap(content)
		} else {
			wrapped = content
		}
	} else {
		// Drawer/sheet/dialog — render with ARIA wrapping, skip layout
		content = screen.Render()
		wrapped = content
	}

	// Build <head>.
	var headChildren []render.HTML
	headChildren = append(headChildren,
		render.VoidTag("meta", map[string]string{"charset": "UTF-8"}),
	)
	headChildren = append(headChildren,
		render.VoidTag("meta", map[string]string{
			"name":    "viewport",
			"content": "width=device-width, initial-scale=1.0",
		}),
	)
	var titleText string
	if screen.Title != "" {
		titleText = screen.Title + " — " + a.Name
	} else {
		titleText = a.Name
	}
	headChildren = append(headChildren,
		render.Tag("title", nil, render.Text(titleText)),
	)

	// Theme CSS custom properties.
	if a.Theme != nil {
		css := a.Theme.CSSCustomProperties()
		headChildren = append(headChildren,
			render.Tag("style", nil, render.Raw(css)),
		)
	}

	head := render.Tag("head", nil, headChildren...)

	// Build <body> with skip link.
	skipLink := render.Tag("a", map[string]string{
		"href":  "#main-content",
		"class": "skip-link",
	}, render.Text("Skip to main content"))

	body := render.Tag("body", nil, skipLink, wrapped)

	// Assemble full document.
	doctype := render.Raw("<!DOCTYPE html>")
	html := render.Tag("html", map[string]string{"lang": "en"}, head, body)

	return render.Join(doctype, html), nil
}

// RenderPartial is the no-context shortcut for RenderPartialContext.
func (a *App) RenderPartial(path string) (render.HTML, error) {
	return a.RenderPartialContext(context.Background(), path)
}

// RenderPartialContext returns just the screen content (no layout, no
// <html>/<head>/<body>). Used for client-side navigation where the layout
// is already in the DOM. Runs the same param-injection / DI / Load pipeline
// as RenderPageContext.
func (a *App) RenderPartialContext(ctx context.Context, path string) (render.HTML, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Lock screen for concurrent-safe param mutation + render
	screen.mu.Lock()
	defer screen.mu.Unlock()

	// Inject route params into ParamSetter components
	if len(params) > 0 {
		if ps, ok := screen.Component.(ParamSetter); ok {
			ps.SetParams(params)
		}
		screen.routeParams = params
	}

	// Inject DI services into screen fields tagged `inject:""`
	if err := a.Inject(screen.Component); err != nil {
		return "", fmt.Errorf("app: DI injection failed for %q: %w", path, err)
	}

	// Run the screen's Load hook if present.
	if loader, ok := screen.Component.(ScreenLoader); ok {
		if err := loader.Load(ctx); err != nil {
			return "", fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}

	if screen.Type == ScreenPage {
		// Return just the component content — client-side router will
		// swap it into the existing <main> element
		html, renderErr := component.SafeRender(screen.Component)
		if renderErr != nil {
			return "", fmt.Errorf("app: component render error for %q: %w", path, renderErr)
		}
		return html, nil
	}

	// Drawer/sheet/dialog — return full ARIA-wrapped content
	return screen.Render(), nil
}
