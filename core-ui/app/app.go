package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/di"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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

	// Lang is the BCP-47 language tag of the document (e.g. "en", "fr",
	// "pt-BR"). It becomes the <html lang="…"> attribute on every page the app
	// renders, so screen readers announce the right language (WCAG 3.1.1).
	// Empty defaults to "en" via EffectiveLang. Set with WithLang.
	Lang string

	// LangFunc resolves the document language per route. It is called with the
	// page path on every full-page render and returns a BCP-47 tag, or "" to
	// fall back to Lang. A multilingual site that serves /es/… in Spanish sets
	// this once instead of tagging every screen. Nil (the default) keeps Lang
	// on every page. Set with WithLangFunc.
	LangFunc func(path string) string

	// SkipLabel is the visible text of the app shell's skip link, the
	// first string a keyboard user meets. Empty renders the default
	// "Skip to main content". Set with WithSkipLabel.
	SkipLabel string

	// SkipLabelFunc resolves the skip-link text per route, like LangFunc
	// for the document language. It is called with the page path on every
	// full-page render; "" falls back to SkipLabel. A multilingual site
	// sets this once alongside LangFunc so the link speaks the page's
	// language (#411). Set with WithSkipLabelFunc.
	SkipLabelFunc func(path string) string
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
// then validates, passing a partially-populated theme (e.g. a
// Color with empty Value) panics with the field path naming the
// missing piece. This catches "silently broken styling" failures at
// startup, not at the first page render.
func (a *App) WithTheme(theme style.Theme) *App {
	style.AutoFillNames(&theme)
	theme.MustValidate()
	a.Theme = &theme
	return a
}

// WithLang sets the document language (BCP-47 tag, e.g. "en", "fr", "pt-BR")
// and returns the app for chaining. It becomes the <html lang="…"> attribute so
// assistive tech announces the page in the right language (WCAG 3.1.1). An empty
// tag is rejected in favor of the "en" default, a bad tag is worse than the
// default, which is at least correct for English content.
func (a *App) WithLang(lang string) *App {
	a.Lang = lang
	return a
}

// WithLangFunc sets a per-route document-language resolver and returns the app
// for chaining. One Lang for the whole app is wrong the moment the app serves
// two languages: every translated page then claims the site language, so a
// screen reader reads Spanish with English pronunciation rules (WCAG 3.1.1) and
// a full-text indexer that reads <html lang> files the page under the wrong
// language and stems it with the wrong rules.
//
// The func is called with the page path on every full-page render. Returning ""
// falls back to Lang, so an app that never sets it renders exactly as before.
// A prefix test is usually all a bilingual site needs:
//
//	application.WithLangFunc(func(path string) string {
//		if strings.HasPrefix(path, "/es/") {
//			return "es"
//		}
//		return ""
//	})
//
// A screen that implements ScreenLanger overrides this for its own page.
func (a *App) WithLangFunc(fn func(path string) string) *App {
	a.LangFunc = fn
	return a
}

// The skip link is the first thing a keyboard user tabs to; a non-English
// app that sets nothing greets them in English.
func (a *App) WithSkipLabel(label string) *App {
	a.SkipLabel = label
	return a
}

// WithSkipLabelFunc sets a per-route skip-link resolver and returns the
// app for chaining. Called with the page path on every full-page render;
// returning "" falls back to SkipLabel. Set it alongside WithLangFunc so
// the link and the document language agree.
func (a *App) WithSkipLabelFunc(fn func(path string) string) *App {
	a.SkipLabelFunc = fn
	return a
}

// defaultSkipLabel keeps a host that sets nothing byte-identical.
const defaultSkipLabel = "Skip to main content"

// SkipLabelForPath returns the skip-link text for a route: SkipLabelFunc's
// answer when it gives one, else SkipLabel, else the English default. It
// mirrors LangForPath, and the value rides the outermost layout layer as
// data-fui-skip-label so the runtime can re-localize the link after a
// client-side navigation.
func (a *App) SkipLabelForPath(path string) string {
	if a.SkipLabelFunc != nil {
		if l := strings.TrimSpace(a.SkipLabelFunc(path)); l != "" {
			return l
		}
	}
	if a.SkipLabel != "" {
		return a.SkipLabel
	}
	return defaultSkipLabel
}

// EffectiveLang returns the document language, defaulting to "en" when unset.
// It is the site-wide fallback; LangForPath is what page rendering asks.
func (a *App) EffectiveLang() string {
	if a.Lang != "" {
		return a.Lang
	}
	return "en"
}

