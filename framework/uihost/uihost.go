// Package uihost wires a core-ui application onto a framework.App's router.
// It mounts page rendering, runtime.js, compiled action JS, SSE island
// streaming, sessions, and signal-driven updates as routes — there is no
// standalone server. The framework.App owns the HTTP listener.
package uihost

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// OG holds Open Graph meta tag values for social sharing.
// Zero-value fields are omitted from output.
type OG struct {
	Title       string
	Description string
	Image       string
	URL         string
	Type        string
}

// TwitterCard holds Twitter Card meta tag values.
// Zero-value fields are omitted from output.
type TwitterCard struct {
	Card        string // e.g. "summary", "summary_large_image"
	Title       string
	Description string
	Image       string
	Site        string // @username
}

// UIHost mounts a core-ui application onto a router. It serves rendered
// pages with runtime.js, compiled action JS, SSE streaming for islands,
// sessions, and signal-driven live updates. The framework.App is
// responsible for ListenAndServe.
type UIHost struct {
	App            *app.App
	Islands        *island.Manager
	mu             sync.RWMutex
	sessions       map[string]*Session                  // sessionID → session
	actionJS       map[string]string                    // componentID → compiled JS
	actionHandlers map[string]*component.ActionRegistry // componentID → action registry for server-side handlers
	customCSS      string                               // extra CSS to inject (e.g. demo.css)
	extraScripts   []string                             // extra <script src="…"> URLs to inject before </body>
	staticDir      string                               // directory to serve static files from
	staticFS       fs.FS                                // embedded filesystem for static files
	routeGraph     *style.RouteGraph                    // route graph for progressive CSS loading
	signals        map[string]SignalAny                 // signalID → signal for live updates
	headHTML       string                               // raw HTML to inject into <head> (escape hatch)
	headTags       []string                             // typed head tags built from convenience options

	// standalone is a private router lazily mounted on first ServeHTTP call,
	// so the host can satisfy http.Handler when it is used outside a
	// framework.App. Tests use this path; production goes through Mount.
	standalone     *router.Router
	standaloneOnce sync.Once
}

// Session represents a connected browser session.
type Session struct {
	ID      string
	Created time.Time
}

// SignalAny is an interface to allow storing heterogeneous signals.
type SignalAny interface {
	GetAsInterface() interface{}
	UpdateAsInterface(v interface{})
	Subscribe() <-chan struct{}
}

// SEOScreen is an optional interface that screens can implement to
// inject per-screen HTML into <head>. This enables per-page SEO:
// Open Graph tags, description meta, structured data, etc. The
// returned HTML is injected alongside any global head tags from
// WithHeadHTML / WithFavicon / etc.
//
// WARNING: the returned HTML is injected verbatim. Implementers must
// escape any dynamic content (e.g. html.EscapeString for user-supplied
// titles or descriptions) to prevent XSS.
type SEOScreen interface {
	HeadHTML() string
}

// routeInfoJSON is the JSON shape sent to the browser as __gofastr_routes.
type routeInfoJSON struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Preload     bool   `json:"preload"`
	CSSChunk    string `json:"cssChunk,omitempty"`
}

// Option configures a UIHost.
type Option func(*UIHost)

// WithCustomCSS adds extra CSS to inject into every page.
func WithCustomCSS(css string) Option {
	return func(ds *UIHost) {
		ds.customCSS = css
	}
}

// WithExtraScripts adds external <script src="…"> URLs to inject before
// </body> on every page. Use for dev-only tooling like livereload.
// CSP-safe — every URL becomes an external resource, no inline JS.
func WithExtraScripts(urls ...string) Option {
	return func(ds *UIHost) {
		ds.extraScripts = append(ds.extraScripts, urls...)
	}
}

// WithRouteGraph sets the route graph for progressive CSS loading.
func WithRouteGraph(rg *style.RouteGraph) Option {
	return func(ds *UIHost) {
		ds.routeGraph = rg
	}
}

// WithHeadHTML injects raw HTML into every page's <head>. This is the
// escape hatch for arbitrary head content. The HTML is injected verbatim
// — callers must ensure it is CSP-compatible (no inline <script> or
// <style> tags). For safe, auto-escaped alternatives, see WithFavicon,
// WithThemeColor, WithDescription, WithOpenGraph, WithTwitterCard,
// WithCanonicalURL, and WithPreconnect.
func WithHeadHTML(html string) Option {
	return func(ds *UIHost) {
		ds.headHTML = html
	}
}

// WithFavicon adds a <link rel="icon"> tag to <head>.
func WithFavicon(href string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="icon" href="%s">`, stdhtml.EscapeString(href)))
	}
}

// WithThemeColor adds a <meta name="theme-color"> tag to <head>.
func WithThemeColor(color string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="theme-color" content="%s">`, stdhtml.EscapeString(color)))
	}
}

// WithDescription adds a <meta name="description"> tag to <head>.
func WithDescription(desc string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="description" content="%s">`, stdhtml.EscapeString(desc)))
	}
}

// WithOpenGraph adds Open Graph <meta property="og:..."> tags to <head>.
// Zero-value fields are omitted.
func WithOpenGraph(og OG) Option {
	return func(ds *UIHost) {
		if og.Title != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:title" content="%s">`, stdhtml.EscapeString(og.Title)))
		}
		if og.Description != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:description" content="%s">`, stdhtml.EscapeString(og.Description)))
		}
		if og.Image != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:image" content="%s">`, stdhtml.EscapeString(og.Image)))
		}
		if og.URL != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:url" content="%s">`, stdhtml.EscapeString(og.URL)))
		}
		if og.Type != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:type" content="%s">`, stdhtml.EscapeString(og.Type)))
		}
	}
}

// WithTwitterCard adds Twitter Card <meta name="twitter:..."> tags to <head>.
// Zero-value fields are omitted.
func WithTwitterCard(tc TwitterCard) Option {
	return func(ds *UIHost) {
		if tc.Card != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:card" content="%s">`, stdhtml.EscapeString(tc.Card)))
		}
		if tc.Title != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:title" content="%s">`, stdhtml.EscapeString(tc.Title)))
		}
		if tc.Description != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:description" content="%s">`, stdhtml.EscapeString(tc.Description)))
		}
		if tc.Image != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:image" content="%s">`, stdhtml.EscapeString(tc.Image)))
		}
		if tc.Site != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:site" content="%s">`, stdhtml.EscapeString(tc.Site)))
		}
	}
}

