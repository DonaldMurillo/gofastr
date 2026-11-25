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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// App is the root of the UI hierarchy.
type App struct {
	// Name is the application name, used in the page title.
	Name     string
	ownStyle *ownstyle.Sheet
	// Container is the dependency injection container.
	Container *di.Container
	// Router maps paths to screens and layouts.
	Router *Router
	// Theme holds optional theme configuration (can be nil).
	Theme *style.Theme

	// NoLLMMD disables auto-generated llm.md for all pages in this app.
	NoLLMMD bool

	// Lang is the document's BCP-47 language tag; empty is "en".
	Lang string

	// LangFunc resolves the document language per route.
	LangFunc func(path string) string

	// SkipLabel is the visible text of the app shell's skip link, the first string a keyboard user meets.
	SkipLabel string

	// SkipLabelFunc resolves the skip-link text per route, like LangFunc for the document language.
	SkipLabelFunc func(path string) string

	// NotFound is the body the 404-outlet outcome renders (an outlet declared FallbackNotFound that no
	// candidate fills, Decided 5): the primary becomes this component through the full screen pipeline
	// — per-request instance, DI, Load, safe render, its title — while every other outlet renders
	// its Default or nothing, and the host answers 404.
	NotFound component.Component
}

// WithNotFoundBody sets the body the 404-outlet outcome renders and returns the app for chaining
// (see App.NotFound).
func (a *App) WithNotFoundBody(c component.Component) *App {
	RefuseRetiredScreenMethods("not-found body", c)
	a.NotFound = c
	return a
}

func (a *App) notFoundBody() component.Component {
	if a.NotFound != nil {
		return a.NotFound
	}
	return minimalNotFoundBody{}
}

type minimalNotFoundBody struct{ component.ContextOnly }

func (minimalNotFoundBody) RenderCtx(ctx context.Context) render.HTML {
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageNotFoundTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, render.Text(pageNotFoundBody.Text(ctx))),
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

// WithTheme sets the application theme and returns the app for chaining.
func (a *App) WithTheme(theme style.Theme) *App {
	style.AutoFillNames(&theme)
	theme.MustValidate()
	if err := ownstyle.CheckTheme(theme); err != nil {
		panic(fmt.Sprintf("app: WithTheme: %v", err))
	}
	a.Theme = &theme
	return a
}

// WithLang sets the document's BCP-47 language tag.
func (a *App) WithLang(lang string) *App {
	a.Lang = lang
	return a
}

// WithLangFunc sets a per-route document-language resolver and returns the app for chaining.
func (a *App) WithLangFunc(fn func(path string) string) *App {
	a.LangFunc = fn
	return a
}

// The skip link is the first thing a keyboard user tabs to; a non-English app that sets nothing
// greets them in English.
func (a *App) WithSkipLabel(label string) *App {
	a.SkipLabel = label
	return a
}

// WithSkipLabelFunc sets a per-route skip-link resolver and returns the app for chaining.
func (a *App) WithSkipLabelFunc(fn func(path string) string) *App {
	a.SkipLabelFunc = fn
	return a
}

func (a *App) docLangFor(ctx context.Context, path string, comp any) string {
	return ScreenLanguage(comp, a.LangFor(ctx, path))
}

// ScreenLanguage returns a screen's trimmed language override or fallback
// when the screen supplies no language or its hook panics.
func ScreenLanguage(comp any, fallback string) string {
	if langer, ok := comp.(ScreenLanger); ok {
		if l := strings.TrimSpace(safeScreenLang(langer)); l != "" {
			return l
		}
	}
	return fallback
}

// SkipLabelFor returns the skip-link text for a route: SkipLabelFunc's answer when it gives one,
// else SkipLabel, else "Skip to main content" in the request's language.
func (a *App) SkipLabelFor(ctx context.Context, path string) string {
	if a.SkipLabelFunc != nil {
		if l := strings.TrimSpace(a.SkipLabelFunc(path)); l != "" {
			return l
		}
	}
	if a.SkipLabel != "" {
		return a.SkipLabel
	}
	return skipToMain.Text(ctx)
}

