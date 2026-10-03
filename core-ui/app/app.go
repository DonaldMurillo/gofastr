package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/di"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// App is the root of the UI hierarchy. It holds the DI container,
// theme, router, and global configuration.
type App struct {
	// Name is the application name, used in the page title.
	Name string
	// ownStyle is the app's owned style (WithStyle); nil for none.
	ownStyle *ownstyle.Sheet
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

	// NotFound is the body the 404-outlet outcome renders (an outlet
	// declared FallbackNotFound that no candidate fills, Decided 5):
	// the primary becomes this component through the full screen
	// pipeline — per-request instance, DI, Load, safe render, its title
	// — while every other outlet renders its Default or nothing, and
	// the host answers 404. Hosts that configure a custom 404
	// (uihost.WithNotFoundScreen) get it wired here by their host
	// constructor; nil keeps the minimal built-in body below.
	NotFound component.Component
}

// WithNotFoundBody sets the body the 404-outlet outcome renders and
// returns the app for chaining (see App.NotFound).
func (a *App) WithNotFoundBody(c component.Component) *App {
	a.NotFound = c
	return a
}

// notFoundBody resolves the 404-outlet outcome's body: App.NotFound
// when the host wired one, else a minimal built-in (heading plus a
// line of copy that stays truthful for a route that DID resolve —
// "this page does not exist", never "no route matched").
func (a *App) notFoundBody() component.Component {
	if a.NotFound != nil {
		return a.NotFound
	}
	return minimalNotFoundBody{}
}

// minimalNotFoundBody is the app-level fallback 404 body: plain
// composed html, no chrome of its own (the root layout owns the
// shell), no request data (the route resolved; echoing a path would
// read as "no route matched" copy).
type minimalNotFoundBody struct{}

func (minimalNotFoundBody) Render() render.HTML {
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("404: Page not found")),
		html.Paragraph(html.TextConfig{}, render.Text("This page does not exist.")),
	)
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

// docLangFor resolves the document language a render of path carries:
// the route rule, then the component's own ScreenLang read after Load so
// a dynamic route can take the tag from the content it fetched.
func (a *App) docLangFor(path string, comp any) string {
	lang := a.LangForPath(path)
	if langer, ok := comp.(ScreenLanger); ok {
		if l := strings.TrimSpace(safeScreenLang(langer)); l != "" {
			lang = l
		}
	}
	return lang
}

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
	// Deferred lists the wire addresses of the route's DEFERRED outlets
	// regions whose fills do not travel with the
	// page request but as their own part requests (X-Gofastr-Part), one
	// per address, launched by the client at the same moment as the
	// page fetch. Nil when no outlet in the route's chain defers — the
	// field is omitted from the manifest so pages without deferral pay
	// nothing.
	Deferred []string
	// Loading carries a screen's (or its group's) swap-slot loading
	// declaration for the route manifest (,
	// the rendered loading HTML plus its
	// After/Min in whole milliseconds. Nil when neither the screen nor
	// any group declares one.
	Loading *LoadingManifest
}

// LoadingManifest is the wire form of a Loading declaration in the
// route manifest (, the loading content
// rendered ONCE (the manifest is built once; loading content is
// presentational config with no request state) and the timings the
// client needs.
type LoadingManifest struct {
	HTML  string
	After int // milliseconds
	Min   int // milliseconds
}

