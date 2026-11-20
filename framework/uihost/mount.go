package uihost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

func (ds *UIHost) handleRuntimeJS(w http.ResponseWriter, r *http.Request) {
	serveVersionedText(w, r, "application/javascript; charset=utf-8", runtime.MustRuntimeJS(), widget.RuntimeHash(), false)
}

// Mount registers the host's routes on r and claims its NotFound and
// MethodNotAllowed handlers, so call it after every other route is registered.
func (ds *UIHost) Mount(r *router.Router) {
	ds.coreRouter = r
	dev.MaybeRegisterLiveReload(r)
	dev.MaybeRegisterLayoutLayer(r)
	ds.enforceStrict()
	ds.AutoCompileActions()
	ds.App.Router.ValidateFills()
	ds.App.Router.ValidateRequires()
	ds.App.Router.ValidateIntercepts()
	ds.enforceNoServerActionsOnEmbeds()

	r.Get("/__gofastr/runtime.js", http.HandlerFunc(ds.handleRuntimeJS))
	ds.mountEmbed(r)
	r.Get("/__gofastr/color-scheme.js", http.HandlerFunc(ds.handleColorSchemeJS))
	r.Get("/__gofastr/actions.js", http.HandlerFunc(ds.handleActionsJS))
	r.Get("/__gofastr/manifest.js", http.HandlerFunc(ds.handleManifestJS))
	r.Get("/__gofastr/app.css", http.HandlerFunc(ds.handleAppCSS))
	r.Get("/__gofastr/sse", http.HandlerFunc(ds.handleSSE))
	r.Post("/__gofastr/session", http.HandlerFunc(ds.handleCreateSession))
	r.Get("/__gofastr/session", http.HandlerFunc(methodNotAllowed))
	r.Post("/__gofastr/action", http.HandlerFunc(ds.handleServerAction))
	r.Get("/__gofastr/action", http.HandlerFunc(methodNotAllowed))
	r.Get("/__gofastr/widget/{id}", http.HandlerFunc(ds.handleWidgetJS))
	r.Get("/__gofastr/comp/{path...}", http.HandlerFunc(ds.handleComponentCSS))
	r.Get("/__gofastr/comp-bundle.css", http.HandlerFunc(ds.handleCompBundleCSS))
	r.Get("/__gofastr/widgets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if token := req.Header.Get(embedGrantHeader); token != "" {
			g, ok := ds.embedGrant(req)
			if !ok {
				i18n.HTTPError(w, req, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
				return
			}
			stripAmbientCredentials(req)
			path := ds.embedSurfacePath(g)
			if path == "" {
				path = "\x00none"
			}
			q := req.URL.Query()
			q.Set("page", (&url.URL{Path: path}).EscapedPath())
			scoped := req.Clone(req.Context())
			scoped.URL.RawQuery = q.Encode()
			widget.ServeWidgetList(w, scoped)
			return
		}
		if _, ok := ds.requireValidSession(w, req); !ok {
			return
		}
		widget.ServeWidgetList(w, req)
	}))

	widget.SetSessionCheck(func(req *http.Request) bool {
		if token := req.Header.Get(embedGrantHeader); token != "" {
			g, ok := ds.embedGrant(req)
			if !ok {
				return false
			}
			def, found := widget.GatedWidget(req)
			if !found {
				return false
			}
			path := ds.embedSurfacePath(g)
			return path != "" && def.IsAvailableOn(path)
		}
		_, ok := ds.verifySessionToken(readSessionCookie(req))
		return ok
	})

	widget.SetAuthenticatedCheck(func(req *http.Request) bool {
		if req.Header.Get(embedGrantHeader) != "" {
			return false
		}
		if _, scoped := access.HeldScopes(req.Context()); scoped {
			return false
		}
		u, ok := handler.GetUser(req.Context())
		return ok && u != nil
	})

	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(widget.ServeRuntimeModule))
	r.Get("/__gofastr/compute/{name}", http.HandlerFunc(widget.ServeComputeAsset))

	if ds.App != nil && ds.llmMDPublic {
		ds.mountPageLLMMD(r)
	}

	if ds.sitemapConfig != nil {
		r.Get("/sitemap.xml", http.HandlerFunc(ds.handleSitemap))
		r.Get("/sitemap/{part}", http.HandlerFunc(ds.handleSitemapPart))
	}
	if ds.robotsConfig != nil {
		r.Get("/robots.txt", http.HandlerFunc(ds.handleRobots))
	}
	ds.mountAppIcons(func(path string, h http.HandlerFunc) {
		r.Get(path, h)
	})
	if ds.pwaConfig != nil {
		r.Get("/manifest.webmanifest", http.HandlerFunc(ds.handlePWAManifest))
		r.Get("/service-worker.js", http.HandlerFunc(ds.handlePWAServiceWorker))
		r.Get("/__gofastr/pwa/register.js", http.HandlerFunc(ds.handlePWARegisterJS))
		r.Get("/__gofastr/pwa/offline", http.HandlerFunc(ds.handlePWAOffline))
	}
	ds.mountAgentReady(r)

	r.NotFound(http.HandlerFunc(ds.serveOrRender))
	r.MethodNotAllowed(http.HandlerFunc(ds.serveMethodNotAllowed))
}

