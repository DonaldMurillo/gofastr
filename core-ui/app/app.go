package app

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/di"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// App is the root of the UI hierarchy. It holds the DI container,
// theme, router, and global configuration.
type App struct {
	// Name is the application name, used in the page title.
	Name string
	// Container is the dependency injection container.
	Container *di.Container
	// Router maps paths to screens and layouts.
	Router *Router
	// Theme holds optional theme configuration (can be nil).
	Theme *style.Theme

	// NoLLMMD disables auto-generated llm.md for all pages in this app.
	NoLLMMD bool
}

// NewApp creates a new application with the given name.
func NewApp(name string) *App {
	return &App{
		Name:      name,
		Container: di.NewContainer(),
		Router:    NewRouter(),
	}
}

// WithTheme sets the application theme and returns the app for
// chaining. Auto-fills missing token Names from struct-field paths
// (Colors.Primary → "primary", Colors.PrimaryFg → "primary-fg"),
// then validates — passing a partially-populated theme (e.g. a
// Color with empty Value) panics with the field path naming the
// missing piece. This catches "silently broken styling" failures at
// startup, not at the first page render.
func (a *App) WithTheme(theme style.Theme) *App {
	style.AutoFillNames(&theme)
	theme.MustValidate()
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
// if it implements ScreenTitler, ScreenDescriber, or ScreenTyper. This is
// the preferred registration API — the component declares its own metadata.
//
//	application.Register("/", &HomeScreen{})  // HomeScreen implements ScreenTitler
//
// If the component does not implement ScreenTyper, it defaults to ScreenPage.
// If it does not implement ScreenTitler, the title defaults to empty.
// If it does not implement ScreenDescriber, the description defaults to empty.
func (a *App) Register(path string, comp component.Component, layout *Layout) {
	screen := &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenPage,
		Component: comp,
	}

	// Read metadata from individual interfaces when implemented.
	// Each is detected independently — a component can implement
	// ScreenTitler alone, or ScreenTitler + ScreenDescriber, etc.
	if titler, ok := comp.(ScreenTitler); ok {
		screen.Title = titler.ScreenTitle()
	}
	if describer, ok := comp.(ScreenDescriber); ok {
		screen.Description = describer.ScreenDescription()
	}
	if typer, ok := comp.(ScreenTyper); ok {
		screen.Type = typer.ScreenType()
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

// RenderScreenRaw is a policy-bypassing convenience over
// Router.RenderRaw. INTENDED FOR INTERNAL/SSG USE ONLY — HTTP
// handlers must use RenderPageResult so the Policy chain is
// honored (auth gating, RenderAlt, Redirect, Block).
func (a *App) RenderScreenRaw(path string) (render.HTML, error) {
	return a.Router.RenderRaw(path)
}

// RenderPage renders a full HTML page (<!DOCTYPE html><html>...) for a
// screen path. It resolves the route, evaluates the screen's policy
// chain, locks the screen for concurrent param safety, injects route
// params, runs DI, calls Load(ctx), and finally renders.
//
// RenderPage is the simple entry point: it returns HTML for Allow and
// RenderAlt decisions, and an error for Redirect/Block (which cannot
// be expressed as HTML). Use RenderPageResult when you need to react
// to all four DecisionKinds.
func (a *App) RenderPage(ctx context.Context, path string) (render.HTML, error) {
	res, err := a.RenderPageResult(ctx, path)
	if err != nil {
		return "", err
	}
	switch res.Kind {
	case DecisionAllow, DecisionRenderAlt:
		return res.HTML, nil
	case DecisionRedirect:
		return "", fmt.Errorf("app: %q policy returned redirect to %q; use RenderPageResult to handle", path, res.URL)
	case DecisionBlock:
		return "", fmt.Errorf("app: %q policy returned block status %d; use RenderPageResult to handle", path, res.Status)
	default:
		return "", fmt.Errorf("app: %q unknown decision kind %d", path, res.Kind)
	}
}

// RenderPageResult is the policy-aware variant of RenderPage. It
// resolves the screen, evaluates the effective Policy chain, and
// returns a RenderResult describing the outcome.
//
//   - DecisionAllow: HTML holds the full <!DOCTYPE>… document.
//   - DecisionRedirect: URL is the destination; HTML is empty.
//   - DecisionRenderAlt: the alt component took the place of the
//     screen's component; HTML holds the full document.
//   - DecisionBlock: Status holds the HTTP status code; HTML is empty.
func (a *App) RenderPageResult(ctx context.Context, path string) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Evaluate policy chain BEFORE Load — a Redirect/Block decision
	// short-circuits without touching the DB.
	decision := ResolvePolicy(ctx, screen)
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Message: decision.Message}, nil
	}

	// Per-request component instance: shallow-copy from the registered
	// template so SetParams / Inject / Load mutations land on storage
	// only this request can see. RenderAlt overrides with its factory.
	comp := screen.newInstance()
	if decision.Kind == DecisionRenderAlt && decision.AltFactory != nil {
		comp = decision.AltFactory()
	}

	// Inject route params into ParamSetter components
	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}

	// Inject DI services into component fields tagged `inject:""`
	if err := a.Inject(comp); err != nil {
		return RenderResult{}, fmt.Errorf("app: DI injection failed for %q: %w", path, err)
	}

	// Run the component's Load hook if present. Loaders run AFTER DI so they can
	// use injected services, and BEFORE render so they can populate fields.
	if loader, ok := comp.(ScreenLoader); ok {
		if err := loader.Load(ctx); err != nil {
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
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
			content, renderErr = component.SafeRenderCtx(ctx, comp)
			if renderErr != nil {
				return RenderResult{}, fmt.Errorf("app: component render error for %q: %w", path, renderErr)
			}
		} else {
			content = renderComponentInScreen(ctx, screen, comp)
		}
		if layout != nil {
			wrapped = layout.Wrap(content)
		} else {
			wrapped = content
		}
	} else {
		// Drawer/sheet/dialog — render with ARIA wrapping, skip layout
		content = renderComponentInScreen(ctx, screen, comp)
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
	// Title: re-read ScreenTitle() AFTER Load so dynamic routes
	// (e.g. /docs/:slug) can compute the title from data fetched in Load.
	// Falls back to the registration-time title, then to the app name alone.
	titleText := a.Name
	effectiveTitle := screen.Title
	if titler, ok := comp.(ScreenTitler); ok {
		if t := titler.ScreenTitle(); t != "" {
			effectiveTitle = t
		}
	}
	if effectiveTitle != "" {
		titleText = effectiveTitle + " — " + a.Name
	}
	headChildren = append(headChildren,
		render.Tag("title", nil, render.Text(titleText)),
	)

	// Theme + custom CSS + the route-graph script are NOT injected inline
	// here. The host (e.g. framework/uihost) is responsible for emitting
	// <link rel="stylesheet"> and <script src="..."> tags pointing at
	// endpoints it serves. That keeps the rendered page strict-CSP-clean
	// (no 'unsafe-inline' required) and lets the host control caching.

	head := render.Tag("head", nil, headChildren...)

	// Build <body> with skip link.
	skipLink := render.Tag("a", map[string]string{
		"href":  "#main-content",
		"class": "skip-link",
	}, render.Text("Skip to main content"))

	// Polite live region for SPA route changes. document.title mutations
	// aren't announced by screen readers; the runtime writes the new
	// page title into here after each partial-nav so AT users hear it.
	routeAnnounce := render.Tag("div", map[string]string{
		"id":          "fui-route-announce",
		"role":        "status",
		"aria-live":   "polite",
		"aria-atomic": "true",
		"class":       "fui-visually-hidden",
	}, render.Text(""))

	body := render.Tag("body", nil, skipLink, routeAnnounce, wrapped)

	// Assemble full document.
	doctype := render.Raw("<!DOCTYPE html>")
	htmlDoc := render.Tag("html", map[string]string{"lang": "en"}, head, body)

	out := RenderResult{HTML: render.Join(doctype, htmlDoc)}
	if decision.Kind == DecisionRenderAlt {
		out.Kind = DecisionRenderAlt
	} else {
		out.Kind = DecisionAllow
	}
	return out, nil
}

