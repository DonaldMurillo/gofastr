package uihost

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func pwaGet(t *testing.T, ds *UIHost, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	return w
}

func pwaManifest(t *testing.T, ds *UIHost) map[string]any {
	t.Helper()
	w := pwaGet(t, ds, "/manifest.webmanifest")
	if w.Code != 200 {
		t.Fatalf("manifest: expected 200, got %d", w.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", err, w.Body.String())
	}
	return m
}

func TestPWARoutes404WithoutOption(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	for _, p := range []string{
		"/manifest.webmanifest",
		"/service-worker.js",
		"/__gofastr/pwa/register.js",
		"/__gofastr/pwa/offline",
	} {
		if w := pwaGet(t, ds, p); w.Code == 200 {
			t.Errorf("%s should not be served without WithPWA, got 200", p)
		}
	}
}

func TestPWAManifestDefaults(t *testing.T) {
	a := app.NewApp("Meridian")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	m := pwaManifest(t, ds)
	if m["name"] != "Meridian" {
		t.Errorf("name should default to the app name, got %v", m["name"])
	}
	if m["start_url"] != "/" {
		t.Errorf("start_url should default to /, got %v", m["start_url"])
	}
	if m["scope"] != "/" {
		t.Errorf("scope should default to /, got %v", m["scope"])
	}
	if m["display"] != "standalone" {
		t.Errorf("display should default to standalone, got %v", m["display"])
	}
	if m["id"] != "/" {
		t.Errorf("id should default to start_url, got %v", m["id"])
	}
}

func TestPWAManifestFull(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{
		ID:              "/app",
		Name:            "Acme Tracker",
		ShortName:       "Acme",
		Description:     "Track the things",
		StartURL:        "/app",
		Scope:           "/app",
		Display:         PWADisplayMinimalUI,
		ThemeColor:      "#112233",
		BackgroundColor: "#ffffff",
		Icons: []PWAIcon{
			{Src: "/static/icon-192.png", Sizes: "192x192", Type: "image/png"},
			{Src: "/static/icon-maskable.png", Sizes: "512x512", Type: "image/png", Purpose: PWAIconPurposeMaskable},
		},
	}))
	m := pwaManifest(t, ds)
	if m["short_name"] != "Acme" || m["description"] != "Track the things" {
		t.Errorf("short_name/description missing: %v", m)
	}
	if m["display"] != "minimal-ui" {
		t.Errorf("display: got %v", m["display"])
	}
	if m["theme_color"] != "#112233" || m["background_color"] != "#ffffff" {
		t.Errorf("colors: got %v / %v", m["theme_color"], m["background_color"])
	}
	icons, ok := m["icons"].([]any)
	if !ok || len(icons) != 2 {
		t.Fatalf("expected 2 icons, got %v", m["icons"])
	}
	second := icons[1].(map[string]any)
	if second["purpose"] != "maskable" {
		t.Errorf("expected maskable purpose, got %v", second["purpose"])
	}
	first := icons[0].(map[string]any)
	if _, has := first["purpose"]; has {
		t.Errorf("empty purpose should be omitted, got %v", first)
	}
}

func TestPWAManifestEscapesJSON(t *testing.T) {
	name := `Acme "Co" </script>`
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{Name: name}))
	m := pwaManifest(t, ds)
	if m["name"] != name {
		t.Errorf("name should round-trip through JSON escaping, got %v", m["name"])
	}
	body := pwaGet(t, ds, "/manifest.webmanifest").Body.String()
	if strings.Contains(body, "</script>") {
		t.Errorf("manifest must not contain a raw </script>: %s", body)
	}
}

func TestPWAManifestHeaders(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	w := pwaGet(t, ds, "/manifest.webmanifest")
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/manifest+json") {
		t.Errorf("expected application/manifest+json, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected no-cache, got %q", cc)
	}
}

func TestPWAServiceWorkerServed(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	w := pwaGet(t, ds, "/service-worker.js")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content type, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("service worker must be no-cache, got %q", cc)
	}
	body := w.Body.String()
	if !strings.Contains(body, "gofastr-pwa-") {
		t.Errorf("cache name should carry the gofastr-pwa- ownership prefix:\n%s", body)
	}
	if !strings.Contains(body, "/__gofastr/pwa/offline") {
		t.Errorf("service worker should reference the offline fallback:\n%s", body)
	}
	if strings.Contains(body, "skipWaiting") {
		t.Errorf("service worker must not force skipWaiting:\n%s", body)
	}
}

