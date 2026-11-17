// Package uihost serves a core-ui application on a framework.App's router.
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

// UIHost serves a core-ui application's pages, assets, islands and sessions.
type UIHost struct {
	App                 *app.App
	Islands             *island.Manager
	mu                  sync.RWMutex
	variants            themeVariants
	sessionKey          []byte
	previousSessionKeys [][]byte
	actionJS            map[string]string
	actionHash          map[string]string
	actionHandlers      map[string]*component.ActionRegistry
	actionComps         map[string]component.Component
	actionScreens       map[string]*app.Screen
	customCSS           string
	extraScripts        []externalScript
	scriptMu            sync.Mutex
	servingStarted      atomic.Bool
	staticDir           string
	staticFS            fs.FS
	staticRootOnce      sync.Once
	staticRoot          *rootfs.Root
	versions            assetVersions
	llmMDPublic         bool
	headHTML            string
	lang                string
	langFunc            func(path string) string
	headTags            []string
	faviconURL          string
	noLiveChannel       bool
	trustedProxies      []string
	proxies             clientip.Proxies
	noInlineFirstPaint  bool
	firstPaint          firstPaintCache
	appIcons            map[string][]byte
	notFoundScreen      component.Component
	errorScreen         component.Component
	pageLoading         component.Component
	organization        *OrganizationConfig
	sitemapConfig       *SitemapConfig
	sitemapMemo         sitemapMemo
	pathLocales         *i18n.PathLocales
	robotsConfig        *RobotsConfig
	agentReady          *agentReadyConfig
	mcpManifestPath     string
	pwaConfig           *PWAConfig
	pwaSWOnce           sync.Once
	pwaSW               string
	pwaSWErr            error
	strict              bool
	coreRouter          *router.Router
	strictConfig        StrictConfig
	siteDescription     string
	embedHost           *fembed.Host
	embedThemes         embedThemeState
	routeScripts        perLocale[string]
	routeMarshalCount   atomic.Int64
	catalogCacheOnce    sync.Once
	catalogCache        string
	catalogMarshalCount atomic.Int64
	appCSSOnce          sync.Once
	appCSSBody          string
	appCSSHash          string
	appCSSContribN      int
	appCSSFrozenWarn    sync.Once
	appCSSLayoutsSeen   atomic.Uint64
	transitionWarnMu    sync.Mutex
	transitionWarned    map[*app.Layout]bool
	manifests           perLocale[manifestEntry]
	standalone          *router.Router
	standaloneOnce      sync.Once
}

// New returns a host for application configured by opts.
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
	if ds.App != nil && ds.App.NotFound == nil {
		ds.App.NotFound = ds.notFoundBodyComponent("")
	}
	ds.reconcileAppIcons()
	if dev.LiveReloadEnabled() {
		ds.extraScripts = append(ds.extraScripts, externalScript{src: dev.LiveReloadScriptURL})
	}
	if dev.LayoutLayerEnabled() {
		ds.extraScripts = append(ds.extraScripts, externalScript{src: dev.LayoutLayerScriptURL})
	}
	if gaps := style.DarkPaletteGaps(ds.activeTheme()); len(gaps) > 0 {
		slog.Default().Warn(fmt.Sprintf(
			"uihost: theme declares a dark palette but %d color tokens have no dark value; under a dark preference they keep their light value. Add them to Theme.DarkColors.",
			len(gaps)), "tokens", gaps)
	}
	return ds
}

// EffectiveLang returns the document language of host-built shells: WithLang,
// then the app's Lang, then "en".
func (ds *UIHost) EffectiveLang() string {
	if ds.lang != "" {
		return ds.lang
	}
	if ds.App != nil && ds.App.Lang != "" {
		return ds.App.Lang
	}
	return "en"
}

// LangFor returns the document language of a host-built shell for path:
// WithLangFunc, the app's LangFunc, the negotiated locale, then EffectiveLang.
// An empty path skips the two route rules.
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