// loadingManifestFor renders a Loading declaration into its manifest
// form. A nil Show yields nil (nothing to carry).
func loadingManifestFor(ld *Loading) *LoadingManifest {
	if ld == nil || ld.Show == nil {
		return nil
	}
	html, err := component.SafeRenderCtx(context.Background(), ld.Show)
	if err != nil {
		html = ""
	}
	return &LoadingManifest{
		HTML:  string(html),
		After: ld.loadingAfterMs(),
		Min:   ld.loadingMinMs(),
	}
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
		// Deferred outlet addresses the client
		// launches one part request per address beside the page fetch.
		deferred := deferredAddrs(chain)
		entries = append(entries, RouteEntry{
			Path:        screen.Path,
			Title:       screen.Title,
			Description: screen.Description,
			Layouts:     layouts,
			Preload:     screen.Preload,
			Intercept:   screen.Intercept,
			NoSPA:       screen.NoSPA,
			Deferred:    deferred,
			// the screen's (or its
			// group's) swap-slot loading declaration, rendered once.
			Loading: loadingManifestFor(screenLoading(screen)),
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

// Layouts returns every layout a registered page renders in: the
// default layout and each screen's layout chain, each layout once,
// sorted by name. The host collects their TransitionCSS into app.css.
func (a *App) Layouts() []*Layout {
	seen := map[*Layout]bool{}
	var out []*Layout
	add := func(l *Layout) {
		if l != nil && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	add(a.Router.defaultLayout)
	paths := a.Router.Paths()
	slices.Sort(paths)
	for _, p := range paths {
		screen, ok := a.Router.ScreenByPattern(p)
		if !ok {
			continue
		}
		for _, layer := range a.Router.layoutChainFor(screen) {
			add(layer.Layout)
		}
	}
	slices.SortStableFunc(out, func(x, y *Layout) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// LayoutsVersion changes whenever a screen or the default layout is
// registered. The host compares it against the value it saw when it
// composed app.css, to notice layouts added afterwards.
func (a *App) LayoutsVersion() uint64 {
	return a.Router.layoutsGen.Load()
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
	return a.renderPageResult(ctx, path)
}

// notFoundScreenFor builds the synthetic page screen a not-found render
// flows through: comp at a pattern no route table can hold, titled for
// the <title> build and the X-Gofastr-Title header. The screen is never
// registered; it exists so the render pipeline treats the 404 body
// exactly like a registered screen (per-request instance, DI, Load,
// article wrap, the default layout chain).
func notFoundScreenFor(comp component.Component) *Screen {
	return NewScreen("/__gofastr_not_found__", comp).WithTitle("Page not found")
}

// RenderNotFoundPageResult renders comp as the not-found page for an
// unmatched path through the same pipeline a registered page takes:
// the app's default (root) layout wraps the body in <main>, the root
// layout's outlets show their defaults, route areas read the requested
// path, and the document shell/head build is the one every page gets.
// The caller owns the 404 status; the result's Kind is DecisionAllow.
func (a *App) RenderNotFoundPageResult(ctx context.Context, path string, comp component.Component) (RenderResult, error) {
	return a.renderScreenPage(ctx, path, notFoundScreenFor(comp), nil)
}

// RenderNotFoundFromResult is RenderNotFoundPageResult's subtree-partial
// form for a client navigating from fromPath: the shared-layer rule is
// the one every route's partial gets, so the answer swaps at the deepest
// layer the two share (the root layout when the origin is any of its
// pages) and carries the kept layers' outlet fills — the defaults
// included. An unknown fromPath keeps the bare partial (empty SwapLayer),
// the runtime's existing deploy-skew recovery answers with a full load.
func (a *App) RenderNotFoundFromResult(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	target := notFoundScreenFor(comp)
	res, err := a.renderScreenPartial(ctx, path, target, nil, nil)
	if err != nil {
		return res, err
	}
	if res.Kind != DecisionAllow && res.Kind != DecisionRenderAlt {
		return res, nil
	}
	return a.partialFromShared(ctx, path, fromPath, res, target, nil, false)
}

// errorScreenFor builds the synthetic page screen a whole-page error
// render flows through: the not-found twin for the 500 case, through
// the same pipeline (per-request instance, DI, Load, article wrap,
// the default layout chain) so the error body renders inside the root
// layout like every page..
func errorScreenFor(comp component.Component) *Screen {
	return NewScreen("/__gofastr_error__", comp).WithTitle("Something went wrong")
}

// RenderErrorPageResult renders comp as the whole-page error body for
// path: the resolver-failure outcome whose page answer is a 500 (see
// PageError). The caller owns the status; the result's Kind is
// DecisionAllow..
func (a *App) RenderErrorPageResult(ctx context.Context, path string, comp component.Component) (RenderResult, error) {
	return a.renderScreenPage(ctx, path, errorScreenFor(comp), nil)
}

// RenderErrorFromResult is RenderErrorPageResult's subtree-partial
// form: the error body under the layers the origin route does not
// share with the root layout, so a navigation whose resolver failed
// whole-page shows the error page inside the live shell instead of
// toasting..
func (a *App) RenderErrorFromResult(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	target := errorScreenFor(comp)
	res, err := a.renderScreenPartial(ctx, path, target, nil, nil)
	if err != nil {
		return res, err
	}
	if res.Kind != DecisionAllow && res.Kind != DecisionRenderAlt {
		return res, nil
	}
	return a.partialFromShared(ctx, path, fromPath, res, target, nil, false)
}

// renderPageResult is RenderPageResult's body.
func (a *App) renderPageResult(ctx context.Context, path string) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}
	return a.renderScreenPage(ctx, path, screen, params)
}

// renderScreenPage is renderPageResult's body over an already-resolved
// screen: a route reached by path, or the synthetic not-found screen
// RenderNotFoundPageResult builds. Everything a page render does —
// match install, policy, per-request instance, DI, Load, fills, the
// layout chain, the document build — runs identically for both, so a
// 404 page renders through the app's root layout with its outlets'
// defaults exactly like a registered page.
func (a *App) renderScreenPage(ctx context.Context, path string, screen *Screen, params map[string]string) (RenderResult, error) {
	// Route areas read the route match from the context. The host's
	// middleware normally installs it; direct renders (SSG, tests) get
	// the same snapshot here so a RouteArea sees the path everywhere.
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}

	// Resolvers the per-request store, the
	// eager declarations, then the policy phase — a guard may read a
	// resolver, and a whole-page failure at this stage IS the page's
	// outcome (ErrNotFound → the not-found page, else the error page).
	ctx = withResolverState(ctx, screen)
	runEagerResolvers(ctx, screen)
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}

	// Evaluate policy chain BEFORE Load, a Redirect/Block decision
	// short-circuits without touching the DB.
	// The layout chain resolves here so the region guards (below) can
	// walk it in the policy phase; the render reuses the same chain.
	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		// Resolved layer keys a {param} group
		// prefix embeds the match's values, so guard addresses, fill
		// addresses and the wrapper markers all carry the resolved key.
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}
	// Fill validation (DESIGN "Validation at mount", render side): a
	// fill naming an outlet whose layout is not in this screen's
	// chain is a wiring mistake that would otherwise be a silent
	// no-op; it panics here naming the layout and the outlet.
	validateFills(screen, chain)
	validateRequires(screen)
	decision := ResolvePolicy(ctx, screen)
	// A guard's resolver read fails the page the same way (the phase
	// recorded on the cell decides, and a guard reads in policy phase);
	// the check sits BEFORE the decision switch so a guard that read a
	// failing resolver answers with the page outcome, not whatever it
	// decided once it saw the error.
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Message: decision.Message}, nil
	}
	// Region guards outlet, area and fill
	// policies run in the policy phase, in declaration order after the
	// screen chain. The first Redirect moves the whole page and no
	// Load runs; RenderAlt/Block are recorded per region and applied
	// when that region resolves.
	guards, guardRedirect := ensureRegionGuards(ctx, screen, chain)
	if guardRedirect.Kind == DecisionRedirect {
		return RenderResult{Kind: DecisionRedirect, URL: guardRedirect.URL}, nil
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
	// The Load phase: resolver reads inside Load are whole-page. A
	// panicking Load takes the same error channel a Load error takes
	// (safeScreenLoad), never an escaped panic, but tagged with
	// ErrScreenPanicked so a host can answer a logged 500 where a
	// returned Load error keeps the 404 contract.
	ctx = withResolvePhase(ctx, resolvePhaseLoad)
	if loader, ok := comp.(ScreenLoader); ok {
		if err := safeScreenLoad(loader, ctx); err != nil {
			// A resolver the Load read (and returned, or swallowed)
			// decides the outcome by its phase: the PageError, not the
			// generic load failure.
			if pe := resolverPageError(ctx, path); pe != nil {
				return RenderResult{}, pe
			}
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}
	// A resolver the Load read may have failed while Load itself
	// succeeded (the value was optional, the error was returned to a
	// caller that ignored it): the cell still decides the outcome.
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}

	// Everything below the screen — fills, route areas, layout
	// builds — reads resolvers in the REGION phase: a failure there is
	// contained to the region that read it, never the page.
	ctx = withResolvePhase(ctx, resolvePhaseRegion)

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
	// The keyed-transition vocabulary of THIS document's chain rides
	// the doc shell (data-fui-vt-kinds, copied onto <html> by the
	// runtime after a swap) beside the pick the answer carries.
	vtKinds := chainVTKinds(chain)
	ctx = withDocShell(ctx, lang, skip, strings.Join(vtKinds, " "))
	// Effective title, resolved BEFORE the render so the route seeding
	// below stamps the value the layouts and fills bind (route.title),
	// and the <head> build reuses it. Same re-read-after-Load rule as
	// before (ScreenTitler, contained), just earlier: nothing between
	// here and the old computation mutates comp.
	titleText := a.Name
	effectiveTitle := screen.Title
	if titler, ok := comp.(ScreenTitler); ok {
		if t := safeScreenTitle(titler); t != "" {
			effectiveTitle = t
		}
	}
	if effectiveTitle != "" {
		titleText = effectiveTitle
		if suffix := " — " + a.Name; !strings.HasSuffix(titleText, suffix) {
			titleText += suffix
		}
	}
	// seed the route.* signal
	// values for this render (bag-resident; Bind stamps them at SSR).
	markChainArea(ctx, chain)
	seedRouteValues(ctx, path, screen.Path, effectiveTitle, params, requestQuery(ctx))
	// Contained fill failures of this render (): nil fills (no
	// tree layers) records none.
	var fillFailures []FillFailure
	notFoundOutcome := false // the 404-outlet outcome, decided once below
	if screen.Type == ScreenPage {
		if len(chain) > 0 {
			var renderErr error
			content, renderErr = renderScreen(ctx, comp)
			if renderErr != nil {
				return RenderResult{}, screenRenderPanicError(path, renderErr)
			}
			content = wrapArticle(screen, comp, content)
			// Outlet fills for tree layers: concurrent, contained
			// (fill.go). A full document never defers: the first load
			// waits for every fill (deferred outlets included) so
			// nothing depends on JavaScript.
			fills := a.resolveFills(ctx, path, params, screen, chain, false, guards)
			if fills != nil { // nil for a chain with no tree layers
				fillFailures = fills.failures
			}
			// The 404-outlet outcome (FallbackNotFound, Decided 5): the
			// route registers, but an outlet nothing fills makes the
			// render the not-found page. The primary becomes the
			// not-found body through the same pipeline a screen gets —
			// fresh instance, DI, Load, safe render, its title — every
			// other outlet renders its Default or nothing, and the host
			// answers 404 (RenderResult.NotFoundOutlet). Only a clean
			// decline decides this; a contained fill failure returned
			// above and left the page alone.
			if fills != nil && fills.notFoundOutlet {
				nfScreen := notFoundScreenFor(a.notFoundBody())
				nfComp := newComponentInstance(nfScreen.Component)
				if err := a.injectComponent(nfComp, path); err != nil {
					return RenderResult{}, err
				}
				if loader, ok := nfComp.(ScreenLoader); ok {
					if err := safeScreenLoad(loader, ctx); err != nil {
						return RenderResult{}, fmt.Errorf("app: not-found body load failed for %q: %w", path, err)
					}
				}
				nfHTML, renderErr := component.SafeRenderCtx(ctx, nfComp)
				if renderErr != nil {
					return RenderResult{}, fmt.Errorf("app: not-found body render error for %q: %w", path, renderErr)
				}
				content = wrapArticle(nfScreen, nfComp, nfHTML)
				comp = nfComp
				effectiveTitle = nfScreen.Title
				if titler, ok := nfComp.(ScreenTitler); ok {
					if t := safeScreenTitle(titler); t != "" {
						effectiveTitle = t
					}
				}
				titleText = a.Name
				if effectiveTitle != "" {
					titleText = effectiveTitle
					if suffix := " — " + a.Name; !strings.HasSuffix(titleText, suffix) {
						titleText += suffix
					}
				}
				// route.title follows the flipped outcome; the rest of
				// the snapshot is the resolved route's own state.
				seedRouteValues(ctx, path, screen.Path, effectiveTitle, params, requestQuery(ctx))
				a.rebuildFillsFromDefaults(ctx, path, params, fills, chain)
				notFoundOutcome = true
			}
			var wrapErr error
			wrapped, wrapErr = renderLayoutChain(ctx, chain, content, fills, guards)
			if wrapErr != nil {
				return RenderResult{}, wrapErr
			}
		} else {
			var renderErr error
			content, renderErr = renderComponentInScreen(ctx, screen, comp)
			if renderErr != nil {
				return RenderResult{}, screenRenderPanicError(path, renderErr)
			}
			wrapped = content
		}
	} else {
		// Drawer/sheet/dialog: render with ARIA wrapping, skip layout
		var renderErr error
		content, renderErr = renderComponentInScreen(ctx, screen, comp)
		if renderErr != nil {
			return RenderResult{}, screenRenderPanicError(path, renderErr)
		}
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
	// Title: effectiveTitle was resolved before the render (see the
	// route seeding above); titleText carries the app-name suffix.
	headChildren = append(headChildren,
		render.Tag("title", nil, render.Text(titleText)),
	)
	// Document language was resolved before the render (it rides the
	// outermost layer too); <html lang> below consumes the same value.

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
	htmlAttrs := map[string]string{"lang": lang}
	if len(vtKinds) > 0 {
		htmlAttrs["data-fui-vt-kinds"] = strings.Join(vtKinds, " ")
	}
	htmlDoc := render.Tag("html", htmlAttrs, head, body)

	out := RenderResult{HTML: render.Join(doctype, htmlDoc), Title: effectiveTitle, Component: comp, FillFailures: fillFailures, NotFoundOutlet: notFoundOutcome, Transition: chainTransitionPick(ctx, chain)}
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
	return a.renderPartialFrom(ctx, path, fromPath, false)
}

// RenderPartialFromResultDefer is RenderPartialFromResult for a page
// request that carries X-Gofastr-Defer: 1 the
// route's DEFERRED outlets skip their loaders entirely and their
// loading content travels in their place — exported as an envelope
// fill for a kept layer, inline in the cell for a rendered layer. The
// fills themselves arrive as separate part requests (RenderPartResult)
// the client launched at the same moment as this one.
func (a *App) RenderPartialFromResultDefer(ctx context.Context, path, fromPath string) (RenderResult, error) {
	return a.renderPartialFrom(ctx, path, fromPath, true)
}

// renderPartialFrom is the shared body of RenderPartialFromResult and
// its deferring variant.
func (a *App) renderPartialFrom(ctx context.Context, path, fromPath string, deferOutlets bool) (RenderResult, error) {
	// The resolver store is per REQUEST: install it here so the base
	// render (renderPartial → renderScreenPartial) and the fills pass
	// below (partialFromShared) share one set of cells — a resolver
	// the Load read is not re-run for the fill that reads it again.
	if target, _, ok := a.Router.Resolve(path); ok {
		ctx = withResolverState(ctx, target)
	}
	res, err := a.renderPartial(ctx, path, nil)
	if err != nil {
		return res, err
	}
	if res.Kind != DecisionAllow && res.Kind != DecisionRenderAlt {
		return res, nil
	}
	target, params, ok := a.Router.Resolve(path)
	if !ok || target.Type != ScreenPage {
		return res, nil
	}
	return a.partialFromShared(ctx, path, fromPath, res, target, params, deferOutlets)
}

// partialFromShared computes the subtree-partial form over an
// already-rendered base result and its resolved target screen: the
// shared-layer rule (deepest chain layer both routes hold), the kept
// layers' fills export, and the swap-layer name. renderPartialFrom
// reaches it after a router resolve; RenderNotFoundFromResult reaches
// it with the synthetic not-found screen, so a 404 partial answers
// with the same boundary semantics any route's partial answers with.
// deferOutlets skips the deferred outlets' loaders and ships their
// loading content in place (X-Gofastr-Defer, .
func (a *App) partialFromShared(ctx context.Context, path, fromPath string, res RenderResult, target *Screen, params map[string]string, deferOutlets bool) (RenderResult, error) {
	// Clients may send the origin with its query string (the intercept
	// module reuses the same header with pathname+search); the chain is
	// a property of the route, so resolve on the pathname alone.
	if i := strings.IndexByte(fromPath, '?'); i >= 0 {
		fromPath = fromPath[:i]
	}
	from, fromParams, ok := a.Router.Resolve(fromPath)
	if !ok {
		return res, nil
	}
	// Resolved keys on BOTH matches the shared
	// depth is computed on the keys the DOM actually holds — a
	// {param} group's layer compares equal only within the same
	// resolved value, so another project's page re-renders the layer
	// instead of silently keeping the first project's chrome.
	tChain := resolveChainKeys(a.Router.layoutChainFor(target), params)
	fChain := resolveChainKeys(a.Router.layoutChainFor(from), fromParams)
	// The partial path validates fills against the TARGET's chain —
	// the same render-time rule the full page applies.
	validateFills(target, tChain)
	validateRequires(target)
	shared := 0
	for shared < len(tChain) && shared < len(fChain) &&
		tChain[shared].Layout == fChain[shared].Layout &&
		tChain[shared].Key() != "" &&
		tChain[shared].Key() == fChain[shared].Key() {
		shared++
	}
	if shared == 0 {
		// A layout-less destination has no layer to carry the doc markers,
		// and the runtime fills the existing <main> with the bare partial,
		// so a language or skip-label change between two layout-less pages
		// could never reach the document. Name a swap layer no DOM holds:
		// the runtime answers a missing layer with a full-page fetch (its
		// deploy-skew recovery), and the full page's bare <main> carries
		// the markers. Same cost as a cross-chain navigation, paid only
		// when the values differ. The origin is resolved by route alone
		// (its ScreenLang is not re-read), so a route whose language comes
		// from ScreenLang declares it in LangFunc as well.
		if len(tChain) == 0 && len(fChain) == 0 {
			lang, skip := a.docLangFor(path, res.Component), a.SkipLabelForPath(path)
			if lang != a.LangForPath(fromPath) || skip != a.SkipLabelForPath(fromPath) {
				res.SwapLayer = docShellSwapPrefix + lang
			}
		}
		return res, nil
	}
	// The doc markers must ride the partial's outermost layer, the same
	// values the full page would carry: without them the document language
	// and skip link could never change on an in-chain swap. ScreenLang is
	// layered like the full-page path so both render shapes agree.
	ctx = withDocShell(ctx, a.docLangFor(path, res.Component), a.SkipLabelForPath(path), strings.Join(chainVTKinds(tChain), " "))
	// renderPartial installed the route match on its own ctx copy; the
	// collect-mode builds below read it from THIS ctx, so install it here
	// too when absent (kept layers' route areas re-run for the target).
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(target.Path, path, params))
	}
	// The fills and kept-layer builds below read resolvers in the
	// REGION phase (the screen's Load already ran on this store inside
	// renderScreenPartial): a failing read is contained to its region,
	// never this partial.
	ctx = withResolvePhase(ctx, resolvePhaseRegion)
	// seed the route.* values for
	// the fills and kept-layer builds below (route-title bindings in a
	// kept layout stamp the TARGET's title, not the origin's). The
	// effective title comes off the rendered result (post-Load), with
	// the registered title as fallback.
	routeTitle := res.Title
	if routeTitle == "" {
		routeTitle = target.Title
	}
	markChainArea(ctx, tChain)
	seedRouteValues(ctx, path, target.Path, routeTitle, params, requestQuery(ctx))
	// The region decisions the policy phase recorded (renderScreenPartial
	// ran the guards on this request's store): the fills pass applies
	// the same per-region outcomes the full page would.
	guards, _ := ensureRegionGuards(ctx, target, tChain)

	// Outlet fills resolve like the full-page path (concurrent,
	// contained), then the kept layers' builds run in collect mode
	// inside renderLayoutChainFrom so their route areas record fresh
	// fills too. The kept layers' (0..shared-1) fills travel on the
	// result; the rendered layers' outlets are inline in the payload.
	// Under X-Gofastr-Defer the deferred outlets' loading content takes
	// the place of every one of those (kept fills and inline cells
	// alike), and the real fills arrive as part requests.
	fills := a.resolveFills(ctx, path, params, target, tChain, deferOutlets, guards)
	// The 404-outlet outcome, decided once on the partial path too: the
	// payload's screen body becomes the not-found body (the same
	// instance/load/render containment the full page gives it), the
	// kept layers' fills are rebuilt from their Defaults, and the host
	// answers the partial 404-shaped (writePartialResult reads
	// NotFoundOutlet). The client applies the envelope and never caches
	// a 404 answer.
	if fills != nil && fills.notFoundOutlet {
		nfScreen := notFoundScreenFor(a.notFoundBody())
		nfComp := newComponentInstance(nfScreen.Component)
		if err := a.injectComponent(nfComp, path); err != nil {
			return res, err
		}
		if loader, ok := nfComp.(ScreenLoader); ok {
			if err := safeScreenLoad(loader, ctx); err != nil {
				return res, fmt.Errorf("app: not-found body load failed for %q: %w", path, err)
			}
		}
		nfHTML, renderErr := component.SafeRenderCtx(ctx, nfComp)
		if renderErr != nil {
			return res, fmt.Errorf("app: not-found body render error for %q: %w", path, renderErr)
		}
		res.HTML = wrapArticle(nfScreen, nfComp, nfHTML)
		res.Component = nfComp
		res.Title = nfScreen.Title
		if titler, ok := nfComp.(ScreenTitler); ok {
			if t := safeScreenTitle(titler); t != "" {
				res.Title = t
			}
		}
		res.NotFoundOutlet = true
		seedRouteValues(ctx, path, target.Path, res.Title, params, requestQuery(ctx))
		a.rebuildFillsFromDefaults(ctx, path, params, fills, tChain)
	}
	html, wrapErr := renderLayoutChainFrom(ctx, tChain, shared, res.HTML, fills, guards)
	if wrapErr != nil {
		return res, wrapErr
	}
	res.HTML = html
	res.Fills = exportFillsFor(tChain, fills, shared)
	res.SwapLayer = tChain[shared-1].Key()
	// The page answer's pick innermost layer
	// with a TransitionFor wins; unknown names already resolved to "".
	res.Transition = chainTransitionPick(ctx, tChain)
	if fills != nil { // nil when the chain holds no tree layers
		res.FillFailures = fills.failures
	}
	return res, nil
}

