package uihost

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
	"github.com/DonaldMurillo/gofastr/internal/retired"
)

// scrubCtl percent-encodes every C0 control byte and DEL in a
// Location header value built from request-derived paths. It is the
// header scrub, and its one deliberate pass-through holds there:
// net/http's http.Redirect hex-escapes every non-ASCII byte in the
// Location value it writes, so C1 and the bidi set cannot reach the
// wire raw downstream of these emit sites (the gap it closes is
// exactly the C0/DEL that hexEscapeNonASCII leaves alone). Tab is
// encoded too, although HTAB is a legal header-value byte: the WHATWG
// URL parser deletes tab, CR and LF from a Location before parsing it,
// so a raw "/\t/evil.example" is the protocol-relative "//evil.example"
// in the browser, while "/%09/evil.example" stays a same-origin path.
// The log sink is NOT this scrub's customer: slog leaves C1/bidi runes
// raw, so log fields use textsafe.ScrubControlBytes instead.
func scrubCtl(s string) string {
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c == 0x7f {
			var b strings.Builder
			b.Grow(len(s))
			for j := range len(s) {
				d := s[j]
				if d < 0x20 || d == 0x7f {
					fmt.Fprintf(&b, "%%%02x", d)
					continue
				}
				b.WriteByte(d)
			}
			return b.String()
		}
	}
	return s
}

// scrubTitleInvisibles strips the invisible/bidi codepoints (the
// core/textsafe set: RLO/LRI isolates, zero-widths, BOM) from the text
// inside the first <title>…</title> element of a rendered page. The page
// renderer escapes markup but not invisible characters, and dynamic-route
// titles re-read ScreenTitle AFTER Load so the tab title is
// user-data-bearing by design; a title computed from loaded data (a doc
// title, a display name) would otherwise reorder or salt the browser's
// tab-title readout. Only the element's text is touched: the surrounding
// document is byte-identical, and the invisible set has no HTML-escaped
// form (render.Text escapes markup only), so the raw runes in the slice
// ARE the characters the browser would show.
func scrubTitleInvisibles(page string) string {
	const openTag = "<title>"
	start := strings.Index(page, openTag)
	if start < 0 {
		return page
	}
	rest := page[start+len(openTag):]
	end := strings.Index(rest, "</title>")
	if end < 0 {
		return page
	}
	inner := rest[:end]
	if !textsafe.ContainsInvisible(inner) {
		return page
	}
	return page[:start+len(openTag)] + textsafe.StripInvisible(inner) + rest[end:]
}

// ServeHTTP makes UIHost satisfy http.Handler by routing through a private
// router that has Mount called on it. Production wiring goes through
// framework.App.Mount(host); ServeHTTP exists so the host can also be used
// standalone (tests, embedded experiments) without dragging in the full
// framework App.
func (ds *UIHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ds.standaloneOnce.Do(func() {
		ds.standalone = router.New()
		// Standalone path doesn't go through framework.App's middleware
		// chain (which wires SecurityHeaders), so apply it here so tests
		// and embedded uses get the same baseline headers as production.
		ds.standalone.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}))
		// framework.App installs the retired-markup scan on its router;
		// the standalone router does the same, so widget and island
		// responses mounted here are read like pages.
		ds.standalone.Use(retired.Middleware(dev.Enabled))
		ds.Mount(ds.standalone)
	})
	ds.standalone.ServeHTTP(w, r)
}

