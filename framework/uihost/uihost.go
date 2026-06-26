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
	"errors"
	"fmt"
	stdhtml "html"
	"io/fs"
	"log/slog"
	"net"
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
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/dev"
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
	llmMDPublic    bool                                 // when true, mount per-screen /llm.md + /llm-pages.md; default disabled (schema disclosure)
	signals        map[string]SignalAny                 // signalID → signal for live updates
	headHTML       string                               // raw HTML to inject into <head> (escape hatch)
	headTags       []string                             // typed head tags built from convenience options
	faviconURL     string                               // configured WithFavicon URL — serveOrRender 204s it when no static file matches
	notFoundScreen component.Component                  // when set, serveNotFound renders this through the default layout instead of the bare 404 fallback
	sitemapConfig  *SitemapConfig                       // when set, /sitemap.xml lists every reachable route
	robotsConfig   *RobotsConfig                        // when set, /robots.txt is served from this config
	agentReady     *agentReadyConfig                    // when set, the agent-discovery surface (/llms.txt, agent card, Link headers, markdown negotiation) is served

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

// HreflangLink declares a locale → URL alternate for the current page.
// Emitted as <link rel="alternate" hreflang="…" href="…">.
type HreflangLink struct {
	Lang string // BCP-47 tag (e.g. "en", "en-US", "x-default")
	URL  string // canonical URL for that locale
}

// ScreenHreflangs is an optional screen interface that declares
// per-page hreflang alternates for multi-locale apps. When present,
// the host emits one <link rel="alternate"> per returned link.
type ScreenHreflangs interface {
	ScreenHreflangs() []HreflangLink
}

// ScreenCanonical is an optional screen interface that declares the
// canonical URL for the current page. Emitted as
// <link rel="canonical" href="…">. Use to prevent duplicate-content
// issues when a page is reachable at multiple URLs (filters, sorts).
type ScreenCanonical interface {
	ScreenCanonical() string
}

// ScreenSchema is an optional screen interface that returns one or
// more typed Schema.org items (from core-ui/seo) to emit as
// <script type="application/ld+json"> blocks.
//
// Implementations typically return:
//
//	[]seo.Thing{
//	    seo.NewArticle(),
//	    seo.NewBreadcrumbList(...),
//	}
type ScreenSchema interface {
	ScreenSchema() []seo.Thing
}

// SEO bundles every per-page SEO declaration in one struct. Use it as
// the return type of ScreenSEO when you'd rather declare everything
// from one method than implement the per-concern interfaces
// individually. Empty fields are silently skipped — only what's set
// is emitted.
type SEO struct {
	Description string         // <meta name="description">
	Canonical   string         // <link rel="canonical">
	Hreflangs   []HreflangLink // <link rel="alternate" hreflang>
	Robots      string         // <meta name="robots"> (e.g. "noindex,nofollow")
	OG          *OG            // Open Graph block
	Twitter     *TwitterCard   // Twitter Card block
	Schema      []seo.Thing    // JSON-LD items
}

// ScreenSEO is the bundle-style alternative to the per-concern
// interfaces. When a screen implements both ScreenSEO AND any of
// ScreenDescriber / ScreenCanonical / ScreenHreflangs / ScreenSchema,
// ScreenSEO wins — its fields override.
//
// Returning a zero-value SEO from ScreenSEO opts out of all per-page
// emission for the screen (useful for routes you want fully naked).
type ScreenSEO interface {
	ScreenSEO() SEO
}

// routeInfoJSON is the JSON shape sent to the browser as __gofastr_routes.
type routeInfoJSON struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Preload     bool   `json:"preload"`
	CSSChunk    string `json:"cssChunk,omitempty"`
	Layout      string `json:"layout,omitempty"`
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

// WithNotFoundScreen overrides the default bare 404 fallback. When a
// request misses every registered screen, static file, and configured
// favicon, the host renders this component through the active layout
// — so the 404 page sees the same nav/footer chrome every other page
// gets. The component's Render() result is wrapped in the default
// layout; pages without their own layout end up with the framework's
// bare <main>.
func WithNotFoundScreen(c component.Component) Option {
	return func(ds *UIHost) {
		ds.notFoundScreen = c
	}
}

// WithFavicon adds a <link rel="icon"> tag to <head>. The host also
// auto-serves 204 No Content at the configured URL when no static file
// matches, so a host that ships no favicon doesn't 404 on every page
// load. Place a real file at the path in staticDir / staticFS to override.
func WithFavicon(href string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="icon" href="%s">`, stdhtml.EscapeString(href)))
		ds.faviconURL = href
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
// Zero-value fields are omitted. URL-typed fields (Image, URL) are
// dropped if they fail the head-URL allow-list (http(s)/relative only)
// — a `javascript:`/`data:` URL there is reflected XSS via any social
// preview crawler that auto-clicks the link.
func WithOpenGraph(og OG) Option {
	return func(ds *UIHost) {
		if og.Title != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:title" content="%s">`, stdhtml.EscapeString(og.Title)))
		}
		if og.Description != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:description" content="%s">`, stdhtml.EscapeString(og.Description)))
		}
		if og.Image != "" && isSafeHeadURL(og.Image) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:image" content="%s">`, stdhtml.EscapeString(og.Image)))
		}
		if og.URL != "" && isSafeHeadURL(og.URL) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:url" content="%s">`, stdhtml.EscapeString(og.URL)))
		}
		if og.Type != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:type" content="%s">`, stdhtml.EscapeString(og.Type)))
		}
	}
}

// WithTwitterCard adds Twitter Card <meta name="twitter:..."> tags to <head>.
// Zero-value fields are omitted. URL-typed fields are scheme-restricted
// per [WithOpenGraph].
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
		if tc.Image != "" && isSafeHeadURL(tc.Image) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:image" content="%s">`, stdhtml.EscapeString(tc.Image)))
		}
		if tc.Site != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:site" content="%s">`, stdhtml.EscapeString(tc.Site)))
		}
	}
}

