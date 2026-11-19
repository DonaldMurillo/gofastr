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
// WithNotFoundScreen) implements to receive the unmatched request path.
type NotFoundRenderer interface {
	RenderNotFound(path string) render.HTML
}

// NotFoundTitler supplies the title for a custom 404 screen. An empty path
// identifies an outlet fallback. An empty title falls back to
// app.ScreenTitler, then the default not-found title.
type NotFoundTitler interface {
	NotFoundTitle(ctx context.Context, path string) string
}

type notFoundBodyScreen struct {
	path string
	comp component.Component
}

func (s *notFoundBodyScreen) Render() render.HTML {
	return s.RenderCtx(context.Background())
}

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

type defaultNotFoundScreen struct {
	component.ContextOnly
	path string
}

func backToHomeLink(ctx context.Context) render.HTML {
	return html.Link(html.LinkConfig{
		Href:  "/",
		Text:  pageBackHome.Text(ctx),
		Class: "fui-button fui-button--primary",
	})
}

func (s *defaultNotFoundScreen) RenderCtx(ctx context.Context) render.HTML {
	if s.path == "" {
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

func (ds *UIHost) notFoundBodyComponent(path string) component.Component {
	if ds.notFoundScreen != nil {
		return &notFoundBodyScreen{path: path, comp: ds.notFoundScreen}
	}
	return &defaultNotFoundScreen{path: path}
}

func (ds *UIHost) serveNotFound(w http.ResponseWriter, r *http.Request, path string) {
	r = r.WithContext(withoutRouteMeta(r.Context()))
	w.Header().Add("Vary", "Accept")
	if acceptsProblemJSON(r) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(newProblem(r.Context(), http.StatusNotFound, problemNotFound, problemNoRoute))
		return
	}
	if ds.agentReady != nil && ds.agentReady.contentNeg && acceptsMarkdown(r) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "# Not found\n\nNo page lives at this address. Try:\n\n"+
			"- [Home](/)\n- [Site map](/sitemap.xml)\n- [llms.txt](/llms.txt)\n")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
	if ds.notFoundScreen != nil {
		screen := ds.notFoundBodyComponent(path)
		if body, err := component.SafeRenderCtx(ctx, screen); err == nil {
			if name := safeNotFoundTitle(ctx, screen, path); name != "" {
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

func writeMinimalPage(w io.Writer, lang, title string, body ...render.HTML) {
	fmt.Fprintf(w,
		`<!DOCTYPE html><html dir="%s" lang="%s"><head><meta charset="UTF-8"><title>%s</title></head>`+
			`<body><main role="main">%s</main></body></html>`,
		i18n.Dir(lang), stdhtml.EscapeString(lang), stdhtml.EscapeString(title), render.Join(body...))
}

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

type defaultErrorScreen struct{ component.ContextOnly }

func (s *defaultErrorScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageErrorTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{}, render.Text(pageErrorBody.Text(ctx))),
		html.Paragraph(html.TextConfig{}, backToHomeLink(ctx)),
	)
}

func (ds *UIHost) errorBodyComponent() component.Component {
	if ds.errorScreen != nil {
		return ds.errorScreen
	}
	return &defaultErrorScreen{}
}

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

func (ds *UIHost) serveError(w http.ResponseWriter, r *http.Request, path string) {
	r = r.WithContext(withoutRouteMeta(r.Context()))
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

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
}

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

func (ds *UIHost) serveScreenMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", screenAllow(w.Header().Get("Allow")))
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ds.renderMethodNotAllowed(w, r.WithContext(withoutMint(r.Context())))
}

func (ds *UIHost) serveMethodNotAllowedPage(w http.ResponseWriter, r *http.Request) {
	ds.renderMethodNotAllowed(w, r)
}

func (ds *UIHost) renderMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(withoutRouteMeta(r.Context()))
	path := r.URL.Path
	allow := w.Header().Get("Allow")
	w.Header().Add("Vary", "Accept")
	if acceptsProblemJSON(r) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusMethodNotAllowed)
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
	ctx := store.WithValues(app.WithRequest(r.Context(), r))
	ds.finishPageDocument(w, r, ctx,
		//gofastr:allow(GOFASTR1403) documentShell HTML-escapes the concatenated title; body is already rendered HTML.
		render.HTML(ds.documentShell(ctx, path, "405: "+appName, string(body))), path, nil, http.StatusMethodNotAllowed)
}

// NotFoundOutletError is the static-render error for a route whose
// FallbackNotFound outlet had no fill, so its answer is 404. The static
// builder skips the route.
type NotFoundOutletError struct {
	Path string
}

func (e *NotFoundOutletError) Error() string {
	return fmt.Sprintf("uihost: static render %q: a FallbackNotFound outlet had no fill, the route's answer is 404", e.Path)
}

// StatusPageError is the static-render error for a page that answers a
// status other than 200, set by app.SetStatus or ScreenStatusCode. The
// static builder skips the route.
type StatusPageError struct {
	Path   string
	Status int
}

func (e *StatusPageError) Error() string {
	return fmt.Sprintf("uihost: static render %q: the page answers %d, not 200", e.Path, e.Status)
}

// PolicyBlockedError reports a static render the screen's policy
// redirected or blocked. The static builder skips the route.
type PolicyBlockedError struct {
	Path     string
	Decision string // "redirect to "/login"" or "block status 403"
}

func (e *PolicyBlockedError) Error() string {
	return fmt.Sprintf("uihost: static render %q: policy returned %s", e.Path, e.Decision)
}
