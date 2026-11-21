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

// ServeHTTP serves the host on a private router of its own, for tests and
// embedded use without a framework.App; production mounts the host instead.
func (ds *UIHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ds.standaloneOnce.Do(func() {
		ds.standalone = router.New()
		ds.standalone.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}))
		ds.standalone.Use(retired.Middleware(dev.Enabled))
		ds.Mount(ds.standalone)
	})
	ds.standalone.ServeHTTP(w, r)
}

func (ds *UIHost) handlePage(w http.ResponseWriter, r *http.Request) {
	w, r = renderdiag.TestResponse(w, r)
	path := routePath(r)
	if addr := r.Header.Get("X-Gofastr-Part"); addr != "" {
		ds.handlePartRequest(w, r, path, addr)
		return
	}
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		ds.handlePartialPage(w, r, path)
		return
	}
	if target, ok := ds.App.ResolveRedirect(path); ok && handler.IsSafeRelativePath(target) {
		http.Redirect(w, r, scrubCtl(carryQuery(target, r.URL.RawQuery)), http.StatusPermanentRedirect)
		return
	}
	if ds.negotiatesMarkdown() {
		if acceptsMarkdown(r) {
			if ds.serveMarkdownForPage(w, r) {
				return
			}
		}
		w.Header().Add("Vary", "Accept")
	}

	ctx := app.WithRequest(r.Context(), r)
	ctx = withRouteMatch(ctx, ds.App.Router, path)
	ctx = store.WithValues(ctx)
	ctx, recorded := app.TrackStatus(ctx)
	var res app.RenderResult
	res, err := ds.App.RenderPageResult(ctx, path)
	if err != nil {
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
		if scriptFetch(r) && writeDocumentRedirect(w, res.URL) {
			return
		}
		http.Redirect(w, r, scrubCtl(res.URL), http.StatusSeeOther)
		return
	case app.DecisionBlock:
		writeBlock(w, r, res)
		return
	}
	ds.finishPageDocument(w, r, withOutcome(ctx, res), res.HTML, path, res.Component, pageStatus(res, recorded()))
}

// negotiatesMarkdown reports whether a page answers text/markdown to a
// request that asks for it.
func (ds *UIHost) negotiatesMarkdown() bool {
	return ds.agentReady != nil && ds.agentReady.contentNeg && ds.llmMDPublic
}

type renderAltKey struct{}

type noRouteMetaKey struct{}

func withoutRouteMeta(ctx context.Context) context.Context {
	return context.WithValue(ctx, noRouteMetaKey{}, true)
}

func routeMetaOff(ctx context.Context) bool {
	nf, _ := ctx.Value(noRouteMetaKey{}).(bool)
	return nf
}

func withOutcome(ctx context.Context, res app.RenderResult) context.Context {
	if res.NotFoundOutlet {
		ctx = withoutRouteMeta(ctx)
	}
	return withRenderAlt(ctx, res)
}

type staticExportKey struct{}

func withRenderAlt(ctx context.Context, res app.RenderResult) context.Context {
	if res.Kind != app.DecisionRenderAlt {
		return ctx
	}
	return context.WithValue(ctx, renderAltKey{}, true)
}

func screenOwns(ctx context.Context, screen *app.Screen, comp component.Component) bool {
	if alt, _ := ctx.Value(renderAltKey{}).(bool); alt {
		return false
	}
	return comp == nil || reflect.TypeOf(comp) == reflect.TypeOf(screen.Component)
}

func pageStatus(res app.RenderResult, recorded int) int {
	if res.NotFoundOutlet {
		return http.StatusNotFound
	}
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

func (ds *UIHost) finishPageDocument(w http.ResponseWriter, r *http.Request, ctx context.Context, html render.HTML, path string, comp component.Component, status int) {
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
	}

	out := injectConfirmTemplate(string(page), r)
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

	ds.writeAgentLinkHeaders(ctx, w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
	}
	fmt.Fprint(w, out)
}

func injectWidgetSSR(page string, r *http.Request) string {
	html, _ := widgetSSRHTML(r)
	if html == "" {
		return page
	}
	return replaceChromeMarker(page, "</body>", html+"</body>", "widget chrome")
}