// LangForPath returns the document language for a route: LangFunc's answer when
// it gives one, else EffectiveLang. It deliberately does not consult
// ScreenLanger, which needs the loaded component; RenderPageResult layers that
// on top. Hosts that build their own document shells (a 404, an offline page)
// call this so those shells land in the same language as the route they stand
// in for.
func (a *App) LangForPath(path string) string {
	if a.LangFunc != nil {
		if lang := strings.TrimSpace(a.LangFunc(path)); lang != "" {
			return lang
		}
	}
	return a.EffectiveLang()
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
// the preferred registration API, the component declares its own metadata.
//
//	application.Register("/", &HomeScreen{})  // HomeScreen implements ScreenTitler
//
// If the component does not implement ScreenTyper, it defaults to ScreenPage.
// If it does not implement ScreenTitler, the title defaults to empty.
// If it does not implement ScreenDescriber, the description defaults to empty.
func (a *App) Register(path string, comp component.Component, layout *Layout, opts ...ScreenOption) {
	screen := &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenPage,
		Component: comp,
	}

	// Read metadata from individual interfaces when implemented.
	// Each is detected independently, a component can implement
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
	// Options last: an explicit registration option outranks whatever
	// the component declared about itself.
	for _, opt := range opts {
		opt(screen)
	}

	a.Router.Screen(screen, layout)
}

// RouteEntry describes a registered route for consumption by the DevServer.
type RouteEntry struct {
	Path        string
	Title       string
	Description string
	// Layouts is the route's resolved layout chain as layer keys, outermost
	// → innermost ("l:<name>" for plain layers, "g:<prefix>" for screen-group
	// layers. See LayoutLayer.Key). The runtime compares it positionally
	// against the DOM's data-fui-layout-key spine to find the deepest layer
	// shared with a navigation target, swapping only below it. Empty when
	// the route has no layout. An unnamed layer contributes "" to keep depth
	// indexes aligned; the runtime treats "" as never-matching.
	Layouts []string
	// Preload declares how the client may prefetch this route's content
	// before it is navigated to: "" (never, the default), "hover" (when a
	// link to it is hovered/focused), "visible" (when a link scrolls into
	// view), or "eager" (at idle after page load). Set with app.Preload.
	Preload string
	// RedirectTo is non-empty when this entry is a redirect (Redirect /
	// RedirectPattern) rather than a screen. Redirect entries render no
	// page, so consumers that enumerate pages (static export, sitemap,
	// llm.md, the strict coverage gate) MUST skip them. The route manifest
	// carries it to the client so SPA navigation rewrites without a
	// round-trip.
	RedirectTo string
	// NoSPA excludes the route from the client manifest: links to it
	// resolve as full document loads instead of soft navigations. For
	// hosts whose pages bind behavior at script load (legacy page-runtimes)
	// a soft swap never re-runs them.
	NoSPA bool
	// Intercept is set when the screen presents as an overlay for soft
	// navigations from a declared origin (see InterceptFrom). The route
	// manifest carries it so the runtime knows, without asking, which
	// links are worth diverting, and loads the intercept module only
	// when at least one route wants it. Nil for ordinary screens.
	Intercept *Intercept
}

// Routes returns every registered route, screens and redirects, as
// RouteEntry slices. Redirect entries carry a non-empty RedirectTo and
// have no screen; page-rendering consumers must skip them.
func (a *App) Routes() []RouteEntry {
	var entries []RouteEntry
	for _, path := range a.Router.Paths() {
		// ScreenByPattern, NOT Resolve: path is a registered PATTERN, and
		// a constrained pattern's own text fails its constraint at
		// resolve time ("/admin/:id:int" is not numeric), Resolve here
		// silently dropped constrained routes from the manifest, export,
		// sitemap, and llm.md.
		screen, ok := a.Router.ScreenByPattern(path)
		if !ok {
			continue
		}
		chain := a.Router.layoutChainFor(screen)
		var layouts []string
		if len(chain) > 0 {
			layouts = make([]string, len(chain))
			for i, layer := range chain {
				layouts[i] = layer.Key()
			}
		}
		entries = append(entries, RouteEntry{
			Path:        screen.Path,
			Title:       screen.Title,
			Description: screen.Description,
			Layouts:     layouts,
			Preload:     screen.Preload,
			Intercept:   screen.Intercept,
			NoSPA:       screen.NoSPA,
		})
	}
	// Map iteration is randomized, sort exact redirects so Routes()
	// (and everything derived from it: the route manifest JSON, the
	// static export walk) is deterministic across processes.
	exactFroms := make([]string, 0, len(a.Router.exactRedir))
	for from := range a.Router.exactRedir {
		exactFroms = append(exactFroms, from)
	}
	sort.Strings(exactFroms)
	for _, from := range exactFroms {
		entries = append(entries, RouteEntry{Path: from, RedirectTo: a.Router.exactRedir[from]})
	}
	for _, pr := range a.Router.patternRedir {
		entries = append(entries, RouteEntry{
			Path:       "/" + strings.Join(pr.segments, "/"),
			RedirectTo: pr.to,
		})
	}
	return entries
}

