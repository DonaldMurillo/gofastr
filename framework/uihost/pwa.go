package uihost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PWADisplay is the manifest display mode.
type PWADisplay string

// Typed constants for the common manifest display modes.
const (
	PWADisplayStandalone PWADisplay = "standalone"
	PWADisplayFullscreen PWADisplay = "fullscreen"
	PWADisplayMinimalUI  PWADisplay = "minimal-ui"
	PWADisplayBrowser    PWADisplay = "browser"
)

// PWAIconPurpose is the manifest icon purpose.
type PWAIconPurpose string

// Typed constants for the common manifest icon purposes.
const (
	PWAIconPurposeAny        PWAIconPurpose = "any"
	PWAIconPurposeMaskable   PWAIconPurpose = "maskable"
	PWAIconPurposeMonochrome PWAIconPurpose = "monochrome"
)

// PWAIcon declares one manifest icon; Src is a same-origin root-absolute path.
type PWAIcon struct {
	Src     string
	Sizes   string // e.g. "192x192"
	Type    string // e.g. "image/png"
	Purpose PWAIconPurpose
}

// PWAConfig configures WithPWA. Every field is optional: Name defaults to
// the app name, StartURL and Scope to "/", ID to StartURL and Display to
// standalone.
type PWAConfig struct {
	ID              string
	Name            string
	ShortName       string
	Description     string
	StartURL        string
	Scope           string
	Display         PWADisplay
	ThemeColor      string
	BackgroundColor string
	Icons           []PWAIcon
	Precache        []string            // extra same-origin paths kept offline; denied paths are dropped
	OfflineScreen   component.Component // the offline page body, precached, so never personalized; nil is the default
	DenyPaths       []string            // paths added to the built-in deny list; a bare "/" is ignored
}