// RenderPartial returns just the screen content (no layout, no
// <html>/<head>/<body>). Used for client-side navigation where the layout
// is already in the DOM. Runs the same param-injection / DI / Load pipeline
// as RenderPage, including policy evaluation. Returns an error for
// Redirect/Block decisions — partials cannot express those; use
// RenderPartialResult instead.
func (a *App) RenderPartial(ctx context.Context, path string) (render.HTML, error) {
	res, err := a.RenderPartialResult(ctx, path)
	if err != nil {
		return "", err
	}
	switch res.Kind {
	case DecisionAllow, DecisionRenderAlt:
		return res.HTML, nil
	case DecisionRedirect:
		return "", fmt.Errorf("app: partial %q policy returned redirect to %q; use RenderPartialResult", path, res.URL)
	case DecisionBlock:
		return "", fmt.Errorf("app: partial %q policy returned block %d; use RenderPartialResult", path, res.Status)
	default:
		return "", fmt.Errorf("app: partial %q unknown decision kind %d", path, res.Kind)
	}
}

// RenderPartialResult is the policy-aware variant of RenderPartial.
// Same semantics as RenderPageResult but returns just the content
// fragment, suitable for client-side navigation swaps.
func (a *App) RenderPartialResult(ctx context.Context, path string) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}

	decision := ResolvePolicy(ctx, screen)
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Message: decision.Message}, nil
	}

	// Per-request component instance — see RenderPageResult for rationale.
	comp := screen.newInstance()
	if decision.Kind == DecisionRenderAlt && decision.AltFactory != nil {
		comp = decision.AltFactory()
	}

	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}

	if err := a.Inject(comp); err != nil {
		return RenderResult{}, fmt.Errorf("app: DI injection failed for %q: %w", path, err)
	}

	if loader, ok := comp.(ScreenLoader); ok {
		if err := loader.Load(ctx); err != nil {
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}

	var body render.HTML
	if screen.Type == ScreenPage {
		html, renderErr := component.SafeRenderCtx(ctx, comp)
		if renderErr != nil {
			return RenderResult{}, fmt.Errorf("app: component render error for %q: %w", path, renderErr)
		}
		body = html
	} else {
		body = renderComponentInScreen(ctx, screen, comp)
	}

	out := RenderResult{HTML: body}
	if decision.Kind == DecisionRenderAlt {
		out.Kind = DecisionRenderAlt
	} else {
		out.Kind = DecisionAllow
	}
	return out, nil
}

// renderComponentInScreen renders comp wrapped in the ARIA scaffolding
// dictated by screen.Type. Lets the caller substitute a different
// component (used for RenderAlt + no-layout fallback) without copying
// the Screen struct (which embeds a sync.Mutex).
func renderComponentInScreen(ctx context.Context, screen *Screen, comp component.Component) render.HTML {
	var content render.HTML
	if cc, ok := comp.(component.ContextComponent); ok {
		content = cc.RenderCtx(ctx)
	} else {
		content = comp.Render()
	}
	return wrapByScreenType(screen.Type, screen.Title, content)
}