// SetDefaultLayout sets the default layout.
func (a *App) SetDefaultLayout(layout *Layout) {
	a.Router.DefaultLayout(layout)
}

// RenderScreenRaw is a policy-bypassing convenience over
// Router.RenderRaw. INTENDED FOR INTERNAL/SSG USE ONLY, HTTP
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
//
// injectComponent applies the DI/value-component contract (#259) for
// every render path (full page AND partial): pointer components get
// injection; value components are the documented stateless form and
// skip DI; a value STRUCT that asks for injection via inject tags is a
// wiring mistake that fails naming the type and the pointer remedy —
// silently rendering it with nil services would be worse.
func (a *App) injectComponent(comp component.Component, path string) error {
	if cv := reflect.ValueOf(comp); cv.Kind() == reflect.Pointer {
		if err := a.Inject(comp); err != nil {
			return fmt.Errorf("app: DI injection failed for %q: %w", path, err)
		}
		return nil
	}
	if n := injectTagCount(reflect.TypeOf(comp)); n > 0 {
		return fmt.Errorf(
			"app: screen component for %q has %d inject-tagged field(s) but was registered by value as %T; register a pointer (&%T{...}) so DI can fill them",
			path, n, comp, comp)
	}
	return nil
}

// injectTagCount reports how many inject-tagged fields a component
// type declares (directly, for struct types). Used to distinguish a
// legitimately stateless value component from a value struct that
// wanted injection and would otherwise render with nil services.
func injectTagCount(t reflect.Type) int {
	if t == nil || t.Kind() != reflect.Struct {
		return 0
	}
	n := 0
	for i := 0; i < t.NumField(); i++ {
		if _, ok := t.Field(i).Tag.Lookup("inject"); ok {
			n++
		}
	}
	return n
}

func (a *App) RenderPageResult(ctx context.Context, path string) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Evaluate policy chain BEFORE Load, a Redirect/Block decision
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

	// Inject DI services into component fields tagged `inject:""`;
	// see injectComponent for the value-component contract (#259).
	if err := a.injectComponent(comp, path); err != nil {
		return RenderResult{}, err
	}

	// Run the component's Load hook if present. Loaders run AFTER DI so they can
	// use injected services, and BEFORE render so they can populate fields.
	// A panicking Load takes the same error channel a Load error takes
	// (safeScreenLoad), never an escaped panic.
	if loader, ok := comp.(ScreenLoader); ok {
		if err := safeScreenLoad(loader, ctx); err != nil {
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}

	// Render the component directly for ScreenPage when the resolved layout
	// chain is non-empty, layer 0 provides the <main> wrapper. For other
	// screen types (drawer, sheet, dialog), always use screen.Render() which
	// adds proper ARIA wrapping and skip layouts entirely since they are
	// overlays.
	var content render.HTML
	var wrapped render.HTML
	// Document language and skip label, resolved BEFORE the render so the
	// values can ride the outermost layer (see docShell) as well as
	// <html lang> and the link text below. ScreenLang is read after Load
	// exactly like the title.
	lang := a.LangForPath(path)
	if langer, ok := comp.(ScreenLanger); ok {
		if l := strings.TrimSpace(safeScreenLang(langer)); l != "" {
			lang = l
		}
	}
	skip := a.SkipLabelForPath(path)
	ctx = withDocShell(ctx, lang, skip)
	if screen.Type == ScreenPage {
		chain := a.Router.layoutChainFor(screen)
		if len(chain) > 0 {
			var renderErr error
			content, renderErr = component.SafeRenderCtx(ctx, comp)
			if renderErr != nil {
				return RenderResult{}, fmt.Errorf("app: component render error for %q: %w", path, renderErr)
			}
			content = wrapArticle(screen, comp, content)
			wrapped = renderLayoutChain(ctx, chain, content)
		} else {
			content = renderComponentInScreen(ctx, screen, comp)
			wrapped = content
		}
	} else {
		// Drawer/sheet/dialog: render with ARIA wrapping, skip layout
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
	// The re-read is contained (safeScreenTitle): a hook that only panics
	// from its second call degrades to the registered title instead of
	// killing the request.
	titleText := a.Name
	effectiveTitle := screen.Title
	if titler, ok := comp.(ScreenTitler); ok {
		if t := safeScreenTitle(titler); t != "" {
			effectiveTitle = t
		}
	}
	if effectiveTitle != "" {
		titleText = effectiveTitle + " — " + a.Name
	}
	// Document language was resolved before the render (it rides the
	// outermost layer too); <html lang> below consumes the same value.
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
		"href":           "#main-content",
		"class":          "skip-link",
		"data-skip-link": "",
	}, render.Text(skip))

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
	htmlDoc := render.Tag("html", map[string]string{"lang": lang}, head, body)

	out := RenderResult{HTML: render.Join(doctype, htmlDoc), Title: effectiveTitle, Component: comp}
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
// Redirect/Block decisions, partials cannot express those; use
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
	return a.renderPartial(ctx, path, nil)
}