// WithPWA serves /manifest.webmanifest, /service-worker.js,
// /__gofastr/pwa/register.js and /__gofastr/pwa/offline, and links the
// manifest and the registration script from every page.
func WithPWA(cfg PWAConfig) Option {
	return func(ds *UIHost) {
		ds.pwaConfig = &cfg
		if cfg.ThemeColor != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="theme-color" content="%s">`, stdhtml.EscapeString(cfg.ThemeColor)))
		}
		ds.extraScripts = append(ds.extraScripts, externalScript{src: "/__gofastr/pwa/register.js"})
	}
}

// PWAEnabled reports whether WithPWA was configured.
func (ds *UIHost) PWAEnabled() bool {
	return ds.pwaConfig != nil
}

func (ds *UIHost) resolvedPWA() PWAConfig {
	cfg := *ds.pwaConfig
	if cfg.Name == "" {
		if ds.App != nil && ds.App.Name != "" {
			cfg.Name = ds.App.Name
		} else {
			cfg.Name = "GoFastr App"
		}
	}
	if cfg.StartURL == "" {
		cfg.StartURL = "/"
	}
	if cfg.Scope == "" {
		cfg.Scope = "/"
	}
	if cfg.Display == "" {
		cfg.Display = PWADisplayStandalone
	}
	if cfg.ID == "" {
		cfg.ID = cfg.StartURL
	}
	return cfg
}

var pwaSensitivePaths = []string{
	"/__gofastr/sse",
	"/__gofastr/session",
	"/__gofastr/action",
	"/__gofastr/widgets",
	"/__gofastr/widget",
	"/core-ui/widget",
	"/api",
	"/auth",
}

func (ds *UIHost) pwaDenyPaths(cfg PWAConfig) []string {
	deny := append([]string(nil), pwaSensitivePaths...)
	for _, d := range cfg.DenyPaths {
		d = strings.TrimRight(strings.TrimSpace(d), "/")
		if d == "" || !strings.HasPrefix(d, "/") {
			continue
		}
		deny = append(deny, d)
	}
	if ds.pathLocales == nil {
		return deny
	}
	base := len(deny)
	for _, tag := range ds.pathLocales.All() {
		for _, d := range deny[:base] {
			deny = append(deny, "/"+tag+d)
		}
	}
	return deny
}

func pwaAllowedPrecache(p string, deny []string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	clean := p
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	clean, err := url.PathUnescape(clean)
	if err != nil {
		return false
	}
	clean = path.Clean(clean)
	for _, d := range deny {
		if clean == d || strings.HasPrefix(clean, d+"/") {
			return false
		}
	}
	return true
}

var pwaAssetURL = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9-]*)\b[^>]*?\s(?:src|href)="(/[^"]+)"`)

var pwaNavTags = map[string]bool{"a": true, "area": true, "base": true}

func (ds *UIHost) pwaPrecachePaths(ctx context.Context, cfg PWAConfig, offlineHTML string, live bool) []string {
	set := map[string]bool{
		"/__gofastr/runtime.js":      true,
		"/__gofastr/color-scheme.js": true,
		"/manifest.webmanifest":      true,
		pwaOfflinePath:               true,
	}
	if ds.pathLocales != nil {
		for _, tag := range ds.pathLocales.Prefixed {
			set[ds.pathLocales.Path(tag, "/manifest.webmanifest")] = true
		}
	}
	if live {
		set["/__gofastr/runtime.js?v="+widget.RuntimeHash()] = true
		set["/__gofastr/color-scheme.js?v="+colorSchemeHash()] = true
		for _, lr := range ds.renderLocales(ctx) {
			if mHash := ds.manifestFor(lr).hash; mHash != "" {
				set["/__gofastr/manifest.js?v="+mHash] = true
			}
		}
	}
	if ds.App != nil {
		set["/__gofastr/app.css"] = true
		if _, cssHash := ds.appCSSCached(); live && cssHash != "" {
			set["/__gofastr/app.css?v="+cssHash] = true
		}
	}
	if ds.HasActions() {
		set["/__gofastr/actions.js"] = true
	}
	for _, name := range runtime.ModuleNames() {
		u := "/__gofastr/runtime/" + name + ".js"
		if hash := widget.RuntimeModuleHash(name); hash != "" {
			u += "?v=" + hash
		}
		set[u] = true
	}
	deny := ds.pwaDenyPaths(cfg)
	for _, m := range pwaAssetURL.FindAllStringSubmatch(offlineHTML, -1) {
		if pwaNavTags[m[1]] {
			continue
		}
		if u := stdhtml.UnescapeString(m[2]); pwaAllowedPrecache(u, deny) {
			set[u] = true
		}
	}
	for _, icon := range cfg.Icons {
		if pwaAllowedPrecache(icon.Src, deny) {
			set[icon.Src] = true
		}
	}
	for _, p := range cfg.Precache {
		if pwaAllowedPrecache(p, deny) {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func pwaPrefix(basePath, p string) string {
	if basePath == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return p
	}
	return basePath + p
}

type pwaManifestDoc struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	ShortName       string            `json:"short_name,omitempty"`
	Description     string            `json:"description,omitempty"`
	StartURL        string            `json:"start_url"`
	Scope           string            `json:"scope"`
	Display         string            `json:"display"`
	ThemeColor      string            `json:"theme_color,omitempty"`
	BackgroundColor string            `json:"background_color,omitempty"`
	Icons           []pwaManifestIcon `json:"icons,omitempty"`
}

type pwaManifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes,omitempty"`
	Type    string `json:"type,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

func pwaManifestPath(ctx context.Context) string {
	return i18n.PathPrefix(ctx) + "/manifest.webmanifest"
}

func pwaManifestLink(ctx context.Context) string {
	return `<link rel="manifest" href="` + stdhtml.EscapeString(urlsafe.Clean(pwaManifestPath(ctx), urlsafe.Resource)) + `">`
}

// PWAManifestJSON renders the web app manifest for ctx's locale; only
// start_url carries the locale prefix. basePath prefixes the same-origin
// paths for a static export under a subpath; pass "" for the live server.
func (ds *UIHost) PWAManifestJSON(ctx context.Context, basePath string) ([]byte, error) {
	if ds.pwaConfig == nil {
		return nil, fmt.Errorf("uihost: PWA not configured")
	}
	cfg := ds.resolvedPWA()
	doc := pwaManifestDoc{
		ID:              pwaPrefix(basePath, cfg.ID),
		Name:            cfg.Name,
		ShortName:       cfg.ShortName,
		Description:     cfg.Description,
		StartURL:        pwaPrefix(basePath, ds.localizeURL(ctx, cfg.StartURL)),
		Scope:           pwaPrefix(basePath, cfg.Scope),
		Display:         string(cfg.Display),
		ThemeColor:      cfg.ThemeColor,
		BackgroundColor: cfg.BackgroundColor,
	}
	for _, icon := range cfg.Icons {
		doc.Icons = append(doc.Icons, pwaManifestIcon{
			Src:     pwaPrefix(basePath, icon.Src),
			Sizes:   icon.Sizes,
			Type:    icon.Type,
			Purpose: string(icon.Purpose),
		})
	}
	return json.MarshalIndent(doc, "", "  ")
}

const pwaOfflinePath = "/__gofastr/pwa/offline"

// PWAOfflineHTML renders the offline fallback page in the document shell,
// outside the app layout. It returns "" when WithPWA was not configured.
func (ds *UIHost) PWAOfflineHTML() string {
	if ds.pwaConfig == nil {
		return ""
	}
	cfg := ds.resolvedPWA()
	ctx := context.Background()
	lang := i18n.DocLang(ctx)
	var body render.HTML
	if cfg.OfflineScreen != nil {
		lang = ""
		var err error
		body, err = component.SafeRenderCtx(ctx, cfg.OfflineScreen)
		if err != nil {
			slog.Warn("uihost: PWA offline screen render panicked; serving default offline page", "err", err)
			body = defaultPWAOfflineBody(ctx)
			lang = i18n.DocLang(ctx)
		}
	} else {
		body = defaultPWAOfflineBody(ctx)
	}
	shell := ds.documentShellLang(ctx, "", lang, pageOfflineDocTitle.Text(ctx, map[string]any{"app": cfg.Name}), `<main role="main">`+string(body)+`</main>`)
	return ds.injectChromeMode(ctx, shell, pwaOfflinePath, "", "", false)
}

func defaultPWAOfflineBody(ctx context.Context) render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(pageOfflineTitle.Text(ctx))) +
		html.Paragraph(html.TextConfig{}, render.Text(pageOfflineBody.Text(ctx)))
}

func (ds *UIHost) pwaVersion(manifest []byte, precache []string, offlineHTML string) string {
	h := sha256.New()
	h.Write(manifest)
	for _, p := range precache {
		h.Write([]byte(p))
		h.Write([]byte{0})
		if !strings.HasPrefix(p, "/__gofastr/") && p != "/manifest.webmanifest" {
			if b := ds.pwaStaticBytes(p); b != nil {
				h.Write(b)
				h.Write([]byte{0})
			}
		}
	}
	h.Write([]byte(offlineHTML))
	h.Write([]byte(runtime.MustRuntimeJS()))
	h.Write([]byte(ds.AppCSS()))
	h.Write([]byte(ds.GetActionJS()))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func (ds *UIHost) pwaStaticBytes(p string) []byte {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if rel == "" || rel == "." {
		return nil
	}
	if root := ds.staticRootOf(); root != nil {
		if b, err := root.ReadFile(filepath.FromSlash(rel)); err == nil {
			return b
		}
	}
	if ds.staticFS != nil {
		if b, err := fs.ReadFile(ds.staticFS, rel); err == nil {
			return b
		}
	}
	return nil
}

func pwaSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "app"
	}
	return slug
}

func pwaNameHash(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:12]
}

func pwaStaticHash(name, basePath string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + basePath))
	return hex.EncodeToString(sum[:])[:12]
}

// PWAServiceWorkerJS generates the live service worker, listing the
// manifests of i18n.Default's locales. Pages hear of a waiting update
// through the "gofastr:pwa-update" window event.
func (ds *UIHost) PWAServiceWorkerJS(basePath string) (string, error) {
	return ds.serviceWorkerJS(context.Background(), basePath)
}

func (ds *UIHost) serviceWorkerJS(ctx context.Context, basePath string) (string, error) {
	if ds.pwaConfig == nil {
		return "", fmt.Errorf("uihost: PWA not configured")
	}
	cfg := ds.resolvedPWA()
	offlineHTML := ds.PWAOfflineHTML()
	manifest, err := ds.PWAManifestJSON(context.Background(), "")
	if err != nil {
		return "", err
	}
	precache := ds.pwaPrecachePaths(ctx, cfg, offlineHTML, true)
	version := ds.pwaVersion(manifest, precache, offlineHTML)
	nameSlug := pwaSlug(cfg.Name)
	prefix := "gofastr-pwa-" + nameSlug + "." + pwaNameHash(cfg.Name) + "."
	oldPrefix := "gofastr-pwa-" + nameSlug + "-"

	precacheJSON, err := pwaMarshalPrefixed(basePath, precache)
	if err != nil {
		return "", err
	}
	shared, err := pwaSharedWorkerArgs(basePath, ds.pwaDenyPaths(cfg))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(pwaServiceWorkerTemplate,
		prefix+version,
		prefix,
		oldPrefix,
		shared.offlineURL,
		precacheJSON,
		shared.denyExactJSON,
		shared.denyPreJSON,
	), nil
}

// PWAStaticExport describes a completed static export to
// PWAStaticServiceWorkerJS.
type PWAStaticExport struct {
	Pages          []string // page URLs as the export's links name them; precached atomically
	Assets         []string // framework-generated files; precached atomically
	OptionalAssets []string // user static-dir files; precached best-effort
	ContentHash    string   // fingerprint of the exported bytes; a change rotates the cache
}

type pwaWorkerArgs struct {
	offlineURL    string
	denyExactJSON []byte
	denyPreJSON   []byte
}

func pwaSharedWorkerArgs(basePath string, deny []string) (pwaWorkerArgs, error) {
	denyExact := make([]string, len(deny))
	denyPrefix := make([]string, len(deny))
	for i, d := range deny {
		denyExact[i] = pwaPrefix(basePath, d)
		denyPrefix[i] = pwaPrefix(basePath, d) + "/"
	}
	denyExactJSON, err := json.Marshal(denyExact)
	if err != nil {
		return pwaWorkerArgs{}, err
	}
	denyPreJSON, err := json.Marshal(denyPrefix)
	if err != nil {
		return pwaWorkerArgs{}, err
	}
	return pwaWorkerArgs{
		offlineURL:    pwaPrefix(basePath, pwaOfflinePath),
		denyExactJSON: denyExactJSON,
		denyPreJSON:   denyPreJSON,
	}, nil
}

func pwaMarshalPrefixed(basePath string, paths []string) ([]byte, error) {
	prefixed := make([]string, len(paths))
	for i, p := range paths {
		prefixed[i] = pwaPrefix(basePath, p)
	}
	return json.Marshal(prefixed)
}

// PWAStaticServiceWorkerJS generates the worker for a static export: it
// precaches the whole exported site and serves navigations cache-first.
func (ds *UIHost) PWAStaticServiceWorkerJS(basePath string, export PWAStaticExport) (string, error) {
	if ds.pwaConfig == nil {
		return "", fmt.Errorf("uihost: PWA not configured")
	}
	cfg := ds.resolvedPWA()
	offlineHTML := ds.PWAOfflineHTML()
	manifest, err := ds.PWAManifestJSON(context.Background(), "")
	if err != nil {
		return "", err
	}
	deny := ds.pwaDenyPaths(cfg)
	seen := map[string]bool{}
	add := func(dst *[]string, paths ...string) {
		for _, p := range paths {
			if !seen[p] && pwaAllowedPrecache(p, deny) {
				seen[p] = true
				*dst = append(*dst, p)
			}
		}
	}
	var precache []string
	add(&precache, ds.pwaPrecachePaths(context.Background(), cfg, offlineHTML, false)...)
	add(&precache, export.Pages...)
	add(&precache, export.Assets...)
	theme := ds.activeTheme()
	for _, e := range registry.All() {
		add(&precache, "/__gofastr/comp/"+e.Name+".css?v="+e.VersionFor(theme))
	}
	sort.Strings(precache)
	var optional []string
	add(&optional, export.OptionalAssets...)
	sort.Strings(optional)

	versionInputs := append(append([]string(nil), precache...), optional...)
	versionInputs = append(versionInputs, "static-content:"+export.ContentHash)
	version := ds.pwaVersion(manifest, versionInputs, offlineHTML)
	nameSlug := pwaSlug(cfg.Name)
	pathSlug := "root"
	if basePath != "" {
		pathSlug = pwaSlug(basePath)
	}
	prefix := "gofastr-pwa-static-" + nameSlug + "-" + pathSlug + "." + pwaStaticHash(cfg.Name, basePath) + "."
	oldPrefix := "gofastr-pwa-static-" + nameSlug + "-" + pathSlug + "-"

	precacheJSON, err := pwaMarshalPrefixed(basePath, precache)
	if err != nil {
		return "", err
	}
	optionalJSON, err := pwaMarshalPrefixed(basePath, optional)
	if err != nil {
		return "", err
	}
	shared, err := pwaSharedWorkerArgs(basePath, deny)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(pwaStaticServiceWorkerTemplate,
		prefix+version,
		prefix,
		oldPrefix,
		shared.offlineURL,
		precacheJSON,
		optionalJSON,
		shared.denyExactJSON,
		shared.denyPreJSON,
	), nil
}

const pwaServiceWorkerTemplate = `/* goFastr service worker: generated by uihost.WithPWA. Do not edit. */
var CACHE_NAME = %q;
var CACHE_PREFIX = %q;
var OLD_PREFIX = %q;
var OFFLINE_URL = %q;
var PRECACHE = %s;
var DENY_EXACT = %s;
var DENY_PREFIX = %s;

self.addEventListener("install", function (event) {
  // Each response is re-wrapped into a fresh Response before caching:
  // static hosts answer the extension-less offline URL with a 301 to
  // its index.html, and a cached redirected response is rejected when
  // used to answer a navigation fetch (redirect mode "manual").
  event.waitUntil(caches.open(CACHE_NAME).then(function (cache) {
    return Promise.all(PRECACHE.map(function (u) {
      return fetch(u, { cache: "no-cache" }).then(function (r) {
        if (!r.ok) throw new Error("precache " + u + ": " + r.status);
        return r.blob().then(function (body) {
          return cache.put(u, new Response(body, { status: 200, headers: r.headers }));
        });
      });
    }));
  }));
});

self.addEventListener("activate", function (event) {
  event.waitUntil(caches.keys().then(function (names) {
    return Promise.all(names.filter(function (n) {
      if (n === CACHE_NAME) return false;
      if (n.indexOf(CACHE_PREFIX) === 0) return true;
      // Migration: a pre-hash deploy left an orphaned own cache named
      // "<OLD_PREFIX><hex-version>" (OLD_PREFIX = gofastr-pwa-<slug>-).
      // Delete it. The all-hex remainder test is what makes this safe:
      // a sibling whose slug extends ours (e.g. app "Acme Docs" seen
      // by "Acme") leaves a "docs-<hex>" remainder containing non-hex,
      // so its cache survives. The identical-slug case (static worker
      // basePaths "/a/b" vs "/ab", which slug identically) was already
      // mutually wiping before the hash existed and cannot be migrated
      // apart: left for manual cleanup.
      return n.indexOf(OLD_PREFIX) === 0 && /^[0-9a-f]+$/.test(n.slice(OLD_PREFIX.length));
    }).map(function (n) { return caches.delete(n); }));
  }).then(function () { return self.clients.claim(); }));
});

function denied(pathname) {
  if (DENY_EXACT.indexOf(pathname) !== -1) return true;
  for (var i = 0; i < DENY_PREFIX.length; i++) {
    if (pathname.indexOf(DENY_PREFIX[i]) === 0) return true;
  }
  return false;
}

self.addEventListener("fetch", function (event) {
  var req = event.request;
  if (req.method !== "GET") return;
  var url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (denied(url.pathname)) return;
  if (req.mode === "navigate") {
    // Documents are network-first and never cached: rendered HTML can
    // be personalized. Offline falls back to the precached screen.
    event.respondWith(fetch(req).catch(function () {
      return caches.match(OFFLINE_URL, { cacheName: CACHE_NAME });
    }));
    return;
  }
  // Assets. URLs are matched exactly; nothing is ever added to the
  // cache at runtime, so Cache Storage only ever holds the versioned
  // app shell. Content-addressed URLs (?v=<hash>) are immutable:
  // cache-first, and a new deployment's URLs miss the old cache and
  // reach the network. Everything else is network-first so fresh HTML
  // never pairs with a previous deployment's runtime/CSS: the cache
  // answers only when the network is gone.
  if (url.searchParams.has("v")) {
    event.respondWith(caches.match(req, { cacheName: CACHE_NAME }).then(function (hit) {
      return hit || fetch(req);
    }));
    return;
  }
  event.respondWith(fetch(req).catch(function (err) {
    return caches.match(req, { cacheName: CACHE_NAME }).then(function (hit) {
      if (hit) return hit;
      throw err;
    });
  }));
});
`

const pwaStaticServiceWorkerTemplate = `/* goFastr service worker (static full-site): generated by uihost.WithPWA + static export. Do not edit. */
var CACHE_NAME = %q;
var CACHE_PREFIX = %q;
var OLD_PREFIX = %q;
var OFFLINE_URL = %q;
var PRECACHE = %s;
var PRECACHE_OPT = %s;
var DENY_EXACT = %s;
var DENY_PREFIX = %s;

function precacheOne(cache, u) {
  // Each response is re-wrapped into a fresh Response before caching:
  // static hosts answer extension-less URLs with a 301 to index.html,
  // and a cached redirected response is rejected when used to answer a
  // navigation fetch (redirect mode "manual").
  return fetch(u, { cache: "no-cache" }).then(function (r) {
    if (!r.ok) throw new Error("precache " + u + ": " + r.status);
    return r.blob().then(function (body) {
      return cache.put(u, new Response(body, { status: 200, headers: r.headers }));
    });
  });
}

self.addEventListener("install", function (event) {
  event.waitUntil(caches.open(CACHE_NAME).then(function (cache) {
    // Pages and framework assets install atomically: the offline
    // guarantee is all-or-nothing for them. User static-dir files are
    // best-effort: one un-servable file must not fail the install and
    // pin every client to the previous deployment.
    return Promise.all(PRECACHE.map(function (u) {
      return precacheOne(cache, u);
    })).then(function () {
      return Promise.all(PRECACHE_OPT.map(function (u) {
        return precacheOne(cache, u).catch(function () {});
      }));
    });
  }));
});

self.addEventListener("activate", function (event) {
  event.waitUntil(caches.keys().then(function (names) {
    return Promise.all(names.filter(function (n) {
      if (n === CACHE_NAME) return false;
      if (n.indexOf(CACHE_PREFIX) === 0) return true;
      // Migration: a pre-hash deploy left an orphaned own cache named
      // "<OLD_PREFIX><hex-version>". Delete it. The all-hex remainder
      // test is what makes this safe: a sibling whose slug extends ours
      // leaves a "<extra>-<hex>" remainder containing non-hex, so it
      // survives. The identical-slug case (basePaths "/a/b" vs "/ab",
      // which slug identically) was already mutually wiping before the
      // hash existed and cannot be migrated apart: left for manual
      // cleanup.
      return n.indexOf(OLD_PREFIX) === 0 && /^[0-9a-f]+$/.test(n.slice(OLD_PREFIX.length));
    }).map(function (n) { return caches.delete(n); }));
  }).then(function () { return self.clients.claim(); }));
});

function denied(pathname) {
  if (DENY_EXACT.indexOf(pathname) !== -1) return true;
  for (var i = 0; i < DENY_PREFIX.length; i++) {
    if (pathname.indexOf(DENY_PREFIX[i]) === 0) return true;
  }
  return false;
}

// Static hosts 301 extension-less pages to their trailing-slash form,
// so history/bookmark URLs may carry a slash the precache key lacks
// (or vice versa). Try the exact pathname, then the toggled form.
function matchPage(pathname) {
  return caches.match(pathname, { cacheName: CACHE_NAME }).then(function (hit) {
    if (hit) return hit;
    var alt = pathname.length > 1 && pathname.charAt(pathname.length - 1) === "/"
      ? pathname.slice(0, -1)
      : pathname + "/";
    return caches.match(alt, { cacheName: CACHE_NAME });
  });
}

self.addEventListener("fetch", function (event) {
  var req = event.request;
  if (req.method !== "GET") return;
  var url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (denied(url.pathname)) return;
  if (req.mode === "navigate") {
    // Navigations are cache-first: the exported page set is immutable,
    // every page was precached at install, and a new deployment ships a
    // byte-different worker whose install repopulates a fresh cache.
    // Network is the fallback for anything outside the precache; the
    // offline screen is the last resort.
    event.respondWith(matchPage(url.pathname).then(function (hit) {
      if (hit) return hit;
      return fetch(req).catch(function () {
        return caches.match(OFFLINE_URL, { cacheName: CACHE_NAME });
      });
    }));
    return;
  }
  // Assets: cache-first with network fallback. URLs are matched
  // exactly, query string included; content-addressed ?v= URLs from a
  // new deployment miss the old cache and reach the network.
  event.respondWith(caches.match(req, { cacheName: CACHE_NAME }).then(function (hit) {
    return hit || fetch(req);
  }));
});
`

// PWARegisterJS generates the registration script, which dispatches
// "gofastr:pwa-update" on window when a new worker is waiting. It returns
// "" when WithPWA was not configured.
func (ds *UIHost) PWARegisterJS(basePath string) string {
	if ds.pwaConfig == nil {
		return ""
	}
	cfg := ds.resolvedPWA()
	return fmt.Sprintf(pwaRegisterTemplate,
		pwaPrefix(basePath, "/service-worker.js"),
		pwaPrefix(basePath, cfg.Scope),
	)
}

const pwaRegisterTemplate = `/* goFastr PWA registration: generated by uihost.WithPWA. Do not edit. */
(function () {
  if (!("serviceWorker" in navigator)) return;
  window.addEventListener("load", function () {
    var url = %q, G = window.__gofastr;
    navigator.serviceWorker.register(G && G._scriptURL ? G._scriptURL(url) : url, { scope: %q }).then(function (reg) {
      reg.addEventListener("updatefound", function () {
        var next = reg.installing;
        if (!next) return;
        next.addEventListener("statechange", function () {
          if (next.state === "installed" && navigator.serviceWorker.controller) {
            window.dispatchEvent(new CustomEvent("gofastr:pwa-update", { detail: { registration: reg } }));
          }
        });
      });
    }).catch(function () { /* registration is best-effort */ });
  });
})();
`

func (ds *UIHost) handlePWAManifest(w http.ResponseWriter, r *http.Request) {
	body, err := ds.PWAManifestJSON(r.Context(), "")
	if err != nil {
		i18n.HTTPError(w, r, http.StatusNotFound, reasonPWANotConfigured)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

func (ds *UIHost) handlePWAServiceWorker(w http.ResponseWriter, r *http.Request) {
	ds.pwaSWOnce.Do(func() {
		ds.pwaSW, ds.pwaSWErr = ds.serviceWorkerJS(r.Context(), "")
	})
	if ds.pwaSWErr != nil {
		i18n.HTTPError(w, r, http.StatusNotFound, reasonPWANotConfigured)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	//gofastr:allow(nostore) the worker is built once under context.Background() (offline page included): deployment-constant, no caller to vary on
	fmt.Fprint(w, ds.pwaSW)
}

func (ds *UIHost) handlePWARegisterJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ds.PWARegisterJS(""))
}

func (ds *UIHost) handlePWAOffline(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	//gofastr:allow(nostore) PWAOfflineHTML renders under context.Background() for the precache: deployment-constant, no caller to vary on
	fmt.Fprint(w, ds.PWAOfflineHTML())
}