// RoutePatterns returns the screen patterns the host serves, redirects excluded.
func (ds *UIHost) RoutePatterns() []string {
	routes := ds.App.Routes()
	patterns := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.RedirectTo != "" {
			continue
		}
		patterns = append(patterns, route.Path)
	}
	return patterns
}

func writeBlock(w http.ResponseWriter, r *http.Request, res app.RenderResult) {
	reason := res.Reason
	if reason.IsZero() {
		reason = i18n.StatusPhrase(res.Status)
	}
	i18n.HTTPError(w, r, res.Status, reason, res.Params)
}

func (ds *UIHost) mountPageLLMMD(r *router.Router) {
	coreApp := ds.App

	if coreApp.NoLLMMD {
		return
	}

	r.Get("/llm-pages.md", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		setPageMarkdownHeaders(w)
		w.Write([]byte(app.AppLLMMDCtx(app.WithRequest(req.Context(), req), coreApp)))
	}))

	seen := make(map[string]bool)
	for _, routePath := range coreApp.Router.Paths() {
		screen, ok := coreApp.Router.ScreenByPattern(routePath)
		if !ok {
			continue
		}
		if screen.NoLLMMD {
			continue
		}
		sc := screen
		base := routePath
		_, exact := coreApp.Router.Screens()[routePath]
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if exact {
				md, ok := ds.pageLLMMD(req, base, sc)
				if !ok {
					ds.handlePage(w, req)
					return
				}
				setPageMarkdownHeaders(w)
				w.Write([]byte(md))
				return
			}
			ctx := app.WithRequest(req.Context(), req)
			d := coreApp.PatternPolicy(ctx, sc)
			if d.Kind == app.DecisionBlock && d.NotFound {
				ds.ServeNotFound(w, req)
				return
			}
			setPageMarkdownHeaders(w)
			var md string
			if d.Kind != app.DecisionAllow {
				md = app.ScreenLLMMDWithheld(sc)
			} else {
				seo := ds.localizeSEO(ctx, base, resolveScreenSEO(ctx, sc))
				fm := screenSEOFrontMatter(sc.Title, seo)
				md = app.ScreenLLMMDWithMeta(ctx, sc, fm)
			}
			w.Write([]byte(md))
		})
		clean := strings.TrimRight(routePath, "/")
		route := clean + "/llm.md"
		if clean == "" {
			route = "/llm.md"
		}
		if seen[route] {
			continue
		}
		seen[route] = true
		r.Get(route, handler)
	}
}

func (ds *UIHost) isScreenPath(r *http.Request) bool {
	if ds.App == nil || ds.App.Router == nil {
		return false
	}
	path := routePath(r)
	if target, ok := ds.App.ResolveRedirect(path); ok && handler.IsSafeRelativePath(target) {
		return false
	}
	_, _, ok := ds.App.Router.Resolve(path)
	return ok
}