func TestPWAServiceWorkerPrecache(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{
		Precache: []string{"/static/hero.png"},
		Icons:    []PWAIcon{{Src: "/static/icon-192.png", Sizes: "192x192", Type: "image/png"}},
	}))
	body := pwaGet(t, ds, "/service-worker.js").Body.String()
	for _, want := range []string{
		"/__gofastr/runtime.js",
		"/__gofastr/color-scheme.js",
		"/__gofastr/app.css",
		"/static/hero.png",
		"/static/icon-192.png",
	} {
		if !strings.Contains(body, `"`+want+`"`) {
			t.Errorf("precache should include %s:\n%s", want, body)
		}
	}
}

func TestPWAServiceWorkerDeterministic(t *testing.T) {
	build := func(precache ...string) string {
		a := app.NewApp("x")
		a.Register("/", &plainComp{}, nil)
		ds := New(a, WithPWA(PWAConfig{Precache: precache}))
		return pwaGet(t, ds, "/service-worker.js").Body.String()
	}
	one, two := build("/static/a.png"), build("/static/a.png")
	if one != two {
		t.Errorf("identical config must produce identical service-worker bytes")
	}
	cacheName := func(sw string) string {
		i := strings.Index(sw, "gofastr-pwa-")
		if i < 0 {
			t.Fatalf("no cache name in sw:\n%s", sw)
		}
		return sw[i : i+strings.IndexAny(sw[i:], `"'`)]
	}
	if cacheName(one) == cacheName(build("/static/b.png")) {
		t.Errorf("changed precache set must rotate the cache version")
	}
}

func TestPWAPrecacheDropsSensitive(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{Precache: []string{
		"/__gofastr/sse",
		"/__gofastr/session",
		"/__gofastr/action",
		"/api/users",
		"/auth/login",
		"https://evil.example/x.js",
		"//evil.example/x.js",
		"relative/no-slash",
		"/static/ok.png",
	}}))
	body := pwaGet(t, ds, "/service-worker.js").Body.String()
	// Parse the PRECACHE array out of the worker source — the deny
	// list legitimately repeats the sensitive paths, so asserting on
	// the whole body would be meaningless.
	i := strings.Index(body, "var PRECACHE = ")
	if i < 0 {
		t.Fatalf("no PRECACHE array in sw:\n%s", body)
	}
	line := body[i+len("var PRECACHE = "):]
	line = line[:strings.Index(line, ";\n")]
	var precache []string
	if err := json.Unmarshal([]byte(line), &precache); err != nil {
		t.Fatalf("PRECACHE is not a JSON array: %v\n%s", err, line)
	}
	got := map[string]bool{}
	for _, p := range precache {
		got[p] = true
	}
	// The only precache survivor from the list above is /static/ok.png.
	if !got["/static/ok.png"] {
		t.Errorf("safe static entry should survive: %v", precache)
	}
	for _, banned := range []string{
		"/__gofastr/sse",
		"/__gofastr/session",
		"/__gofastr/action",
		"/api/users",
		"/auth/login",
		"https://evil.example/x.js",
		"//evil.example/x.js",
		"relative/no-slash",
	} {
		if got[banned] {
			t.Errorf("sensitive/unsafe precache entry %s must be dropped: %v", banned, precache)
		}
	}
}

func TestPWAServiceWorkerDenyList(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	body := pwaGet(t, ds, "/service-worker.js").Body.String()
	for _, deny := range []string{
		"/__gofastr/sse",
		"/__gofastr/session",
		"/__gofastr/action",
		"/api/",
		"/auth/",
	} {
		if !strings.Contains(body, deny) {
			t.Errorf("fetch deny-list should cover %s:\n%s", deny, body)
		}
	}
}

func TestPWARegisterScript(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	w := pwaGet(t, ds, "/__gofastr/pwa/register.js")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `register("/service-worker.js"`) {
		t.Errorf("register.js should register the root service worker:\n%s", body)
	}
	if !strings.Contains(body, "gofastr:pwa-update") {
		t.Errorf("register.js should dispatch the update event:\n%s", body)
	}
	if strings.Contains(body, "skipWaiting") {
		t.Errorf("register.js must not force skipWaiting:\n%s", body)
	}
}