// RenderPartialFromResult renders the screen at path as a subtree partial
// for a client navigating from fromPath. Both routes' layout chains are
// resolved; the outermost layers they share are assumed live in the
// client's DOM, so the response contains the target's content wrapped
// only in the layers BELOW the deepest shared one. SwapLayer names that
// shared layer; the client swaps the content cell marked
// data-fui-layout-slot=SwapLayer.
//
// Layers are compared by *Layout pointer identity plus group prefix, the
// same identities that produced the layer keys in the route manifest, so
// the server and client always agree on the boundary. When fromPath is
// unknown, or the chains share no addressable layer, the result is
// identical to RenderPartialResult (bare content, empty SwapLayer) and
// the client falls back to a full-page fetch.
func (a *App) RenderPartialFromResult(ctx context.Context, path, fromPath string) (RenderResult, error) {
	res, err := a.renderPartial(ctx, path, nil)
	if err != nil {
		return res, err
	}
	if res.Kind != DecisionAllow && res.Kind != DecisionRenderAlt {
		return res, nil
	}
	target, _, ok := a.Router.Resolve(path)
	if !ok || target.Type != ScreenPage {
		return res, nil
	}
	// Clients may send the origin with its query string (the intercept
	// module reuses the same header with pathname+search); the chain is
	// a property of the route, so resolve on the pathname alone.
	if i := strings.IndexByte(fromPath, '?'); i >= 0 {
		fromPath = fromPath[:i]
	}
	from, _, ok := a.Router.Resolve(fromPath)
	if !ok {
		return res, nil
	}
	tChain := a.Router.layoutChainFor(target)
	fChain := a.Router.layoutChainFor(from)
	shared := 0
	for shared < len(tChain) && shared < len(fChain) &&
		tChain[shared].Layout == fChain[shared].Layout &&
		tChain[shared].GroupPrefix == fChain[shared].GroupPrefix &&
		tChain[shared].Key() != "" {
		shared++
	}
	if shared == 0 {
		return res, nil
	}
	// The doc markers must ride the partial's outermost layer, the same
	// values the full page would carry: without them the document language
	// and skip link could never change on an in-chain swap. ScreenLang is
	// layered like the full-page path so both render shapes agree.
	lang := a.LangForPath(path)
	if langer, ok := res.Component.(ScreenLanger); ok {
		if l := strings.TrimSpace(safeScreenLang(langer)); l != "" {
			lang = l
		}
	}
	ctx = withDocShell(ctx, lang, a.SkipLabelForPath(path))
	res.HTML = renderLayoutChainFrom(ctx, tChain, shared, res.HTML)
	res.SwapLayer = tChain[shared-1].Key()
	return res, nil
}

// RenderOverlayResult renders the screen at path as an intercepted
// overlay, the same component, the same Load, wrapped in `as` instead
// of the screen's own type.
//
// Callers must have already asked Router.InterceptFor whether this
// navigation is entitled to an overlay. This function does not
// re-authorize; it renders what it is told.
func (a *App) RenderOverlayResult(ctx context.Context, path string, as ScreenType) (RenderResult, error) {
	return a.renderPartial(ctx, path, &as)
}

