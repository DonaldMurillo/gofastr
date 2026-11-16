// Package uihost wires a core-ui application onto a framework.App's router.
// It mounts page rendering, runtime.js, compiled action JS, SSE island
// streaming, and sessions as routes. There is no standalone server. The
// framework.App owns the HTTP listener.
package uihost

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/rootfs"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// UIHost mounts a core-ui application onto a router. It serves rendered
// pages with runtime.js, compiled action JS, SSE streaming for islands,
// and sessions. The framework.App is responsible for ListenAndServe.
type UIHost struct {
	App     *app.App
	Islands *island.Manager
	mu      sync.RWMutex
	// variants holds themes servable under app.css besides App.Theme.
	// See themevariant.go. Its own lock, not ds.mu: registration happens at
	// wire time while reads happen per request, and coupling them would put
	// stylesheet lookups behind the host's general-purpose mutex.
	variants themeVariants
	// sessionKey signs and verifies the stateless session tokens that
	// replaced the old in-memory session map (issue #112): any process
	// holding the same key accepts any process's tokens, so sessions are
	// portable across replicas with zero shared state. New() self-mints a
	// per-boot key (single-replica zero-config path; sessions roll on
	// restart exactly like the map did); framework.App.Mount overwrites it
	// via SetSessionKey with a key derived from the app secret
	// (WithSecret / GOFASTR_SECRET).
	sessionKey []byte
	// previousSessionKeys are verify-only keys retained for a drain window
	// after a GOFASTR_SECRET rotation, mirroring the CSRF AdditionalKeys
	// idiom. New tokens are always signed with sessionKey; tokens signed by
	// any key here still verify until the window closes and the operator
	// drops the old key. Set via SetSessionKeys (framework.App.Mount).
	previousSessionKeys [][]byte
	actionJS            map[string]string                    // componentID → compiled JS
	actionHash          map[string]string                    // componentID → content hash of the compiled JS (?v= addressing)
	actionHandlers      map[string]*component.ActionRegistry // componentID → action registry for server-side handlers
	actionComps         map[string]component.Component       // componentID → the component the registry was compiled FROM (embed_actions.go walks it)
	actionScreens       map[string]*app.Screen               // componentID → the screen whose Policy chain gates its actions (AutoCompileActions)
	customCSS           string                               // extra CSS to inject (e.g. demo.css)
	extraScripts        []externalScript                     // extra <script defer src="…"> rail closing <head>; every-page + document-lifetime entries (register_script.go)
	scriptMu            sync.Mutex                           // guards post-construction extraScripts appends + the servingStarted latch (see register_script.go)
	servingStarted      atomic.Bool                          // latched on the first full-shell render; RegisterExternalScript refuses after it
	staticDir           string                               // directory to serve static files from
	staticFS            fs.FS                                // embedded filesystem for static files
	staticRootOnce      sync.Once                            // guards staticRoot init; staticDir reads are kernel-contained
	staticRoot          *rootfs.Root                         // os.Root over staticDir: static reads refuse symlinked escapes out of the static tree
	versions            assetVersions                        // content hashes of static files, for AssetURL and immutable serving
	llmMDPublic         bool                                 // when true, mount per-screen /llm.md + /llm-pages.md; default disabled (schema disclosure)
	headHTML            string                               // raw HTML to inject into <head> (escape hatch)
	lang                string                               // WithLang document language for host-built shells; EffectiveLang resolves it
	langFunc            func(path string) string             // WithLangFunc per-route language for host-built shells; LangFor resolves it
	headTags            []string                             // typed head tags built from convenience options
	faviconURL          string                               // configured WithFavicon URL: serveOrRender 204s it when no static file matches
	noLiveChannel       bool                                 // WithNoLiveChannel: omit the gofastr-sse meta (no EventSource)
	trustedProxies      []string                             // WithTrustedProxies: the island stream cap reads X-Forwarded-For from these peers
	proxies             clientip.Proxies                     // trustedProxies parsed in New: the peers resolveBaseURL honors X-Forwarded-Proto from
	noInlineFirstPaint  bool                                 // WithNoInlineFirstPaint: head CSS and the color-scheme bootstrap always ship as links
	firstPaint          firstPaintCache                      // blocks a live page carries inline in its head (firstpaint.go)
	appIcons            map[string][]byte                    // WithAppIcon-generated PNGs, URL path → bytes; also served at /favicon.ico
	notFoundScreen      component.Component                  // when set, serveNotFound renders this through the default layout instead of the bare 404 fallback
	errorScreen         component.Component                  // when set, a whole-page resolver failure (app.PageError) renders this through the default layout instead of the bare 500 fallback
	pageLoading         component.Component                  // WithPageLoading: rendered once into every full page, shown while a navigation is in flight (replaces the default progress strip)
	organization        *OrganizationConfig                  // when set, Organization JSON-LD is embedded in every full page head (see organization.go)
	sitemapConfig       *SitemapConfig                       // when set, /sitemap.xml lists every reachable route
	sitemapMemo         sitemapMemo                          // the live sitemap's last render (liveSitemap)
	pathLocales         *i18n.PathLocales                    // SetPathLocales: the app routes by locale prefix
	robotsConfig        *RobotsConfig                        // when set, /robots.txt is served from this config
	agentReady          *agentReadyConfig                    // when set, the agent-discovery surface (/llms.txt, agent card, Link headers, markdown negotiation) is served
	mcpManifestPath     string
	pwaConfig           *PWAConfig      // when set, the installable-PWA surface (manifest, service worker, offline screen) is served
	pwaSWOnce           sync.Once       // guards the memoized service-worker body below
	pwaSW               string          // deployment-constant service worker, computed on first request
	pwaSWErr            error           // paired with pwaSW
	strict              bool            // WithStrict: Mount refuses to serve an app that fails the strict checks (strict.go)
	coreRouter          *router.Router  // the framework router Mount received; strict mode reads its route table for the internal-links check
	strictConfig        StrictConfig    // per-check levels + route exemptions; zero value enforces everything
	siteDescription     string          // WithDescription's text: the description of a page that names none of its own (siteDescriptionTag)
	embedHost           *fembed.Host    // set by WithEmbed; nil means the app hands out no pieces of itself and mounts no embed routes
	embedThemes         embedThemeState // per-surface cap on distinct customer theme variants

	// Hot-path caches. The route table and the default-theme component catalog
	// are static after wire time, so they are marshaled once and reused on every
	// page render instead of rebuilt per request. The *MarshalCount atomics let
	// tests (and a host observing itself) confirm the per-request marshal is gone.
	// The route script carries loading content, so it is built once per
	// catalog locale (localecache.go).
	routeScripts        perLocale[string]
	routeMarshalCount   atomic.Int64
	catalogCacheOnce    sync.Once
	catalogCache        string
	catalogMarshalCount atomic.Int64
	// Default-theme app.css, composed + fingerprinted once at first render.
	// Every page was re-concatenating the full sheet (theme tokens, layout
	// base, every style.Contribute fragment) per request, and the response
	// carried no validator at all. Theme VARIANTS stay uncached here.
	// They're keyed and evicted through the variant registry.
	appCSSOnce       sync.Once
	appCSSBody       string
	appCSSHash       string
	appCSSContribN   int
	appCSSFrozenWarn sync.Once
	// The layouts app.css collected transitions from, and the
	// registration count they were read at (see appCSSCached).
	appCSSLayoutsGen        uint64
	appCSSTransitionLayouts map[*app.Layout]bool
	appCSSLayoutsWarn       sync.Once
	// External data manifest (catalog + module hashes + action hashes +
	// route graph), composed + fingerprinted once per catalog locale
	// after Mount. See manifestjs.go.
	manifests perLocale[manifestEntry]

	// standalone is a private router lazily mounted on first ServeHTTP call,
	// so the host can satisfy http.Handler when it is used outside a
	// framework.App. Tests use this path; production goes through Mount.
	standalone     *router.Router
	standaloneOnce sync.Once
}