func TestPWAOfflineDefault(t *testing.T) {
	a := app.NewApp("Meridian")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	w := pwaGet(t, ds, "/__gofastr/pwa/offline")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected html content type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(strings.ToLower(body), "offline") {
		t.Errorf("default offline screen should mention being offline:\n%s", body)
	}
	if !strings.Contains(body, "/__gofastr/app.css") {
		t.Errorf("offline page should carry the app chrome (app.css):\n%s", body)
	}
}

type offlineMarkerComp struct{}

func (offlineMarkerComp) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.Text("custom-offline-marker"))
}

func TestPWAOfflineCustomScreen(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{OfflineScreen: offlineMarkerComp{}}))
	body := pwaGet(t, ds, "/__gofastr/pwa/offline").Body.String()
	if !strings.Contains(body, "custom-offline-marker") {
		t.Errorf("configured OfflineScreen should render:\n%s", body)
	}
}

func TestPWAHeadTags(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{ThemeColor: "#123456"}))
	body := pwaGet(t, ds, "/").Body.String()
	if !strings.Contains(body, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Errorf("page should link the manifest:\n%s", body)
	}
	if !strings.Contains(body, `src="/__gofastr/pwa/register.js"`) {
		t.Errorf("page should load the external register script:\n%s", body)
	}
	if !strings.Contains(body, `<meta name="theme-color" content="#123456">`) {
		t.Errorf("page should carry the PWA theme color:\n%s", body)
	}
}

// pwaSWPrecache parses the PRECACHE JSON array out of a generated
// service worker.
func pwaSWPrecache(t *testing.T, sw string) []string {
	t.Helper()
	i := strings.Index(sw, "var PRECACHE = ")
	if i < 0 {
		t.Fatalf("no PRECACHE array in sw:\n%s", sw)
	}
	line := sw[i+len("var PRECACHE = "):]
	line = line[:strings.Index(line, ";\n")]
	var precache []string
	if err := json.Unmarshal([]byte(line), &precache); err != nil {
		t.Fatalf("PRECACHE is not a JSON array: %v\n%s", err, line)
	}
	return precache
}

var pwaStyleSeq atomic.Int64

// newPWAStyles registers n unique component styles (NOT LoadAlways —
// the process-global registry has no reset, and eager styles would
// leak into every other test's CSS bundle; unique names per test run,
// same convention as framework/static's registry tests).
func newPWAStyles(t *testing.T, n int) []*registry.Style {
	t.Helper()
	styles := make([]*registry.Style, n)
	for i := range styles {
		name := fmt.Sprintf("pwa-comp-%d", pwaStyleSeq.Add(1))
		styles[i] = registry.RegisterStyle(name, func(theme style.Theme) string {
			return style.NewComponentSheet(name, theme).
				Rule(".x").Set("color", "red").End().
				MustBuild()
		})
	}
	return styles
}

// styledOfflineComp renders two styled components — enough to trigger
// bundling on the offline page under the old bundle=true chrome.
type styledOfflineComp struct{ styles []*registry.Style }

func (c styledOfflineComp) Render() render.HTML {
	out := render.HTML("<p>offline</p>")
	for _, st := range c.styles {
		out += st.WrapHTML(render.HTML(`<div class="x">x</div>`))
	}
	return out
}

// TestPWAOfflineNoBundleCSS: the offline page must link per-component
// CSS directly, never the query-parameterized bundle endpoint — the
// bundle URL depends on the page's component set, poisons the cache
// for every other page's bundle request, and does not exist at all in
// static exports.
func TestPWAOfflineNoBundleCSS(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{OfflineScreen: styledOfflineComp{newPWAStyles(t, 2)}}))
	offline := ds.PWAOfflineHTML()
	if strings.Contains(offline, "comp-bundle.css") {
		t.Errorf("offline page must not reference the bundle endpoint:\n%s", offline)
	}
	if !strings.Contains(offline, "/__gofastr/comp/") {
		t.Errorf("offline page should link per-component CSS directly:\n%s", offline)
	}
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pwaSWPrecache(t, sw) {
		if strings.Contains(p, "comp-bundle.css") {
			t.Errorf("precache must not contain a bundle URL: %s", p)
		}
	}
}