// EffectiveLang returns the document language, defaulting to "en" when unset.
func (a *App) EffectiveLang() string {
	if a.Lang != "" {
		return a.Lang
	}
	return "en"
}

// LangFor returns LangFunc's answer for path, else the negotiated locale, else EffectiveLang.
func (a *App) LangFor(ctx context.Context, path string) string {
	if a.LangFunc != nil {
		if lang := strings.TrimSpace(a.LangFunc(path)); lang != "" {
			return lang
		}
	}
	if tag := i18n.FromContext(ctx).Tag; tag != "" {
		return tag
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

// Register adds a screen to the app by reading metadata from the component if it implements
// ScreenTitler, ScreenDescriber, or ScreenTyper.
func (a *App) Register(path string, comp component.Component, layout *Layout, opts ...ScreenOption) {
	screen := &Screen{
		Path:      path,
		Name:      path,
		Type:      ScreenPage,
		Component: comp,
	}

	if titler, ok := comp.(ScreenTitler); ok {
		screen.Title = titler.ScreenTitle(context.Background())
	}
	if describer, ok := comp.(ScreenDescriber); ok {
		screen.Description = describer.ScreenDescription(context.Background())
	}
	if typer, ok := comp.(ScreenTyper); ok {
		screen.Type = typer.ScreenType()
	}
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
	// Layouts is the route's resolved layout chain as layer keys, outermost → innermost ("l:<name>"
	// for plain layers, "g:<prefix>" for screen-group layers.
	Layouts []string
	// Preload declares how the client may prefetch this route's content before it is navigated to: ""
	// (never, the default), "hover" (when a link to it is hovered/focused), "visible" (when a link
	// scrolls into view), or "eager" (at idle after page load).
	Preload string
	// RedirectTo is non-empty when this entry is a redirect (Redirect / RedirectPattern) rather than a
	// screen.
	RedirectTo string
	// NoSPA excludes the route from the client manifest: links to it resolve as full document loads
	// instead of soft navigations.
	NoSPA bool
	// Intercept is set when the screen presents as an overlay for soft navigations from a declared
	// origin (see InterceptFrom).
	Intercept *Intercept
	// Deferred lists the wire addresses of the route's DEFERRED outlets regions whose fills do not
	// travel with the page request but as their own part requests (X-Gofastr-Part), one per address,
	// launched by the client at the same moment as the page fetch.
	Deferred []string
	// Loading carries the screen's or its group's swap-slot loading declaration for the manifest.
	Loading *LoadingManifest
}

// LoadingManifest is the wire form of a Loading declaration in the route manifest: the loading
// content and the timings the client needs.
type LoadingManifest struct {
	Show  component.Component
	After int // milliseconds
	Min   int // milliseconds
}

// HTML renders the loading content under ctx, which carries the reader's locale.
func (m *LoadingManifest) HTML(ctx context.Context) string {
	if m == nil || m.Show == nil {
		return ""
	}
	html, err := component.SafeRenderCtx(ctx, m.Show)
	if err != nil {
		return ""
	}
	return string(html)
}

func loadingManifestFor(ld *Loading) *LoadingManifest {
	if ld == nil || ld.Show == nil {
		return nil
	}
	return &LoadingManifest{
		Show:  ld.Show,
		After: ld.loadingAfterMs(),
		Min:   ld.loadingMinMs(),
	}
}

// Routes returns every registered route, screens and redirects, as RouteEntry slices.
func (a *App) Routes() []RouteEntry {
	var entries []RouteEntry
	for _, path := range a.Router.Paths() {
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
			Loading:     loadingManifestFor(screenLoading(screen)),
		})
	}
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

// Layouts returns every layout a registered page renders in: the default layout and each screen's
// layout chain, each layout once, sorted by name.
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

// LayoutsVersion changes whenever a screen or the default layout is registered.
func (a *App) LayoutsVersion() uint64 {
	return a.Router.layoutsGen.Load()
}

// RenderScreenRaw is a policy-bypassing convenience over Router.RenderRaw.
func (a *App) RenderScreenRaw(path string) (render.HTML, error) {
	return a.Router.RenderRaw(path)
}

// RenderPage renders a full HTML page (<!DOCTYPE html><html>...) for a screen path.
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

func (a *App) injectComponent(comp component.Component, path string) error {
	if cv := reflect.ValueOf(comp); cv.Kind() == reflect.Pointer {
		if err := a.Inject(comp); err != nil {
			return fmt.Errorf("app: DI injection failed for %q: %w", path, err)
		}
		return nil
	}
	if n := di.InjectTagCount(reflect.TypeOf(comp)); n > 0 {
		return fmt.Errorf(
			"app: screen component for %q has %d inject-tagged field(s) but was registered by value as %T; register a pointer (&%T{...}) so DI can fill them",
			path, n, comp, comp)
	}
	return nil
}

func (a *App) RenderPageResult(ctx context.Context, path string) (RenderResult, error) {
	return a.renderPageResult(ctx, path)
}

func notFoundScreenFor(ctx context.Context, comp component.Component) *Screen {
	RefuseRetiredScreenMethods("not-found body", comp)
	screen := NewScreen("/__gofastr_not_found__", comp).WithTitle(pageNotFoundDocTitle.Text(ctx))
	screen.notFoundOutcome = true
	return screen
}

// RenderNotFoundPageResult renders comp as the not-found page through the full page pipeline.
func (a *App) RenderNotFoundPageResult(ctx context.Context, path string, comp component.Component) (RenderResult, error) {
	return a.renderScreenPage(ctx, path, notFoundScreenFor(ctx, comp), nil)
}

// RenderNotFoundFromResult is RenderNotFoundPageResult as a subtree partial from fromPath.
func (a *App) RenderNotFoundFromResult(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	return withPageIDs(ctx, func(ctx context.Context) (RenderResult, error) {
		return a.renderNotFoundFromResultIDs(ctx, path, fromPath, comp)
	})
}

func (a *App) renderNotFoundFromResultIDs(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	return a.renderSyntheticFromResultIDs(ctx, path, fromPath, notFoundScreenFor(ctx, comp))
}

func errorScreenFor(ctx context.Context, comp component.Component) *Screen {
	RefuseRetiredScreenMethods("error body", comp)
	screen := NewScreen("/__gofastr_error__", comp).WithTitle(pageErrorDocTitle.Text(ctx))
	screen.errorOutcome = true
	return screen
}

// RenderErrorPageResult renders comp as the whole-page error body for path: the resolver-failure
// outcome whose page answer is a 500 (see PageError).
func (a *App) RenderErrorPageResult(ctx context.Context, path string, comp component.Component) (RenderResult, error) {
	return a.renderScreenPage(ctx, path, errorScreenFor(ctx, comp), nil)
}

// RenderErrorFromResult is RenderErrorPageResult as a subtree partial from fromPath.
func (a *App) RenderErrorFromResult(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	return withPageIDs(ctx, func(ctx context.Context) (RenderResult, error) {
		return a.renderErrorFromResultIDs(ctx, path, fromPath, comp)
	})
}

func (a *App) renderErrorFromResultIDs(ctx context.Context, path, fromPath string, comp component.Component) (RenderResult, error) {
	return a.renderSyntheticFromResultIDs(ctx, path, fromPath, errorScreenFor(ctx, comp))
}

func (a *App) renderSyntheticFromResultIDs(ctx context.Context, path, fromPath string, target *Screen) (RenderResult, error) {
	res, err := a.renderScreenPartial(ctx, path, target, nil, nil)
	if err != nil {
		return res, err
	}
	if res.Title == "" {
		res.Title = target.Title
	}
	if res.Kind != DecisionAllow && res.Kind != DecisionRenderAlt {
		return res, nil
	}
	return a.partialFromShared(ctx, path, fromPath, res, target, nil, false)
}

func (a *App) renderPageResult(ctx context.Context, path string) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}
	return a.renderScreenPage(ctx, path, screen, params)
}