// PartOutcome classifies a part request's answer (RenderPartResult,
// .
type PartOutcome int

const (
	// PartApplied: the fill rendered; Fill carries its address and HTML
	// (empty HTML for an outlet nothing fills — that is the region's
	// answer, not a failure).
	PartApplied PartOutcome = iota
	// PartReset: a WHOLE-PAGE outcome. The route did not resolve, the
	// address is not a deferred outlet of it, the policy phase
	// redirected or blocked, or the render errored before the fill
	// could run. The host answers 409 with X-Gofastr-Part-Reset and
	// applies nothing; the client reloads the URL as a whole document.
	PartReset
)

// RenderPartResult renders ONE deferred outlet's fill for a part
// request (X-Gofastr-Part, the policy phase runs,
// then the outlet's candidates resolve and ONLY the winning fill's
// loader executes — no screen Load, no other fills, no area builds,
// no layout chain rendering. A failing fill is contained to the region
// exactly as the page contains it (the fallback HTML is the answer), so
// the outcome is PartReset only for whole-page disagreement.
func (a *App) RenderPartResult(ctx context.Context, path, addr string) (Fill, PartOutcome) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok || screen.Type != ScreenPage {
		return Fill{}, PartReset
	}
	chain := resolveChainKeys(a.Router.layoutChainFor(screen), params)
	slots := fillSlots(screen, chain)
	var slot *fillSlot
	for i := range slots {
		if slots[i].addr == addr {
			slot = &slots[i]
			break
		}
	}
	// A forged address, or one that belongs to another route's chain or
	// to an outlet that does not defer, is a whole-page disagreement.
	if slot == nil || !slot.spec.Deferred {
		return Fill{}, PartReset
	}
	// The policy phase, resolvers included: a whole-page resolver
	// outcome (eager, or a guard's read) is the 409 reset — the part
	// answer must never carry one side of a disagreement.
	ctx = withResolverState(ctx, screen)
	runEagerResolvers(ctx, screen)
	if resolverPageError(ctx, path) != nil {
		return Fill{}, PartReset
	}
	if decision := ResolvePolicy(ctx, screen); decision.Kind == DecisionRedirect || decision.Kind == DecisionBlock {
		return Fill{}, PartReset
	}
	if resolverPageError(ctx, path) != nil {
		return Fill{}, PartReset
	}
	// The region guards ran in the policy phase above (same store); a
	// Redirect among them is a whole-page outcome the part can never
	// carry — the reset. The requested region's own recorded decision
	// (Block → its fallback, RenderAlt → the alt) applies through
	// resolveFillSlot exactly as the page applies it.
	guards, guardRedirect := ensureRegionGuards(ctx, screen, chain)
	if guardRedirect.Kind == DecisionRedirect {
		return Fill{}, PartReset
	}
	// The fill renders in the region phase: its resolver reads are
	// contained to it, never the page.
	ctx = withResolvePhase(ctx, resolvePhaseRegion)
	// Route values seed before the fill renders so a fill binding
	// route.* stamps the target's state (the registered title: the
	// effective one is only known after a screen Load, which a part
	// never runs).
	markChainArea(ctx, chain)
	seedRouteValues(ctx, path, screen.Path, screen.Title, params, requestQuery(ctx))
	res := a.resolveFillSlot(ctx, path, params, *slot, guards)
	if res.notFound {
		// The 404-outlet outcome is a WHOLE-PAGE outcome: the page this
		// part belongs to answers the not-found page with status 404, so
		// the part applies nothing and the client reloads the URL as a
		// whole document (the reset) — the full load then receives the
		// real 404 (DESIGN "Parallel requests": a part reaching a
		// whole-page outcome answers 409 and applies nothing).
		return Fill{}, PartReset
	}
	// A failing fill is contained to its region exactly as the page
	// contains it: the fallback HTML IS the answer (PartApplied), so
	// the outcome is PartReset only for whole-page disagreement.
	return Fill{Addr: addr, HTML: res.html}, PartApplied
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