// WithCanonicalURL adds a <link rel="canonical"> tag to <head>.
func WithCanonicalURL(url string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="canonical" href="%s">`, stdhtml.EscapeString(url)))
	}
}

// WithPreconnect adds <link rel="preconnect"> tags for the given origins.
// Use for early DNS/TCP/TLS connections to external resources (fonts, CDNs).
func WithPreconnect(origins ...string) Option {
	return func(ds *UIHost) {
		for _, o := range origins {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="preconnect" href="%s">`, stdhtml.EscapeString(o)))
		}
	}
}

// WithStaticDir sets the directory to serve static files from.
func WithStaticDir(dir string) Option {
	return func(ds *UIHost) {
		ds.staticDir = dir
	}
}

// StaticDir returns the configured static directory path.
func (ds *UIHost) StaticDir() string {
	return ds.staticDir
}

// SetStaticFS sets an embedded filesystem for serving static files.
func (ds *UIHost) SetStaticFS(fsys fs.FS) {
	ds.staticFS = fsys
}

// HasStaticFS reports whether an embedded static FS is configured.
func (ds *UIHost) HasStaticFS() bool {
	return ds.staticFS != nil
}

// StaticFS returns the configured embedded static FS, or nil if none.
func (ds *UIHost) StaticFS() fs.FS {
	return ds.staticFS
}

// CustomCSS returns the extra CSS string injected into every page.
func (ds *UIHost) CustomCSS() string {
	return ds.customCSS
}

// AppCSS returns the merged app-level stylesheet body: theme :root
// custom properties + every registered theme override
// (.fui-theme-<hash> blocks for ui.Themed wrappers) + customCSS,
// in that order. Used by the SSG so static export ships the same
// single asset the live server serves.
//
// The :root block is ALWAYS emitted, even when the host has no
// explicit App.Theme. Per-component CSS emits bare var(--*)
// references (no in-CSS fallbacks); without the :root floor every
// component would render with UA defaults. The DefaultTheme()
// floor guarantees var() resolution.
func (ds *UIHost) AppCSS() string {
	t := ds.activeTheme() // falls back to DefaultTheme() when App.Theme nil
	out := t.CSSCustomProperties() + "\n"
	// Framework-built-in helpers: visually-hidden for skip links, live
	// regions, etc. Inlined here (not via WithCustomCSS) so apps don't
	// have to opt in to have working accessibility primitives.
	out += frameworkBuiltinCSS
	if overrides := style.AllThemeOverridesCSS(); overrides != "" {
		out += overrides + "\n"
	}
	out += ds.customCSS
	return out
}

// frameworkBuiltinCSS ships with every app — minimal helpers the
// framework's own SSR output relies on (skip link, polite live
// region). Apps can override these classes; the framework just
// guarantees the defaults exist.
const frameworkBuiltinCSS = `
.fui-visually-hidden {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
/* Skip-link: visually hidden until focused, then revealed as a
   visible overlay so keyboard users can jump to #main-content.
   Apps can override via their own .skip-link / .skip-link:focus
   rules. */
.skip-link {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.skip-link:focus {
  position: fixed !important;
  top: 8px; left: 8px;
  width: auto; height: auto;
  padding: 8px 16px;
  margin: 0;
  overflow: visible;
  clip: auto;
  white-space: normal;
  z-index: 9999;
  background: #18181B;
  color: #FAFAFA;
  border-radius: 4px;
  font: 0.9rem system-ui, -apple-system, sans-serif;
  text-decoration: none;
}
/* SPA-nav failure toast — shown when loadPage can't fetch the new
   page (offline, server error). Positioned bottom-right; auto-hides
   after 4s via the runtime. Strict-CSP-clean (no inline styles). */
.fui-nav-toast {
  position: fixed;
  right: 16px; bottom: 16px;
  z-index: 9999;
  max-width: calc(100vw - 32px);
  padding: 12px 16px;
  background: #18181B;
  color: #FAFAFA;
  border-radius: 8px;
  font: 0.9rem system-ui, -apple-system, sans-serif;
  box-shadow: 0 10px 25px rgba(0,0,0,0.25);
  opacity: 0;
  transform: translateY(8px);
  transition: opacity 0.18s, transform 0.18s;
  pointer-events: none;
}
.fui-nav-toast.is-visible {
  opacity: 1;
  transform: translateY(0);
}
/* Progress indicator on slow SPA navigation. Apps can override the
   color via .fui-nav-busy { background: ... } */
html[aria-busy="true"] {
  cursor: progress;
}
html[aria-busy="true"]::after {
  content: '';
  position: fixed;
  inset: 0 0 auto 0;
  height: 2px;
  background: linear-gradient(90deg, transparent, currentColor 50%, transparent);
  animation: fui-nav-progress 1s linear infinite;
  z-index: 9999;
  pointer-events: none;
  color: #4F46E5;
}
@keyframes fui-nav-progress {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}
`

// ActiveTheme returns the configured theme or the default if unset.
// Exposed for tooling (e.g. the static-site builder) that needs to
// resolve theme tokens at build time.
func (ds *UIHost) ActiveTheme() style.Theme {
	return ds.activeTheme()
}

// ComponentCSSFiles returns one asset per registered component:
// urlPath ("/__gofastr/comp/<name>.css") and the scoped CSS body
// resolved under the active theme. Used by the static-site builder.
func (ds *UIHost) ComponentCSSFiles() map[string]string {
	all := registry.All()
	if len(all) == 0 {
		return nil
	}
	theme := ds.activeTheme()
	out := make(map[string]string, len(all))
	for _, e := range all {
		out["/__gofastr/comp/"+e.Name+".css"] = e.CSSFor(theme)
	}
	return out
}