// TestPWAServiceWorkerExactMatching: no ignoreSearch — matching a
// content-addressed URL against a different version's body serves
// stale code; split-module precache entries must instead carry their
// real ?v=<hash> so exact matching works offline.
func TestPWAServiceWorkerExactMatching(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sw, "ignoreSearch") {
		t.Errorf("service worker must match URLs exactly (no ignoreSearch):\n%s", sw)
	}
	sawModule := false
	for _, p := range pwaSWPrecache(t, sw) {
		if strings.HasPrefix(p, "/__gofastr/runtime/") {
			sawModule = true
			if !strings.Contains(p, "?v=") {
				t.Errorf("module precache entry should be content-addressed: %s", p)
			}
		}
	}
	if !sawModule {
		t.Errorf("precache should include the split runtime modules")
	}
}

// TestPWAServiceWorkerRewrapsPrecache: install must store re-wrapped
// responses (cache.put + new Response), not cache.addAll — a static
// host answers the extension-less offline URL with a 301, and a cached
// redirected response is rejected for navigation fetches.
func TestPWAServiceWorkerRewrapsPrecache(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sw, "addAll") {
		t.Errorf("install must not use cache.addAll (keeps the redirected flag):\n%s", sw)
	}
	if !strings.Contains(sw, "cache.put") || !strings.Contains(sw, "new Response") {
		t.Errorf("install should cache.put re-wrapped responses:\n%s", sw)
	}
}