func screenAllow(routed string) string {
	methods := []string{http.MethodGet, http.MethodHead}
	for m := range strings.SplitSeq(routed, ",") {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(methods, m) {
			methods = append(methods, m)
		}
	}
	return strings.Join(methods, ", ")
}

func (ds *UIHost) serveOrRender(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/favicon.ico" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if rp := routePath(r); rp != "/" && !strings.HasSuffix(rp, "/") {
		if _, _, ok := ds.App.Router.Resolve(rp); !ok {
			if _, _, ok := ds.App.Router.Resolve(rp + "/"); ok {
				target := app.EscapeRoutePath(rp) + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, scrubCtl(target), http.StatusMovedPermanently)
				return
			}
		}
	}
	if path != "/" {
		if root := ds.staticRootOf(); root != nil {
			if rel := staticRel(path); rel != "" && staticServable(root.FS(), rel) {
				if info, statErr := root.Stat(filepath.FromSlash(rel)); statErr == nil && !info.IsDir() {
					if f, openErr := root.Open(filepath.FromSlash(rel)); openErr == nil {
						devStaticNoStore(w)
						ds.markVersionedStatic(w, r, assetKey{rel: rel}, f)
						http.ServeContent(w, r, path, info.ModTime(), f)
						_ = f.Close()
						return
					}
				}
			}
		}
		if ds.staticFS != nil {
			cleanPath := strings.TrimPrefix(path, "/")
			if cleanPath != "" && staticServable(ds.staticFS, cleanPath) {
				//gofastr:allow(rootread) caller-supplied fs.FS: embed.FS carries no symlinks and os.DirFS is re-rooted through os.OpenRoot at install time; the *os.File branch above already goes through os.Root
				if f, err := ds.staticFS.Open(cleanPath); err == nil && !fsEntryIsDir(f) {
					devStaticNoStore(w)
					if rs, ok := f.(io.ReadSeeker); ok && wantsVersion(r) {
						if info, err := f.Stat(); err == nil && !info.IsDir() {
							ds.markVersionedStatic(w, r, assetKey{fromFS: true, rel: cleanPath}, f)
							http.ServeContent(w, r, path, info.ModTime(), rs)
							_ = f.Close()
							return
						}
					}
					_ = f.Close()
					http.ServeFileFS(w, r, ds.staticFS, cleanPath)
					return
				}
			}
		}
	}
	if ds.faviconURL != "" && path == ds.faviconURL {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if md, ok := ds.dynamicPageLLMMD(r, routePath(r)); ok {
		setPageMarkdownHeaders(w)
		w.Write([]byte(md))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && ds.isScreenPath(r) {
		ds.serveScreenMethodNotAllowed(w, r)
		return
	}
	ds.handlePage(w, r)
}

func (ds *UIHost) dynamicPageLLMMD(r *http.Request, path string) (string, bool) {
	if !ds.pageDocsOn() || !strings.HasSuffix(path, "/llm.md") {
		return "", false
	}
	base := strings.TrimSuffix(path, "/llm.md")
	if base == "" {
		base = "/"
	}
	scr, _, ok := ds.App.Router.Resolve(base)
	if !ok || scr.NoLLMMD {
		return "", false
	}
	return ds.pageLLMMD(r, base, scr)
}

func (ds *UIHost) pageLLMMD(r *http.Request, base string, scr *app.Screen) (string, bool) {
	pr := r.Clone(r.Context())
	esc := app.EscapeRoutePath(base)
	decoded, err := url.PathUnescape(esc)
	if err != nil {
		decoded = base
	}
	pr.URL.Path, pr.URL.RawPath = decoded, esc
	ctx, recorded := app.TrackStatus(app.WithRequest(r.Context(), pr))
	res, ok := app.ScreenLLMMDForPath(ctx, ds.App, base)
	if !ok {
		return "", false
	}
	if status := pageStatus(app.RenderResult{Component: res.Component}, recorded()); status != 0 && status != http.StatusOK {
		return "", false
	}
	return ds.LLMMDWithSEO(ctx, base, scr, res), true
}

// LLMMDWithSEO returns res's markdown behind the screen's SEO front matter, the
// llm.md the host serves and the static export writes for base.
func (ds *UIHost) LLMMDWithSEO(ctx context.Context, base string, scr *app.Screen, res app.ScreenLLMMDResult) string {
	md := res.MD
	if !res.Allowed {
		return md
	}
	title := res.Title
	if title == "" {
		title = scr.Title
	}
	seo := ds.localizeSEO(ctx, base, resolveScreenSEOFor(ctx, scr, res.Component))
	if fm := screenSEOFrontMatter(title, seo); fm != "" {
		md = fm + "\n" + md
	}
	return md
}

// PageHandler renders the screen at path with full chrome, for a framework
// route that must own the path explicitly.
func (ds *UIHost) PageHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p != path && path != "" && !isDynamicScreenPath(path) {
			u := *r.URL
			u.Path = path
			r2 := r.Clone(r.Context())
			r2.URL = &u
			ds.handlePage(w, r2)
			return
		}
		ds.handlePage(w, r)
	}
}