// RegisterSignal registers a signal with the devserver so the signal update
// endpoint can apply client-sent values. Safe for concurrent use; the
// signal-update HTTP handler reads the same map.
func (ds *UIHost) RegisterSignal(id string, s SignalAny) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.signals == nil {
		ds.signals = make(map[string]SignalAny)
	}
	ds.signals[id] = s
}

// New creates a new development server.
func New(application *app.App, opts ...Option) *UIHost {
	ds := &UIHost{
		App:            application,
		Islands:        island.NewManager(),
		sessions:       make(map[string]*Session),
		actionJS:       make(map[string]string),
		actionHandlers: make(map[string]*component.ActionRegistry),
	}
	for _, opt := range opts {
		opt(ds)
	}
	return ds
}

// RegisterWidget registers a widget with the island manager for a session.
// Returns the widget wrapped as an island for rendering.
func (ds *UIHost) RegisterWidget(sessionID string, w *component.Widget) *island.Island {
	isl := island.NewIsland(fmt.Sprintf("%s-%s", w.ID, sessionID), w)
	isl.SessionID = sessionID
	ds.Islands.Register(isl)
	return isl
}

// CompileActions compiles a component's action methods to JS and caches them.
// It also stores the action registry so handleServerAction can invoke Go handlers.
func (ds *UIHost) CompileActions(componentID string, comp component.Component) string {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if js, ok := ds.actionJS[componentID]; ok {
		return js
	}

	// If interactive, generate action registration JS
	if ic, ok := comp.(component.InteractiveComponent); ok {
		actions := component.ExtractActions(ic)
		if actions != nil {
			js := actionsToJS(componentID, actions)
			ds.actionJS[componentID] = js
			ds.actionHandlers[componentID] = actions
			return js
		}
	}
	return ""
}

// AutoCompileActions scans all registered screens and compiles actions for
// any that implement InteractiveComponent. The component ID is derived from
// ScreenComponentID.ComponentID() if implemented, otherwise from the route path.
func (ds *UIHost) AutoCompileActions() {
	for _, route := range ds.App.Routes() {
		screen, _, ok := ds.App.Router.Resolve(route.Path)
		if !ok {
			continue
		}
		if _, ok := screen.Component.(component.InteractiveComponent); ok {
			var id string
			if cid, ok := screen.Component.(app.ScreenComponentID); ok {
				id = cid.ComponentID()
			} else {
				id = pathToActionID(route.Path)
			}
			ds.CompileActions(id, screen.Component)
		}
	}
}

// pathToActionID derives a component action ID from a route path.
// "/" → "home", "/products" → "products", "/products/:slug" → "products-detail"
func pathToActionID(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home"
	}
	// Replace / and : for valid JS identifiers
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name
}

// buildRouteScript auto-builds the __gofastr_routes script from registered screens.
// CSS chunk names are auto-derived from the screen path unless overridden via RouteGraph.
func (ds *UIHost) buildRouteScript() string {
	routes := ds.App.Routes()
	if len(routes) == 0 {
		return ""
	}
	infos := make([]routeInfoJSON, len(routes))
	for i, r := range routes {
		info := routeInfoJSON{
			Path:        r.Path,
			Title:       r.Title,
			Description: r.Description,
			Preload:     i == 0, // preload first route
			CSSChunk:    pathToChunkName(r.Path),
		}
		// Allow RouteGraph to override chunk name
		if ds.routeGraph != nil {
			if ri, ok := ds.routeGraph.Routes[r.Path]; ok && ri.CSSChunk != "" {
				info.CSSChunk = ri.CSSChunk
			}
		}
		infos[i] = info
	}
	rgJSON, _ := json.Marshal(infos)
	// JS body only — no <script> wrapper. The body is served as an
	// external file via /__gofastr/routes.js (CSP-safe under
	// default-src 'self'); injectChrome references it with
	// <script src="…">. SSG writes the same body to disk.
	return fmt.Sprintf("window.__gofastr_routes = %s;", string(rgJSON))
}

// pathToChunkName derives a CSS chunk filename from a route path.
// "/" → "home.css", "/about" → "about.css", "/products/:slug" → "products-slug.css"
func pathToChunkName(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home.css"
	}
	// Replace / and : with - for valid filenames
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name + ".css"
}

// GetActionJS returns all compiled action JS concatenated.
func (ds *UIHost) GetActionJS() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var sb strings.Builder
	for _, js := range ds.actionJS {
		sb.WriteString(js)
		sb.WriteString("\n")
	}
	return sb.String()
}

// CreateSession creates a new browser session. The ID is 16 bytes of
// crypto/rand encoded as hex — the prior `sess-<UnixNano()>` form
// could collide under load when two CreateSession calls landed in
// the same nanosecond.
func (ds *UIHost) CreateSession() *Session {
	id := "sess-" + newSessionID()
	sess := &Session{
		ID:      id,
		Created: time.Now(),
	}
	ds.mu.Lock()
	ds.sessions[id] = sess
	ds.mu.Unlock()
	return sess
}

func newSessionID() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// crypto/rand on supported platforms cannot fail; fall back
		// to the timestamp-based ID rather than panic on a wedged
		// kernel CSPRNG.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// GetSession retrieves a session by ID.
func (ds *UIHost) GetSession(id string) (*Session, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	s, ok := ds.sessions[id]
	return s, ok
}

// ServeHTTP makes UIHost satisfy http.Handler by routing through a private
// router that has Mount called on it. Production wiring goes through
// framework.App.Mount(host); ServeHTTP exists so the host can also be used
// standalone (tests, embedded experiments) without dragging in the full
// framework App.
func (ds *UIHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ds.standaloneOnce.Do(func() {
		ds.standalone = router.New()
		ds.Mount(ds.standalone)
	})
	ds.standalone.ServeHTTP(w, r)
}