// handlePage renders a full page with runtime.js, SSE meta tag, and compiled actions.
func (ds *UIHost) handlePage(w http.ResponseWriter, r *http.Request) {
	w, r = renderdiag.TestResponse(w, r)
	path := routePath(r)
	// A part request (X-Gofastr-Part, is answered
	// before anything else: it never renders a document, never mints a
	// session, and every whole-page outcome is its 409 reset — so it
	// must not fall into any full-render arm by mistake.
	if addr := r.Header.Get("X-Gofastr-Part"); addr != "" {
		ds.handlePartRequest(w, r, path, addr)
		return
	}
	// Client-side navigation: return just the screen content (no layout)
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		ds.handlePartialPage(w, r, path)
		return
	}
	// Declarative redirects (Redirect / RedirectPattern): short-circuit
	// before screen resolution with a permanent redirect to the target.
	// scrubCtl: the substituted target carries percent-decoded request-path
	// params, and net/http's http.Redirect hex-escapes only non-ASCII —
	// raw C0/DEL bytes pass through into the Location header verbatim,
	// where they forge log lines and header values downstream. Percent-
	// encode them, the same contract sanitizeHeaderValue pins elsewhere.
	// handler.IsSafeRelativePath is the same gate the navigation arm
	// applies: a target that is not a same-origin relative path (a tab
	// a browser deletes in front of "/evil.example", a "//host") is no
	// redirect at all, and the request falls through to the router.
	if target, ok := ds.App.ResolveRedirect(path); ok && handler.IsSafeRelativePath(target) {
		http.Redirect(w, r, scrubCtl(carryQuery(target, r.URL.RawQuery)), http.StatusPermanentRedirect)
		return
	}
	// Agent content negotiation: when WithMarkdownNegotiation (or the
	// bundle) is on and the request prefers markdown, render the page's
	// markdown via the per-screen LLM doc instead of HTML. Both variants
	// of a negotiable URL carry Vary: Accept (serveMarkdownForPage adds
	// it on its arm; the HTML arm below adds it on the fall-through) so
	// a shared cache never replays one representation to a client that
	// asked for the other.
	if ds.agentReady != nil && ds.agentReady.contentNeg && ds.llmMDPublic {
		if acceptsMarkdown(r) {
			if ds.serveMarkdownForPage(w, r) {
				return
			}
			// No screen matched. Fall through to the normal HTML path.
		}
		w.Header().Add("Vary", "Accept")
	}

	// Make the live request available to ScreenLoader.Load(ctx) so
	// screens can read URL query params, headers, etc. SSG builds
	// pass nil and the helpers degrade to empty values.
	ctx := app.WithRequest(r.Context(), r)
	// Route match on the render context: RouteMatchMiddleware
	// normally installed it before application middleware. Standalone
	// hosts (and hosts whose app skipped the middleware) get it here
	// so a Policy or ScreenLoader on a dynamic route can read the
	// same params the render pipeline injects.
	ctx = withRouteMatch(ctx, ds.App.Router, path)
	// Install the per-request signal value bag BEFORE rendering so a
	// producer's Slice.Seed(ctx, v) during Load (and a consumer's
	// Bind(ctx, …) during RenderCtx) write/read the same bag that
	// injectHeadData resolves below.
	ctx = store.WithValues(ctx)
	ctx, recorded := app.TrackStatus(ctx)
	var res app.RenderResult
	res, err := ds.App.RenderPageResult(ctx, path)
	if err != nil {
		// A whole-page resolver failure the
		// not-found page for ErrNotFound, the error page through the
		// root layout for anything else — both logged here, never
		// with the error text in the body.
		var pe *app.PageError
		if errors.As(err, &pe) {
			ds.serveResolverError(w, r, path, pe)
			return
		}
		ds.serveRenderError(w, r, path, err)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		// The runtime's whole-page fetch (a navigation across layout
		// chains) cannot follow a 303 off the origin.
		if scriptFetch(r) && writeDocumentRedirect(w, res.URL) {
			return
		}
		http.Redirect(w, r, scrubCtl(res.URL), http.StatusSeeOther)
		return
	case app.DecisionBlock:
		writeBlock(w, r, res)
		return
	}
	ds.finishPageDocument(w, r, withRenderAlt(ctx, res), res.HTML, path, res.Component, pageStatus(res, recorded()))
}

type renderAltKey struct{}

// staticExportKey marks a render for a static export (RenderStaticPage*,
// RenderStaticNotFoundPage). The head built for it differs from the PWA
// offline shell's, which shares the export mode but runs the live
// runtime: the export's static runtime never loads rpc.js.
type staticExportKey struct{}

// withRenderAlt marks ctx when res is a RenderAlt answer, for the head
// built after it (screenOwns).
func withRenderAlt(ctx context.Context, res app.RenderResult) context.Context {
	if res.Kind != app.DecisionRenderAlt {
		return ctx
	}
	return context.WithValue(ctx, renderAltKey{}, true)
}

// screenOwns reports whether the screen's registration-time metadata
// (Description, Article, its registration instance's HeadHTML)
// describes comp: comp is nil or a copy of the screen's component, and
// no RenderAlt replaced it. An alt, an auth-failure screen or a
// not-found body read only their own: the guarded screen's title,
// description and HeadHTML rode them to the visitor it refused.
func screenOwns(ctx context.Context, screen *app.Screen, comp component.Component) bool {
	if alt, _ := ctx.Value(renderAltKey{}).(bool); alt {
		return false
	}
	return comp == nil || reflect.TypeOf(comp) == reflect.TypeOf(screen.Component)
}

// pageStatus is the status of a rendered page, decided once by the one
// ranking every page path shares: a 404-outlet outcome (Decided 5: the
// route's answer is the not-found page), else the component's own
// ScreenStatusCode, else what the render set through app.SetStatus
// (a record view that drew its not-found state), else 0, the implicit
// 200.
func pageStatus(res app.RenderResult, recorded int) int {
	if res.NotFoundOutlet {
		return http.StatusNotFound
	}
	// A code outside 100-999 is ignored, as app.SetStatus ignores one:
	// net/http panics on it in WriteHeader.
	if sc, ok := res.Component.(ScreenStatusCode); ok {
		if code := sc.ScreenStatusCode(); code >= 100 && code <= 999 && code != http.StatusOK {
			return code
		}
	}
	if recorded < 100 || recorded > 999 || recorded == http.StatusOK {
		return 0
	}
	return recorded
}

