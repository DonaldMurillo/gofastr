package uihost

import (
	"context"
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

// NOTE: there is deliberately NO generic HTTP roster endpoint. A presence
// roster is "who is viewing topic X". Exposing it over an ungated URL leaks
// identities (emails) to anyone who can guess a topic string. The framework
// can't know an app's per-topic authorization, so the roster stays an
// in-process primitive (island.Manager.PresenceRoster) plus live-push via the
// existing SSE lane. An app that wants an HTTP roster builds one behind its
// own authz. See framework/docs/content/presence.md.

// handleRuntimeJS serves the core-ui runtime JavaScript. The injected
// <script> carries ?v=<widget.RuntimeHash()> so the response is
// immutable per deploy; the modules were already served that way while
// the core bundle re-downloaded on every visit.
func (ds *UIHost) handleRuntimeJS(w http.ResponseWriter, r *http.Request) {
	serveVersionedText(w, r, "application/javascript; charset=utf-8", runtime.MustRuntimeJS(), widget.RuntimeHash(), false)
}

// Mount registers the UI's HTTP handlers on the given router.
//
// It registers:
//   - All `/__gofastr/*` infrastructure endpoints (runtime.js, actions.js,
//     SSE, session, server actions, widget JS, CSS chunks)
//   - A NotFound handler that first attempts static-file resolution (from
//     either staticDir or staticFS) and falls back to page rendering.
//
// Mount must be called after the framework.App has registered its other
// routes (entity CRUD, custom endpoints) so the page handler only takes
// requests that nothing else claimed.
func (ds *UIHost) Mount(r *router.Router) {
	// Stash the framework router first: Mount's strict checks (and, at
	// boot, the internal-link check in ValidateBoot) resolve against it.
	// The link check deliberately does NOT run here — the table is still
	// partial at Mount (batteries, plugins, and App.Start itself all
	// register routes afterwards). App.Start calls ValidateBoot once the
	// table is complete; a host mounted outside a framework.App never
	// reaches that point and never gets the link check.
	ds.coreRouter = r
	// The page links the dev scripts whenever their gates are on, so the
	// router it mounts on must serve them. framework.NewApp already
	// registered them on its router (both calls are idempotent); a
	// standalone host or a custom bootstrap gets them here.
	dev.MaybeRegisterLiveReload(r)
	dev.MaybeRegisterLayoutLayer(r)
	ds.enforceStrict()
	ds.AutoCompileActions()
	// Fill validation (DESIGN "Validation at mount"): a fill naming
	// an outlet whose layout is not in the screen's chain panics at
	// boot, naming the layout and the outlet — the same
	// refuse-at-mount posture the action-id collision check above
	// takes. Screens registered after Mount are caught at render.
	ds.App.Router.ValidateFills()
	// Requires validation, the same refuse-at-mount posture: a group
	// Requires name no resolver declaration matches is a silently
	// dropped policy check, so it panics here at boot, naming the
	// group and the screen. Screens registered after Mount are caught
	// at render (the render paths re-check, like fills).
	ds.App.Router.ValidateRequires()
	ds.App.Router.ValidateIntercepts()
	// Refuse a server action on an embeddable surface at boot, before any
	// route is served: G.serverAction is dead inside a frame, and finding out
	// in a customer's page is the failure this exists to prevent. See
	// embed_actions.go.
	ds.enforceNoServerActionsOnEmbeds()

	r.Get("/__gofastr/runtime.js", http.HandlerFunc(ds.handleRuntimeJS))
	// Embeddable surfaces. Registered only when WithEmbed configured a host,
	// so an app that hands out no pieces of itself serves no embed route at
	// all, not even a 404 that would confirm the feature exists.
	ds.mountEmbed(r)
	r.Get("/__gofastr/color-scheme.js", http.HandlerFunc(ds.handleColorSchemeJS))
	r.Get("/__gofastr/actions.js", http.HandlerFunc(ds.handleActionsJS))
	r.Get("/__gofastr/manifest.js", http.HandlerFunc(ds.handleManifestJS))
	r.Get("/__gofastr/app.css", http.HandlerFunc(ds.handleAppCSS))
	r.Get("/__gofastr/sse", http.HandlerFunc(ds.handleSSE))
	// Session minting is POST-only: GET would let CSRF / form-action /
	// image-src style attacks mint sessions silently. Explicit GET
	// registration returns 405 (the router's NotFound handler would
	// otherwise mask the method-mismatch with a 404 page render).
	r.Post("/__gofastr/session", http.HandlerFunc(ds.handleCreateSession))
	r.Get("/__gofastr/session", http.HandlerFunc(methodNotAllowed))
	r.Post("/__gofastr/action", http.HandlerFunc(ds.handleServerAction))
	r.Get("/__gofastr/action", http.HandlerFunc(methodNotAllowed))
	r.Get("/__gofastr/widget/{id}", http.HandlerFunc(ds.handleWidgetJS))
	// Per-component scoped CSS + bundle endpoint for first paint.
	// See core-ui/registry and core-ui/ARCHITECTURE.md.
	r.Get("/__gofastr/comp/{path...}", http.HandlerFunc(ds.handleComponentCSS))
	r.Get("/__gofastr/comp-bundle.css", http.HandlerFunc(ds.handleCompBundleCSS))
	// runtime.js auto-discovers core-ui/widget widgets at
	// /__gofastr/widgets. Delegate to the widget registry so apps that
	// mount widgets (preset.Modal, ToastStack, …) are visible to the
	// runtime; the registry returns an empty list when nothing has
	// been registered, so plain framework apps still get the safe
	// "no widgets" response that prevents a 404 in the console.
	// The session cookie this asks for is no sign-in: any page view
	// mints one, so an anonymous visitor reads the catalog too. What
	// keeps it from naming more than the caller may use is the catalog
	// itself: it lists only the widgets the page offers this caller
	// (widget.Definition.OfferedTo: the session gate and ListedFor).
	r.Get("/__gofastr/widgets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// An embedded surface has no session cookie, none is ever sent from
		// inside a frame, so a cookie-only gate meant every widget in an
		// embed silently did nothing: the catalog 401'd, _widgetCatalog stayed
		// empty, and a modal trigger was dead DOM. Worse, in a same-site
		// framing an ambient cookie COULD satisfy it, so whether an embed's
		// widgets worked depended on the viewer's unrelated app session.
		//
		// A grant answers only for ITS OWN surface. The caller supplies the
		// page to filter by, and a grant proves nothing about any page but the
		// one its surface renders, so the grant's path is substituted rather
		// than trusted from the query. Without that, a grant for a public
		// pricing surface (no subject, handed to every anonymous visitor) asked
		// for ?page=/admin, or omitted the parameter entirely and read the
		// unfiltered registry.
		if token := req.Header.Get(embedGrantHeader); token != "" {
			// A presented grant's verdict is final here too. Falling through to
			// the cookie let an EXPIRED embed request, which the runtime keeps
			// sending on purpose so the server 401s, be answered on the
			// strength of the viewer's unrelated session in a same-site framing,
			// skipping the per-surface scoping entirely.
			g, ok := ds.embedGrant(req)
			if !ok {
				i18n.HTTPError(w, req, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
				return
			}
			stripAmbientCredentials(req)
			path := ds.embedSurfacePath(g)
			if path == "" {
				// The surface vanished after the grant was minted. Answer the
				// empty catalog rather than the whole one.
				path = "\x00none"
			}
			// The catalog unescapes ?page= once, as the runtime sends it
			// (encodeURIComponent of an escaped pathname); a surface path is
			// a decoded route, so escape it to the same shape.
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

	// Install the predicate widget.Definition.RequireSession consults.
	// Without this the gate has no way to say yes, so a widget that
	// asked for a session would 403 forever. The gate fails closed by
	// design, and this is what makes it usable. Same signature check as
	// requireValidSession, minus the response write.
	//
	// An embed grant satisfies it too, and has to: the catalog above already
	// lists RequireSession widgets to a frame, so without this the frame
	// fetched the widget's chrome with the only credential it has and got 403,
	// dead DOM one hop further along than the bug the catalog branch fixed.
	widget.SetSessionCheck(func(req *http.Request) bool {
		if token := req.Header.Get(embedGrantHeader); token != "" {
			// Same rule as the catalog: a presented grant decides this request,
			// and a refused one does not fall back to an ambient cookie.
			g, ok := ds.embedGrant(req)
			if !ok {
				return false
			}
			// A grant satisfies the gate only for widgets that actually belong
			// to its own surface.
			//
			// Scoping the CATALOG was not enough: /core-ui/widget/<name>/state
			// and /chrome are predictable URLs, so filtering discovery only
			// hides names from a caller who does not need them. Reducing the
			// grant to a boolean here let anyone with devtools on a legitimate
			// customer page read the state of a widget scoped to /admin.
			//
			// The widget comes from the gate that is asking, not the URL: a
			// name parsed from /core-ui/widget/<name>/ left every RPC (and
			// every custom or group-prefixed path) with no widget, so a
			// grant was refused on routes Reach admits.
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

	// The stricter predicate Definition.RequireAuthenticated consults.
	// Where the any-session check above accepts the session handlePage
	// mints for every visitor, this one is satisfied only when the
	// request resolves to an authenticated principal: the user the app's
	// session middleware (battery/auth SessionMiddleware or RequireAuth,
	// installed app-wide via fwApp.Use) loaded onto the request context.
	// The anonymous auto-minted session never produces one, so a
	// RequireAuthenticated widget fails closed for anonymous visitors —
	// which is the level's entire reason to exist ("not safe to expose
	// anonymously" must not be satisfied by anyone who loaded a page
	// once). Apps that wire no session middleware get a gate that can
	// only say no: fail closed, same contract as the widget-level
	// default.
	//
	// An embed grant never satisfies it. A grant is delegated, scoped
	// authority sitting in a third party's page; the same reasoning that
	// makes battery/auth's RequireRole refuse a grant (the subject's
	// roles are the subject's, not the frame's) makes an authentication
	// gate refuse one: the viewer did not sign in, the embed author
	// vouched. Grant-bearing requests are refused outright rather than
	// falling through to an ambient cookie, mirroring the catalog and
	// any-session rules above.
	//
	// A scoped API token is refused for the same reason, as
	// battery/auth's RequireSession refuses it: its owner is the user,
	// but a leaked token must not escape its scopes through a gate that
	// asks only whether someone signed in. Held scopes mark both a
	// token and a resolved grant.
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

	// Split runtime modules: /__gofastr/runtime/<name>.js. core.js's
	// loader fetches them on demand (hover prefetch, idle, or click
	// await). Delegated to core-ui/widget so back-compat for hosts that
	// already call widget.MountRuntime keeps a single source of truth.
	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(widget.ServeRuntimeModule))
	// Registered worker + WebAssembly assets: content-addressed and
	// same-origin so the default CSP can load them without blob: URLs.
	r.Get("/__gofastr/compute/{name}", http.HandlerFunc(widget.ServeComputeAsset))

	// Page-level LLM documentation endpoints.
	// - /llm-pages.md: top-level index of all screens
	// - /{screen-path}/llm.md: per-screen documentation
	// Disabled by default. Apps opt in via [WithPublicLLMMD].
	if ds.App != nil && ds.llmMDPublic {
		ds.mountPageLLMMD(r)
	}

	// SEO endpoints: only mounted when WithSitemap / WithRobots
	// were passed, so apps that don't opt in don't accidentally
	// expose either endpoint.
	if ds.sitemapConfig != nil {
		r.Get("/sitemap.xml", http.HandlerFunc(ds.handleSitemap))
		r.Get("/sitemap/{part}", http.HandlerFunc(ds.handleSitemapPart))
	}
	if ds.robotsConfig != nil {
		r.Get("/robots.txt", http.HandlerFunc(ds.handleRobots))
	}
	// Generated app icons (WithAppIcon), including the /favicon.ico
	// alias, which takes precedence over serveOrRender's 204 fallback.
	ds.mountAppIcons(func(path string, h http.HandlerFunc) {
		r.Get(path, h)
	})
	// Installable-PWA surface: only mounted via WithPWA. The manifest
	// and worker live at the root so the worker's default scope covers
	// the whole app.
	if ds.pwaConfig != nil {
		r.Get("/manifest.webmanifest", http.HandlerFunc(ds.handlePWAManifest))
		r.Get("/service-worker.js", http.HandlerFunc(ds.handlePWAServiceWorker))
		r.Get("/__gofastr/pwa/register.js", http.HandlerFunc(ds.handlePWARegisterJS))
		r.Get("/__gofastr/pwa/offline", http.HandlerFunc(ds.handlePWAOffline))
	}
	// Agent-discovery surface (/llms.txt, /.well-known/agent-card.json,
	// legacy /.well-known/agent.json). Opt-in via WithAgentReady or the
	// granular WithLLMsTxt / WithAgentCard options.
	ds.mountAgentReady(r)

	r.NotFound(http.HandlerFunc(ds.serveOrRender))
	// MethodNotAllowed catch-all: restores fall-through so a screen at
	// path /x still renders (200) when a non-GET route like POST /x is
	// registered. Without this, the bare 405 from the router's method
	// mismatch path shadows the screen. For genuinely unsupported
	// methods on non-screen paths, serveMethodNotAllowed renders a
	// styled 405 page preserving the Allow header.
	r.MethodNotAllowed(http.HandlerFunc(ds.serveMethodNotAllowed))
}

// RoutePatterns returns the concrete screen paths the host will serve.
// framework.App uses this optional preflight surface to diagnose an entity
// CRUD/screen collision before Mount reaches net/http's opaque duplicate-route
// panic. Infrastructure and catch-all routes are intentionally excluded.
func (ds *UIHost) RoutePatterns() []string {
	routes := ds.App.Routes()
	patterns := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.RedirectTo != "" {
			continue // redirects are not screens; exclude from entity-collision checks
		}
		patterns = append(patterns, route.Path)
	}
	return patterns
}

// writeBlock answers a DecisionBlock: its reason, or the status's
// generic phrase, in the request's locale.
func writeBlock(w http.ResponseWriter, r *http.Request, res app.RenderResult) {
	reason := res.Reason
	if reason.IsZero() {
		reason = i18n.StatusPhrase(res.Status)
	}
	i18n.HTTPError(w, r, res.Status, reason, res.Params)
}

// mountPageLLMMD registers LLM-friendly documentation routes for every
// screen in the app. Two route types are added:
//   - GET /llm-pages.md: top-level index listing all screens
//   - GET /{screen-path}/llm.md: per-screen markdown documentation
//
// An exact route's handler serves the per-URL doc (pageLLMMD); a
// dynamic route's handler sits at its pattern text and serves the
// pattern doc, while its concrete URLs reach dynamicPageLLMMD.
func (ds *UIHost) mountPageLLMMD(r *router.Router) {
	coreApp := ds.App

	// Global opt-out
	if coreApp.NoLLMMD {
		return
	}

	// Top-level page index
	r.Get("/llm-pages.md", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		setPageMarkdownHeaders(w)
		// Request-scoped: policy-gated screens list metadata-free for
		// anonymous agents; an authenticated request sees the full index.
		w.Write([]byte(app.AppLLMMDCtx(app.WithRequest(req.Context(), req), coreApp)))
	}))

	// Per-screen documentation
	seen := make(map[string]bool)
	for _, routePath := range coreApp.Router.Paths() {
		screen, ok := coreApp.Router.ScreenByPattern(routePath)
		if !ok {
			continue
		}
		// Per-screen opt-out
		if screen.NoLLMMD {
			continue
		}
		// Capture for closure
		sc := screen
		base := routePath
		_, exact := coreApp.Router.Screens()[routePath]
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// An exact route's URL is its pattern: it gets the per-URL doc
			// the static export writes, and answers 404 where its page
			// does (a resolver that names no row, an outlet nothing fills).
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
			setPageMarkdownHeaders(w)
			// A screen whose policy phase doesn't Allow this request gets
			// the metadata-free withheld doc: the pattern doc's title,
			// description, SEO front matter, AND content are all
			// component-supplied, which the policy protects on the page
			// render too. PatternPolicy runs the region guards as well; a
			// dynamic pattern carrying one is withheld, since a guard
			// decides per URL and this doc names none. No front matter on
			// the withheld path.
			var md string
			ctx := app.WithRequest(req.Context(), req)
			if coreApp.PatternPolicy(ctx, sc).Kind != app.DecisionAllow {
				md = app.ScreenLLMMDWithheld(sc)
			} else {
				// Under /es/ the canonical, hreflangs and links name the
				// Spanish pages, as the per-URL doc's do.
				seo := ds.localizeSEO(ctx, base, resolveScreenSEO(ctx, sc))
				fm := screenSEOFrontMatter(sc.Title, seo)
				md = app.ScreenLLMMDWithMeta(ctx, sc, fm)
			}
			w.Write([]byte(md))
		})
		// Clean trailing slash to avoid double-slash patterns
		// (e.g. "/docs/" + "/llm.md" → "//llm.md" which panics).
		// For root "/" , clean becomes "", so the route is "/llm.md".
		// This is safe because the entity API index now lives at /api/llm.md.
		clean := strings.TrimRight(routePath, "/")
		route := clean + "/llm.md"
		if clean == "" {
			route = "/llm.md"
		}
		// A group index registered at both the slashless and trailing-slash
		// path (/studio and /studio/) collapses to the same llm.md route;
		// register it once, else the second r.Get panics on a dup pattern (#89).
		if seen[route] {
			continue
		}
		seen[route] = true
		r.Get(route, handler)
	}
}

// isScreenPath reports whether the request's path names a screen: the
// app router resolves it and no declared redirect claims it first
// (handlePage answers a redirect before it resolves a screen, so a
// redirect's path is not a screen's, whatever the method).
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

// screenAllow is the Allow header of a screen path: GET and HEAD, then
// each method a route registered at the same path serves (the router's
// own Allow, when it dispatched a method mismatch), each once.
func screenAllow(routed string) string {
	methods := []string{http.MethodGet, http.MethodHead}
	for m := range strings.SplitSeq(routed, ",") {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(methods, m) {
			methods = append(methods, m)
		}
	}
	return strings.Join(methods, ", ")
}

// serveOrRender is the catch-all NotFound handler. It first tries static
// file resolution (filesystem or embedded FS), and if no file matches it
// falls through to page rendering. Resolution steps are mirrored by
// resolvesStaticOrScreen (the MethodNotAllowed fall-through predicate).
// Keep the two in sync.
func (ds *UIHost) serveOrRender(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/favicon.ico" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Canonicalize missing trailing slash: when only "/foo/" is
	// registered and "/foo" arrives, 301 to the slash form. ServeMux
	// gives this for free for subtree patterns, but UIHost dispatches
	// inside handlePage via App.Router.Resolve, so we do it here.
	if rp := routePath(r); rp != "/" && !strings.HasSuffix(rp, "/") {
		if _, _, ok := ds.App.Router.Resolve(rp); !ok {
			if _, _, ok := ds.App.Router.Resolve(rp + "/"); ok {
				target := app.EscapeRoutePath(rp) + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				// The path and query are request input; scrubCtl keeps raw
				// C0/DEL out of the Location header (same contract as the
				// 308 pattern-redirect above).
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
				// A directory is no static file: it falls through to the
				// screens, as the static-dir branch above does. Handed to
				// http.ServeFileFS it answered a 301 to "docs/", whose
				// trailing slash fs.ValidPath refuses, so a screen sharing
				// the directory's name was a redirect into a 404.
				//gofastr:allow(rootread) caller-supplied fs.FS: embed.FS carries no symlinks and os.DirFS is re-rooted through os.OpenRoot at install time; the *os.File branch above already goes through os.Root
				if f, err := ds.staticFS.Open(cleanPath); err == nil && !fsEntryIsDir(f) {
					devStaticNoStore(w)
					// A versioned URL is served from the handle it was
					// hashed from; see markVersionedStatic.
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
	// WithFavicon configured a URL but no real file lives there: 204
	// rather than 404 so the browser's per-page favicon fetch doesn't
	// noisily fail in the dev console.
	if ds.faviconURL != "" && path == ds.faviconURL {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Concrete-URL llm.md for dynamic routes: static routes get explicit
	// /<path>/llm.md handlers at mount time (mountPageLLMMD), but a
	// dynamic route's concrete URLs (/docs/<slug>/llm.md,
	// /products/42/llm.md) can't be pre-registered, so they land here.
	// Serve the per-instance doc when the base path resolves to a screen.
	// Mirrored in resolvesStaticOrScreen. Keep the two in sync.
	if md, ok := ds.dynamicPageLLMMD(r, routePath(r)); ok {
		setPageMarkdownHeaders(w)
		w.Write([]byte(md))
		return
	}
	// A screen answers GET and HEAD. Any other method no route took
	// stops here, before handlePage runs the screen's policy and Load
	// or mints a session; a path that is no screen keeps its 404.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && ds.isScreenPath(r) {
		ds.serveScreenMethodNotAllowed(w, r)
		return
	}
	ds.handlePage(w, r)
}

// dynamicPageLLMMD serves the llm.md for a concrete URL of a dynamic
// route. Returns ok=false unless path ends in "/llm.md", llm.md is
// enabled, and the base path resolves to a screen that hasn't opted out.
// Static routes never reach it: their explicit mountPageLLMMD handlers
// match first, so this only fires for dynamic-route URLs (which fall
// through to the NotFound catch-all).
func (ds *UIHost) dynamicPageLLMMD(r *http.Request, path string) (string, bool) {
	// Same opt-in gate as mountPageLLMMD: per-screen docs are public
	// only via WithPublicLLMMD (see the security suite's
	// PageLLMScreenDocDisabledByDefault).
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

// pageLLMMD renders the llm.md for the concrete URL base of screen scr.
// ok is false when the page answers 404 or another status (the caller
// answers with the page's 404).
func (ds *UIHost) pageLLMMD(r *http.Request, base string, scr *app.Screen) (string, bool) {
	// Same context the page render gets: policies and Load hooks read the
	// live request (session, headers, query) through app.WithRequest. It
	// is the PAGE's request: the /llm.md URL rebased onto the page, so a
	// screen that builds links off the request path (entityui.List)
	// points them at the page, not at <page>/llm.md/<id>.
	// Built from base, the routing form, never by trimming the suffix
	// off the raw URL: base keeps a key's encoded slash and a page's
	// trailing slash however the /llm.md suffix was spelled.
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
	// A page that answers another status (app.SetStatus for a missing
	// record, a ScreenStatusCode) has no doc to serve: answer 404 the
	// way a Load's ErrNotFound does, never a 200 doc of its not-found body.
	if status := pageStatus(app.RenderResult{Component: res.Component}, recorded()); status != 0 && status != http.StatusOK {
		return "", false
	}
	return ds.LLMMDWithSEO(ctx, base, scr, res), true
}

// LLMMDWithSEO is the llm.md the host serves for res, the doc
// app.ScreenLLMMDForPath rendered for the concrete URL base of screen
// scr: its markdown behind the screen's SEO front matter. The static
// export writes it too, so an exported doc matches the served one. ctx
// is the context the doc rendered under.
func (ds *UIHost) LLMMDWithSEO(ctx context.Context, base string, scr *app.Screen, res app.ScreenLLMMDResult) string {
	md := res.MD
	// SEO front matter ONLY for an allowed render, resolved against the
	// LOADED instance (the registration instance is a zero-value template
	// for dynamic routes, and for a non-Allow decision, all
	// component-supplied metadata is policy-protected).
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

// PageHandler returns an http.HandlerFunc that renders the registered
// screen at path with full chrome (assets, runtime, session minting) —
// the same serving path NotFound-dispatched screens take. Hosts need it
// when a framework-router route must own the path explicitly: an app
// wildcard ({rest...}) claims the bare path and the mux redirects it to
// a trailing-slash form the screen dispatch would never resolve. The
// request's auth/policy context flows through unchanged.
func (ds *UIHost) PageHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p != path && path != "" && !isDynamicScreenPath(path) {
			// Keep query intact; only the dispatch path is forced. Dynamic
			// screen paths ({id} or :id) are NEVER forced: rewriting would
			// feed the literal pattern to param capture (id="{id}"); the
			// real request path already matches the registered dynamic
			// screen.
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

// isDynamicScreenPath reports whether path names a dynamic screen in
// either spelling the router accepts: a whole "{name}" segment, or a
// ":name" segment (the form the router stores in Screen.Path). A brace
// inside a literal segment, "/reports/rev{1}", is a static path.
func isDynamicScreenPath(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, ":") || (len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}') {
			return true
		}
	}
	return false
}

// RenderStaticPage produces a fully-rendered page suitable for static-site
// generation: it runs the screen's Load(ctx) hook, applies layout/theme,
// and injects runtime.js, compiled actions, custom CSS, and the route
// graph, but skips the SSE meta tag because there is no live session.
// The result is safe to write to disk and serve from any static host.
func (ds *UIHost) RenderStaticPage(ctx context.Context, path string) (string, error) {
	page, _, err := ds.RenderStaticPageResult(ctx, path)
	return page, err
}

// RenderStaticPageResult is RenderStaticPage with the contained fill
// failures of the render beside the page (,
// a fill that fails and is contained ( variant
// B) degrades just its outlet on a live server, but a static export
// must not bake the degraded bytes into the page — the builder reads
// this record and refuses. Same rendering semantics as
// RenderStaticPage.
func (ds *UIHost) RenderStaticPageResult(ctx context.Context, path string) (string, []app.FillFailure, error) {
	ctx = context.WithValue(ctx, staticExportKey{}, true)
	// The chrome renders with the same context a live request gets. A
	// layout component that reads the path from the request, such as a
	// sidebar for the section being read or a header whose language follows
	// the page, rendered for no page at all in an export, so every exported
	// page carried the chrome of the site root. An SSG build passes no
	// request, so one is made for the path, and the route match is
	// installed the way handlePage does it.
	if app.RequestFromContext(ctx) == nil {
		if req, err := pageRequest(ctx, path); err == nil {
			ctx = app.WithRequest(ctx, req)
		}
	}
	ctx = withRouteMatch(ctx, ds.App.Router, path)
	// Install the value bag so producer-seeded slice values are captured
	// during the static render (matches the live handlePage path).
	ctx = store.WithValues(ctx)
	ctx, recorded := app.TrackStatus(ctx)
	// RenderPageResult (not RenderPage) so the loaded per-request
	// component reaches the chrome injection: a dynamic route's meta
	// description only exists on the loaded instance.
	res, err := ds.App.RenderPageResult(ctx, path)
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
	// The 404-outlet outcome: the route's own answer is the not-found
	// page, so there is no page to bake in. The builder skips the route
	// with a warning (NotFoundOutletError) the way it skips a gated or
	// unexpandable one.
	if res.NotFoundOutlet {
		return "", nil, &NotFoundOutletError{Path: path}
	}
	// A page that answers another status (app.SetStatus, a
	// ScreenStatusCode) is skipped the same way: a static host serves
	// every file 200, so a missing record's page would read as present.
	if status := pageStatus(res, recorded()); status != 0 && status != http.StatusOK {
		return "", nil, &StatusPageError{Path: path, Status: status}
	}
	// bundle=false: static hosts don't serve query-paramed files, so
	// emit one <link rel=stylesheet> per registered component instead
	// of the comp-bundle.css?names= form.
	page := ds.injectChromeModeFor(withRenderAlt(ctx, res), string(res.HTML), path, "", "", false, res.Component)
	return injectHeadData(ctx, page), res.FillFailures, nil
}

// RenderStaticNotFoundPage renders the not-found page as a full static
// document — the same page serveNotFound's HTML arm serves live (the
// root layout's shell with its outlets' defaults, WithNotFoundScreen's
// body or the built-in one) with static-mode chrome and no session —
// for a static export's 404.html. ok=false when the app declares no
// root layout or the render fails; the builder then emits no 404.html.
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
	ctx = store.WithValues(ctx)
	// The body gets no path: the file answers every missing URL, and a
	// custom NotFoundRenderer sees "" for the same reason.
	res, err := ds.App.RenderNotFoundPageResult(ctx, path, ds.notFoundBodyComponent(""))
	if err != nil {
		slog.Warn("uihost: static not-found render failed; exporting no 404.html", "err", err)
		return "", false
	}
	page := ds.injectChromeModeFor(ctx, string(res.HTML), path, "", "", false, res.Component)
	return injectHeadData(ctx, page), true
}