// ErrScreenPanicked reports that a screen's Load or Render panicked
// while the app rendered it. Every render entry point (RenderPageResult,
// RenderPartialResult, RenderPartialFromResult, RenderOverlayResult, and
// Router.RenderRaw) wraps it into the error a contained panic becomes,
// and nothing else does: a Load that returns an error, an unknown path,
// and a DI wiring failure all keep their own errors. Hosts discriminate
// with errors.Is so a panic answers a logged 500 while every other
// render error keeps the 404 it contracted.
var ErrScreenPanicked = errors.New("screen panicked")

// renderScreen renders a screen's component under the SSR containment.
// A screen that implements component.ErrorBoundary has taken ownership of
// its own render failure: its RenderError markup is the answer and no
// error is returned (the panic is still logged with its stack by the
// component recovery). Any other screen's contained panic comes back as
// the error the caller wraps with screenRenderPanicError.
func renderScreen(ctx context.Context, comp component.Component) (render.HTML, error) {
	out, err := component.SafeRenderCtx(ctx, comp)
	if err != nil {
		if _, ok := comp.(component.ErrorBoundary); ok {
			return out, nil
		}
		return "", err
	}
	return out, nil
}

// screenRenderPanicError is the one wrap every render site puts around a
// contained render panic: it names the path and tags the chain with
// ErrScreenPanicked so a host can tell a 500-worthy panic apart from the
// 404 a Load error keeps.
func screenRenderPanicError(path string, err error) error {
	return fmt.Errorf("app: component render error for %q: %w: %w", path, ErrScreenPanicked, err)
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
	return a.renderScreenPartial(ctx, path, screen, params, overlay)
}