// finishPageDocument is the ONE page-finishing tail every document that
// renders through the app's layout runs: the normal page arm above, the
// not-found and error documents, the 405 page, and RenderScreen's full
// arm. The bare fallback documents (no root layout) stay bare.
//
// Get or create session. The cookie name + Secure flag depend on the
// request origin (see setSessionCookie): a plaintext loopback dev
// server can't round-trip a Secure cookie, so it gets the relaxed
// form; everything else keeps the hardened __Host- cookie.
//
// A cookie whose token fails verification (expired, tampered, minted
// under a rotated or per-boot key) must be re-minted, not reused;
// otherwise the embedded SSE id and every island RPC would reference
// a dead session and 401 until the user manually cleared the cookie.
// The cookie stores the signed token; only the bare id goes into the
// page chrome (SSE URL), so the credential never appears in URLs.
// This is what makes an error document a real page: a 404/500/405
// loaded directly used to ship chrome with NO session id, so the
// widget catalog fetch and every island on it 401'd, and the first
// navigation away raced a part request against a session the page
// never minted (the 409 X-Gofastr-Part-Reset reload).
//
// no-store + Vary: Cookie BEFORE the live/dead split, exactly like
// handlePartialPage: the document this tail ships is per-user rendered
// content (caller-context SSR HTML, /__gofastr/sse?session=<id>
// chrome, gated widget chrome SSR-inlined with request context), and
// the re-mint arm additionally carries a Set-Cookie token. A shared
// cache that stores no-freshness responses keyed on URL alone would
// serve visitor B this session's page.
//
// A ctx marked withoutMint (the screen-path 405) still reads a live
// cookie and puts its id in the chrome, but never mints for a request
// without one: the document then ships session-less chrome (no SSE
// meta), the shape a static export has. A request that never asked to
// read a page does not open a session.
func (ds *UIHost) finishPageDocument(w http.ResponseWriter, r *http.Request, ctx context.Context, html render.HTML, path string, comp component.Component, status int) {
	// The <title> element is user-data-bearing on dynamic routes (the
	// post-Load ScreenTitle) and escapes markup, not invisibles: strip the
	// textsafe invisible set before the document ships. See
	// scrubTitleInvisibles.
	page := render.HTML(scrubTitleInvisibles(string(html)))

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Cookie")
	sessionID, live := ds.verifySessionToken(readSessionCookie(r))
	switch {
	case !live && mintRefused(ctx):
		sessionID = ""
	case !live:
		sess := ds.CreateSession()
		sessionID = sess.ID
		setSessionCookie(w, r, sess.Token)
		// Same seam as the partial path: a re-mint response carries a
		// Set-Cookie session token AND embeds the fresh stream id in the
		// page chrome. The no-store above already covers this arm; the
		// comment stays for the threat model.
	}

	// The confirm dialog goes in before the chrome: the head's
	// component-stylesheet scan must see the markers inside it.
	out := injectConfirmTemplate(string(page), r)
	// A head small enough for the first round trip ships inline; the
	// response's own policy then allows exactly those blocks by hash.
	// SSR-inline registered widgets: non-hidden auto-mount widgets
	// always go in, and a Hidden deep-link widget goes in when the
	// request URL matches its deep link (open at first paint). The
	// widget chrome lives just inside </body>; the runtime's
	// _mountByName checks for an existing root before fetching
	// cfg.chromePath. It goes in after the chrome, so a widget's sheets
	// load on demand rather than in the head, but a first-paint subset
	// keeps the rules its classes need. The toast stack is the one
	// widget whose sheets go in the head (see inlineHead.sheets).
	widgets, toastSheets := widgetSSRHTML(r)
	inline := &inlineHead{
		allow:  func(styles, scripts []string) bool { return allowInlineHashes(w.Header(), styles, scripts) },
		markup: widgets,
		sheets: toastSheets,
	}
	out = ds.injectChromeDoc(ctx, out, path, sessionID, boundedPresenceParam(r), true, comp, inline, status)
	out = injectHeadData(ctx, out)
	if widgets != "" {
		out = replaceChromeMarker(out, "</body>", widgets+"</body>", "widget chrome")
	}

	ds.writeAgentLinkHeaders(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
	}
	fmt.Fprint(w, out)
}

// injectWidgetSSR inlines the widgets the page wants open at first
// paint: every non-hidden auto-mount widget (toast stack, banner,
// panel), plus any widget — Hidden included — whose deep link matches
// the request URL, so a shared/refreshed deep-link URL paints the
// dialog without a chrome round-trip. A Hidden widget with NO deep
// link, or one whose deep link does not match, is NOT inlined: the
// runtime lazy-fetches its chrome from cfg.chromePath on the first
// data-cui-open, keeping page responses minimal.
//
// Inlined chrome is rendered with NO trigger ctx (arrival by URL has
// no trigger to carry data-cui-ctx, #321). That is safe only because
// the runtime's _mountByName drops the inlined node when a
// ctx-carrying trigger opens the widget and takes the (name, ctx)
// keyed chrome fetch instead — per-entity dialogs (Hidden + DeepLink,
// the documented shape) depend on that pairing. Pinned by
// TestInjectWidgetSSR_HiddenDeepLinkInlinedOnMatch here and
// TestWidgetChromeCtx_SSRInlinedDeepLinkOpensPerCtx in core-ui/runtime.
func injectWidgetSSR(page string, r *http.Request) string {
	html, _ := widgetSSRHTML(r)
	if html == "" {
		return page
	}
	return replaceChromeMarker(page, "</body>", html+"</body>", "widget chrome")
}