// WithCanonicalURL adds a <link rel="canonical"> tag to <head>. Unsafe
// URLs (non-http(s)/relative) are dropped.
func WithCanonicalURL(url string) Option {
	return func(ds *UIHost) {
		if !isSafeHeadURL(url) {
			return
		}
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

// WithPublicLLMMD opts the host into mounting the page-level LLM-friendly
// markdown routes (/llm-pages.md, /<screen>/llm.md). Disabled by default
// because the documents enumerate every screen and the data shape attached
// to it — useful for AI agents in trusted environments, schema disclosure
// elsewhere.
func WithPublicLLMMD() Option {
	return func(ds *UIHost) {
		ds.llmMDPublic = true
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
	// Structural CSS for the layout shells the app package emits (.layout-body,
	// the sidebar row, the WithContainer centered column). Owned by core-ui/app
	// next to its markup; injected once here so no app or generator ships it.
	out += app.LayoutBaseCSS() + "\n"
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
/* Border-box everywhere: padding/borders count toward declared widths, so
   padded full-width bars don't overflow the viewport. The single most common
   reset every app needs — shipping it here means no app re-declares it. */
*, *::before, *::after { box-sizing: border-box; }
/* Base surface + typography floor. The page picks up the theme background/text
   tokens (so it's readable on any OS canvas, light or dark) and the --font-body
   family. Headings use --font-heading (falls back to body). Without an explicit
   body font-family the UA default (Times/serif) renders, which reads as
   unstyled. Apps override via their own WithCustomCSS (appended after this
   block — last rule wins). */
html { background-color: var(--color-background, #fff); }
body {
  margin: 0;
  min-height: 100vh;
  background-color: var(--color-background, #fff);
  color: var(--color-text, #18181b);
  font-family: var(--font-body, 'Inter', system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif);
  line-height: 1.5;
  -webkit-text-size-adjust: 100%;
  -webkit-font-smoothing: antialiased;
}
h1, h2, h3, h4 { font-family: var(--font-heading, var(--font-body, inherit)); }
/* Columns of numbers (money, counts) align when figures are tabular. */
[data-fui-comp="ui-data-table"] td,
[data-fui-comp="ui-stat-card"],
[data-fui-comp="ui-bar-chart"],
[data-fui-comp="ui-detail-list"] .ui-detail-list__value,
.ui-money,
td[data-align="end"] {
  font-variant-numeric: tabular-nums;
  font-feature-settings: "tnum" 1, "lnum" 1;
}
/* The SPA runtime focuses the <main id="main-content"> landmark after a
   client-side nav so screen readers announce the new page. It's a
   programmatic (tabindex=-1) focus target, not a tabbable control, so the
   browser's default focus ring around the whole content region is noise. */
#main-content:focus, main[tabindex="-1"]:focus { outline: none; }
/* Visually-hidden helper — exposed under BOTH .fui-* (framework runtime
   uses this for the SPA route-announce region) and .ui-* (framework/ui
   components — CommandPalette's SR-only trigger, CopyButton's status
   region, etc.). Apps no longer have to call ui.BaseCSS() to opt in;
   the helper is part of the built-in auto-emitted app.css floor. Apps
   that want a custom visually-hidden recipe can override either class
   via their own WithCustomCSS — last rule wins. */
.fui-visually-hidden, .ui-visually-hidden {
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
	// Auto-inject the livereload client script when dev-mode env says so.
	// The matching SSE/JS routes are auto-registered by framework.NewApp
	// (see framework/dev/livereload.go). Both halves are gated by the
	// same env predicate, so the host needs zero code change.
	if dev.LiveReloadEnabled() {
		ds.extraScripts = append(ds.extraScripts, dev.LiveReloadScriptURL)
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
// CSS chunk names are auto-derived from the screen path.
func (ds *UIHost) buildRouteScript() string {
	routes := ds.App.Routes()
	if len(routes) == 0 {
		return ""
	}
	infos := make([]routeInfoJSON, len(routes))
	for i, r := range routes {
		infos[i] = routeInfoJSON{
			Path:        r.Path,
			Title:       r.Title,
			Description: r.Description,
			Preload:     i == 0, // preload first route
			CSSChunk:    pathToChunkName(r.Path),
			Layout:      r.Layout,
		}
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

	sb := borrowBuilder()
	defer returnBuilder(sb)
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

// sessionCookieSecureName is the cookie name used over a secure (TLS)
// origin. The __Host- prefix locks the cookie to a Secure, Path=/,
// Domain-less scope — the browser refuses to accept it unless those
// hold, which is the defense against subdomain/fixation attacks.
const sessionCookieSecureName = "__Host-gofastr-session"

// sessionCookieDevName is used on a plaintext loopback dev origin.
// Browsers won't store (and reject the __Host- prefix on) a Secure
// cookie sent over http://, so a dev server on http://localhost would
// never get the cookie back — every island RPC and the SSE stream would
// 401, and the console fills with reconnect errors. On loopback the
// connection is already trusted, so we drop Secure and the prefix there.
// Any non-loopback or TLS origin keeps the hardened __Host- form.
const sessionCookieDevName = "gofastr-session"

// requestIsSecure reports whether the request arrived over TLS, directly
// or terminated at a proxy that set X-Forwarded-Proto: https.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// requestIsLoopback reports whether the request targets a loopback host.
// Only loopback origins are eligible for the relaxed dev cookie.
func requestIsLoopback(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// useSecureSessionCookie decides whether this request should carry the
// hardened __Host- cookie: Secure everywhere EXCEPT a plaintext loopback
// dev origin, where a Secure cookie can't round-trip.
func useSecureSessionCookie(r *http.Request) bool {
	return requestIsSecure(r) || !requestIsLoopback(r)
}

// setSessionCookie writes the session cookie with the name + Secure flag
// appropriate to this request's origin.
func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	secure := useSecureSessionCookie(r)
	name := sessionCookieDevName
	if secure {
		name = sessionCookieSecureName
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// readSessionCookie returns the session id from the cookie that matches
// this request's security mode. An origin's mode is stable (loopback
// http is always dev, TLS/remote is always hardened), so reading only
// the mode-appropriate name avoids picking up a stale cross-mode cookie
// — e.g. a __Host- cookie left over from a prior https run won't shadow
// the dev cookie on a later http://localhost run.
func readSessionCookie(r *http.Request) string {
	name := sessionCookieDevName
	if useSecureSessionCookie(r) {
		name = sessionCookieSecureName
	}
	if c, err := r.Cookie(name); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
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
		// Standalone path doesn't go through framework.App's middleware
		// chain (which wires SecurityHeaders), so apply it here so tests
		// and embedded uses get the same baseline headers as production.
		ds.standalone.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}))
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
	// Agent content negotiation: when WithMarkdownNegotiation (or the
	// bundle) is on and the request prefers markdown, render the page's
	// markdown via the per-screen LLM doc instead of HTML.
	if ds.agentReady != nil && ds.agentReady.contentNeg && ds.llmMDPublic && acceptsMarkdown(r) {
		if ds.serveMarkdownForPage(w, r) {
			return
		}
		// No screen matched — fall through to the normal HTML path.
	}

	// Make the live request available to ScreenLoader.Load(ctx) so
	// screens can read URL query params, headers, etc. SSG builds
	// pass nil and the helpers degrade to empty values.
	ctx := app.WithRequest(r.Context(), r)
	// Install the per-request signal value bag BEFORE rendering so a
	// producer's Slice.Seed(ctx, v) during Load (and a consumer's
	// Bind(ctx, …) during RenderCtx) write/read the same bag that
	// injectSignalSeed resolves below.
	ctx = store.WithValues(ctx)
	res, err := ds.App.RenderPageResult(ctx, path)
	if err != nil {
		ds.serveNotFound(w, path)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		http.Redirect(w, r, res.URL, http.StatusSeeOther)
		return
	case app.DecisionBlock:
		msg := res.Message
		if msg == "" {
			msg = http.StatusText(res.Status)
		}
		http.Error(w, msg, res.Status)
		return
	}
	html := res.HTML

	// Get or create session. The cookie name + Secure flag depend on the
	// request origin (see setSessionCookie): a plaintext loopback dev
	// server can't round-trip a Secure cookie, so it gets the relaxed
	// form; everything else keeps the hardened __Host- cookie.
	//
	// A cookie whose session no longer exists server-side (sessions are
	// in-memory, so a restart/deploy wipes them) must be re-minted, not
	// reused — otherwise the embedded SSE id and every island RPC would
	// reference a dead session and 401 until the user manually cleared
	// the cookie.
	sessionID := readSessionCookie(r)
	if _, live := ds.GetSession(sessionID); sessionID == "" || !live {
		sess := ds.CreateSession()
		sessionID = sess.ID
		setSessionCookie(w, r, sessionID)
	}

	page := ds.injectChrome(string(html), path, sessionID)
	page = injectSignalSeed(ctx, page)

	// SSR-inline registered widgets — open ones whose deep-link
	// matches the request URL go in unhidden; hidden ones are
	// preloaded so the runtime can hydrate without a chrome fetch
	// when the user clicks. The widget chrome lives just inside
	// </body>; the runtime's _mountByName checks for an existing
	// root before fetching cfg.chromePath.
	page = injectWidgetSSR(page, r.URL)

	ds.writeAgentLinkHeaders(w, r)
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
	b := borrowBuilder()
	defer returnBuilder(b)
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
	return replaceChromeMarker(page, "</body>", b.String()+"</body>", "widget chrome")
}

// replaceChromeMarker replaces the first occurrence of marker in page, the way
// the chrome-injection sites need. Unlike a bare strings.Replace, it WARNS when
// the marker is absent instead of silently returning the page unchanged — a
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
func (ds *UIHost) injectChrome(page, pagePath, sessionID string) string {
	return ds.injectChromeMode(page, pagePath, sessionID, true)
}

// screenHeadHTML returns per-screen head content. It composes from
// two sources:
//
//   - ScreenDescriber: when the screen has a non-empty Description
//     (set automatically at Register time from the ScreenDescriber
//     interface), a `<meta name="description">` tag is emitted. Per-
//     page descriptions appear AFTER the global WithDescription tag,
//     so search engines pick the per-page text. No app code needs to
//     remember both the interface AND WithDescription().
//
//   - SEOScreen.HeadHTML(): the explicit escape hatch for screens
//     that need to declare canonical, OG, JSON-LD, etc. inline.
//
// pagePath is the route path used to resolve the screen.
func (ds *UIHost) screenHeadHTML(pagePath string) string {
	if ds.App == nil || pagePath == "" {
		return ""
	}
	screen, _, ok := ds.App.Router.Resolve(pagePath)
	if !ok {
		return ""
	}
	// ScreenSEO is the bundle-style override. When present, it takes
	// precedence over the per-concern interfaces for the fields it
	// declares. Empty fields fall through so a screen can use
	// ScreenSEO for some and per-concern interfaces for others.
	var bundle SEO
	if b, ok := screen.Component.(ScreenSEO); ok {
		bundle = b.ScreenSEO()
	}

	var parts []string
	// Description: bundle → ScreenDescriber (via screen.Description).
	desc := bundle.Description
	if desc == "" {
		desc = screen.Description
	}
	if desc != "" {
		parts = append(parts, fmt.Sprintf(
			`<meta name="description" content="%s">`,
			stdhtml.EscapeString(desc),
		))
	}
	// Robots: bundle only.
	if bundle.Robots != "" {
		parts = append(parts, fmt.Sprintf(
			`<meta name="robots" content="%s">`,
			stdhtml.EscapeString(bundle.Robots),
		))
	}
	// Canonical: bundle → ScreenCanonical.
	canonical := bundle.Canonical
	if canonical == "" {
		if c, ok := screen.Component.(ScreenCanonical); ok {
			canonical = c.ScreenCanonical()
		}
	}
	if canonical != "" && isSafeHeadURL(canonical) {
		parts = append(parts, fmt.Sprintf(
			`<link rel="canonical" href="%s">`,
			stdhtml.EscapeString(canonical),
		))
	}
	// Hreflangs: bundle → ScreenHreflangs.
	hreflangs := bundle.Hreflangs
	if len(hreflangs) == 0 {
		if h, ok := screen.Component.(ScreenHreflangs); ok {
			hreflangs = h.ScreenHreflangs()
		}
	}
	for _, link := range hreflangs {
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
	if bundle.OG != nil {
		parts = append(parts, ogTags(*bundle.OG)...)
	}
	if bundle.Twitter != nil {
		parts = append(parts, twitterTags(*bundle.Twitter)...)
	}
	// Schema: bundle → ScreenSchema.
	schema := bundle.Schema
	if len(schema) == 0 {
		if s, ok := screen.Component.(ScreenSchema); ok {
			schema = s.ScreenSchema()
		}
	}
	if len(schema) > 0 {
		parts = append(parts, string(seo.Render(schema...)))
	}
	// Catch-all per-screen HTML escape hatch. Caller-supplied — scrub
	// inline <script> tags before injection (XSS defense-in-depth).
	if seoScreen, ok := screen.Component.(SEOScreen); ok {
		if h := seoScreen.HeadHTML(); h != "" {
			parts = append(parts, stripInlineScripts(h))
		}
	}
	return strings.Join(parts, "\n")
}

// ogTags returns the per-page Open Graph meta tags for the given OG
// values. Mirrors the format WithOpenGraph emits sitewide.
func ogTags(og OG) []string {
	var out []string
	if og.Title != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:title" content="%s">`,
			stdhtml.EscapeString(og.Title)))
	}
	if og.Description != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:description" content="%s">`,
			stdhtml.EscapeString(og.Description)))
	}
	if og.Image != "" && isSafeHeadURL(og.Image) {
		out = append(out, fmt.Sprintf(`<meta property="og:image" content="%s">`,
			stdhtml.EscapeString(og.Image)))
	}
	if og.URL != "" && isSafeHeadURL(og.URL) {
		out = append(out, fmt.Sprintf(`<meta property="og:url" content="%s">`,
			stdhtml.EscapeString(og.URL)))
	}
	if og.Type != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:type" content="%s">`,
			stdhtml.EscapeString(og.Type)))
	}
	return out
}

// twitterTags returns the per-page Twitter Card meta tags.
func twitterTags(tc TwitterCard) []string {
	var out []string
	if tc.Card != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:card" content="%s">`,
			stdhtml.EscapeString(tc.Card)))
	}
	if tc.Title != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:title" content="%s">`,
			stdhtml.EscapeString(tc.Title)))
	}
	if tc.Description != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:description" content="%s">`,
			stdhtml.EscapeString(tc.Description)))
	}
	if tc.Image != "" && isSafeHeadURL(tc.Image) {
		out = append(out, fmt.Sprintf(`<meta name="twitter:image" content="%s">`,
			stdhtml.EscapeString(tc.Image)))
	}
	if tc.Site != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:site" content="%s">`,
			stdhtml.EscapeString(tc.Site)))
	}
	return out
}