func (a *App) renderScreenPage(ctx context.Context, path string, screen *Screen, params map[string]string) (RenderResult, error) {
	return withPageIDs(ctx, func(ctx context.Context) (RenderResult, error) {
		return a.renderScreenPageIDs(ctx, path, screen, params)
	})
}

func (a *App) renderScreenPageIDs(ctx context.Context, path string, screen *Screen, params map[string]string) (RenderResult, error) {
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}

	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}
	validateFills(screen, chain)
	validateRequires(screen)
	ctx, phase := runPolicyPhase(ctx, screen, path, chain)
	if phase.err != nil {
		return RenderResult{}, phase.err
	}
	decision := phase.decision
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Reason: decision.Reason, Params: decision.Params}, nil
	}
	guards, guardRedirect := phase.guards, phase.redirect
	if guardRedirect.Kind == DecisionRedirect {
		return RenderResult{Kind: DecisionRedirect, URL: guardRedirect.URL}, nil
	}

	comp := screen.newInstance()
	if decision.Kind == DecisionRenderAlt && decision.AltFactory != nil {
		comp = decision.AltFactory()
		RefuseRetiredScreenMethods("screen "+screen.Path+" RenderAlt", comp)
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

	ctx = withResolvePhase(ctx, resolvePhaseRegion)

	var content render.HTML
	var wrapped render.HTML
	lang := a.docLangFor(ctx, path, comp)
	skip := a.SkipLabelFor(ctx, path)
	vtKinds := chainVTKinds(chain)
	ctx = withDocShell(ctx, lang, skip, strings.Join(vtKinds, " "))
	titleText := a.Name
	effectiveTitle := registeredTitle(screen, decision.Kind)
	if titler, ok := comp.(ScreenTitler); ok {
		if t := safeScreenTitle(ctx, titler); t != "" {
			effectiveTitle = t
		}
	}
	if effectiveTitle != "" {
		titleText = effectiveTitle
		if suffix := " — " + a.Name; !strings.HasSuffix(titleText, suffix) {
			titleText += suffix
		}
	}
	markChainArea(ctx, chain)
	seedRouteValues(ctx, path, screen.Path, effectiveTitle, params, requestQuery(ctx))
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
			fills := a.resolveFills(ctx, path, params, screen, chain, false, guards)
			if fills != nil { // nil for a chain with no tree layers
				fillFailures = fills.failures
			}
			if fills != nil && fills.notFoundOutlet && !screen.notFoundOutcome {
				nfScreen := notFoundScreenFor(ctx, a.notFoundBody())
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
				lang = a.docLangFor(ctx, path, comp)
				ctx = withDocShell(ctx, lang, skip, strings.Join(vtKinds, " "))
				effectiveTitle = nfScreen.Title
				if titler, ok := nfComp.(ScreenTitler); ok {
					if t := safeScreenTitle(ctx, titler); t != "" {
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
			content, renderErr = renderComponentInScreen(ctx, screen, comp, registeredTitle(screen, decision.Kind))
			if renderErr != nil {
				return RenderResult{}, screenRenderPanicError(path, renderErr)
			}
			wrapped = content
		}
	} else {
		var renderErr error
		content, renderErr = renderComponentInScreen(ctx, screen, comp, registeredTitle(screen, decision.Kind))
		if renderErr != nil {
			return RenderResult{}, screenRenderPanicError(path, renderErr)
		}
		wrapped = content
	}

	var headChildren []render.HTML
	headChildren = append(headChildren,
		render.VoidTag("meta", map[string]string{"charset": "UTF-8"}),
	)
	headChildren = append(headChildren,
		render.VoidTag("meta", map[string]string{
			"name":    "viewport",
			"content": "width=device-width, initial-scale=1.0",
		}),
		render.VoidTag("meta", map[string]string{
			"name":    "text-scale",
			"content": "scale",
		}),
	)
	headChildren = append(headChildren,
		render.Tag("title", nil, render.Text(titleText)),
	)

	head := render.Tag("head", nil, headChildren...)

	skipLink := render.Tag("a", map[string]string{
		"href":           "#main-content",
		"class":          "skip-link",
		"data-skip-link": "",
	}, render.Text(skip))

	routeAnnounce := render.Tag("div", map[string]string{
		"id":          "cui-route-announce",
		"role":        "status",
		"aria-live":   "polite",
		"aria-atomic": "true",
		"class":       "cui-visually-hidden",
	}, render.Text(""))

	body := render.Tag("body", nil, skipLink, routeAnnounce, wrapped)

	doctype := render.Raw("<!DOCTYPE html>")
	htmlAttrs := map[string]string{"lang": lang, "dir": i18n.Dir(lang)}
	if len(vtKinds) > 0 {
		htmlAttrs["data-cui-vt-kinds"] = strings.Join(vtKinds, " ")
	}
	htmlDoc := render.Tag("html", htmlAttrs, head, body)

	out := RenderResult{HTML: render.Join(doctype, htmlDoc), Title: effectiveTitle, DocumentLanguage: lang, Component: comp, FillFailures: fillFailures, NotFoundOutlet: notFoundOutcome, Transition: chainTransitionPick(ctx, chain)}
	if decision.Kind == DecisionRenderAlt {
		out.Kind = DecisionRenderAlt
	} else {
		out.Kind = DecisionAllow
	}
	return out, nil
}

// RenderPartial returns just the screen content (no layout, no <html>/<head>/<body>).
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
func (a *App) RenderPartialResult(ctx context.Context, path string) (RenderResult, error) {
	return a.renderPartial(ctx, path, nil)
}

// RenderPartialFromResult renders the screen at path as a subtree partial for a client navigating
// from fromPath.
func (a *App) RenderPartialFromResult(ctx context.Context, path, fromPath string) (RenderResult, error) {
	return a.renderPartialFrom(ctx, path, fromPath, false)
}

// RenderPartialFromResultDefer is RenderPartialFromResult with the route's deferred outlets skipped.
func (a *App) RenderPartialFromResultDefer(ctx context.Context, path, fromPath string) (RenderResult, error) {
	return a.renderPartialFrom(ctx, path, fromPath, true)
}

func (a *App) renderPartialFrom(ctx context.Context, path, fromPath string, deferOutlets bool) (RenderResult, error) {
	return withPageIDs(ctx, func(ctx context.Context) (RenderResult, error) {
		return a.renderPartialFromIDs(ctx, path, fromPath, deferOutlets)
	})
}

func (a *App) renderPartialFromIDs(ctx context.Context, path, fromPath string, deferOutlets bool) (RenderResult, error) {
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

func (a *App) partialFromShared(ctx context.Context, path, fromPath string, res RenderResult, target *Screen, params map[string]string, deferOutlets bool) (RenderResult, error) {
	if i := strings.IndexByte(fromPath, '?'); i >= 0 {
		fromPath = fromPath[:i]
	}
	from, fromParams, ok := a.Router.Resolve(fromPath)
	if !ok {
		return res, nil
	}
	tChain := resolveChainKeys(a.Router.layoutChainFor(target), params)
	fChain := resolveChainKeys(a.Router.layoutChainFor(from), fromParams)
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
		if len(tChain) == 0 && len(fChain) == 0 {
			lang, skip := res.DocumentLanguage, a.SkipLabelFor(ctx, path)
			if lang != a.LangFor(ctx, fromPath) || skip != a.SkipLabelFor(ctx, fromPath) {
				res.SwapLayer = docShellSwapPrefix + lang
			}
		}
		return res, nil
	}
	skip, vtKinds := a.SkipLabelFor(ctx, path), strings.Join(chainVTKinds(tChain), " ")
	ctx = withDocShell(ctx, res.DocumentLanguage, skip, vtKinds)
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(target.Path, path, params))
	}
	ctx = withResolvePhase(ctx, resolvePhaseRegion)
	routeTitle := res.Title
	if routeTitle == "" {
		routeTitle = registeredTitle(target, res.Kind)
	}
	markChainArea(ctx, tChain)
	seedRouteValues(ctx, path, target.Path, routeTitle, params, requestQuery(ctx))
	guards, _ := ensureRegionGuards(ctx, target, tChain)

	fills := a.resolveFills(ctx, path, params, target, tChain, deferOutlets, guards)
	if fills != nil && fills.notFoundOutlet && !target.notFoundOutcome {
		nfScreen := notFoundScreenFor(ctx, a.notFoundBody())
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
		res.DocumentLanguage = a.docLangFor(ctx, path, nfComp)
		ctx = withDocShell(ctx, res.DocumentLanguage, skip, vtKinds)
		res.Title = nfScreen.Title
		if titler, ok := nfComp.(ScreenTitler); ok {
			if t := safeScreenTitle(ctx, titler); t != "" {
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
	res.Transition = chainTransitionPick(ctx, tChain)
	if fills != nil { // nil when the chain holds no tree layers
		res.FillFailures = fills.failures
	}
	return res, nil
}

// PartOutcome classifies a part request's answer (RenderPartResult, .
type PartOutcome int

const (
	// PartApplied: the fill rendered; Fill carries its address and HTML (empty HTML for an outlet
	// nothing fills — that is the region's answer, not a failure).
	PartApplied PartOutcome = iota
	// PartReset: a WHOLE-PAGE outcome.
	PartReset
)

// RenderPartResult renders one deferred outlet's fill for a part request.
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
	if slot == nil || !slot.spec.Deferred {
		return Fill{}, PartReset
	}
	ctx, phase := runPolicyPhase(ctx, screen, path, chain)
	if phase.err != nil {
		return Fill{}, PartReset
	}
	decision := phase.decision
	if decision.Kind == DecisionRedirect || decision.Kind == DecisionBlock {
		return Fill{}, PartReset
	}
	guards, guardRedirect := phase.guards, phase.redirect
	if guardRedirect.Kind == DecisionRedirect {
		return Fill{}, PartReset
	}
	ctx = withResolvePhase(ctx, resolvePhaseRegion)
	markChainArea(ctx, chain)
	seedRouteValues(ctx, path, screen.Path, registeredTitle(screen, decision.Kind), params, requestQuery(ctx))
	res := a.resolveDeferredSlot(ctx, path, params, *slot, guards)
	if res.notFound {
		return Fill{}, PartReset
	}
	return Fill{Addr: addr, HTML: res.html}, PartApplied
}

// RenderOverlayResult renders the screen at path as an intercepted overlay, the same component, the
// same Load, wrapped in `as` instead of the screen's own type.
func (a *App) RenderOverlayResult(ctx context.Context, path, origin string, as ScreenType) (RenderResult, error) {
	return a.renderPartial(withOverlayOrigin(ctx, origin), path, &as)
}

// ErrScreenPanicked reports that a screen's Load or Render panicked while the app rendered it.
var ErrScreenPanicked = errors.New("screen panicked")

func renderScreen(ctx context.Context, comp component.Component) (out render.HTML, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", errors.New("component render panic: ErrorBoundary.RenderError: "+textsafe.Recovered(r))
		}
	}()
	out, err = component.SafeRenderCtx(ctx, comp)
	if err != nil {
		if _, ok := comp.(component.ErrorBoundary); ok {
			return out, nil
		}
		return "", err
	}
	return out, nil
}