func widgetSSRHTML(r *http.Request) (string, []string) {
	b := borrowBuilder()
	defer returnBuilder(b)
	var toastSheets []string
	u := r.URL
	q := u.Query()
	for _, d := range widget.AvailableOn(u.Path) {
		var open bool
		switch {
		case d.DeepLinkKey != "" && d.DeepLinkValue != "":
			open = q.Get(d.DeepLinkKey) == d.DeepLinkValue
		case !d.Hidden:
			open = true
		default:
			continue
		}
		if !open {
			continue
		}
		if !d.OfferedTo(r) {
			continue
		}
		chrome := widget.RenderChromeCtx(r.Context(), d)
		if preset.IsToastStack(d) {
			live, held := registry.ScanSplit(chrome)
			toastSheets = append(append(toastSheets, live...), held...)
		}
		b.WriteString(chrome)
	}
	return b.String(), toastSheets
}

func injectConfirmTemplate(page string, r *http.Request) string {
	tpl := preset.ConfirmTemplateHTML(r.Context())
	if tpl == "" {
		return page
	}
	return replaceChromeMarker(page, "</body>", string(tpl)+"</body>", "confirm dialog")
}

func replaceChromeMarker(page, marker, replacement, what string) string {
	if !strings.Contains(page, marker) {
		slog.Default().Warn("uihost: page is missing a structural marker; chrome not injected",
			"marker", marker, "injecting", what,
			"hint", "custom layouts must emit <head>…</head> and …</body> so the host can inject the runtime")
		return page
	}
	return strings.Replace(page, marker, replacement, 1)
}

func (ds *UIHost) injectChrome(ctx context.Context, page, pagePath, sessionID, presenceTopic string) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, true, nil)
}

func (ds *UIHost) injectChromeFor(ctx context.Context, page, pagePath, sessionID, presenceTopic string, comp component.Component) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, true, comp)
}

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