// New creates a new development server.
func New(application *app.App, opts ...Option) *UIHost {
	ds := &UIHost{
		App:            application,
		sessionKey:     selfMintedSessionKey(),
		actionJS:       make(map[string]string),
		actionHash:     make(map[string]string),
		actionHandlers: make(map[string]*component.ActionRegistry),
		actionComps:    make(map[string]component.Component),
		actionScreens:  make(map[string]*app.Screen),
	}
	for _, opt := range opts {
		opt(ds)
	}
	ds.Islands = island.NewManager(island.WithTrustedProxies(ds.trustedProxies...))
	ds.proxies = clientip.ParseProxies(ds.trustedProxies)
	// WithAppIcon ↔ WithPWA reconciliation happens after ALL options so
	// their relative order doesn't matter.
	// The app's 404-outlet outcome body (FallbackNotFound renders the
	// route's own request as the not-found page): give the app the same
	// not-found body the host serves for an unmatched path — the
	// configured screen when there is one, else the built-in default —
	// unless the app already wired its own (app.WithNotFoundBody). The
	// path-less form: the outlet's route DID resolve, so "no route
	// matched" copy would be a lie.
	if ds.App != nil && ds.App.NotFound == nil {
		ds.App.NotFound = ds.notFoundBodyComponent("")
	}
	ds.reconcileAppIcons()
	// Auto-inject the livereload client script when dev-mode env says so.
	// The matching SSE/JS routes are auto-registered by framework.NewApp
	// (see framework/dev/livereload.go). Both halves are gated by the
	// same env predicate, so the host needs zero code change.
	if dev.LiveReloadEnabled() {
		ds.extraScripts = append(ds.extraScripts, externalScript{src: dev.LiveReloadScriptURL})
	}
	if dev.LayoutLayerEnabled() {
		ds.extraScripts = append(ds.extraScripts, externalScript{src: dev.LayoutLayerScriptURL})
	}
	// #215: a partial DarkColors silently keeps light values in dark mode
	// (the map is what renders under a dark preference; editing Colors
	// alone changes nothing on screen). One warning naming the exact
	// tokens, so the fix loop doesn't dead-end at "nothing happened".
	if gaps := style.DarkPaletteGaps(ds.activeTheme()); len(gaps) > 0 {
		slog.Default().Warn(fmt.Sprintf(
			"uihost: theme declares a dark palette but %d color tokens have no dark value; under a dark preference they keep their light value. Add them to Theme.DarkColors.",
			len(gaps)), "tokens", gaps)
	}
	return ds
}