// renderScreenPartial is renderPartial's body over an already-resolved
// screen (a route, or the synthetic not-found screen), the partial-path
// twin of renderScreenPage.
func (a *App) renderScreenPartial(ctx context.Context, path string, screen *Screen, params map[string]string, overlay *ScreenType) (RenderResult, error) {
	// Same route-match installation as RenderPageResult: route areas of
	// tree layouts read the match off the context on every render path.
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}

	// Resolvers, the same phase ladder the full
	// page walks: store + eager, policy, Load, then the region phase
	// for whatever renders below (renderPartialFrom's fills re-run on
	// the SAME store this installed — one cell per name per request).
	ctx = withResolverState(ctx, screen)
	runEagerResolvers(ctx, screen)
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}

	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}
	decision := ResolvePolicy(ctx, screen)
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Message: decision.Message}, nil
	}
	// Region guards run here too a Redirect
	// moves the page even on the bare partial path, and the recorded
	// per-region decisions ride the store for partialFromShared's fills
	// pass — one set of decisions per request.
	guards, guardRedirect := ensureRegionGuards(ctx, screen, chain)
	if guardRedirect.Kind == DecisionRedirect {
		return RenderResult{Kind: DecisionRedirect, URL: guardRedirect.URL}, nil
	}
	_ = guards // the bare partial renders no regions; the fills pass reads the store

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

	ctx = withResolvePhase(ctx, resolvePhaseLoad)
	if loader, ok := comp.(ScreenLoader); ok {
		if err := safeScreenLoad(loader, ctx); err != nil {
			if pe := resolverPageError(ctx, path); pe != nil {
				return RenderResult{}, pe
			}
			return RenderResult{}, fmt.Errorf("app: load failed for %q: %w", path, err)
		}
	}
	if pe := resolverPageError(ctx, path); pe != nil {
		return RenderResult{}, pe
	}
	// The partial's own body is the screen's; a layout's fills and
	// areas (renderPartialFrom) render in the region phase.
	ctx = withResolvePhase(ctx, resolvePhaseRegion)

	// seed the route.* values
	// before the component renders — a SCREEN-level route binding (not
	// only layouts and fills) must resolve on the partial path too.
	// Title here is the registered one (pre-Load);
	// RenderPartialFromResult re-seeds the effective title after Load.
	partialRouteTitle := screen.Title
	markChainArea(ctx, chain)
	seedRouteValues(ctx, path, screen.Path, partialRouteTitle, params, requestQuery(ctx))
	effType := screen.Type
	if overlay != nil {
		effType = *overlay
	}
	var body render.HTML
	if effType == ScreenPage {
		html, renderErr := renderScreen(ctx, comp)
		if renderErr != nil {
			return RenderResult{}, screenRenderPanicError(path, renderErr)
		}
		// Same article wrapping as the full-page path, without it, SPA
		// navigation silently dropped the <article> element Reader Mode
		// keys on, so an article page lost reader support after the first
		// client-side visit.
		body = wrapArticle(screen, comp, html)
	} else {
		var renderErr error
		body, renderErr = renderComponentAs(ctx, screen, effType, comp)
		if renderErr != nil {
			return RenderResult{}, screenRenderPanicError(path, renderErr)
		}
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
func renderComponentInScreen(ctx context.Context, screen *Screen, comp component.Component) (render.HTML, error) {
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
// kill the request with no response. A panicking screen returns the
// contained error instead of the generic fallback box; the caller wraps
// it with ErrScreenPanicked so the host answers a logged 500 rather than
// shipping the panic text inside a 200 page. An ErrorBoundary screen
// answers with its own RenderError markup instead (renderScreen).
func renderComponentAs(ctx context.Context, screen *Screen, effType ScreenType, comp component.Component) (render.HTML, error) {
	content, renderErr := renderScreen(ctx, comp)
	if renderErr != nil {
		return "", renderErr
	}
	content = wrapArticle(screen, comp, content)
	// A layout-less ScreenPage page carries the doc markers on its bare
	// <main>: that element is what the runtime's swapShell targets for a
	// layout-less destination, and the document language / skip label
	// must arrive with it. Mirrors wrapByScreenType's ScreenPage arm with
	// the extra attributes; every other type keeps the shared wrapper.
	if attrs := docShellAttrs(ctx); attrs != nil && effType == ScreenPage {
		return html.Main(html.MainConfig{ExtraAttrs: attrs}, content), nil
	}
	return wrapByScreenType(effType, screen.Title, content), nil
}

// safeScreenLoad runs the ScreenLoader hook under the SSR containment: a
// panicking Load is converted to the same error channel a Load error
// takes, never an escaped panic — but tagged with ErrScreenPanicked so a
// host can answer 500 + a logged panic where a returned Load error keeps
// the 404 contract. The panicked-on bytes are scrubbed through
// textsafe.Recovered; they are host/component state and must not forge
// log lines.
func safeScreenLoad(loader ScreenLoader, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: Load: %s", ErrScreenPanicked, textsafe.Recovered(r))
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