func isDynamicScreenPath(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, ":") || (len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}') {
			return true
		}
	}
	return false
}

// RenderStaticPage renders path as a full page for a static export, with no
// live channel.
func (ds *UIHost) RenderStaticPage(ctx context.Context, path string) (string, error) {
	page, _, err := ds.RenderStaticPageResult(ctx, path)
	return page, err
}

// RenderStaticPageResult is RenderStaticPage with the render's contained fill
// failures, which a static export refuses to bake in.
func (ds *UIHost) RenderStaticPageResult(ctx context.Context, path string) (string, []app.FillFailure, error) {
	ctx = context.WithValue(ctx, staticExportKey{}, true)
	if app.RequestFromContext(ctx) == nil {
		if req, err := pageRequest(ctx, path); err == nil {
			ctx = app.WithRequest(ctx, req)
		}
	}
	ctx = withRouteMatch(ctx, ds.App.Router, path)
	ctx = store.WithValues(ctx)
	ctx, recorded := app.TrackStatus(ctx)
	res, err := ds.App.RenderPageResult(ctx, path)
	if pe, ok := errors.AsType[*app.PageError](err); ok && pe.Policy {
		return "", nil, &PolicyBlockedError{Path: path, Decision: "not found"}
	}
	if err != nil {
		return "", nil, err
	}
	switch res.Kind {
	case app.DecisionAllow, app.DecisionRenderAlt:
	case app.DecisionRedirect:
		return "", nil, &PolicyBlockedError{Path: path, Decision: fmt.Sprintf("redirect to %q", res.URL)}
	default:
		return "", nil, &PolicyBlockedError{Path: path, Decision: fmt.Sprintf("block status %d", res.Status)}
	}
	if res.NotFoundOutlet {
		return "", nil, &NotFoundOutletError{Path: path}
	}
	if status := pageStatus(res, recorded()); status != 0 && status != http.StatusOK {
		return "", nil, &StatusPageError{Path: path, Status: status}
	}
	page := ds.injectChromeModeFor(withRenderAlt(ctx, res), string(res.HTML), path, "", "", false, res.Component)
	return injectHeadData(ctx, page), res.FillFailures, nil
}

// RenderStaticNotFoundPage renders the not-found page for a static export's
// 404.html; ok is false without a root layout or when the render fails.
func (ds *UIHost) RenderStaticNotFoundPage(ctx context.Context, path string) (string, bool) {
	if ds.App == nil || ds.App.Router.GetDefaultLayout() == nil {
		return "", false
	}
	ctx = context.WithValue(ctx, staticExportKey{}, true)
	if app.RequestFromContext(ctx) == nil {
		if req, err := pageRequest(ctx, path); err == nil {
			ctx = app.WithRequest(ctx, req)
		}
	}
	ctx = withoutRouteMeta(store.WithValues(ctx))
	res, err := ds.App.RenderNotFoundPageResult(ctx, path, ds.notFoundBodyComponent(""))
	if err != nil {
		slog.Warn("uihost: static not-found render failed; exporting no 404.html", "err", err)
		return "", false
	}
	page := ds.injectChromeModeFor(ctx, string(res.HTML), path, "", "", false, res.Component)
	return injectHeadData(ctx, page), true
}