// EffectiveLang returns the document language for host-built shells (404, PWA
// offline, embed). WithLang ([WithLang]) overrides the app's own Lang
// ([app.App.WithLang]); both default to "en" so an unconfigured host keeps the
// pre-config English attribute.
func (ds *UIHost) EffectiveLang() string {
	if ds.lang != "" {
		return ds.lang
	}
	if ds.App != nil && ds.App.Lang != "" {
		return ds.App.Lang
	}
	return "en"
}

// LangFor returns the document language for a host-built shell standing in
// for path: WithLangFunc ([WithLangFunc]) first, then the app's own LangFunc
// ([app.App.WithLangFunc]) so a site that configured its languages once on the
// app gets them here too, then the locale the request negotiated
// (i18n.FromContext), then EffectiveLang. An empty path (a shell that belongs
// to no route, such as the PWA offline page) skips the route rules.
func (ds *UIHost) LangFor(ctx context.Context, path string) string {
	if path != "" {
		if ds.langFunc != nil {
			if lang := strings.TrimSpace(ds.langFunc(path)); lang != "" {
				return lang
			}
		}
		if ds.App != nil && ds.App.LangFunc != nil {
			if lang := strings.TrimSpace(ds.App.LangFunc(path)); lang != "" {
				return lang
			}
		}
	}
	if tag := i18n.FromContext(ctx).Tag; tag != "" {
		return tag
	}
	return ds.EffectiveLang()
}