func screenRenderPanicError(path string, err error) error {
	return fmt.Errorf("app: component render error for %q: %w: %w", path, ErrScreenPanicked, err)
}

func (a *App) renderPartial(ctx context.Context, path string, overlay *ScreenType) (RenderResult, error) {
	return withPageIDs(ctx, func(ctx context.Context) (RenderResult, error) {
		return a.renderPartialIDs(ctx, path, overlay)
	})
}

func (a *App) renderPartialIDs(ctx context.Context, path string, overlay *ScreenType) (RenderResult, error) {
	screen, params, ok := a.Router.Resolve(path)
	if !ok {
		return RenderResult{}, fmt.Errorf("app: no screen registered for path %q", path)
	}
	return a.renderScreenPartial(ctx, path, screen, params, overlay)
}

func (a *App) renderScreenPartial(ctx context.Context, path string, screen *Screen, params map[string]string, overlay *ScreenType) (RenderResult, error) {
	if _, ok := MatchFromContext(ctx); !ok {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}
	if overlay != nil {
		ctx = withOverlay(ctx, *overlay)
	}

	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}
	ctx, phase := runPolicyPhase(ctx, screen, path, chain)
	if phase.err != nil {
		return RenderResult{}, phase.err
	}
	decision := phase.decision
	switch decision.Kind {
	case DecisionRedirect:
		return RenderResult{Kind: DecisionRedirect, URL: decision.URL}, nil
	case DecisionBlock:
		return RenderResult{Kind: DecisionBlock, Status: decision.Status, Reason: decision.Reason, Params: decision.Params}, nil
	}
	guards, guardRedirect := phase.guards, phase.redirect
	if guardRedirect.Kind == DecisionRedirect {
		return RenderResult{Kind: DecisionRedirect, URL: guardRedirect.URL}, nil
	}
	_ = guards // the bare partial renders no regions; the fills pass reads the store

	comp := screen.newInstance()
	if decision.Kind == DecisionRenderAlt && decision.AltFactory != nil {
		comp = decision.AltFactory()
		RefuseRetiredScreenMethods("screen "+screen.Path+" RenderAlt", comp)
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
	ctx = withResolvePhase(ctx, resolvePhaseRegion)

	lang := a.docLangFor(ctx, path, comp)
	partialRouteTitle := registeredTitle(screen, decision.Kind)
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
		body = wrapArticle(screen, comp, html)
	} else {
		var renderErr error
		body, renderErr = renderComponentAs(ctx, screen, effType, comp, partialRouteTitle)
		if renderErr != nil {
			return RenderResult{}, screenRenderPanicError(path, renderErr)
		}
	}

	out := RenderResult{HTML: body, DocumentLanguage: lang, Component: comp}
	if t, ok := comp.(ScreenTitler); ok {
		out.Title = safeScreenTitle(ctx, t)
	}
	if decision.Kind == DecisionRenderAlt {
		out.Kind = DecisionRenderAlt
	} else {
		out.Kind = DecisionAllow
	}
	return out, nil
}