// widgetSSRHTML renders the widget chrome injectWidgetSSR places, and
// lists the component sheets the inlined toast stacks name, their row
// template's included: the head carries those (inlineHead.sheets).
func widgetSSRHTML(r *http.Request) (string, []string) {
	b := borrowBuilder()
	defer returnBuilder(b)
	var toastSheets []string
	u := r.URL
	q := u.Query()
	// Per-page filter: widgets scoped via .Pages / .PagesPrefix /
	// .PagesMatch only appear on paths they declared. Empty Routes
	// (the default) means the widget is global.
	for _, d := range widget.AvailableOn(u.Path) {
		// Decide: should this widget be open on this page load?
		var open bool
		switch {
		case d.DeepLinkKey != "" && d.DeepLinkValue != "":
			// Deep-link widget: open iff the URL says so.
			open = q.Get(d.DeepLinkKey) == d.DeepLinkValue
		case !d.Hidden:
			// Non-hidden auto-mount widget (toast stack, banner, panel).
			open = true
		default:
			// Hidden click-to-open widget: skip; runtime lazy-fetches.
			continue
		}
		if !open {
			continue
		}
		// Respect the widget's session gate here too. /state and /chrome
		// are gated at Mount, but SSR inlining is a third chrome surface:
		// baking a gated widget's chrome into the anonymous page would
		// hand out exactly the HTML its /chrome endpoint refuses. The
		// any-session level still inlines for every visitor (a page load
		// mints the session that satisfies it); the authenticated level
		// inlines only when the request resolved a signed-in user.
		// ListedFor rides the same verdict (OfferedTo): a widget the
		// catalog keeps from this caller is not baked into their page.
		if !d.OfferedTo(r) {
			continue
		}
		// Render with the REQUEST context so context-aware slots (role-aware
		// nav drawer, tenant-scoped chrome) see the signed-in user on FIRST
		// paint. The lazy /chrome endpoint already threaded r.Context(); the
		// SSR inline path must match, or the same slot renders anonymous here
		// and personalized when the user reopens it.
		chrome := widget.RenderChromeCtx(r.Context(), d)
		if preset.IsToastStack(d) {
			live, held := registry.ScanSplit(chrome)
			toastSheets = append(append(toastSheets, live...), held...)
		}
		b.WriteString(chrome)
	}
	return b.String(), toastSheets
}

// injectConfirmTemplate emits the kit's confirm dialog
// (preset.ConfirmTemplateHTML) once per document, just inside </body>,
// outside every layout shell, so a client navigation that swaps the
// shell keeps it. Every page carries it, not just pages with a
// data-cui-confirm control: a page reached by client navigation runs
// on the first document's body. Its component markers sit inside the
// inert template, so the head links the dialog's and the buttons'
// sheets the way it links any rendered component's.
func injectConfirmTemplate(page string, r *http.Request) string {
	tpl := preset.ConfirmTemplateHTML(r.Context())
	if tpl == "" {
		return page
	}
	return replaceChromeMarker(page, "</body>", string(tpl)+"</body>", "confirm dialog")
}

// replaceChromeMarker replaces the first occurrence of marker in page, the way
// the chrome-injection sites need. Unlike a bare strings.Replace, it WARNS when
// the marker is absent instead of silently returning the page unchanged. A
// missing <head>/</head>/</body> means a custom layout dropped a structural tag
// the host relies on to inject the runtime, color-scheme bootstrap, SEO head,
// and widget chrome, so the page would ship subtly broken with no signal.
func replaceChromeMarker(page, marker, replacement, what string) string {
	if !strings.Contains(page, marker) {
		slog.Default().Warn("uihost: page is missing a structural marker; chrome not injected",
			"marker", marker, "injecting", what,
			"hint", "custom layouts must emit <head>…</head> and …</body> so the host can inject the runtime")
		return page
	}
	return strings.Replace(page, marker, replacement, 1)
}

// injectChrome adds links and scripts pointing at the host's served
// endpoints. pagePath is the route path used for SEOScreen resolution.
func (ds *UIHost) injectChrome(ctx context.Context, page, pagePath, sessionID, presenceTopic string) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, true, nil)
}

// injectChromeFor is injectChrome with the loaded per-request component,
// so per-instance metadata (the meta description of a dynamic route)
// lands in the head. comp may be nil.
func (ds *UIHost) injectChromeFor(ctx context.Context, page, pagePath, sessionID, presenceTopic string, comp component.Component) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, true, comp)
}

// screenHeadHTML returns the per-page head content: the screen's own
// tags (screenOwnHead) with any description after the first dropped,
// led by the site-level description (siteDescriptionTag) when they
// carry none. The site description rides
// here, in the page metadata a client navigation swaps, and never in
// the fixed head (the head assembly drops it from WithHeadHTML), so a
// page carries exactly one description and leaving a described page
// restores the site's.
func (ds *UIHost) screenHeadHTML(ctx context.Context, pagePath string, comp component.Component) string {
	head, own := scanDescriptions(ds.screenOwnHead(ctx, pagePath, comp), 1)
	if own != "" {
		return head
	}
	site := ds.siteDescriptionTag()
	if site == "" {
		return head
	}
	if head == "" {
		return site
	}
	return site + "\n" + head
}