// handlePage renders a full page with runtime.js, SSE meta tag, and compiled actions.
func (ds *UIHost) handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Client-side navigation: return just the screen content (no layout)
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		ds.handlePartialPage(w, r, path)
		return
	}

	// Make the live request available to ScreenLoader.Load(ctx) so
	// screens can read URL query params, headers, etc. SSG builds
	// pass nil and the helpers degrade to empty values.
	ctx := app.WithRequest(r.Context(), r)
	html, err := ds.App.RenderPage(ctx, path)
	if err != nil {
		ds.serveNotFound(w, path)
		return
	}

	// Get or create session
	sessionCookie, err := r.Cookie("gofastr-session")
	var sessionID string
	if err == nil {
		sessionID = sessionCookie.Value
	}
	if sessionID == "" {
		sess := ds.CreateSession()
		sessionID = sess.ID
		http.SetCookie(w, &http.Cookie{
			Name:     "gofastr-session",
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	page := ds.injectChrome(string(html), path, sessionID)

	// SSR-inline registered widgets — open ones whose deep-link
	// matches the request URL go in unhidden; hidden ones are
	// preloaded so the runtime can hydrate without a chrome fetch
	// when the user clicks. The widget chrome lives just inside
	// </body>; the runtime's _mountByName checks for an existing
	// root before fetching cfg.chromePath.
	page = injectWidgetSSR(page, r.URL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, page)
}

// injectWidgetSSR inlines ONLY the widgets the page actually wants
// open at first paint: deep-link matches or non-hidden auto-mount.
// Hidden click-to-open widgets are NOT inlined — the runtime
// fetches their chrome lazily from cfg.chromePath the first time
// the user clicks data-fui-open. That keeps page responses minimal
// (no payload for surfaces the user may never trigger) while
// preserving the SSR contract for surfaces that are visible on
// arrival (deep-linked modals, persistent panels, sidebars).
func injectWidgetSSR(page string, u *url.URL) string {
	var b strings.Builder
	q := u.Query()
	// Per-page filter — widgets scoped via .Pages / .PagesPrefix /
	// .PagesMatch only appear on paths they declared. Empty Routes
	// (the default) means the widget is global.
	for _, d := range widget.AvailableOn(u.Path) {
		// Decide: should this widget be open on this page load?
		var open bool
		switch {
		case d.DeepLinkKey != "" && d.DeepLinkValue != "":
			// Deep-link widget — open iff the URL says so.
			open = q.Get(d.DeepLinkKey) == d.DeepLinkValue
		case !d.Hidden:
			// Non-hidden auto-mount widget (toast stack, banner, panel).
			open = true
		default:
			// Hidden click-to-open widget — skip; runtime lazy-fetches.
			continue
		}
		if !open {
			continue
		}
		b.WriteString(widget.RenderChrome(d))
	}
	if b.Len() == 0 {
		return page
	}
	return strings.Replace(page, "</body>", b.String()+"</body>", 1)
}

// injectChrome adds links and scripts pointing at the host's served
// endpoints. pagePath is the route path used for SEOScreen resolution.
func (ds *UIHost) injectChrome(page, pagePath, sessionID string) string {
	return ds.injectChromeMode(page, pagePath, sessionID, true)
}

// screenHeadHTML returns per-screen head content from the SEOScreen
// interface. pagePath is the route path used to resolve the screen.
func (ds *UIHost) screenHeadHTML(pagePath string) string {
	if ds.App == nil || pagePath == "" {
		return ""
	}
	screen, _, ok := ds.App.Router.Resolve(pagePath)
	if !ok {
		return ""
	}
	if seo, ok := screen.Component.(SEOScreen); ok {
		return seo.HeadHTML()
	}
	return ""
}

// injectChromeMode is the underlying chrome injector. bundle=false
// suppresses the comp-bundle.css endpoint and emits one <link> per
// component instead — used by static export, since static hosts
// don't typically serve query-parameterized files. Live HTTP mode
// always passes bundle=true. pagePath is used for SEOScreen resolution.
func (ds *UIHost) injectChromeMode(page, pagePath, sessionID string, bundle bool) string {
	// <head>
	// Color-scheme bootstrap runs SYNCHRONOUSLY at the top of <head>
	// (before any CSS parses) so dark-mode tokens take effect during
	// the same first paint — no FOUC. Reads localStorage("gofastr.
	// colorScheme") + prefers-color-scheme media query, sets
	// <html data-color-scheme="dark|light">.
	page = strings.Replace(page,
		"<head>",
		`<head><script src="/__gofastr/color-scheme.js"></script>`, 1)
	if sessionID != "" {
		sseMeta := fmt.Sprintf(`<meta name="gofastr-sse" content="/__gofastr/sse?session=%s">`, sessionID)
		page = strings.Replace(page, "</head>", sseMeta+"\n</head>", 1)
	}
	// Single app-level CSS asset: theme :root vars + the host's
	// customCSS payload concatenated. Always emitted — the :root
	// floor is load-bearing for component CSS that uses bare
	// var(--*) refs. Legacy endpoints stay 410 GONE.
	if ds.App != nil {
		page = strings.Replace(page,
			"</head>",
			`<link rel="stylesheet" href="/__gofastr/app.css">`+"\n</head>", 1)
	}

	// Head injection: global typed tags + raw HTML + per-screen SEOScreen.
	// Order: typed helpers → WithHeadHTML → SEOScreen. Typed helpers are
	// set at New() time; SEOScreen is per-request.
	var headParts []string
	for _, tag := range ds.headTags {
		headParts = append(headParts, tag)
	}
	if ds.headHTML != "" {
		headParts = append(headParts, ds.headHTML)
	}
	if screenHead := ds.screenHeadHTML(pagePath); screenHead != "" {
		headParts = append(headParts, screenHead)
	}
	if len(headParts) > 0 {
		page = strings.Replace(page, "</head>", strings.Join(headParts, "\n")+"\n</head>", 1)
	}
	// Route graph + component catalog ship as inline JSON in
	// <script type="application/json"> blocks — the browser treats
	// these as inert data (NOT scripts) so they pass under strict
	// CSP (default-src 'self'). runtime.js reads + parses them on
	// boot. Saves two HTTP requests per page load vs separate
	// /__gofastr/{routes,catalog}.js files.
	if routes := routesJSONScript(ds); routes != "" {
		page = strings.Replace(page, "</head>", routes+"\n</head>", 1)
	}

	// Component CSS: scan the rendered page for data-fui-comp markers
	// and emit a single bundled <link> (or one direct <link> for a
	// single component) so first paint has every needed sheet in
	// <head>. LoadAlways entries are included whether the page used
	// them or not.
	if tags := ds.componentCSSTags(page, bundle); tags != "" {
		page = strings.Replace(page, "</head>", tags+"\n</head>", 1)
	}
	if catalog := catalogJSONScript(ds); catalog != "" {
		page = strings.Replace(page, "</head>", catalog+"\n</head>", 1)
	}
	// Runtime module manifest — name → ?v=<hash> for every split
	// module under core-ui/runtime/src/. The client-side loader reads
	// this on boot to cache-bust per-module URLs.
	if manifest := runtimeModuleManifestScript(); manifest != "" {
		page = strings.Replace(page, "</head>", manifest+"\n</head>", 1)
	}

	// <body>
	page = strings.Replace(page,
		"</body>",
		`<script src="/__gofastr/runtime.js"></script>`+"\n</body>", 1)
	if ds.GetActionJS() != "" {
		page = strings.Replace(page,
			"</body>",
			`<script src="/__gofastr/actions.js"></script>`+"\n</body>", 1)
	}
	for _, src := range ds.extraScripts {
		page = strings.Replace(page,
			"</body>",
			fmt.Sprintf(`<script src=%q></script>`, src)+"\n</body>", 1)
	}

	return page
}

// handleAppCSS serves the app-level CSS asset: theme :root custom
// properties + registered .fui-theme-<hash> override blocks +
// WithCustomCSS payload concatenated. One request per page replaces
// the legacy theme.css + styles.css split.
func (ds *UIHost) handleAppCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ds.AppCSS())
}