func (ds *UIHost) screenOwnHead(ctx context.Context, pagePath string, comp component.Component) string {
	if ds.App == nil || pagePath == "" || routeMetaOff(ctx) {
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
	if resolved.OG != nil {
		parts = append(parts, ogTags(*resolved.OG)...)
	}
	if resolved.Twitter != nil {
		parts = append(parts, twitterTags(*resolved.Twitter)...)
	}
	if len(resolved.Schema) > 0 {
		parts = append(parts, string(seo.Render(resolved.Schema...)))
	}
	src := component.Component(screen.Component)
	if comp != nil {
		src = comp
	}
	if seoScreen, ok := src.(SEOScreen); ok {
		if h := seoScreen.HeadHTML(); h != "" {
			h = stripInlineScripts(h)
			if resolved.Description != "" {
				h, _ = scanDescriptions(h, 0)
			}
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, "\n")
}

func (ds *UIHost) injectChromeMode(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool) string {
	return ds.injectChromeModeFor(ctx, page, pagePath, sessionID, presenceTopic, bundle, nil)
}

func (ds *UIHost) injectChromeModeFor(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool, comp component.Component) string {
	return ds.injectChromeDoc(ctx, page, pagePath, sessionID, presenceTopic, bundle, comp, nil, 0)
}

type inlineHead struct {
	allow  func(styles, scripts []string) bool
	markup string
	sheets []string
}

func (ds *UIHost) injectChromeDoc(ctx context.Context, page, pagePath, sessionID, presenceTopic string, bundle bool, comp component.Component, inline *inlineHead, status int) string {
	ds.markServingBegun()
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
		sseURL := "/__gofastr/sse?session=" + sessionID
		if presenceTopic != "" {
			sseURL += "&presence=" + url.QueryEscape(presenceTopic)
		}
		if status == 0 || (status >= 200 && status < 300) {
			sseURL += ds.declaredPresenceQuery(ctx, comp)
		}
		fmt.Fprintf(headClose, `<meta name="gofastr-sse" content="%s">`+"\n", sseURL)
	}

	var headParts []string
	if screenHead := ds.screenHeadHTML(ctx, pagePath, comp); screenHead != "" {
		headParts = append(headParts, markScreenHead(screenHead))
	}
	if host, _ := scanDescriptions(stripInlineScripts(ds.headHTML), 0); host != "" {
		headParts = append(headParts, host)
	}
	if ds.pwaConfig != nil {
		headParts = append(headParts, pwaManifestLink(ctx))
	}
	for _, tag := range ds.headTags {
		headParts = append(headParts, tag)
	}
	if ld := ds.organizationJSONLD(); ld != "" {
		headParts = append(headParts, ld)
	}
	if len(headParts) > 0 {
		headClose.WriteString(strings.Join(headParts, "\n"))
		headClose.WriteByte('\n')
	}
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
	if inlined != nil && !inlined.linkApp {
		headClose.WriteString(`<style id="cui-app-css">` + inlined.app.body + "</style>\n")
	} else if ds.App != nil {
		href := "/__gofastr/app.css"
		if bundle {
			_, cssHash := ds.appCSSCached()
			href += "?v=" + cssHash
		}
		headClose.WriteString(`<link rel="stylesheet" href="` + href + `" id="cui-app-css">`)
		headClose.WriteByte('\n')
	}
	if bundle {
		if compute := widget.ComputeManifestScript(); compute != "" {
			headClose.WriteString(compute)
			headClose.WriteByte('\n')
		}
		if blocks := registry.DataBlocksHTML(); blocks != "" {
			headClose.WriteString(blocks)
			headClose.WriteByte('\n')
		}
	} else {
		if catalog := catalogJSONScript(ds); catalog != "" {
			headClose.WriteString(catalog)
			headClose.WriteByte('\n')
		}
		if manifest := runtimeModuleManifestScript(); manifest != "" {
			headClose.WriteString(manifest)
			headClose.WriteByte('\n')
		}
	}
	skip := ""
	if ctx.Value(staticExportKey{}) != nil {
		skip = "rpc"
	}
	if preloads := runtimeModulePreloadLinks(page, skip); preloads != "" {
		headClose.WriteString(preloads)
		headClose.WriteByte('\n')
	}

	runtimeSrc := "/__gofastr/runtime.js"
	if bundle {
		_, mHash := ds.manifestJS(ctx)
		scripts.WriteString(`<script defer src="/__gofastr/manifest.js?v=` + mHash + `"></script>`)
		scripts.WriteByte('\n')
		runtimeSrc += "?v=" + widget.RuntimeHash()
	}
	scripts.WriteString(`<script defer src="` + runtimeSrc + `"></script>`)
	scripts.WriteByte('\n')
	if bundle {
		for _, tag := range ds.actionScriptTags(page) {
			scripts.WriteString(tag)
			scripts.WriteByte('\n')
		}
	} else if ds.HasActions() {
		scripts.WriteString(`<script defer src="/__gofastr/actions.js"></script>`)
		scripts.WriteByte('\n')
	}
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

	colorTag := `<script src="/__gofastr/color-scheme.js"></script>`
	switch {
	case inlined != nil:
		//gofastr:allow(GOFASTR1805) hashed first-paint script: the CSP carries its sha256
		colorTag = "<script>" + inlined.scheme.body + "</script>"
	case bundle:
		colorTag = `<script src="/__gofastr/color-scheme.js?v=` + colorSchemeHash() + `"></script>`
	}
	page = replaceChromeMarker(page, "<head>", "<head>"+colorSchemeMeta+colorTag, "color-scheme bootstrap")
	headClose.WriteString(scripts.String())
	page = replaceChromeMarker(page, "</head>", headClose.String()+"</head>", "head chrome (SEO/CSS/scripts)")
	return page
}

func (ds *UIHost) documentShell(ctx context.Context, path, title, body string) string {
	return ds.documentShellLang(ctx, path, "", title, body)
}

func (ds *UIHost) documentShellLang(ctx context.Context, path, lang, title, body string) string {
	if lang == "" {
		lang = ds.LangFor(ctx, path)
	}
	return fmt.Sprintf(
		`<!DOCTYPE html><html dir="%s" lang="%s"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><meta name="text-scale" content="scale"><title>%s</title></head><body>%s</body></html>`,
		i18n.Dir(lang), stdhtml.EscapeString(lang), stdhtml.EscapeString(title), body)
}

var screenMetaTagPrefix = regexp.MustCompile(`(?i)<(meta|link)\b`)

func markScreenHead(head string) string {
	return screenMetaTagPrefix.ReplaceAllString(head, `<$1 data-cui-page-meta=""`)
}

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