func registeredTitle(screen *Screen, kind DecisionKind) string {
	if kind == DecisionRenderAlt {
		return ""
	}
	return screen.Title
}

func renderComponentInScreen(ctx context.Context, screen *Screen, comp component.Component, title string) (render.HTML, error) {
	return renderComponentAs(ctx, screen, screen.Type, comp, title)
}

func renderComponentAs(ctx context.Context, screen *Screen, effType ScreenType, comp component.Component, title string) (render.HTML, error) {
	content, renderErr := renderScreen(ctx, comp)
	if renderErr != nil {
		return "", renderErr
	}
	content = wrapArticle(screen, comp, content)
	if attrs := docShellAttrs(ctx); attrs != nil && effType == ScreenPage {
		return html.Main(html.MainConfig{ExtraAttrs: attrs}, content), nil
	}
	return wrapByScreenType(effType, title, content), nil
}

func safeScreenLoad(loader ScreenLoader, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: Load: %s", ErrScreenPanicked, textsafe.Recovered(r))
		}
	}()
	return loader.Load(ctx)
}

func safeScreenTitle(ctx context.Context, titler ScreenTitler) (title string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("app: ScreenTitle re-read panicked; using registered title",
				"panic", textsafe.Recovered(r))
			title = ""
		}
	}()
	return titler.ScreenTitle(ctx)
}

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
