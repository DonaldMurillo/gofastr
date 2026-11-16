package uihost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// NotFoundRenderer is an optional interface a custom 404 screen (see
// WithNotFoundScreen) may implement to receive the unmatched request
// path. The path arrives as an argument, so a single shared screen
// instance can render per-request detail without a data race. Screens
// that don't implement it just render via component.Component.Render.
type NotFoundRenderer interface {
	RenderNotFound(path string) render.HTML
}

// NotFoundTitler supplies the title for a custom 404 screen. The context
// and path belong to the current render. An empty path identifies an outlet
// fallback. Returning an empty title falls back to app.ScreenTitler, then
// the framework's default not-found title.
type NotFoundTitler interface {
	NotFoundTitle(ctx context.Context, path string) string
}

// notFoundBodyScreen adapts the host's configured 404 screen
// ([WithNotFoundScreen]) to the app render pipeline's component slot:
// RenderNotFound(path) when the screen implements it, else RenderCtx or
// Render, the way any component renders. A panicking screen stays
// contained — the pipeline renders the adapter under
// component.SafeRenderCtx, the guard this path used to hand-roll.
type notFoundBodyScreen struct {
	path string
	comp component.Component
}

func (s *notFoundBodyScreen) Render() render.HTML {
	return s.RenderCtx(context.Background())
}

// RenderCtx hands the request context to a screen written as
// component.ContextOnly + RenderCtx, whose Render answers "".
func (s *notFoundBodyScreen) RenderCtx(ctx context.Context) render.HTML {
	if nf, ok := s.comp.(NotFoundRenderer); ok {
		return nf.RenderNotFound(s.path)
	}
	return component.RenderComponentCtx(ctx, s.comp)
}

func (s *notFoundBodyScreen) ScreenTitle(ctx context.Context) string {
	if nf, ok := s.comp.(NotFoundTitler); ok {
		if title := nf.NotFoundTitle(ctx, s.path); title != "" {
			return title
		}
	}
	if titled, ok := s.comp.(app.ScreenTitler); ok {
		return titled.ScreenTitle(ctx)
	}
	return ""
}

// defaultNotFoundScreen is the framework's built-in 404 body, the same
// shape the 405 page uses: heading, the escaped request path, a home
// link. Composed from design-system elements, zero bespoke CSS. It
// renders INSIDE the root layout's <main> slot, so it carries no
// role="main" wrapper of its own.
type defaultNotFoundScreen struct {
	component.ContextOnly
	path string
}

// backToHomeLink is the default error pages' primary action: a typed
// anchor dressed in framework/ui's button-link class vocabulary
// (fui-button fui-button--primary). uihost sits BELOW the component
// layer and must not import framework/ui (framework/layering_test.go
// pins it), so the classes are spelled, not imported — the
// composition framework/ui's own component-options contract
// sanctions ("a caller who writes fui-button on their own markup
// gets the framework's styling whenever this sheet is on the page"):
// the ui-button sheet is plain class rules and ships eagerly on
// every host that links the package. On a host that does not, the
// anchor degrades to the browser's link look — no sheet exists to
// fetch, so no marker is emitted either.
func backToHomeLink(ctx context.Context) render.HTML {
	return html.Link(html.LinkConfig{
		Href:  "/",
		Text:  pageBackHome.Text(ctx),
		Class: "fui-button fui-button--primary",
	})
}

func (s *defaultNotFoundScreen) RenderCtx(ctx context.Context) render.HTML {
	if s.path == "" {
		// A static export's 404.html: rendered once at build time, so
		// there is no request path to name.
		return render.Join(
			html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageNotFoundTitle.Text(ctx))),
			html.Paragraph(html.TextConfig{}, render.Text(pageNotFoundBody.Text(ctx))),
			html.Paragraph(html.TextConfig{}, backToHomeLink(ctx)),
		)
	}
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageNotFoundTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, phraseAround(ctx, pageNoRoute, "path",
			html.Code(html.TextConfig{}, render.Text(textsafe.StripInvisible(s.path))), nil)),
		html.Paragraph(html.TextConfig{}, backToHomeLink(ctx)),
	)
}

