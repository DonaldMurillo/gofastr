package uihost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── PWA ───────────────────────────────────────────────────────────
//
// WithPWA turns a UIHost app into an installable Progressive Web App:
// a typed web app manifest, a CSP-safe external service worker with a
// versioned app-shell precache, an offline fallback screen, and the
// registration script — without the host app hand-wiring any of it.
//
// The service worker is deliberately conservative: document navigations
// are network-first and NEVER cached (rendered HTML can be
// personalized), and only the goFastr runtime/app shell, the manifest,
// declared icons, explicit Precache entries, and the offline screen
// live in Cache Storage. API, auth, session, action, signal, and SSE
// endpoints are never intercepted and can never be precached.

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

// PWAIcon declares one manifest icon. Src must be a same-origin
// root-absolute path (usually a static asset, e.g. "/static/icon-192.png").
// Chromium's installability check needs at least a 192x192 and a 512x512
// icon; add a maskable variant for adaptive home-screen shapes.
type PWAIcon struct {
	Src     string
	Sizes   string // e.g. "192x192"
	Type    string // e.g. "image/png"
	Purpose PWAIconPurpose
}

// PWAConfig configures WithPWA. Every field is optional; sensible
// defaults are derived at serve time:
//
//   - Name defaults to the core-ui app title.
//   - StartURL and Scope default to "/".
//   - ID defaults to StartURL.
//   - Display defaults to standalone.
//   - OfflineScreen defaults to a framework-provided offline notice.
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

	// Precache lists extra same-origin root-absolute paths (static
	// assets, fonts, hero images) to keep available offline alongside
	// the runtime/app shell. Entries that point at sensitive framework
	// endpoints (API, auth, session, action, signal, SSE) or at another
	// origin are dropped — the app-shell cache can never hold them.
	Precache []string

	// OfflineScreen renders the offline fallback page served at
	// /__gofastr/pwa/offline and shown when a navigation fails with no
	// network. The page is precached at service-worker install time, so
	// it must not render personalized content; it is deliberately NOT
	// wrapped in the app layout for the same reason. Nil uses the
	// framework default.
	OfflineScreen component.Component

	// DenyPaths extends the built-in sensitive-path deny list
	// (/__gofastr/{sse,session,signal,action,widgets}, /api, /auth)
	// with app-specific mounts — e.g. a CRUD API at a custom prefix or
	// an auth battery at a custom base path. Listed paths (and
	// everything under them) can never be precached and are never
	// intercepted by the service worker. Root-relative; a bare "/" is
	// ignored (it would deny the whole app).
	DenyPaths []string
}