// injectChromeMode is the underlying chrome injector. bundle=false
// suppresses the comp-bundle.css endpoint and emits one <link> per
// component instead — used by static export, since static hosts
// don't typically serve query-parameterized files. Live HTTP mode
// always passes bundle=true. pagePath is used for SEOScreen resolution.
func (ds *UIHost) injectChromeMode(page, pagePath, sessionID string, bundle bool) string {
	headClose := borrowBuilder()
	defer returnBuilder(headClose)
	bodyClose := borrowBuilder()
	defer returnBuilder(bodyClose)

	if sessionID != "" {
		fmt.Fprintf(headClose, `<meta name="gofastr-sse" content="/__gofastr/sse?session=%s">`+"\n", sessionID)
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
	// first-match — they stop at the first occurrence of a given property.
	// Putting per-page tags before global sitewide defaults ensures the
	// per-page og:title / og:description / og:image is the one they pick.
	// If the page provides no OG data, the global fallback still fires.
	//
	// Typed helpers (WithOpenGraph, WithDescription, …) are set at New()
	// time and serve as sitewide fallbacks. Per-screen SEOScreen data is
	// per-request.
	var headParts []string
	if screenHead := ds.screenHeadHTML(pagePath); screenHead != "" {
		headParts = append(headParts, screenHead)
	}
	// Caller-supplied head HTML (WithHeadHTML) is scrubbed for <script>
	// tags before injection. The head escape hatch is intended for
	// meta/link/style tags only; inline scripts would violate the
	// strict CSP and are a common XSS foot-gun. See stripInlineScripts.
	// SEOScreen.HeadHTML() is scrubbed inside screenHeadHTML so we
	// preserve the auto-generated JSON-LD <script type="application/
	// ld+json"> blocks emitted by seo.Render.
	if ds.headHTML != "" {
		headParts = append(headParts, stripInlineScripts(ds.headHTML))
	}
	for _, tag := range ds.headTags {
		headParts = append(headParts, tag)
	}
	if len(headParts) > 0 {
		headClose.WriteString(strings.Join(headParts, "\n"))
		headClose.WriteByte('\n')
	}
	// Route graph + component catalog ship as inline JSON in
	// <script type="application/json"> blocks — the browser treats
	// these as inert data (NOT scripts) so they pass under strict
	// CSP (default-src 'self'). runtime.js reads + parses them on
	// boot. Saves two HTTP requests per page load vs separate
	// /__gofastr/{routes,catalog}.js files.
	if routes := routesJSONScript(ds); routes != "" {
		headClose.WriteString(routes)
		headClose.WriteByte('\n')
	}

	// Component CSS: scan the rendered page for data-fui-comp markers
	// and emit a single bundled <link> (or one direct <link> for a
	// single component) so first paint has every needed sheet in
	// <head>. LoadAlways entries are included whether the page used
	// them or not.
	if tags := ds.componentCSSTags(page, bundle); tags != "" {
		headClose.WriteString(tags)
		headClose.WriteByte('\n')
	}
	// app.css comes AFTER the component CSS so it wins cascade ties
	// against framework defaults. Hosts that override e.g. a button's
	// padding or a header's drawer position can do so by writing to
	// the same selector — no specificity gymnastics needed.
	if ds.App != nil {
		headClose.WriteString(`<link rel="stylesheet" href="/__gofastr/app.css">`)
		headClose.WriteByte('\n')
	}
	if catalog := catalogJSONScript(ds); catalog != "" {
		headClose.WriteString(catalog)
		headClose.WriteByte('\n')
	}
	// Runtime module manifest — name → ?v=<hash> for every split
	// module under core-ui/runtime/src/. The client-side loader reads
	// this on boot to cache-bust per-module URLs.
	if manifest := runtimeModuleManifestScript(); manifest != "" {
		headClose.WriteString(manifest)
		headClose.WriteByte('\n')
	}
	// Module preload hints — emit <link rel="modulepreload"> per
	// demand-load runtime module whose marker substring appears in
	// the rendered page. Lets the browser parallel-fetch modules with
	// initial render instead of stalling on hover/click. Content-
	// addressed ?v=<hash> URLs match the immutable cache headers.
	if preloads := runtimeModulePreloadLinks(page); preloads != "" {
		headClose.WriteString(preloads)
		headClose.WriteByte('\n')
	}

	// <body>
	bodyClose.WriteString(`<script src="/__gofastr/runtime.js"></script>`)
	bodyClose.WriteByte('\n')
	if ds.GetActionJS() != "" {
		bodyClose.WriteString(`<script src="/__gofastr/actions.js"></script>`)
		bodyClose.WriteByte('\n')
	}
	for _, src := range ds.extraScripts {
		fmt.Fprintf(bodyClose, `<script src=%q></script>`+"\n", src)
	}

	// Color-scheme bootstrap runs SYNCHRONOUSLY at the top of <head>
	// (before any CSS parses) so dark-mode tokens take effect during
	// the same first paint — no FOUC. Reads localStorage("gofastr.
	// colorScheme") + prefers-color-scheme media query, sets
	// <html data-color-scheme="dark|light">.
	page = replaceChromeMarker(page,
		"<head>",
		`<head><script src="/__gofastr/color-scheme.js"></script>`, "color-scheme bootstrap")
	if headClose.Len() > 0 {
		page = replaceChromeMarker(page, "</head>", headClose.String()+"</head>", "head chrome (SEO/CSS)")
	}
	if bodyClose.Len() > 0 {
		page = replaceChromeMarker(page, "</body>", bodyClose.String()+"</body>", "body-close scripts")
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

// NotFoundRenderer is an optional interface a custom 404 screen (see
// WithNotFoundScreen) may implement to receive the unmatched request
// path. The path arrives as an argument, so a single shared screen
// instance can render per-request detail without a data race. Screens
// that don't implement it just render via component.Component.Render.
type NotFoundRenderer interface {
	RenderNotFound(path string) render.HTML
}

// serveNotFound writes a 404. When WithNotFoundScreen is set, the
// configured component renders through the same chrome (default
// layout, runtime.js, theme bootstrap) as every other page; otherwise
// the framework falls back to a minimal HTML body so something
// always renders.
func (ds *UIHost) serveNotFound(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if ds.notFoundScreen != nil && ds.App != nil {
		// If the screen implements NotFoundRenderer, hand it the unmatched
		// path so it can echo the real URL instead of a placeholder. The
		// path is passed as an argument (not stored on the shared screen
		// instance) so concurrent 404s don't race.
		var body render.HTML
		if nf, ok := ds.notFoundScreen.(NotFoundRenderer); ok {
			body = nf.RenderNotFound(path)
		} else {
			body = ds.notFoundScreen.Render()
		}
		if layout := ds.App.Router.GetDefaultLayout(); layout != nil {
			body = layout.Wrap(body)
		}
		appName := "GoFastr"
		if ds.App.Name != "" {
			appName = ds.App.Name
		}
		// Build a minimal document shell so injectChrome's strings.Replace
		// targets (`<head>`, `</head>`, `<body>`) actually exist — without
		// this the customCSS / runtime / color-scheme bootstrap silently
		// don't attach and the page renders as bare browser-default styles.
		shell := fmt.Sprintf(
			`<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>404 — %s</title></head><body>%s</body></html>`,
			stdhtml.EscapeString(appName), string(body))
		page := ds.injectChrome(shell, path, "")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, page)
		return
	}

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
	ctx = store.WithValues(ctx) // capture producer-seeded values for the partial seed
	res, err := ds.App.RenderPartialResult(ctx, path)
	if err != nil {
		ds.serveNotFound(w, path)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		// MUST be 200 + X-Gofastr-Location, not 3xx — the runtime
		// fetcher uses redirect:'follow' so a 303 here would be chased
		// silently and the header would never reach client JS. The
		// client-side router reads the header and pushState's to the
		// new URL, then loads the partial there.
		//
		// SAFETY: only emit X-Gofastr-Location for safe same-origin
		// relative paths. An absolute, protocol-relative, or scheme-
		// bearing URL would be fed directly into loadPage(), which
		// does a fetch with credentials — turning the partial-
		// redirect signal into a cross-origin XSRF / credential-leak
		// vector. For unsafe URLs fall back to a hard 303 redirect;
		// the browser handles those safely (cross-origin redirects
		// don't propagate cookies, javascript:/data: schemes are
		// blocked at the navigation layer).
		if isSafePartialRedirect(res.URL) {
			w.Header().Set("X-Gofastr-Location", res.URL)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, res.URL, http.StatusSeeOther)
		return
	case app.DecisionBlock:
		msg := res.Message
		if msg == "" {
			msg = http.StatusText(res.Status)
		}
		http.Error(w, msg, res.Status)
		return
	}

	// Look up screen title from route info. Percent-encode it: a title
	// with non-ASCII (e.g. the em-dash in "Docs — GoFastr") sent raw in an
	// HTTP header is non-conformant and a reader decodes the UTF-8 bytes as
	// Latin-1 ("Docs â GoFastr"). The runtime must decodeURIComponent it.
	if scr, _, ok := ds.App.Router.Resolve(path); ok && scr.Title != "" {
		title := scr.Title + " — " + ds.App.Name
		w.Header().Set("X-Gofastr-Title", url.PathEscape(title))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Gofastr-Partial", "true")
	fmt.Fprint(w, partialSeedIsland(ctx, string(res.HTML))+string(res.HTML))
}

// handleSSE streams island updates to the client.
func (ds *UIHost) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}
	// Reject forged session ids — only ids minted by CreateSession may
	// receive the update stream. Without this, an attacker could open an
	// SSE connection with an attacker-chosen id and receive any future
	// PushUpdate sent to that id, including ones a legitimate caller
	// might later choose if id space collides.
	ds.mu.RLock()
	_, ok := ds.sessions[sessionID]
	ds.mu.RUnlock()
	if !ok {
		http.Error(w, "unknown session", http.StatusUnauthorized)
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

// maxMutatingBodyBytes bounds JSON bodies accepted by mutating
// /__gofastr/* endpoints (signal updates, server actions, session
// creation). Anything past 64 KiB is rejected with 413 — these
// endpoints take small structured commands, not file uploads.
const maxMutatingBodyBytes = 64 * 1024

// requireValidSession resolves the session id from the session cookie
// (hardened __Host- form on TLS/non-loopback origins, relaxed dev form
// on plaintext localhost) and verifies it exists in ds.sessions. On
// failure writes 401 and
// returns "", false. Mutating /__gofastr/* endpoints call this so
// attackers can't pass a forged session id via query string or invent
// a cookie that was never minted by CreateSession.
func (ds *UIHost) requireValidSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := readSessionCookie(r)
	if id == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	ds.mu.RLock()
	_, ok := ds.sessions[id]
	ds.mu.RUnlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return id, true
}

// rejectCrossOrigin returns true (and has already written a 403) when
// the request carries an Origin header whose host differs from r.Host.
// Used by mutating /__gofastr/* endpoints to deny CSRF from
// attacker-controlled origins. Requests without an Origin header are
// allowed through — same-origin XHR / fetch may legitimately omit it,
// and non-browser callers (curl, server-to-server) never set it.
func rejectCrossOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	if !strings.EqualFold(u.Host, r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	return false
}

// decodeBounded decodes a JSON body capped at maxMutatingBodyBytes. On
// oversize, writes 413 and returns false; on JSON error writes 400 and
// returns false. Used by mutating /__gofastr/* endpoints.
func decodeBounded(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMutatingBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

// handleActionsJS serves all compiled action JS. The output enumerates
// every registered server-action and component id on the host — useful
// surface for an attacker mapping the app, so we require a session
// before serving it. The runtime fetches this script after first paint
// from a same-origin page, by which point the session cookie has been
// minted.
func (ds *UIHost) handleActionsJS(w http.ResponseWriter, r *http.Request) {
	if _, ok := ds.requireValidSession(w, r); !ok {
		return
	}
	js := ds.GetActionJS()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	fmt.Fprint(w, js)
}

// handleCreateSession creates a new session and returns its ID.
func (ds *UIHost) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if rejectCrossOrigin(w, r) {
		return
	}
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
	if rejectCrossOrigin(w, r) {
		return
	}
	// Reject obviously oversize bodies before any further work so a
	// DoS payload doesn't get to allocate auth/lookup state.
	if r.ContentLength > maxMutatingBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Parse: /__gofastr/signal/{signalID}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/__gofastr/signal/"), "/")
	if len(parts) == 0 {
		http.Error(w, "invalid signal path", http.StatusBadRequest)
		return
	}
	signalID := parts[0]

	// Resolve the signal first so probing arbitrary ids returns 404
	// (signal-not-found is not a credentials-leak — the runtime ships
	// signal ids in client JS already).
	ds.mu.RLock()
	sig, sigOK := ds.signals[signalID]
	ds.mu.RUnlock()
	if !sigOK {
		http.NotFound(w, r)
		return
	}

	// Auth required for mutation against a known signal.
	sessionID, ok := ds.requireValidSession(w, r)
	if !ok {
		return
	}

	var body map[string]interface{}
	if !decodeBounded(w, r, &body) {
		return
	}

	// Apply the signal update once. The previous in-loop version
	// applied the update N times (once per subscribing island) and
	// also read ds.signals without holding ds.mu while RegisterSignal
	// writes it — a real data race under concurrent registration.
	if val, exists := body["value"]; exists {
		sig.UpdateAsInterface(val)
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
	if rejectCrossOrigin(w, r) {
		return
	}
	if r.ContentLength > maxMutatingBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var body struct {
		Action      string            `json:"action"`
		Params      map[string]string `json:"params"`
		Session     string            `json:"session"`
		ComponentID string            `json:"componentId"`
	}
	if !decodeBounded(w, r, &body) {
		return
	}

	componentID := body.ComponentID
	actionName := body.Action

	// Look up the action registry for this component. Return 404 for
	// unknown component/action so probing reveals only what already-
	// rendered HTML reveals.
	ds.mu.RLock()
	reg, ok := ds.actionHandlers[componentID]
	ds.mu.RUnlock()

	if !ok || reg == nil {
		http.NotFound(w, r)
		return
	}

	actionDef, found := reg.Get(actionName)
	if !found {
		http.NotFound(w, r)
		return
	}

	// Auth required to actually invoke a known action.
	if _, ok := ds.requireValidSession(w, r); !ok {
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
	r.Get("/__gofastr/sse", http.HandlerFunc(ds.handleSSE))
	// Session minting is POST-only: GET would let CSRF / form-action /
	// image-src style attacks mint sessions silently. Explicit GET
	// registration returns 405 (the router's NotFound handler would
	// otherwise mask the method-mismatch with a 404 page render).
	r.Post("/__gofastr/session", http.HandlerFunc(ds.handleCreateSession))
	r.Get("/__gofastr/session", http.HandlerFunc(methodNotAllowed))
	r.Post("/__gofastr/signal/{id}", http.HandlerFunc(ds.handleSignalUpdate))
	r.Get("/__gofastr/signal/{id}", http.HandlerFunc(methodNotAllowed))
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
	// Widget catalog enumerates every server-registered widget surface
	// — useful infrastructure for the runtime, but a recon target for
	// anonymous callers. Gate behind the session cookie.
	r.Get("/__gofastr/widgets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := ds.requireValidSession(w, req); !ok {
			return
		}
		widget.ServeWidgetList(w, req)
	}))

	// Split runtime modules — /__gofastr/runtime/<name>.js. core.js's
	// loader fetches them on demand (hover prefetch, idle, or click
	// await). Delegated to core-ui/widget so back-compat for hosts that
	// already call widget.MountRuntime keeps a single source of truth.
	r.Get("/__gofastr/runtime/{name}", http.HandlerFunc(widget.ServeRuntimeModule))

	// Page-level LLM documentation endpoints.
	// - /llm-pages.md — top-level index of all screens
	// - /{screen-path}/llm.md — per-screen documentation
	// Disabled by default — apps opt in via [WithPublicLLMMD].
	if ds.App != nil && ds.llmMDPublic {
		ds.mountPageLLMMD(r)
	}

	// SEO endpoints — only mounted when WithSitemap / WithRobots
	// were passed, so apps that don't opt in don't accidentally
	// expose either endpoint.
	if ds.sitemapConfig != nil {
		r.Get("/sitemap.xml", http.HandlerFunc(ds.handleSitemap))
	}
	if ds.robotsConfig != nil {
		r.Get("/robots.txt", http.HandlerFunc(ds.handleRobots))
	}
	// Agent-discovery surface (/llms.txt, /.well-known/agent-card.json,
	// legacy /.well-known/agent.json). Opt-in via WithAgentReady or the
	// granular WithLLMsTxt / WithAgentCard options.
	ds.mountAgentReady(r)

	r.NotFound(http.HandlerFunc(ds.serveOrRender))
}

// methodNotAllowed is registered alongside POST-only endpoints so a wrong-
// method request gets a clear 405 instead of falling through to the UI
// page handler and getting a misleading 404.
func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "POST")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// mountPageLLMMD registers LLM-friendly documentation routes for every
// screen in the app. Two route types are added:
//   - GET /llm-pages.md — top-level index listing all screens
//   - GET /{screen-path}/llm.md — per-screen markdown documentation
//
// Dynamic routes (e.g. /products/:slug) are documented with their
// pattern, not concrete values.
func (ds *UIHost) mountPageLLMMD(r *router.Router) {
	coreApp := ds.App

	// Global opt-out
	if coreApp.NoLLMMD {
		return
	}

	// Top-level page index
	r.Get("/llm-pages.md", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(app.AppLLMMD(coreApp)))
	}))

	// Per-screen documentation
	for _, routePath := range coreApp.Router.Paths() {
		screen, _, ok := coreApp.Router.Resolve(routePath)
		if !ok {
			continue
		}
		// Per-screen opt-out
		if screen.NoLLMMD {
			continue
		}
		// Capture for closure
		sc := screen
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write([]byte(app.ScreenLLMMD(sc)))
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
		r.Get(route, handler)
	}
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
	// Canonicalize missing trailing slash: when only "/foo/" is
	// registered and "/foo" arrives, 301 to the slash form. ServeMux
	// gives this for free for subtree patterns, but UIHost dispatches
	// inside handlePage via App.Router.Resolve, so we do it here.
	if path != "/" && !strings.HasSuffix(path, "/") {
		if _, _, ok := ds.App.Router.Resolve(path); !ok {
			if _, _, ok := ds.App.Router.Resolve(path + "/"); ok {
				target := path + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
		}
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
	// WithFavicon configured a URL but no real file lives there — 204
	// rather than 404 so the browser's per-page favicon fetch doesn't
	// noisily fail in the dev console.
	if ds.faviconURL != "" && path == ds.faviconURL {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ds.handlePage(w, r)
}

// RenderStaticPage produces a fully-rendered page suitable for static-site
// generation: it runs the screen's Load(ctx) hook, applies layout/theme,
// and injects runtime.js, compiled actions, custom CSS, and the route
// graph — but skips the SSE meta tag because there is no live session.
// The result is safe to write to disk and serve from any static host.
func (ds *UIHost) RenderStaticPage(ctx context.Context, path string) (string, error) {
	// Install the value bag so producer-seeded slice values are captured
	// during the static render (matches the live handlePage path).
	ctx = store.WithValues(ctx)
	html, err := ds.App.RenderPage(ctx, path)
	if err != nil {
		return "", err
	}
	// bundle=false: static hosts don't serve query-paramed files, so
	// emit one <link rel=stylesheet> per registered component instead
	// of the comp-bundle.css?names= form.
	page := ds.injectChromeMode(string(html), path, "", false)
	return injectSignalSeed(ctx, page), nil
}

// actionsToJS converts an ActionRegistry to browser-runnable JavaScript
// using the ClientJS field from each ActionDef. Each action's ClientJS
// is wrapped in a handler function and registered with the runtime.
func actionsToJS(componentID string, reg *component.ActionRegistry) string {
	if !reg.HasActions() {
		return ""
	}

	sb := borrowBuilder()
	defer returnBuilder(sb)
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
		b := borrowBuilder()
		defer returnBuilder(b)
		for i, n := range names {
			e, ok := registry.Lookup(n)
			if !ok {
				continue
			}
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(b, `<link rel="stylesheet" href="/__gofastr/comp/%s.css?v=%s">`,
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

// signalsJSONScript returns the inline JSON block seeding the client
// signal store (core-ui/store) with server-resolved initial values.
// Same inert-data pattern as catalogJSONScript: type="application/json"
// is parsed by JSON.parse, never executed, so strict CSP is happy.
// runtime.js reads #gofastr-signals on boot and seeds __gofastr._signals
// BEFORE hydration. Returns "" when there is nothing to seed.
func signalsJSONScript(seed map[string]any) string {
	if len(seed) == 0 {
		return ""
	}
	buf, err := json.Marshal(seed)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-signals">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// injectSignalSeed resolves the signal seed for the rendered page (names
// referenced in the HTML plus all app-global slices), and splices the
// JSON block into <head> just before </head> so it is in place before
// runtime.js (end of <body>) reads it. ctx must carry the request value
// bag (store.WithValues), so producer-seeded per-request values win over
// declared defaults. No-op when there is nothing to seed.
func injectSignalSeed(ctx context.Context, page string) string {
	block := signalsJSONScript(store.SeedFor(ctx, page))
	if block == "" {
		return page
	}
	if i := strings.LastIndex(page, "</head>"); i >= 0 {
		return page[:i] + block + "\n" + page[i:]
	}
	return page
}

// partialSeedIsland builds the scope-split seed block for a SPA-nav
// partial. It is embedded inside the swapped content; the runtime reads
// #gofastr-signals-partial after the swap and merges it (page-scoped
// applied always, globals only when first seen). Returns "" when empty.
func partialSeedIsland(ctx context.Context, html string) string {
	page, global := store.SeedSplit(ctx, html)
	if len(page) == 0 && len(global) == 0 {
		return ""
	}
	buf, err := json.Marshal(map[string]map[string]any{"p": page, "g": global})
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-signals-partial">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// runtimeModuleManifestScript delegates to widget.RuntimeModuleManifestScript
// so the same JSON manifest ships from both framework/uihost-rendered pages
// and kiln-style hosts that consume widget.RuntimeTag() directly.
func runtimeModuleManifestScript() string {
	return widget.RuntimeModuleManifestScript()
}

// runtimeModulePreloadLinks emits <link rel="modulepreload"> tags for
// every demand-load runtime module whose marker substring appears in
// pageHTML (post-render scan via runtime.NeededModules). The href
// carries the content-addressed ?v=<hash> URL so preload hits the same
// immutable cache entry as the eventual fetch.
func runtimeModulePreloadLinks(pageHTML string) string {
	mods := runtime.NeededModules(pageHTML)
	if len(mods) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range mods {
		hash := widget.RuntimeModuleHash(name)
		href := "/__gofastr/runtime/" + name + ".js"
		if hash != "" {
			href += "?v=" + hash
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(`<link rel="modulepreload" href="` + href + `">`)
	}
	return b.String()
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

// scriptTagRe matches inline <script>…</script> blocks and
// self-closing / attribute-only forms like <script src="…"></script>
// or <script src="…" />.
//
// The pattern is assembled from fragments so the no-inline-scripts
// linter (core-ui/check/noinlinescripts.go) doesn't flag this file as
// emitting an inline <script> tag literal. Each fragment alone is not
// a recognizable <script> open tag.
var scriptTagRe = regexp.MustCompile(`(?is)<scrip` + `t\b[^>]*?(/>|>.*?</scrip` + `t\s*>)`)

// dangerousHeadTagsRe matches content tag families that have no place
// in a server-controlled <head>: iframes, inline styles, scriptable
// media (svg/math/audio/video), forms, the marquee/portal grab-bag,
// and template/noscript wrappers. The opening tag alone is enough to
// match — an UNCLOSED opener like `<svg onload=alert(1)>` (no `/>`, no
// closing tag) is created and its handler fired by the browser's lenient
// parser, so requiring a closing tag would let it slip through. The
// optional closing-tag group greedily consumes any inner content +
// matching close so the whole element (not just its opener) is removed
// when it IS closed.
var dangerousHeadTagsRe = regexp.MustCompile(
	`(?is)<(iframe|object|style|svg|math|audio|video|form|button|picture|marquee|portal|template|noscript|foreignObject|details|summary)\b[^>]*?(/>|>(.*?</\s*\w+\s*>)?)`,
)

// voidHeadTagsRe matches void/empty dangerous tags in the head: base
// (re-roots the page), img (active resource fetch), embed (plugin
// loader), source (used by media + picture). Void tags have no closing
// pair so they need their own pattern.
var voidHeadTagsRe = regexp.MustCompile(
	`(?is)<(base|embed|img|source)\b[^>]*?/?>`,
)

// eventHandlerAttrRe matches any `on…=` event-handler attribute (quoted,
// single-quoted, or bare value) plus the bare `autofocus` boolean. Form
// controls (input/select/textarea/keygen) are hoisted into <body> by the
// browser's lenient parser, and an `autofocus` on one fires its `onfocus`
// handler with no user interaction — an XSS vector that survives the
// tag-family block-lists because those tags aren't in them. Stripping the
// attributes (rather than the tags) also future-proofs any new
// interactive element that lands in caller-supplied head HTML.
//
// The leading boundary is [\s/], not just \s: HTML5 treats '/' as an
// attribute separator (no whitespace required), so `<input/onfocus=…>`
// is a live handler the browser parses. A whitespace-only boundary would
// miss the slash-delimited form. The same [\s/] applies to the value's
// terminator so a trailing `/autofocus` is consumed, not left dangling.
var eventHandlerAttrRe = regexp.MustCompile(`(?is)[\s/]on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+)`)

// autofocusAttrRe matches the bare `autofocus` boolean attribute. Like
// eventHandlerAttrRe, the leading boundary is [\s/] to catch the HTML5
// '/'-as-separator form (`<input/autofocus>`).
var autofocusAttrRe = regexp.MustCompile(`(?is)[\s/]autofocus(\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+))?`)

// metaRefreshRe matches `<meta http-equiv="refresh" …>` in any casing
// or attribute order. Meta-refresh is the canonical "redirect via
// markup" primitive and has no business being injected by an SEO
// escape hatch.
var metaRefreshRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?\s*refresh\s*["']?[^>]*>`)

// linkTagRe matches a single <link> tag (always void). Used to walk
// link tags and drop the ones whose href / rel / as combos pull in
// scripts or use unsafe schemes.
var linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*?/?>`)

// stripInlineScripts removes <script> tags AND a broader set of
// dangerous tag families from caller-supplied head HTML (WithHeadHTML,
// SEOScreen.HeadHTML). The name is kept for back-compat; the behavior
// is now defense-in-depth across the whole "active in head" tag set.
// Allowed survivors: <meta> (except http-equiv=refresh), <link> with
// http(s)/relative href, <title>, and inline text content — and even
// those have any on*= event-handler / autofocus attribute scrubbed.
func stripInlineScripts(s string) string {
	if s == "" {
		return s
	}
	out := scriptTagRe.ReplaceAllString(s, "")
	out = dangerousHeadTagsRe.ReplaceAllString(out, "")
	out = voidHeadTagsRe.ReplaceAllString(out, "")
	out = metaRefreshRe.ReplaceAllString(out, "")
	// Generic on*= / autofocus strip so any surviving tag (notably form
	// controls the browser hoists into <body>) can't fire an event handler.
	out = eventHandlerAttrRe.ReplaceAllString(out, "")
	out = autofocusAttrRe.ReplaceAllString(out, "")
	out = linkTagRe.ReplaceAllStringFunc(out, func(tag string) string {
		if isSafeLinkTag(tag) {
			return tag
		}
		return ""
	})
	return out
}

// isSafeLinkTag reports whether a <link …> tag's href and rel are safe
// to inject into the head from a caller-supplied head HTML escape
// hatch. The framework's own per-render <link> emissions never go
// through this path — they're constructed directly.
func isSafeLinkTag(tag string) bool {
	low := strings.ToLower(tag)
	// Reject preload/modulepreload/prefetch — they pull arbitrary
	// resources into the page on the framework's behalf.
	for _, rel := range []string{`rel="modulepreload"`, `rel='modulepreload'`, `rel=modulepreload`, `rel="prefetch"`, `rel='prefetch'`, `rel=prefetch`, `rel="preload"`, `rel='preload'`, `rel=preload`} {
		if strings.Contains(low, rel) {
			return false
		}
	}
	href := extractAttrValue(tag, "href")
	if href == "" {
		// No href → benign (e.g., `<link rel=canonical>` is set by typed
		// helpers, not by the escape hatch).
		return true
	}
	return isSafeHeadURL(href)
}

// extractAttrValue is a permissive attribute-value extractor for the
// scrub path. Not for production rendering — only for "is this href
// safe enough to keep" decisions on caller-supplied HTML fragments.
func extractAttrValue(tag, attr string) string {
	low := strings.ToLower(tag)
	key := strings.ToLower(attr) + "="
	i := strings.Index(low, key)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(key):]
	if rest == "" {
		return ""
	}
	quote := rest[0]
	if quote == '"' || quote == '\'' {
		end := strings.IndexByte(rest[1:], quote)
		if end < 0 {
			return ""
		}
		return rest[1 : 1+end]
	}
	end := strings.IndexAny(rest, " \t/>")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// isSafeHeadURL is the allow-list for URLs that may appear in caller-
// supplied head tags. Mirrors the framework/ui safety policy: relative,
// http(s), or fragment.
func isSafeHeadURL(u string) bool {
	if u == "" {
		return false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	low := strings.ToLower(u)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return false
	}
	if strings.HasPrefix(u, "//") {
		return false
	}
	if strings.HasPrefix(u, "/") || strings.HasPrefix(u, "#") || strings.HasPrefix(u, "?") || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return true
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c == ':' {
			scheme := strings.ToLower(u[:i])
			switch scheme {
			case "http", "https":
				return true
			default:
				return false
			}
		}
		if c == '/' || c == '?' || c == '#' {
			return true
		}
	}
	return true
}

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

// ReadCustomCSSFile reads a CSS file and returns its content.
// This is a helper for the demo main.go.
func ReadCustomCSSFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