// notFoundBodyComponent picks the 404 body component: the host's
// configured screen, else the built-in default.
func (ds *UIHost) notFoundBodyComponent(path string) component.Component {
	if ds.notFoundScreen != nil {
		return &notFoundBodyScreen{path: path, comp: ds.notFoundScreen}
	}
	return &defaultNotFoundScreen{path: path}
}

// serveNotFound writes a 404, content-negotiated when the request's
// Accept header names a machine representation: JSON (application/json
// or application/problem+json) gets an RFC 9457 problem document, and
// text/markdown gets a recovery-links body when markdown negotiation is
// enabled. The JSON arm is always on — a correct machine-readable error
// shape is an error-format fix, not an agent feature. Neither machine
// arm ever reflects the request path; the HTML fallback escapes it.
// When the app declares a default (root) layout, the HTML arm renders
// the not-found body — WithNotFoundScreen's component when set, else
// the built-in default — through that layout's shell (outlets show
// their defaults), with the same runtime.js/theme chrome every page
// carries; a navigation request (X-Gofastr-Navigate + a known
// X-Gofastr-From) instead gets the partial/envelope shape any route
// answers with, swapping at layer 0. Without a root layout the
// framework falls back to the minimal HTML body so something always
// renders.
func (ds *UIHost) serveNotFound(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Add("Vary", "Accept")
	if acceptsProblemJSON(r) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		// The problem document deliberately omits the request path:
		// echoing a hostile URL into a machine body is a reflection
		// vector no error page needs.
		json.NewEncoder(w).Encode(newProblem(r.Context(), http.StatusNotFound, problemNotFound, problemNoRoute))
		return
	}
	// Markdown recovery arm: agent-readable, gated on the same
	// content-negotiation opt-in as negotiated pages (the HTML arm
	// below remains the default 404). Root-relative links only: the
	// requested path is never echoed back, escaped or otherwise.
	if ds.agentReady != nil && ds.agentReady.contentNeg && acceptsMarkdown(r) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "# Not found\n\nNo page lives at this address. Try:\n\n"+
			"- [Home](/)\n- [Site map](/sitemap.xml)\n- [llms.txt](/llms.txt)\n")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// a navigation fetch that missed every
	// route gets the answer shape any route gives it — the not-found
	// body as a partial/envelope swapping at the deepest layer the
	// origin shares with the root layout (layer 0 for a same-shell
	// nav), status 404 — so the runtime shows the error page inside
	// the live shell instead of toasting. A fetch without X-Gofastr-From
	// (boot, an unknown origin) takes the full-document arm below: the
	// runtime reads a whole document as an envelope () in one
	// fetch.
	if ds.App != nil && r.Header.Get("X-Gofastr-Navigate") == "1" && r.Header.Get("X-Gofastr-From") != "" {
		if ds.serveNotFoundPartial(w, r, path) {
			return
		}
	}
	if ds.renderNotFoundDocument(w, r, path) {
		return
	}

	w.WriteHeader(http.StatusNotFound)
	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	ctx := r.Context()
	title := pageNotFoundDocTitle.Text(ctx, map[string]any{"app": appName})
	// With no root layout the configured screen still renders, in the
	// bare <main>; a screen that panics gets the built-in body.
	if ds.notFoundScreen != nil {
		screen := ds.notFoundBodyComponent(path)
		if body, err := component.SafeRenderCtx(ctx, screen); err == nil {
			if name := safeNotFoundTitle(ctx, screen, path); name != "" {
				// Same convention as the page pipeline: "<page> — <app>".
				title = name
				if suffix := " — " + appName; !strings.HasSuffix(title, suffix) {
					title += suffix
				}
			}
			writeMinimalPage(w, ds.LangFor(ctx, path), textsafe.StripInvisible(title), body)
			return
		}
	}
	writeMinimalPage(w, ds.LangFor(ctx, path), title,
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageNotFoundTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, phraseAround(ctx, pageNoRoute, "path",
			html.Code(html.TextConfig{}, render.Text(textsafe.StripInvisible(path))), nil)),
		html.Paragraph(html.TextConfig{}, html.Link(html.LinkConfig{Href: "/", Text: pageBackHome.Text(ctx)})),
	)
}