// WithPWA opts the host into the installable-PWA surface: it mounts
// /manifest.webmanifest, /service-worker.js, /__gofastr/pwa/register.js,
// and /__gofastr/pwa/offline, and injects the manifest link + external
// registration script into every rendered page. The injection rides
// the generic headTags/extraScripts rails (all values are fixed at
// option time), so chrome assembly has a single code path.
func WithPWA(cfg PWAConfig) Option {
	return func(ds *UIHost) {
		ds.pwaConfig = &cfg
		ds.headTags = append(ds.headTags, `<link rel="manifest" href="/manifest.webmanifest">`)
		if cfg.ThemeColor != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="theme-color" content="%s">`, stdhtml.EscapeString(cfg.ThemeColor)))
		}
		ds.extraScripts = append(ds.extraScripts, "/__gofastr/pwa/register.js")
	}
}

// PWAEnabled reports whether WithPWA was configured. The static builder
// uses it to decide whether to emit the PWA assets.
func (ds *UIHost) PWAEnabled() bool {
	return ds.pwaConfig != nil
}

// resolvedPWA returns the config with defaults applied. Called at serve
// time so ds.App (the Name source) is guaranteed populated.
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

// pwaSensitivePaths are framework endpoints that must never be
// intercepted for caching or precached: they carry credentials,
// per-user state, or live streams. Matched as exact path or path
// prefix + "/" so /__gofastr/actions.js (a shell asset) stays allowed
// while /__gofastr/action (the server-action endpoint) is denied.
var pwaSensitivePaths = []string{
	"/__gofastr/sse",
	"/__gofastr/session",
	"/__gofastr/signal",
	"/__gofastr/action",
	"/__gofastr/widgets",
	"/api",
	"/auth",
}

// pwaDenyPaths returns the effective deny list: the built-in sensitive
// framework endpoints plus normalized config DenyPaths entries.
func pwaDenyPaths(cfg PWAConfig) []string {
	deny := append([]string(nil), pwaSensitivePaths...)
	for _, d := range cfg.DenyPaths {
		d = strings.TrimRight(strings.TrimSpace(d), "/")
		if d == "" || !strings.HasPrefix(d, "/") {
			continue // "" was a bare "/" (denies everything) or a non-root-relative value
		}
		deny = append(deny, d)
	}
	return deny
}

// pwaAllowedPrecache reports whether a Precache entry may enter the
// app-shell cache: same-origin root-absolute paths only, never a
// denied endpoint.
func pwaAllowedPrecache(p string, deny []string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	clean := p
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	for _, d := range deny {
		if clean == d || strings.HasPrefix(clean, d+"/") {
			return false
		}
	}
	return true
}

// pwaAssetURL matches root-absolute src/href attribute values in the
// rendered offline page so its stylesheet/script dependencies join the
// precache and the page renders styled with no network.
var pwaAssetURL = regexp.MustCompile(`(?:src|href)="(/[^"]+)"`)