// screenOwnHead returns per-screen head content. It composes from
// two sources:
//
//   - ScreenDescriber: when the screen has a non-empty Description
//     (set automatically at Register time from the ScreenDescriber
//     interface), a `<meta name="description">` tag is emitted, in
//     place of the site's WithDescription one.
//
//   - SEOScreen.HeadHTML(): the explicit escape hatch for screens
//     that need to declare canonical, OG, JSON-LD, etc. inline. A
//     description tag in it is dropped when the screen has a typed
//     description.
//
// pagePath is the route path used to resolve the screen.
//
// The per-concern SEO fields are resolved first via resolveScreenSEO
// (the shared resolution path also used to render the per-screen
// llm.md front-matter), then rendered to HTML.
func (ds *UIHost) screenOwnHead(ctx context.Context, pagePath string, comp component.Component) string {
	if ds.App == nil || pagePath == "" {
		return ""
	}
	screen, _, ok := ds.App.Router.Resolve(pagePath)
	if !ok {
		return ""
	}
	resolved := ds.localizeSEO(ctx, pagePath, resolveScreenSEOFor(ctx, screen, comp))

	var parts []string
	if resolved.Description != "" {
		parts = append(parts, fmt.Sprintf(
			`<meta name="description" content="%s">`,
			stdhtml.EscapeString(resolved.Description),
		))
	}
	if resolved.Robots != "" {
		parts = append(parts, fmt.Sprintf(
			`<meta name="robots" content="%s">`,
			stdhtml.EscapeString(resolved.Robots),
		))
	}
	if resolved.Canonical != "" && isSafeHeadURL(resolved.Canonical) {
		parts = append(parts, fmt.Sprintf(
			`<link rel="canonical" href="%s">`,
			stdhtml.EscapeString(resolved.Canonical),
		))
	}
	for _, link := range resolved.Hreflangs {
		if link.Lang == "" || link.URL == "" || !isSafeHeadURL(link.URL) {
			continue
		}
		parts = append(parts, fmt.Sprintf(
			`<link rel="alternate" hreflang="%s" href="%s">`,
			stdhtml.EscapeString(link.Lang),
			stdhtml.EscapeString(link.URL),
		))
	}
	// OG + Twitter: bundle only (the global WithOpenGraph / WithTwitterCard
	// already handle the site-wide defaults).
	if resolved.OG != nil {
		parts = append(parts, ogTags(*resolved.OG)...)
	}
	if resolved.Twitter != nil {
		parts = append(parts, twitterTags(*resolved.Twitter)...)
	}
	if len(resolved.Schema) > 0 {
		parts = append(parts, string(seo.Render(resolved.Schema...)))
	}
	// Catch-all per-screen HTML escape hatch. Caller-supplied. Scrub
	// inline <script> tags before injection (XSS defense-in-depth).
	// The rendered component's: the registration instance knows no
	// params, and under RenderAlt it is the screen the policy refused.
	src := component.Component(screen.Component)
	if comp != nil {
		src = comp
	}
	if seoScreen, ok := src.(SEOScreen); ok {
		if h := seoScreen.HeadHTML(); h != "" {
			h = stripInlineScripts(h)
			// The typed description above wins over a tag here, so the
			// page carries one.
			if resolved.Description != "" {
				h, _ = scanDescriptions(h, 0)
			}
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, "\n")
}

// injectChromeMode is the underlying chrome injector. bundle=false
// suppresses the comp-bundle.css endpoint and emits one <link> per
// component instead, used by static export, since static hosts
// don't typically serve query-parameterized files. Live HTTP mode
// always passes bundle=true. pagePath is used for SEOScreen resolution.
func (ds *UIHost) injectChromeMode(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, bundle, nil)
}

func (ds *UIHost) injectChromeModeFor(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool, comp component.Component) string {
	return ds.injectChromeDoc(ctx, page, pagePath, sessionID, presenceTopic, bundle, comp, nil, 0)
}

// inlineHead is how a live page may inline its head (firstpaint.go).
type inlineHead struct {
	// allow is asked whether the response's policy can take the hashes
	// of the blocks the head would inline; it allows them on the
	// response when it says yes.
	allow func(styles, scripts []string) bool
	// markup is what the response adds to the page after the chrome
	// (the first-paint widgets), whose classes a subset must keep.
	markup string
	// sheets are component sheets the head carries though the page's
	// markers do not name them live: the toast stack's, row template
	// included. A widget's sheets otherwise load on demand, but the
	// toast that reports a lost connection is raised exactly when no
	// sheet can load, and it drew as bare text.
	sheets []string
}