// safeNotFoundTitle asks a configured 404 screen for its title. A title
// provider that panics names nothing, and the page keeps the default
// title, as the layout pipeline's title lookup does.
func safeNotFoundTitle(ctx context.Context, screen component.Component, path string) (title string) {
	t, ok := screen.(app.ScreenTitler)
	if !ok {
		return ""
	}
	defer func() {
		if err := recover(); err != nil {
			title = ""
			slog.Error("uihost: not-found screen title panicked",
				"path", textsafe.ScrubControlBytes(path),
				"panic", textsafe.Recovered(err))
		}
	}()
	return t.ScreenTitle(ctx)
}

// writeMinimalPage writes the bare error document a host with no root
// layout serves: lang and title escaped, body already rendered HTML.
func writeMinimalPage(w io.Writer, lang, title string, body ...render.HTML) {
	fmt.Fprintf(w,
		`<!DOCTYPE html><html dir="%s" lang="%s"><head><meta charset="UTF-8"><title>%s</title></head>`+
			`<body><main role="main">%s</main></body></html>`,
		i18n.Dir(lang), stdhtml.EscapeString(lang), stdhtml.EscapeString(title), render.Join(body...))
}

// renderNotFoundDocument renders the not-found page as a full document
// through the app's root layout and finishes it through the same tail
// every page runs (finishPageDocument): session verify-or-mint with the
// id in the chrome, no-store + Vary: Cookie, signal seed, widget SSR.
// The body lands in the layer-0 shell's <main> slot, the shell's
// outlets show their defaults, route areas read the requested path, and
// injectChrome adds the same runtime.js/theme chrome every page
// carries. ok=false keeps the caller on the bare legacy 404 page (no
// root layout, or the render failed).
func (ds *UIHost) renderNotFoundDocument(w http.ResponseWriter, r *http.Request, path string) bool {
	if ds.App == nil || ds.App.Router.GetDefaultLayout() == nil {
		return false
	}
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	res, err := ds.App.RenderNotFoundPageResult(ctx, path, ds.notFoundBodyComponent(path))
	if err != nil {
		slog.Warn("uihost: not-found page render failed; serving bare 404", "err", err)
		return false
	}
	ds.finishPageDocument(w, r, ctx, res.HTML, path, res.Component, http.StatusNotFound)
	return true
}

// serveNotFoundPartial answers a navigation fetch (X-Gofastr-Navigate)
// that missed every route with the same partial shape any route answers
// with: the not-found body rendered under the layers the origin route
// does not share with the root layout — a same-shell navigation swaps at
// layer 0 — and the kept layers' outlet fills beside it, status 404.
// Returns false (nothing written) when the app has no root layout or
// the render fails; the caller falls back to the full-document arm.
func (ds *UIHost) serveNotFoundPartial(w http.ResponseWriter, r *http.Request, path string) bool {
	if ds.App == nil || ds.App.Router.GetDefaultLayout() == nil {
		return false
	}
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	res, err := ds.App.RenderNotFoundFromResult(ctx, path, fromPath(r), ds.notFoundBodyComponent(path))
	if err != nil {
		slog.Warn("uihost: not-found partial render failed; serving document 404", "err", err)
		return false
	}
	switch res.Kind {
	case app.DecisionAllow, app.DecisionRenderAlt:
	default:
		return false
	}
	ds.writePartialResult(w, r, ctx, path, res, nil, http.StatusNotFound)
	return true
}

// defaultErrorScreen is the framework's built-in 500 body: a heading,
// one honest sentence, a home link. It deliberately names NO detail of
// the failure (the resolver's error text is logged, never rendered)
// and renders inside the root layout's <main> slot.
type defaultErrorScreen struct{ component.ContextOnly }

func (s *defaultErrorScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageErrorTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, render.Text(pageErrorBody.Text(ctx))),
		html.Paragraph(html.TextConfig{}, backToHomeLink(ctx)),
	)
}