// renderPartial is the shared body. overlay, when non-nil, replaces the
// screen's registered type for wrapping only, routing, policy, params,
// DI, and Load are identical, so an intercepted render can never diverge
// from the canonical one in anything but its outermost element.
func (a *App) renderPartial(ctx context.Context, path string, overlay *ScreenType) (RenderResult, error) {
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

	// Per-request component instance. See RenderPageResult for rationale.
	comp := screen.newInstance()
	if decision.Kind == DecisionRenderAlt && decision.AltFactory != nil {
		comp = decision.AltFactory()
	}

	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}

	if err := a.injectComponent(comp, path); err != nil {
		return RenderResult{}, err
	}

	if loader, ok := comp.(ScreenLoader); ok {
		if err := safeScreenLoad(loader, ctx); err != nil {
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}

	effType := screen.Type
	if overlay != nil {
		effType = *overlay
	}
	var body render.HTML
	if effType == ScreenPage {
		html, renderErr := component.SafeRenderCtx(ctx, comp)
		if renderErr != nil {
			return RenderResult{}, fmt.Errorf("app: component render error for %q: %w", path, renderErr)
		}
		// Same article wrapping as the full-page path, without it, SPA
		// navigation silently dropped the <article> element Reader Mode
		// keys on, so an article page lost reader support after the first
		// client-side visit.
		body = wrapArticle(screen, comp, html)
	} else {
		body = renderComponentAs(ctx, screen, effType, comp)
	}

	out := RenderResult{HTML: body, Component: comp}
	// Effective title AFTER Load: dynamic routes register with an empty
	// (or generic) title, so the partial path must re-read it from the
	// loaded instance, same as the full-page path's <title> build.
	if t, ok := comp.(ScreenTitler); ok {
		out.Title = safeScreenTitle(t)
	}
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
	return renderComponentAs(ctx, screen, screen.Type, comp)
}

// renderComponentAs renders comp wrapped in the ARIA scaffolding for an
// explicit screen type, which an intercepted render supplies instead of
// the screen's registered one.
//
// The render runs under the SSR containment every host-supplied render
// hook gets (SafeRenderCtx): the empty-layout ScreenPage full-page arm and
// every drawer/sheet/dialog/intercept-overlay arm flow through here, and a
// standalone host wires no recovery middleware, so an escaped panic would
// kill the request with no response. A panicking screen renders its
// SafeRenderCtx fallback; the failure is logged, not propagated.
func renderComponentAs(ctx context.Context, screen *Screen, effType ScreenType, comp component.Component) render.HTML {
	content, renderErr := component.SafeRenderCtx(ctx, comp)
	if renderErr != nil {
		slog.Default().Error("app: screen render panicked; rendering fallback",
			"panic", textsafe.Recovered(renderErr))
	}
	content = wrapArticle(screen, comp, content)
	// A layout-less ScreenPage page carries the doc markers on its bare
	// <main>: that element is what the runtime's swapShell targets for a
	// layout-less destination, and the document language / skip label
	// must arrive with it. Mirrors wrapByScreenType's ScreenPage arm with
	// the extra attributes; every other type keeps the shared wrapper.
	if attrs := docShellAttrs(ctx); attrs != nil && effType == ScreenPage {
		return html.Main(html.MainConfig{ExtraAttrs: attrs}, content)
	}
	return wrapByScreenType(effType, screen.Title, content)
}

// safeScreenLoad runs the ScreenLoader hook under the SSR containment: a
// panicking Load is converted to the same error channel a Load error takes
// (the host's not-found path), never an escaped panic. The panicked-on
// bytes are scrubbed through textsafe.Recovered; they are host/component
// state and must not forge log lines.
func safeScreenLoad(loader ScreenLoader, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("screen Load panicked: " + textsafe.Recovered(r))
		}
	}()
	return loader.Load(ctx)
}

// safeScreenTitle reads the post-Load ScreenTitle re-read under the same
// containment: a hook that works at registration (the 1st read) but panics
// on the per-request copy (the 2nd) degrades to the registered title
// instead of killing the request.
func safeScreenTitle(titler ScreenTitler) (title string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("app: ScreenTitle re-read panicked; using registered title",
				"panic", textsafe.Recovered(r))
			title = ""
		}
	}()
	return titler.ScreenTitle()
}

// safeScreenLang is safeScreenTitle for the ScreenLang re-read: a panicking
// hook degrades to the route/app language.
func safeScreenLang(langer ScreenLanger) (lang string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("app: ScreenLang re-read panicked; using route language",
				"panic", textsafe.Recovered(r))
			lang = ""
		}
	}()
	return langer.ScreenLang()
}