// injectChromeDoc is the chrome injector. inline, when set, lets a head
// small enough for the first round trip carry its resources. A nil
// inline, or export mode, links every head resource. status is the code
// the page answers with (0 for 200): only a 2xx page signs the topics
// its screen declares.
func (ds *UIHost) injectChromeDoc(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool, comp component.Component, inline *inlineHead, status int) string {
	// First full-shell render freezes the extra-script rail: registering
	// after this point would ship a script on some pages and not others.
	ds.markServingBegun()
	// the page-wide loading component rides
	// every full page at BODY level (outside any shell a navigation can
	// swap, so it survives every soft navigation), hidden until one is
	// in flight (frameworkCSS keys its visibility off html[aria-busy]).
	// Injected BEFORE the component-CSS scan below so the component's
	// stylesheet ships in the head bundle. Presentational config only:
	// rendered per page with no Load/DI.
	if ds.pageLoading != nil {
		if c, _ := component.SafeRenderCtx(context.Background(), ds.pageLoading); c != "" {
			page = replaceChromeMarker(page, "<body>",
				"<body>"+`<div data-fui-page-loading>`+string(c)+"</div>", "page loading")
		}
	}
	headClose := borrowBuilder()
	defer returnBuilder(headClose)
	scripts := borrowBuilder()
	defer returnBuilder(scripts)

	if sessionID != "" && !ds.noLiveChannel {
		// The SSE connection URL. The topics the screen declares
		// (PresenceScreen) ride signed for this viewer and join with no
		// hook. presenceTopic (the page's own ?presence= query value) rides
		// as a request the SSE handler admits only through AuthorizeTopic.
		// QueryEscaped to keep the values safe inside the HTML attribute
		// and the downstream URL.
		sseURL := "/__gofastr/sse?session=" + sessionID
		if presenceTopic != "" {
			sseURL += "&presence=" + url.QueryEscape(presenceTopic)
		}
		// A page that answers 403/404/500 refused or failed the viewer
		// (a Load that found another tenant's record sets 404 and draws
		// its own not-found state), so its screen's topics stay unsigned.
		if status == 0 || (status >= 200 && status < 300) {
			sseURL += ds.declaredPresenceQuery(ctx, comp)
		}
		fmt.Fprintf(headClose, `<meta name="gofastr-sse" content="%s">`+"\n", sseURL)
	}
	// app.css is injected AFTER component CSS (see further down) so
	// that host overrides win cascade ties against the framework's
	// component defaults. CSS variables resolve at use time, so the
	// :root tokens (which app.css owns) still resolve correctly for
	// component CSS that uses bare var(--*) refs regardless of source
	// order.

	// Head injection order: per-screen SEOScreen FIRST, then WithHeadHTML,
	// then global typed tags last.
	//
	// Rationale: social-preview crawlers (Open Graph, Twitter Card) are
	// first-match: they stop at the first occurrence of a given property.
	// Putting per-page tags before global sitewide defaults ensures the
	// per-page og:title / og:description / og:image is the one they pick.
	// If the page provides no OG data, the global fallback still fires.
	//
	// Typed helpers (WithOpenGraph, …) are set at New() time and serve
	// as sitewide fallbacks. Per-screen SEOScreen data is per-request,
	// and so is the site description: screenHeadHTML emits it only for
	// a page that names no description of its own.
	var headParts []string
	if screenHead := ds.screenHeadHTML(ctx, pagePath, comp); screenHead != "" {
		headParts = append(headParts, markScreenHead(screenHead))
	}
	// Caller-supplied head HTML (WithHeadHTML) is scrubbed for <script>
	// tags before injection. The head escape hatch is intended for
	// meta/link/style tags only; inline scripts would violate the
	// strict CSP and are a common XSS foot-gun. See stripInlineScripts.
	// SEOScreen.HeadHTML() is scrubbed inside screenHeadHTML so we
	// preserve the auto-generated JSON-LD <script type="application/
	// ld+json"> blocks emitted by seo.Render.
	// Its description tag, if any, is the site description and rides
	// the per-page head above instead (siteDescriptionTag).
	if host, _ := scanDescriptions(stripInlineScripts(ds.headHTML), 0); host != "" {
		headParts = append(headParts, host)
	}
	if ds.pwaConfig != nil {
		headParts = append(headParts, pwaManifestLink(ctx))
	}
	for _, tag := range ds.headTags {
		headParts = append(headParts, tag)
	}
	// WithOrganization's JSON-LD block: framework-built (encoding/json
	// from typed config, see organization.go), so like the seo.Render
	// blocks above it does not go through the caller-HTML scrubber —
	// stripInlineScripts exists for host-supplied strings, and would
	// eat the (inert, non-executable) ld+json data block.
	if ld := ds.organizationJSONLD(); ld != "" {
		headParts = append(headParts, ld)
	}
	if len(headParts) > 0 {
		headClose.WriteString(strings.Join(headParts, "\n"))
		headClose.WriteByte('\n')
	}
	// Route graph: live pages get it from manifest.js (one cached,
	// content-addressed fetch per deploy) instead of paying its bytes
	// in every document; it grows with the app, and nothing reads it
	// before the runtime boots. Export mode keeps the inline JSON
	// block (inert data, CSP-safe) because exported pages must be
	// self-contained files.
	if !bundle {
		if routes := routesJSONScript(ctx, ds); routes != "" {
			headClose.WriteString(routes)
			headClose.WriteByte('\n')
		}
		if locales := ds.localesJSONScript(); locales != "" {
			headClose.WriteString(locales)
			headClose.WriteByte('\n')
		}
	}

	// Component CSS: scan the rendered page for data-cui-comp markers
	// and emit a single bundled <link> (or one direct <link> for a
	// single component) so first paint has every needed sheet in
	// <head>. LoadAlways entries are included whether the page used
	// them or not; sheets only <template> content needs are prefetched.
	var inlined *firstPaintPlan
	if bundle && inline != nil && !ds.noInlineFirstPaint {
		names, held := pageSheetNames(page, inline.sheets...)
		if p, ok := ds.planFirstPaint(names, page, inline.markup); ok && inline.allow(p.styleHashes(), p.scriptHashes()) {
			inlined = p
			if p.comp != nil {
				headClose.WriteString(`<style data-cui-bundle="` + strings.Join(p.comp.names, ","))
				if p.comp.href != "" {
					headClose.WriteString(`" data-cui-bundle-href="` + stdhtml.EscapeString(p.comp.href))
				}
				headClose.WriteString(`">` + p.comp.body + "</style>\n")
			}
			theme := ds.activeTheme()
			if prefetch := prefetchLinks(held, setOf(names), theme, style.ThemeHash(theme)); prefetch != "" {
				headClose.WriteString(prefetch)
				headClose.WriteByte('\n')
			}
		}
	}
	if inlined == nil {
		var always []string
		if inline != nil {
			always = inline.sheets
		}
		if tags := ds.componentCSSTags(page, bundle, always...); tags != "" {
			headClose.WriteString(tags)
			headClose.WriteByte('\n')
		}
	}
	// app.css comes AFTER the component CSS so it wins cascade ties
	// against framework defaults. Hosts that override e.g. a button's
	// padding or a header's drawer position can do so by writing to
	// the same selector, no specificity gymnastics needed. Live pages
	// carry ?v=<fingerprint> so the response is immutable per deploy;
	// export mode (bundle=false: static site, PWA offline shell) stays
	// query-free because the files land on disk under their bare paths
	// and naive static hosts/mirrors resolve by path (the wget
	// regression that already forced query-free module writes).
	if inlined != nil && !inlined.linkApp {
		headClose.WriteString(`<style id="cui-app-css">` + inlined.app.body + "</style>\n")
	} else if ds.App != nil {
		href := "/__gofastr/app.css"
		if bundle {
			_, cssHash := ds.appCSSCached()
			href += "?v=" + cssHash
		}
		// The id is the cascade anchor: the runtime inserts every
		// lazily loaded component sheet before it, so app.css keeps
		// winning ties against the kit (the inline form carries it too).
		headClose.WriteString(`<link rel="stylesheet" href="` + href + `" id="cui-app-css">`)
		headClose.WriteByte('\n')
	}
	if bundle {
		// Live pages carry the component catalog + runtime-module manifest
		// as ONE external content-addressed script (manifest.js, injected
		// before runtime.js below) instead of re-inlining ~3 KB of JSON
		// into every page head. The compute manifest is rare and stays
		// inline either way.
		if compute := widget.ComputeManifestScript(); compute != "" {
			headClose.WriteString(compute)
			headClose.WriteByte('\n')
		}
		// Registered data blocks (registry.RegisterDataBlock): an
		// optional package's server-declared data for its module,
		// core-ui/localdb's schema among them. Small and static per
		// deploy, inline like compute's; a host that links no such
		// package emits nothing.
		if blocks := registry.DataBlocksHTML(); blocks != "" {
			headClose.WriteString(blocks)
			headClose.WriteByte('\n')
		}
	} else {
		// Export mode: static pages and the PWA offline shell must be
		// self-contained files, so the inline blocks stay.
		if catalog := catalogJSONScript(ds); catalog != "" {
			headClose.WriteString(catalog)
			headClose.WriteByte('\n')
		}
		if manifest := runtimeModuleManifestScript(); manifest != "" {
			headClose.WriteString(manifest)
			headClose.WriteByte('\n')
		}
	}
	// Module preload hints: emit <link rel="preload" as="script"> per
	// demand-load runtime module whose marker substring appears in
	// the rendered page. Lets the browser parallel-fetch modules with
	// initial render instead of stalling on hover/click. Content-
	// addressed ?v=<hash> URLs match the immutable cache headers.
	// A static export's runtime answers data-cui-rpc with its rpc stub and
	// boot never loads the rpc module, so its preload was a wasted fetch.
	skip := ""
	if ctx.Value(staticExportKey{}) != nil {
		skip = "rpc"
	}
	if preloads := runtimeModulePreloadLinks(page, skip); preloads != "" {
		headClose.WriteString(preloads)
		headClose.WriteByte('\n')
	}

	// Deferred scripts, emitted at the end of <head> below.
	runtimeSrc := "/__gofastr/runtime.js"
	if bundle {
		// manifest.js assigns the catalog/module/action globals and MUST
		// precede runtime.js: deferred scripts execute in document order,
		// so the kernel finds them set at boot. Every rail script carries
		// defer, so none of them blocks the parser or first paint, and
		// they all run after the document is parsed, still in order.
		_, mHash := ds.manifestJS(ctx)
		scripts.WriteString(`<script defer src="/__gofastr/manifest.js?v=` + mHash + `"></script>`)
		scripts.WriteByte('\n')
		runtimeSrc += "?v=" + widget.RuntimeHash()
	}
	scripts.WriteString(`<script defer src="` + runtimeSrc + `"></script>`)
	scripts.WriteByte('\n')
	if bundle {
		// Per-screen action scripts: only the compiled registries whose
		// component ids actually appear on THIS page, each content-
		// addressed and served private, no-cache so every use revalidates
		// through the session gate. The whole-app actions.js concat
		// re-shipped every screen's actions on every page uncached.
		// Navigation to a screen whose actions aren't loaded yet is
		// covered by the actionloader module reading __gofastr_actions.
		for _, tag := range ds.actionScriptTags(page) {
			scripts.WriteString(tag)
			scripts.WriteByte('\n')
		}
	} else if ds.HasActions() {
		// Export mode keeps the self-contained concat file.
		scripts.WriteString(`<script defer src="/__gofastr/actions.js"></script>`)
		scripts.WriteByte('\n')
	}
	// Document-lifetime scopes are asked about the ROUTE, not the
	// concrete path: the same identity the manifest uses. A predicate
	// that saw "/session/:id" when the manifest was built and
	// "/session/42" here could answer differently, and a disagreement is
	// a partial swap that keeps a capability the destination never
	// emitted. An unregistered path (a 404 shell) is its own identity.
	scopeKey := pagePath
	if ds.App != nil && ds.App.Router != nil {
		if screen, _, ok := ds.App.Router.Resolve(pagePath); ok && screen != nil {
			scopeKey = screen.Path
		}
	}
	for _, s := range ds.extraScripts {
		if s.scope != nil && !s.scope(scopeKey) {
			continue // document-lifetime script out of scope on this page
		}
		docAttr := ""
		if s.scope != nil {
			docAttr = " data-cui-doc"
		}
		//gofastr:allow(GOFASTR1412) src passed validExternalScriptSrc at registration: a same-origin path, no scheme or host.
		fmt.Fprintf(scripts, `<script defer src="%s"%s></script>`+"\n", stdhtml.EscapeString(s.src), docAttr)
	}

	// Color-scheme bootstrap runs SYNCHRONOUSLY at the top of <head>
	// (before any CSS parses) so dark-mode tokens take effect during
	// the same first paint, no FOUC. Reads localStorage("gofastr.
	// colorScheme") + prefers-color-scheme media query, sets
	// <html data-color-scheme="dark|light">.
	colorTag := `<script src="/__gofastr/color-scheme.js"></script>`
	switch {
	case inlined != nil:
		//gofastr:allow(GOFASTR1805) hashed first-paint script: the CSP carries its sha256
		colorTag = "<script>" + inlined.scheme.body + "</script>"
	case bundle:
		colorTag = `<script src="/__gofastr/color-scheme.js?v=` + colorSchemeHash() + `"></script>`
	}
	page = replaceChromeMarker(page, "<head>", "<head>"+colorTag, "color-scheme bootstrap")
	// The deferred scripts close <head>, after the stylesheets: defer
	// keeps them off the first paint, the preload scanner finds them
	// with the first bytes, and they run before any deferred script
	// in the page's own markup, which may reach for window.__gofastr
	// the way it could when runtime.js ran synchronously.
	headClose.WriteString(scripts.String())
	page = replaceChromeMarker(page, "</head>", headClose.String()+"</head>", "head chrome (SEO/CSS/scripts)")
	return page
}