// errorBodyComponent picks the 500 body component: the host's
// configured screen ([WithErrorScreen]), else the built-in default.
func (ds *UIHost) errorBodyComponent() component.Component {
	if ds.errorScreen != nil {
		return ds.errorScreen
	}
	return &defaultErrorScreen{}
}

// serveResolverError answers a whole-page resolver failure
// (app.PageError, ErrNotFound takes the
// not-found page's whole pipeline (status 404); anything else the
// error page (status 500). The failure is logged here, scrubbed —
// the bodies never carry error text.
func (ds *UIHost) serveResolverError(w http.ResponseWriter, r *http.Request, path string, pe *app.PageError) {
	slog.Error("uihost: route resolver failed; serving error page",
		"path", textsafe.StripUnsafe(path),
		"err", textsafe.Recovered(pe.Err),
		"not_found", pe.NotFound)
	if pe.NotFound {
		ds.serveNotFound(w, r, path)
		return
	}
	ds.serveError(w, r, path)
}

// serveRenderError discriminates the render pipeline's error channel,
// which folds every failure into one error return. A contained panic
// (app.ErrScreenPanicked, from a screen's Render or its Load) is a
// server bug: one Error log line naming the path and the scrubbed
// panic, then a 500. The silent 404 this used to answer cost an agent
// its longest debugging loop. Every other error keeps the 404 it always
// had (a Load that RETURNS an error is the documented screen-chooses-
// not-found contract), but a route that resolves is no longer silent:
// one Warn line carries the path and the error. A path no route owns
// stays a plain, unlogged 404; an unknown URL is not an incident.
func (ds *UIHost) serveRenderError(w http.ResponseWriter, r *http.Request, path string, err error) {
	if errors.Is(err, app.ErrScreenPanicked) {
		slog.Default().Error("uihost: screen render panicked; serving 500",
			"path", textsafe.ScrubControlBytes(path),
			"panic", textsafe.Recovered(err))
		ds.serveError(w, r, path)
		return
	}
	if _, _, ok := ds.App.Router.Resolve(path); ok {
		slog.Default().Warn("uihost: screen render failed; serving 404",
			"path", textsafe.ScrubControlBytes(path),
			"err", textsafe.Recovered(err))
	}
	ds.serveNotFound(w, r, path)
}

// serveError is serveNotFound's 500 twin. A request whose Accept header
// names a machine representation gets an RFC 9457 problem document;
// otherwise the error body renders through the app's root layout when
// one exists (a navigation fetch carrying X-Gofastr-From gets the
// partial/envelope shape instead, so the runtime shows the page inside
// the live shell), else a minimal document. Vary: Accept on every arm,
// and no error text or request path in any body.
func (ds *UIHost) serveError(w http.ResponseWriter, r *http.Request, path string) {
	if acceptsProblemJSON(r) {
		ds.serveServerError(w, r, path)
		return
	}
	w.Header().Add("Vary", "Accept")
	if ds.App != nil && r.Header.Get("X-Gofastr-Navigate") == "1" && r.Header.Get("X-Gofastr-From") != "" {
		if ds.serveErrorPartial(w, r, path) {
			return
		}
	}
	if ds.renderErrorDocument(w, r, path) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	ctx := r.Context()
	writeMinimalPage(w, ds.LangFor(ctx, path), pageErrorTitle.Text(ctx),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageErrorTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, render.Text(pageErrorBody.Text(ctx))),
		html.Paragraph(html.TextConfig{}, html.Link(html.LinkConfig{Href: "/", Text: pageBackHome.Text(ctx)})),
	)
}