// handleThemeCSS / handleStylesCSS — retained as 410 GONE so any
// stale browser reference fails loudly instead of silently 404'ing.
// New code should reference /__gofastr/app.css instead.
func (ds *UIHost) handleThemeCSS(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "/__gofastr/theme.css was removed — use /__gofastr/app.css", http.StatusGone)
}
func (ds *UIHost) handleStylesCSS(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "/__gofastr/styles.css was removed — use /__gofastr/app.css", http.StatusGone)
}

// handleRoutesJS is retained as a 410 GONE so any stale browser
// reference to /__gofastr/routes.js surfaces clearly instead of
// silently 404'ing alongside other static assets. The route graph
// now ships inline as a <script type="application/json"> block
// inside the SSR'd page.
func (ds *UIHost) handleRoutesJS(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/__gofastr/routes.js was removed — routes ship inline as JSON in the page", http.StatusGone)
}

// serveNotFound writes a minimal HTML 404 with a real <title>. The
// previous http.Error path returned text/plain with no <title>, which
// left document.title="" on SPA-nav errors and bled into subsequent
// page navs.
func (ds *UIHost) serveNotFound(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	fmt.Fprintf(w,
		`<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>Not found — %s</title></head>`+
			`<body><main role="main"><h1>404 — Page not found</h1><p>No route matched <code>%s</code>.</p>`+
			`<p><a href="/">Back to home</a></p></main></body></html>`,
		stdhtml.EscapeString(appName), stdhtml.EscapeString(path))
}

// handlePartialPage returns just the screen content for client-side navigation.
// The runtime.js router swaps the <main> content without a full page reload.
func (ds *UIHost) handlePartialPage(w http.ResponseWriter, r *http.Request, path string) {
	// Mirror handlePage: expose the live *http.Request to ScreenLoader
	// via app.WithRequest so partial-fetched screens can still read URL
	// query (sort, page, filters) just like full-render screens do.
	ctx := app.WithRequest(r.Context(), r)
	html, err := ds.App.RenderPartial(ctx, path)
	if err != nil {
		ds.serveNotFound(w, path)
		return
	}

	// Look up screen title from route info
	if scr, _, ok := ds.App.Router.Resolve(path); ok && scr.Title != "" {
		title := scr.Title
		if title != "" {
			title = title + " — " + ds.App.Name
		}
		w.Header().Set("X-Gofastr-Title", title)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Gofastr-Partial", "true")
	fmt.Fprint(w, html)
}

// handleSSE streams island updates to the client.
func (ds *UIHost) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}

	// Use the island manager's SSE handler
	ds.Islands.ServeSSE(w, r)
}

// handleRuntimeJS serves the core-ui runtime JavaScript.
func (ds *UIHost) handleRuntimeJS(w http.ResponseWriter, r *http.Request) {
	js := runtime.MustRuntimeJS()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, js)
}

// handleColorSchemeJS serves the color-scheme bootstrap script — a
// tiny synchronous snippet that ships at the top of <head> so dark-
// mode CSS tokens take effect during first paint with no FOUC.
func (ds *UIHost) handleColorSchemeJS(w http.ResponseWriter, r *http.Request) {
	js, err := runtime.ColorSchemeJS()
	if err != nil {
		http.Error(w, "color-scheme bootstrap unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// Long cache — content rarely changes, file-hash query string would
	// be ideal but isn't critical for a 1KB script.
	w.Header().Set("Cache-Control", "public, max-age=300")
	fmt.Fprint(w, js)
}

// handleActionsJS serves all compiled action JS.
func (ds *UIHost) handleActionsJS(w http.ResponseWriter, r *http.Request) {
	js := ds.GetActionJS()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	fmt.Fprint(w, js)
}

// handleCreateSession creates a new session and returns its ID.
func (ds *UIHost) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sess := ds.CreateSession()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sess.ID})
}