// TestPWAVersionTracksAssetBytes: replacing a precached static asset
// in place (same path, new bytes — e.g. swapping the placeholder icons
// for real branding) must rotate the cache version.
func TestPWAVersionTracksAssetBytes(t *testing.T) {
	build := func(iconBytes []byte) string {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "icons"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "icons", "icon-192.png"), iconBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		a := app.NewApp("x")
		a.Register("/", &plainComp{}, nil)
		ds := New(a,
			WithStaticDir(dir),
			WithPWA(PWAConfig{Icons: []PWAIcon{{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"}}}),
		)
		sw, err := ds.PWAServiceWorkerJS("")
		if err != nil {
			t.Fatal(err)
		}
		return sw
	}
	one := build([]byte("placeholder-png"))
	two := build([]byte("real-branding-png"))
	name := func(sw string) string {
		i := strings.Index(sw, "gofastr-pwa-")
		return sw[i : i+strings.IndexAny(sw[i:], `"'`)]
	}
	if name(one) == name(two) {
		t.Errorf("changed asset bytes at the same path must rotate the cache version")
	}
}

// TestPWADenyPathsExtendDenyList: apps with a custom API prefix (or
// auth mount) extend the sensitive-path deny list; entries are both
// un-precacheable and never intercepted.
func TestPWADenyPathsExtendDenyList(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{
		DenyPaths: []string{"/v1"},
		Precache:  []string{"/v1/products", "/static/ok.png"},
	}))
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pwaSWPrecache(t, sw) {
		if p == "/v1/products" {
			t.Errorf("DenyPaths entry must be un-precacheable: %v", p)
		}
	}
	if !strings.Contains(sw, `"/v1"`) || !strings.Contains(sw, `"/v1/"`) {
		t.Errorf("deny arrays should carry the custom path:\n%s", sw)
	}
}

func TestPWAGeneratorsNilSafeWithoutOption(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	if got := ds.PWARegisterJS(""); got != "" {
		t.Errorf("PWARegisterJS without WithPWA should return empty, got %q", got)
	}
	if got := ds.PWAOfflineHTML(); got != "" {
		t.Errorf("PWAOfflineHTML without WithPWA should return empty, got %q", got)
	}
	if _, err := ds.PWAManifestJSON(""); err == nil {
		t.Errorf("PWAManifestJSON without WithPWA should error")
	}
	if _, err := ds.PWAServiceWorkerJS(""); err == nil {
		t.Errorf("PWAServiceWorkerJS without WithPWA should error")
	}
}

func TestPWANoHeadTagsWithoutOption(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	body := pwaGet(t, ds, "/").Body.String()
	if strings.Contains(body, "manifest.webmanifest") || strings.Contains(body, "/__gofastr/pwa/") {
		t.Errorf("no PWA markup should be emitted without WithPWA:\n%s", body)
	}
}

// Static exports are a closed, immutable page set: the static worker
// precaches the WHOLE site at install and serves navigations cache-first,
// so an installed PWA works fully offline. The version must track exported
// CONTENT (not just the path list) or a redeploy with edited pages would
// never rotate the cache.
func TestStaticServiceWorkerPrecachesWholeSite(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAStaticServiceWorkerJS("", PWAStaticExport{
		Pages:          []string{"/", "/docs", "/api/tokens"},
		Assets:         []string{"/static/hero.png"},
		OptionalAssets: []string{"/media/big-video.mp4"},
		ContentHash:    "content-hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/"`, `"/docs"`, `"/static/hero.png"`, `"/__gofastr/runtime.js"`, "/__gofastr/pwa/offline"} {
		if !strings.Contains(sw, want) {
			t.Errorf("static worker missing %s:\n%s", want, sw)
		}
	}
	if strings.Contains(sw, "/api/tokens") {
		t.Errorf("denied path must never be precached:\n%s", sw)
	}
	if strings.Contains(sw, "skipWaiting") {
		t.Errorf("static worker must not force skipWaiting:\n%s", sw)
	}
	if strings.Contains(sw, "ignoreSearch") {
		t.Errorf("URL matching stays exact — no ignoreSearch:\n%s", sw)
	}
	// Same page set, different content → the cache version must rotate.
	sw2, err := ds.PWAStaticServiceWorkerJS("", PWAStaticExport{
		Pages:          []string{"/", "/docs", "/api/tokens"},
		Assets:         []string{"/static/hero.png"},
		OptionalAssets: []string{"/media/big-video.mp4"},
		ContentHash:    "content-hash-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	name := func(s string) string {
		i := strings.Index(s, "gofastr-pwa-")
		j := i + strings.IndexAny(s[i:], `"`)
		return s[i:j]
	}
	if name(sw) == name(sw2) {
		t.Error("content change must rotate the cache version")
	}
}

func TestStaticServiceWorkerHonorsBasePath(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAStaticServiceWorkerJS("/sub", PWAStaticExport{Pages: []string{"/docs"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sw, `"/sub/docs"`) {
		t.Errorf("basePath not applied to precached pages:\n%s", sw)
	}
}

func TestStaticServiceWorkerRequiresPWA(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	if _, err := New(a).PWAStaticServiceWorkerJS("", PWAStaticExport{ContentHash: "h"}); err == nil {
		t.Fatal("expected error without WithPWA")
	}
}

// Review-driven contracts for the static worker: versioned component CSS in
// the precache (pages request ?v= URLs and matching is exact), best-effort
// tier for user static-dir files, slash-tolerant navigation lookup, and a
// cache prefix that can never collide with the live worker's.
func TestStaticServiceWorkerReviewContracts(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAStaticServiceWorkerJS("", PWAStaticExport{
		Pages:          []string{"/"},
		OptionalAssets: []string{"/media/huge.mp4"},
		ContentHash:    "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Component CSS must be precached under the SAME ?v= form pages request.
	if !strings.Contains(sw, `/__gofastr/comp/`) || !strings.Contains(sw, `.css?v=`) {
		t.Errorf("versioned component CSS missing from precache:\n%s", sw)
	}
	// Optional tier: present, and installed best-effort (failure swallowed).
	if !strings.Contains(sw, `"/media/huge.mp4"`) || !strings.Contains(sw, "PRECACHE_OPT") {
		t.Errorf("optional best-effort precache tier missing:\n%s", sw)
	}
	if !strings.Contains(sw, `.catch(function () {})`) {
		t.Errorf("optional entries must not fail the install:\n%s", sw)
	}
	// Slash-tolerant navigation lookup.
	if !strings.Contains(sw, "matchPage") || !strings.Contains(sw, `pathname + "/"`) {
		t.Errorf("navigation lookup must tolerate trailing-slash redirects:\n%s", sw)
	}
	// Prefix isolation: the live worker cleans caches under
	// "gofastr-pwa-<slug>-"; the static prefix must NOT fall under it.
	liveSW, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	prefix := func(s string) string {
		i := strings.Index(s, "var CACHE_PREFIX = \"")
		rest := s[i+len("var CACHE_PREFIX = \""):]
		return rest[:strings.Index(rest, `"`)]
	}
	livePrefix, staticPrefix := prefix(liveSW), prefix(sw)
	if strings.HasPrefix(staticPrefix, livePrefix) || strings.HasPrefix(livePrefix, staticPrefix) {
		t.Errorf("live (%q) and static (%q) cache prefixes overlap — activates would delete each other's caches", livePrefix, staticPrefix)
	}
	// Two static exports at different subpaths stay isolated too.
	swSub, err := ds.PWAStaticServiceWorkerJS("/docs", PWAStaticExport{Pages: []string{"/"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if prefix(swSub) == staticPrefix {
		t.Errorf("static exports at different basePaths must not share a cache prefix")
	}
}