// serveServerError writes a bare 500 with no root layout: an RFC 9457
// problem document when the Accept header names a machine
// representation, a minimal HTML page otherwise, Vary: Accept on both
// arms. It echoes nothing: neither the path (a hostile URL is a
// reflection vector no error page needs) nor any panic text
// (host/component state). serveError delegates its problem+json arm
// here; the embed content route uses it directly, since a frame must
// not receive the site's root layout.
func (ds *UIHost) serveServerError(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Add("Vary", "Accept")
	if acceptsProblemJSON(r) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(newProblem(r.Context(), http.StatusInternalServerError, problemServerError, problemRenderFailed))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	ctx := r.Context()
	writeMinimalPage(w, ds.LangFor(ctx, path), pageServerErrorDocTitle.Text(ctx, map[string]any{"app": appName}),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageServerErrorTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, render.Text(problemRenderFailed.Text(ctx))),
		html.Paragraph(html.TextConfig{}, html.Link(html.LinkConfig{Href: "/", Text: pageBackHome.Text(ctx)})),
	)
}

// renderErrorDocument is renderNotFoundDocument's 500 twin, finished
// through the same page tail (finishPageDocument).
func (ds *UIHost) renderErrorDocument(w http.ResponseWriter, r *http.Request, path string) bool {
	if ds.App == nil || ds.App.Router.GetDefaultLayout() == nil {
		return false
	}
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	res, err := ds.App.RenderErrorPageResult(ctx, path, ds.errorBodyComponent())
	if err != nil {
		slog.Warn("uihost: error page render failed; serving bare 500", "err", err)
		return false
	}
	ds.finishPageDocument(w, r, ctx, res.HTML, path, res.Component, http.StatusInternalServerError)
	return true
}

// serveErrorPartial is serveNotFoundPartial's 500 twin: the error body
// as a partial/envelope at the deepest layer the origin shares with
// the root layout, status 500. The runtime's path applies it
// inside the live shell.
func (ds *UIHost) serveErrorPartial(w http.ResponseWriter, r *http.Request, path string) bool {
	if ds.App == nil || ds.App.Router.GetDefaultLayout() == nil {
		return false
	}
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	res, err := ds.App.RenderErrorFromResult(ctx, path, fromPath(r), ds.errorBodyComponent())
	if err != nil {
		slog.Warn("uihost: error partial render failed; serving document 500", "err", err)
		return false
	}
	switch res.Kind {
	case app.DecisionAllow, app.DecisionRenderAlt:
	default:
		return false
	}
	ds.writePartialResult(w, r, ctx, path, res, nil, http.StatusInternalServerError)
	return true
}

// methodNotAllowed is registered alongside POST-only endpoints so a wrong-
// method request gets a clear 405 instead of falling through to the UI
// page handler and getting a misleading 404.
func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
}

// serveMethodNotAllowed is the router's 405 catch-all. For safe
// methods (GET/HEAD) where a static file or screen resolves at the
// path, it delegates to serveOrRender so the page renders normally
// (200). This restores fall-through for a screen that shares its path
// with a POST-only sibling route. Any other method on such a path gets
// the screen-path answer (serveScreenMethodNotAllowed), with the
// screen's GET and HEAD added to the router's Allow. For all other
// cases it renders a styled 405 page through the default layout,
// preserving the Allow header the router already set.
func (ds *UIHost) serveMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && ds.resolvesStaticOrScreen(r) {
		ds.serveOrRender(w, r)
		return
	}
	if ds.isScreenPath(r) {
		ds.serveScreenMethodNotAllowed(w, r)
		return
	}
	ds.serveMethodNotAllowedPage(w, r)
}

// serveScreenMethodNotAllowed answers a method other than GET or HEAD
// to a screen path that no registered route takes. A screen is a GET
// resource: rendering it for a POST ran its policy and Load and minted
// a session for a request that never asked to read the page. OPTIONS
// gets 204 with the Allow header; every other method gets 405 and the
// kit's 405 document. Nothing of the screen's runs: no policy, no Load,
// and no session is minted (a live cookie is read, never renewed). A
// route registered for the method at the path, an explicit PageHandler
// included, is dispatched by the router before this is reached.
func (ds *UIHost) serveScreenMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", screenAllow(w.Header().Get("Allow")))
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ds.renderMethodNotAllowed(w, r.WithContext(withoutMint(r.Context())))
}