// pwaPrecachePaths returns the deduplicated, sorted, basePath-neutral
// precache manifest: the runtime/app shell, the offline screen and its
// asset dependencies, the web manifest, declared icons, and the
// filtered user Precache list. The service worker matches URLs
// exactly, so entries the client requests with a content-addressed
// ?v=<hash> (split runtime modules, component CSS) are listed with
// that exact query.
func (ds *UIHost) pwaPrecachePaths(cfg PWAConfig, offlineHTML string) []string {
	set := map[string]bool{
		"/__gofastr/runtime.js":      true,
		"/__gofastr/color-scheme.js": true,
		"/manifest.webmanifest":      true,
		pwaOfflinePath:               true,
	}
	if ds.App != nil {
		set["/__gofastr/app.css"] = true
	}
	if ds.GetActionJS() != "" {
		set["/__gofastr/actions.js"] = true
	}
	// Split runtime modules are demand-loaded parts of the shell; an
	// offline app still needs theme switching, widgets, toasts, etc.
	// The loader requests them as <name>.js?v=<hash>, so cache them
	// under that exact URL.
	for _, name := range runtime.ModuleNames() {
		u := "/__gofastr/runtime/" + name + ".js"
		if hash := widget.RuntimeModuleHash(name); hash != "" {
			u += "?v=" + hash
		}
		set[u] = true
	}
	deny := pwaDenyPaths(cfg)
	// Whatever the offline page links (per-component CSS, app.css) must
	// be cached with it or it renders unstyled when the network is gone.
	for _, m := range pwaAssetURL.FindAllStringSubmatch(offlineHTML, -1) {
		if pwaAllowedPrecache(m[1], deny) {
			set[m[1]] = true
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

// pwaPrefix prepends basePath to a root-absolute same-origin path.
// External, protocol-relative, and already-relative values pass through
// untouched. No-op for the live server (basePath == "").
func pwaPrefix(basePath, p string) string {
	if basePath == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return p
	}
	return basePath + p
}

// pwaManifestDoc is the typed web app manifest shape. Emitted via
// encoding/json so every field is escaped correctly.
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

// PWAManifestJSON renders the web app manifest. basePath prefixes
// start_url, scope, id, and same-origin icon paths for static exports
// mounted under a subpath; pass "" for the live server.
func (ds *UIHost) PWAManifestJSON(basePath string) ([]byte, error) {
	if ds.pwaConfig == nil {
		return nil, fmt.Errorf("uihost: PWA not configured")
	}
	cfg := ds.resolvedPWA()
	doc := pwaManifestDoc{
		ID:              pwaPrefix(basePath, cfg.ID),
		Name:            cfg.Name,
		ShortName:       cfg.ShortName,
		Description:     cfg.Description,
		StartURL:        pwaPrefix(basePath, cfg.StartURL),
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

// PWAOfflineHTML renders the offline fallback page: the configured (or
// default) offline screen inside the standard document shell with the
// usual chrome (theme bootstrap, app.css, runtime). It is deliberately
// NOT wrapped in the app layout — the page is precached at
// service-worker install time, so nothing personalized may render into
// it. Returns "" when WithPWA was not configured.
func (ds *UIHost) PWAOfflineHTML() string {
	if ds.pwaConfig == nil {
		return ""
	}
	cfg := ds.resolvedPWA()
	var body render.HTML
	if cfg.OfflineScreen != nil {
		body = cfg.OfflineScreen.Render()
	} else {
		// Minimal semantic HTML, same treatment as the bare 404
		// fallback: typography comes from app.css, no component CSS.
		// (uihost deliberately does not import framework/ui — linking
		// it would force its LoadAlways styles into every host's CSS
		// bundle. Apps that want a richer screen pass OfflineScreen.)
		body = html.Heading(html.HeadingConfig{Level: 1}, render.Text("You're offline")) +
			html.Paragraph(html.TextConfig{}, render.Text("This page isn't available without a connection. Reconnect and try again."))
	}
	shell := fmt.Sprintf(
		`<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>Offline — %s</title></head><body><main role="main">%s</main></body></html>`,
		stdhtml.EscapeString(cfg.Name), string(body))
	// bundle=false (same as RenderStaticPage): component CSS must be
	// direct per-component links. The bundle URL depends on the page's
	// component set — precaching it would answer every OTHER page's
	// bundle request with the offline page's set, and static exports
	// don't emit the bundle endpoint at all.
	return ds.injectChromeMode(shell, pwaOfflinePath, "", "", false)
}

// pwaVersion fingerprints the deployment: the manifest, the precache
// URL list (content-addressed framework assets carry their hash in the
// URL), the offline page, the shell asset bodies, and the bytes of
// every precache entry resolvable from the host's static storage —
// so swapping an icon or a precached asset in place (same path, new
// bytes) rotates the cache. Entries served from elsewhere (a reverse
// proxy) contribute their URL only; give those a new path or query to
// bust returning clients. Identical deployments produce identical
// service workers.
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

// pwaStaticBytes resolves a precache path against the host's static
// storage (embedded FS first, then the static dir) and returns its
// bytes, or nil when the path isn't served from static storage. The
// path is cleaned to its root-relative form, so traversal segments
// can't escape the static root.
func (ds *UIHost) pwaStaticBytes(p string) []byte {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if rel == "" || rel == "." {
		return nil
	}
	if ds.staticFS != nil {
		if b, err := fs.ReadFile(ds.staticFS, rel); err == nil {
			return b
		}
	}
	if ds.staticDir != "" {
		if b, err := os.ReadFile(filepath.Join(ds.staticDir, filepath.FromSlash(rel))); err == nil {
			return b
		}
	}
	return nil
}

// pwaSlug derives the cache-ownership slug from the app name so
// activate only ever deletes caches belonging to this application.
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

// PWAServiceWorkerJS generates the service worker. The worker precaches
// the app shell under a deterministically versioned cache name, keeps
// document navigations network-first with the offline screen as
// fallback, never caches anything at runtime, and on activate deletes
// only obsolete caches owned by this application. It never calls
// skipWaiting — a new worker activates once existing tabs release the
// old one; pages hear about a waiting update via the
// "gofastr:pwa-update" window event dispatched by register.js.
func (ds *UIHost) PWAServiceWorkerJS(basePath string) (string, error) {
	if ds.pwaConfig == nil {
		return "", fmt.Errorf("uihost: PWA not configured")
	}
	cfg := ds.resolvedPWA()
	offlineHTML := ds.PWAOfflineHTML()
	manifest, err := ds.PWAManifestJSON("")
	if err != nil {
		return "", err
	}
	precache := ds.pwaPrecachePaths(cfg, offlineHTML)
	version := ds.pwaVersion(manifest, precache, offlineHTML)
	prefix := "gofastr-pwa-" + pwaSlug(cfg.Name) + "-"

	prefixed := make([]string, len(precache))
	for i, p := range precache {
		prefixed[i] = pwaPrefix(basePath, p)
	}
	precacheJSON, err := json.Marshal(prefixed)
	if err != nil {
		return "", err
	}
	deny := pwaDenyPaths(cfg)
	denyExact := make([]string, len(deny))
	denyPrefix := make([]string, len(deny))
	for i, d := range deny {
		denyExact[i] = pwaPrefix(basePath, d)
		denyPrefix[i] = pwaPrefix(basePath, d) + "/"
	}
	denyExactJSON, err := json.Marshal(denyExact)
	if err != nil {
		return "", err
	}
	denyPrefixJSON, err := json.Marshal(denyPrefix)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(pwaServiceWorkerTemplate,
		prefix+version,
		prefix,
		pwaPrefix(basePath, pwaOfflinePath),
		precacheJSON,
		denyExactJSON,
		denyPrefixJSON,
	), nil
}

// pwaServiceWorkerTemplate is the generated worker source. Kept ES5-safe
// and dependency-free; all dynamic values arrive as JSON literals.
const pwaServiceWorkerTemplate = `/* goFastr service worker — generated by uihost.WithPWA. Do not edit. */
var CACHE_NAME = %q;
var CACHE_PREFIX = %q;
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
      return n.indexOf(CACHE_PREFIX) === 0 && n !== CACHE_NAME;
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
    // Documents are network-first and never cached — rendered HTML can
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
  // never pairs with a previous deployment's runtime/CSS — the cache
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

// PWARegisterJS generates the external, CSP-safe registration script.
// Registration on every page load doubles as the update check; when a
// new worker is installed and waiting behind a controlling one, the
// script dispatches "gofastr:pwa-update" on window so apps can show an
// "update available" prompt. Returns "" when WithPWA was not configured.
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

const pwaRegisterTemplate = `/* goFastr PWA registration — generated by uihost.WithPWA. Do not edit. */
(function () {
  if (!("serviceWorker" in navigator)) return;
  window.addEventListener("load", function () {
    navigator.serviceWorker.register(%q, { scope: %q }).then(function (reg) {
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

// ─── Handlers ──────────────────────────────────────────────────────

func (ds *UIHost) handlePWAManifest(w http.ResponseWriter, _ *http.Request) {
	body, err := ds.PWAManifestJSON("")
	if err != nil {
		http.Error(w, "pwa not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

func (ds *UIHost) handlePWAServiceWorker(w http.ResponseWriter, _ *http.Request) {
	// The worker is deployment-constant, but generating it renders the
	// offline page and hashes the shell assets — too much to repeat on
	// every update check (browsers re-fetch the worker on page loads).
	// Memoized on first request: by then the app has rendered at least
	// one page, so the style registries have settled (same reasoning
	// as the deployment fingerprint itself).
	ds.pwaSWOnce.Do(func() {
		ds.pwaSW, ds.pwaSWErr = ds.PWAServiceWorkerJS("")
	})
	if ds.pwaSWErr != nil {
		http.Error(w, "pwa not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// The browser re-fetches the worker to detect updates; long-lived
	// caching here would pin every client to a stale deployment.
	w.Header().Set("Cache-Control", "no-cache")
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
	fmt.Fprint(w, ds.PWAOfflineHTML())
}