// documentShell is the one document envelope every host-rendered
// fallback page (404, 405, recovery screens) is built from: a bare
// <html> with charset and viewport meta, a title, and the body, so
// injectChrome's `<head>` / `</head>` / `<body>` targets exist and the
// runtime, theme, and colour-scheme bootstrap attach exactly as they
// do on a registered screen. Title and lang are escaped here; body is
// already-rendered HTML.
//
// path is the route the shell stands in for, and with ctx's negotiated
// locale only decides the document language and direction (see LangFor).
// Pass "" for a shell that belongs to no route.
func (ds *UIHost) documentShell(ctx context.Context, path, title, body string) string {
	return ds.documentShellLang(ctx, path, "", title, body)
}

// documentShellLang is documentShell with the document language named
// by the caller; "" resolves it for path the way documentShell does.
func (ds *UIHost) documentShellLang(ctx context.Context, path, lang, title, body string) string {
	if lang == "" {
		lang = ds.LangFor(ctx, path)
	}
	return fmt.Sprintf(
		`<!DOCTYPE html><html dir="%s" lang="%s"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>%s</title></head><body>%s</body></html>`,
		i18n.Dir(lang), stdhtml.EscapeString(lang), stdhtml.EscapeString(title), body)
}

var screenMetaTagPrefix = regexp.MustCompile(`(?i)<(meta|link)\b`)

func markScreenHead(head string) string {
	return screenMetaTagPrefix.ReplaceAllString(head, `<$1 data-cui-page-meta=""`)
}

// withRouteMatch puts the screen route path matched on ctx, unless
// RouteMatchMiddleware already did, and tells the tracing middleware
// which route that is: the mux sees every screen through one catch-all,
// so without the note every page's span would read "unmatched".
func withRouteMatch(ctx context.Context, router *app.Router, path string) context.Context {
	m, ok := app.MatchFromContext(ctx)
	if !ok {
		if m, ok = router.MatchFor(path); !ok {
			return ctx
		}
		ctx = app.WithMatch(ctx, m)
	}
	middleware.NoteRoute(ctx, m.ScreenID())
	return ctx
}