// serveMethodNotAllowedPage renders a styled 405 page through the
// default layout, mirroring serveNotFound's page-building machinery so
// the 405 is visually consistent with the 404. The Allow header is
// preserved (set by the router before dispatching). Composed entirely
// from design-system elements, zero bespoke CSS.
func (ds *UIHost) serveMethodNotAllowedPage(w http.ResponseWriter, r *http.Request) {
	ds.renderMethodNotAllowed(w, r)
}

// renderMethodNotAllowed writes the 405 answer: an RFC 9457 problem
// document when the Accept header names one (the 404 and 500 shape),
// else the 405 document. A request context marked withoutMint (the
// screen-path refusal) keeps the document's tail from minting.
func (ds *UIHost) renderMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	allow := w.Header().Get("Allow")
	w.Header().Add("Vary", "Accept")
	if acceptsProblemJSON(r) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		// Neither the path nor the method is echoed, as in the 404's
		// problem document.
		json.NewEncoder(w).Encode(newProblem(r.Context(), http.StatusMethodNotAllowed, problemMethodNotAllowed, problemMethodRefused))
		return
	}

	rctx := r.Context()
	children := []render.HTML{
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageMethodTitle.Text(rctx))),
		html.Paragraph(html.TextConfig{}, phraseAround(rctx, pageMethodBody, "path",
			html.Code(html.TextConfig{}, render.Text(path)), map[string]any{"method": r.Method})),
	}
	if allow != "" {
		children = append(children, html.Paragraph(html.TextConfig{}, phraseAround(rctx, pageMethodAllowed, "methods",
			html.Code(html.TextConfig{}, render.Text(allow)), nil)))
	}
	children = append(children, html.Paragraph(html.TextConfig{}, backToHomeLink(rctx)))
	body := html.Div(html.DivConfig{Role: "main"}, children...)

	if ds.App != nil && ds.App.Router != nil {
		if layout := ds.App.Router.GetDefaultLayout(); layout != nil {
			body = layout.WrapCtx(r.Context(), body)
		}
	}
	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	// The same page-finishing tail every document through the layout
	// runs (finishPageDocument): session verify-or-mint, no-store +
	// Vary: Cookie, chrome with the session id, seed, widget SSR.
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	ds.finishPageDocument(w, r, ctx,
		//gofastr:allow(GOFASTR1403) documentShell HTML-escapes the concatenated title; body is already rendered HTML.
		render.HTML(ds.documentShell(ctx, path, "405: "+appName, string(body))), path, nil, http.StatusMethodNotAllowed)
}

// NotFoundOutletError is the static-render form of the 404-outlet
// outcome (FallbackNotFound, Decided 5): the route registered, but its
// render is the not-found page — status 404, the app's not-found body
// in the primary. A static export skips such a route (the builder
// logs this error beside it) exactly like a dynamic route without
// StaticPaths: baking a 404 page in at the route's URL would be
// wrong for every visitor, and the live server keeps answering it.
type NotFoundOutletError struct {
	Path string
}

func (e *NotFoundOutletError) Error() string {
	return fmt.Sprintf("uihost: static render %q: a FallbackNotFound outlet had no fill, the route's answer is 404", e.Path)
}

// StatusPageError is the static-render form of a page that answers a
// status other than 200: a component called app.SetStatus while it
// drew (entityui's Record for a missing row) or the screen declares
// ScreenStatusCode. The builder skips the route with a warning, like
// NotFoundOutletError, because the static host would serve the file
// with a 200.
type StatusPageError struct {
	Path   string
	Status int
}

func (e *StatusPageError) Error() string {
	return fmt.Sprintf("uihost: static render %q: the page answers %d, not 200", e.Path, e.Status)
}

// PolicyBlockedError reports a static render refused by the screen's
// policy: a redirect or a block, never a render failure. The static
// builder skips such routes (with a warning) instead of aborting the
// whole export. The route stays reachable via SSR on the live server.
type PolicyBlockedError struct {
	Path     string
	Decision string // "redirect to "/login"" or "block status 403"
}

func (e *PolicyBlockedError) Error() string {
	return fmt.Sprintf("uihost: static render %q: policy returned %s", e.Path, e.Decision)
}