// handleSignalUpdate receives a signal update from the client and pushes
// island updates via SSE.
func (ds *UIHost) handleSignalUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse: /__gofastr/signal/{signalID}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/__gofastr/signal/"), "/")
	if len(parts) == 0 {
		http.Error(w, "invalid signal path", http.StatusBadRequest)
		return
	}
	signalID := parts[0]

	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		// Try cookie
		if cookie, err := r.Cookie("gofastr-session"); err == nil {
			sessionID = cookie.Value
		}
	}

	// Apply the signal update once. The previous in-loop version
	// applied the update N times (once per subscribing island) and
	// also read ds.signals without holding ds.mu while RegisterSignal
	// writes it — a real data race under concurrent registration.
	ds.mu.RLock()
	sig, sigOK := ds.signals[signalID]
	ds.mu.RUnlock()
	if sigOK {
		if val, exists := body["value"]; exists {
			sig.UpdateAsInterface(val)
		}
	}

	// Push island updates for this session.
	islandIDs := ds.Islands.ListBySession(sessionID)
	for _, id := range islandIDs {
		isl, ok := ds.Islands.Get(id)
		if !ok {
			continue
		}
		html := isl.Update()
		ds.Islands.PushUpdate(island.IslandUpdate{
			IslandID: id,
			HTML:     string(html),
		}, sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleServerAction receives a server action invocation from the client.
// The client POSTs the action name, component ID, and parameters;
// the server looks up the registered Go handler, invokes it, and
// responds with a JSON result.
func (ds *UIHost) handleServerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Action      string            `json:"action"`
		Params      map[string]string `json:"params"`
		Session     string            `json:"session"`
		ComponentID string            `json:"componentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	sessionID := body.Session
	if sessionID == "" {
		if cookie, err := r.Cookie("gofastr-session"); err == nil {
			sessionID = cookie.Value
		}
	}

	componentID := body.ComponentID
	actionName := body.Action

	// Look up the action registry for this component
	ds.mu.RLock()
	reg, ok := ds.actionHandlers[componentID]
	ds.mu.RUnlock()

	if !ok || reg == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  fmt.Sprintf("no action registry for component %q", componentID),
		})
		return
	}

	actionDef, found := reg.Get(actionName)
	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  fmt.Sprintf("no action %q registered for component %q", actionName, componentID),
		})
		return
	}

	// Invoke the Go handler if one exists
	if actionDef.Handler != nil {
		ctx := component.NewComponentContext(actionName, "", body.Params)
		actionDef.Handler(ctx)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"action":  actionName,
			"message": "Server action processed",
		})
		return
	}

	// No Go handler — just acknowledge
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"action":  actionName,
		"message": "Server action acknowledged (no handler)",
	})
}

// handleWidgetJS serves compiled JavaScript for a specific widget.
// This enables lazy hydration: widgets load their behavior JS only on first interaction.
func (ds *UIHost) handleWidgetJS(w http.ResponseWriter, r *http.Request) {
	widgetID := strings.TrimPrefix(r.URL.Path, "/__gofastr/widget/")
	widgetID = strings.TrimSuffix(widgetID, ".js")

	ds.mu.RLock()
	js, ok := ds.actionJS[widgetID]
	ds.mu.RUnlock()

	if !ok {
		http.Error(w, "widget not found: "+widgetID, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, js)
}

// PushIsland re-renders an island and pushes the update via SSE.
func (ds *UIHost) PushIsland(islandID string) error {
	return ds.Islands.Push(islandID)
}

// Mount registers the UI's HTTP handlers on the given router.
//
// It registers:
//   - All `/__gofastr/*` infrastructure endpoints (runtime.js, actions.js,
//     SSE, session, signal updates, server actions, widget JS, CSS chunks)
//   - A NotFound handler that first attempts static-file resolution (from
//     either staticDir or staticFS) and falls back to page rendering.
//
// Mount must be called after the framework.App has registered its other
// routes (entity CRUD, custom endpoints) so the page handler only takes
// requests that nothing else claimed.
func (ds *UIHost) Mount(r *router.Router) {
	ds.AutoCompileActions()

	r.Get("/__gofastr/runtime.js", http.HandlerFunc(ds.handleRuntimeJS))
	r.Get("/__gofastr/color-scheme.js", http.HandlerFunc(ds.handleColorSchemeJS))
	r.Get("/__gofastr/actions.js", http.HandlerFunc(ds.handleActionsJS))
	r.Get("/__gofastr/app.css", http.HandlerFunc(ds.handleAppCSS))
	// Legacy endpoints — 410 GONE redirects.
	r.Get("/__gofastr/theme.css", http.HandlerFunc(ds.handleThemeCSS))
	r.Get("/__gofastr/styles.css", http.HandlerFunc(ds.handleStylesCSS))
	r.Get("/__gofastr/routes.js", http.HandlerFunc(ds.handleRoutesJS))
	r.Get("/__gofastr/sse", http.HandlerFunc(ds.handleSSE))
	r.Get("/__gofastr/session", http.HandlerFunc(ds.handleCreateSession))
	r.Post("/__gofastr/session", http.HandlerFunc(ds.handleCreateSession))
	r.Post("/__gofastr/signal/{id}", http.HandlerFunc(ds.handleSignalUpdate))
	r.Get("/__gofastr/signal/{id}", http.HandlerFunc(methodNotAllowed))
	r.Post("/__gofastr/action", http.HandlerFunc(ds.handleServerAction))
	r.Get("/__gofastr/action", http.HandlerFunc(methodNotAllowed))
	r.Get("/__gofastr/widget/{id}", http.HandlerFunc(ds.handleWidgetJS))
	r.Get("/__gofastr/css/{path...}", http.HandlerFunc(ds.handleCSSChunk))
	// Per-component scoped CSS + bundle endpoint for first paint.
	// See core-ui/registry and core-ui/ARCHITECTURE.md.
	r.Get("/__gofastr/comp/{path...}", http.HandlerFunc(ds.handleComponentCSS))
	r.Get("/__gofastr/comp-bundle.css", http.HandlerFunc(ds.handleCompBundleCSS))
	r.Get("/__gofastr/catalog.js", http.HandlerFunc(ds.handleCatalogJS))
	// runtime.js auto-discovers core-ui/widget widgets at
	// /__gofastr/widgets. Delegate to the widget registry so apps that
	// mount widgets (preset.Modal, ToastStack, …) are visible to the
	// runtime; the registry returns an empty list when nothing has
	// been registered, so plain framework apps still get the safe
	// "no widgets" response that prevents a 404 in the console.
	r.Get("/__gofastr/widgets", http.HandlerFunc(widget.ServeWidgetList))

	// Split runtime modules — /__gofastr/runtime/<name>.js. core.js's
	// loader fetches them on demand (hover prefetch, idle, or click
	// await). Delegated to core-ui/widget so back-compat for hosts that
	// already call widget.MountRuntime keeps a single source of truth.
	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(widget.ServeRuntimeModule))

	r.NotFound(http.HandlerFunc(ds.serveOrRender))
}

// methodNotAllowed is registered alongside POST-only endpoints so a wrong-
// method request gets a clear 405 instead of falling through to the UI
// page handler and getting a misleading 404.
func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "POST")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// serveOrRender is the catch-all NotFound handler. It first tries static
// file resolution (filesystem or embedded FS), and if no file matches it
// falls through to page rendering.
func (ds *UIHost) serveOrRender(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/favicon.ico" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if path != "/" {
		if ds.staticDir != "" {
			filePath := filepath.Join(ds.staticDir, filepath.Clean(path))
			absPath, _ := filepath.Abs(filePath)
			absStatic, _ := filepath.Abs(ds.staticDir)
			if strings.HasPrefix(absPath, absStatic+string(filepath.Separator)) || absPath == absStatic {
				if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
					http.ServeFile(w, r, filePath)
					return
				}
			}
		}
		if ds.staticFS != nil {
			cleanPath := strings.TrimPrefix(path, "/")
			if cleanPath != "" {
				if f, err := ds.staticFS.Open(cleanPath); err == nil {
					f.Close()
					http.ServeFileFS(w, r, ds.staticFS, cleanPath)
					return
				}
			}
		}
	}
	ds.handlePage(w, r)
}

// RenderStaticPage produces a fully-rendered page suitable for static-site
// generation: it runs the screen's Load(ctx) hook, applies layout/theme,
// and injects runtime.js, compiled actions, custom CSS, and the route
// graph — but skips the SSE meta tag because there is no live session.
// The result is safe to write to disk and serve from any static host.
func (ds *UIHost) RenderStaticPage(ctx context.Context, path string) (string, error) {
	html, err := ds.App.RenderPage(ctx, path)
	if err != nil {
		return "", err
	}
	// bundle=false: static hosts don't serve query-paramed files, so
	// emit one <link rel=stylesheet> per registered component instead
	// of the comp-bundle.css?names= form.
	return ds.injectChromeMode(string(html), path, "", false), nil
}

// actionsToJS converts an ActionRegistry to browser-runnable JavaScript
// using the ClientJS field from each ActionDef. Each action's ClientJS
// is wrapped in a handler function and registered with the runtime.
func actionsToJS(componentID string, reg *component.ActionRegistry) string {
	if !reg.HasActions() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// Component: %s\n", componentID))
	sb.WriteString("(() => {\n")
	sb.WriteString(fmt.Sprintf("  const id = %q;\n", componentID))
	sb.WriteString("  const G = window.__gofastr;\n")
	sb.WriteString("  const handlers = {\n")

	first := true
	for _, action := range reg.All() {
		if !first {
			sb.WriteString(",\n")
		}
		first = false

		body := strings.TrimSpace(action.ClientJS)
		if body == "" {
			body = fmt.Sprintf("// no client handler for %q", action.Event)
		}

		// Inject componentId into serverAction calls so the server can route
		// to the correct action handler.
		body = strings.ReplaceAll(body, "G.serverAction(", fmt.Sprintf("G._serverActionFor(%q, ", componentID))

		sb.WriteString(fmt.Sprintf("    %q: (params) => {\n      %s\n    }", action.Event, body))
	}

	sb.WriteString("\n  };\n")
	sb.WriteString("  G.register(id, handlers);\n")
	sb.WriteString("})();\n")

	return sb.String()
}

// PushUpdate pushes an island update for a specific session.
// This is a convenience method that wraps the island manager's push mechanism.
func (ds *UIHost) PushUpdate(islandID string, html string, sessionID string) {
	ds.Islands.PushUpdate(island.IslandUpdate{
		IslandID: islandID,
		HTML:     html,
	}, sessionID)
}

// componentCSSTags returns the <link> tags to inject into <head> for
// the components rendered on this page. It scans page for
// data-fui-comp markers, adds every LoadAlways entry, and emits one
// bundled link when ≥2 names are involved (single direct link
// otherwise). Inline emission is forbidden — the bundle endpoint is
// content-addressed so the browser caches it across pages with the
// same component set.
func (ds *UIHost) componentCSSTags(page string, bundle bool) string {
	used := registry.Scan(page)
	eager := registry.EagerNames()
	if len(used) == 0 && len(eager) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(used)+len(eager))
	for _, n := range used {
		seen[n] = struct{}{}
	}
	for _, n := range eager {
		seen[n] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	theme := ds.activeTheme()
	if len(names) == 1 || !bundle {
		// Static-export path also takes this branch — emit one <link>
		// per component to avoid the query-paramed bundle URL.
		var b strings.Builder
		for i, n := range names {
			e, ok := registry.Lookup(n)
			if !ok {
				continue
			}
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, `<link rel="stylesheet" href="/__gofastr/comp/%s.css?v=%s">`,
				n, e.VersionFor(theme))
		}
		return b.String()
	}
	// Bundle. The hash combines the per-component versions in the
	// sorted order embedded in `names`, so any change to any included
	// component busts the bundle URL.
	versions := make([]string, 0, len(names))
	for _, n := range names {
		e, ok := registry.Lookup(n)
		if !ok {
			continue
		}
		versions = append(versions, e.VersionFor(theme))
	}
	bundleV := hashStrings(versions...)
	joined := strings.Join(names, ",")
	return fmt.Sprintf(
		`<link rel="stylesheet" href="/__gofastr/comp-bundle.css?names=%s&v=%s" data-fui-bundle="%s">`,
		joined, bundleV, joined)
}

// catalogJSONScript returns the inline JSON block embedding the
// component catalog into the SSR'd page. The browser parses it as
// inert data because of type="application/json" — no JS execution,
// so strict CSP (default-src 'self') is happy.
//
// runtime.js reads it on boot:
//
//	const el = document.getElementById('gofastr-catalog');
//	if (el) window.__gofastr_catalog = JSON.parse(el.textContent);
func catalogJSONScript(ds *UIHost) string {
	all := registry.All()
	if len(all) == 0 {
		return ""
	}
	theme := ds.activeTheme()
	cat := make(map[string]map[string]any, len(all))
	for _, e := range all {
		cat[e.Name] = map[string]any{
			"stylePath": "/__gofastr/comp/" + e.Name + ".css",
			"version":   e.VersionFor(theme),
			"loadMode":  loadModeString(e.Load),
		}
	}
	buf, err := json.Marshal(cat)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-catalog">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// runtimeModuleManifestScript delegates to widget.RuntimeModuleManifestScript
// so the same JSON manifest ships from both framework/uihost-rendered pages
// and kiln-style hosts that consume widget.RuntimeTag() directly.
func runtimeModuleManifestScript() string {
	return widget.RuntimeModuleManifestScript()
}

// routesJSONScript embeds the route graph as inert JSON. Same model
// as catalogJSONScript. Returns "" when no routes are registered
// (e.g. a host used standalone in tests without a real app).
func routesJSONScript(ds *UIHost) string {
	body := strings.TrimSpace(ds.buildRouteScript())
	if body == "" {
		return ""
	}
	// buildRouteScript returns `window.__gofastr_routes = <JSON>;`.
	// Strip the wrapper to get just the JSON payload.
	body = strings.TrimPrefix(body, "window.__gofastr_routes =")
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, ";")
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return `<script type="application/json" id="gofastr-routes">` +
		escapeJSONForScript([]byte(body)) +
		`</script>`
}

// escapeJSONForScript escapes the one HTML sequence that can
// prematurely terminate an inline <script>…</script> block: the
// closing `</` characters. JSON itself never produces `</` (no
// language feature emits it), but URL strings in the payload might
// (e.g. a path like `/foo</bar` — exotic, but defending against it
// is cheap).
func escapeJSONForScript(buf []byte) string {
	return strings.ReplaceAll(string(buf), "</", `<\/`)
}

// validNameRe restricts component names accepted by the comp CSS
// endpoint to the same character class registry uses for marker
// values. Defense-in-depth: any path with /, .., null bytes, etc.
// fails before reaching Lookup.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_:.-]+$`)

// handleComponentCSS serves a single registered component's scoped
// stylesheet at /__gofastr/comp/{name}.css.
//
// Cache policy: the URL is content-addressed via ?v=<hash>. We only
// stamp the response as `immutable` when the supplied v matches the
// current Entry.VersionFor — otherwise a stale cached HTML
// referencing an old ?v= URL would receive fresh bytes back and
// the browser would cache the (old-URL, new-body) pair as immutable
// for a year. On mismatch we serve no-cache so the browser
// re-fetches next time.
func (ds *UIHost) handleComponentCSS(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/__gofastr/comp/")
	name = strings.TrimSuffix(name, ".css")
	if !validNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	e, ok := registry.Lookup(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	theme := ds.activeTheme()
	css := e.CSSFor(theme)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if v := r.URL.Query().Get("v"); v != "" && v == e.VersionFor(theme) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	fmt.Fprint(w, css)
}

// handleCompBundleCSS serves /__gofastr/comp-bundle.css?names=a,b,c
// — concatenates the named components' scoped CSS. Names come in
// sorted order (the host emits them sorted) so cache keys are
// stable across requests.
//
// Cache policy mirrors handleComponentCSS: `immutable` only when
// the supplied v matches the freshly-computed combined hash;
// otherwise no-cache to avoid pinning a stale URL to fresh bytes.
// Unknown names 404 — the contract is that the client requests
// names it learned from the SSR-emitted <link>, which by
// construction lists only registered entries.
func (ds *UIHost) handleCompBundleCSS(w http.ResponseWriter, r *http.Request) {
	namesParam := r.URL.Query().Get("names")
	if namesParam == "" {
		http.NotFound(w, r)
		return
	}
	names := strings.Split(namesParam, ",")
	theme := ds.activeTheme()
	versions := make([]string, 0, len(names))
	bodies := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if !validNameRe.MatchString(n) {
			http.NotFound(w, r)
			return
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		e, ok := registry.Lookup(n)
		if !ok {
			http.NotFound(w, r)
			return
		}
		versions = append(versions, e.VersionFor(theme))
		bodies = append(bodies, e.CSSFor(theme))
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	wantV := hashStrings(versions...)
	if v := r.URL.Query().Get("v"); v != "" && v == wantV {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	for _, body := range bodies {
		fmt.Fprint(w, body)
		fmt.Fprint(w, "\n")
	}
}

// handleCatalogJS is retained as a 410 GONE so any stale browser
// reference to /__gofastr/catalog.js surfaces clearly instead of
// silently 404'ing alongside other static assets. The catalog now
// ships inline as a <script type="application/json"> block inside
// the SSR'd page.
func (ds *UIHost) handleCatalogJS(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/__gofastr/catalog.js was removed — catalog ships inline as JSON in the page", http.StatusGone)
}

// activeTheme returns the configured theme or the default if unset.
func (ds *UIHost) activeTheme() style.Theme {
	if ds.App != nil && ds.App.Theme != nil {
		return *ds.App.Theme
	}
	return style.DefaultTheme()
}

func loadModeString(m registry.LoadMode) string {
	switch m {
	case registry.LoadAlways:
		return "always"
	case registry.LoadPrewarm:
		return "prewarm"
	default:
		return "auto"
	}
}

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:6])
}

// handleCSSChunk responds 410 GONE for any /__gofastr/css/<path>
// request. The per-screen CSS chunk system has been replaced by
// per-component scoped sheets at /__gofastr/comp/<name>.css. Apps
// declare CSS on the component via registry.RegisterStyle; the
// runtime loads it on demand from the SSR-emitted <link>. Returning
// 410 (instead of silently 404'ing) surfaces stale wiring so it gets
// fixed rather than masked.
func (ds *UIHost) handleCSSChunk(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "/__gofastr/css/<path> was removed — declare CSS per component via registry.RegisterStyle and the runtime will load /__gofastr/comp/<name>.css on demand", http.StatusGone)
}

// ReadCustomCSSFile reads a CSS file and returns its content.
// This is a helper for the demo main.go.
func ReadCustomCSSFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
