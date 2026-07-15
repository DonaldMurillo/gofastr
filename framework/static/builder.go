package static

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Builder generates a static-site snapshot from a UIHost.
type Builder struct {
	// Host is the UIHost to render through. Must already have its screens,
	// actions, and static assets configured.
	Host *uihost.UIHost
	// OutDir is the destination directory; existing contents are overwritten.
	OutDir string
	// BasePath is the URL subpath the static site is served under (e.g.
	// "/gofastr" for a GitHub Pages project site at
	// https://user.github.io/gofastr/). Empty = apex (root-absolute
	// URLs, the default). When set, the builder rewrites every
	// root-absolute /__gofastr/... asset URL and internal nav link in
	// the emitted HTML, and bakes the prefix into runtime.js's
	// dynamically-constructed module URLs, so assets and navigation
	// resolve correctly under the mount path.
	BasePath string
	// Logger receives one line per produced or copied file. Nil disables it.
	Logger func(format string, args ...any)
}

// Result is a summary of a Build run.
type Result struct {
	Pages  []string // route paths rendered (post-expansion for dynamic routes)
	Assets []string // static asset paths copied
}

// Build renders every reachable route to HTML and copies runtime/actions/static
// assets into b.OutDir. Returns a Result that lists what was produced.
func (b *Builder) Build(ctx context.Context) (Result, error) {
	if b.Host == nil {
		return Result{}, fmt.Errorf("static: Host is required")
	}
	if strings.TrimSpace(b.OutDir) == "" {
		return Result{}, fmt.Errorf("static: OutDir is required")
	}
	if err := os.MkdirAll(b.OutDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("static: create out dir: %w", err)
	}

	res := Result{}

	// Pages.
	for _, route := range b.Host.App.Routes() {
		paths, err := expandRoute(ctx, b.Host.App, route.Path)
		if err != nil {
			return res, fmt.Errorf("static: expand %q: %w", route.Path, err)
		}
		for _, p := range paths {
			html, err := b.Host.RenderStaticPage(ctx, p)
			if err != nil {
				return res, fmt.Errorf("static: render %q: %w", p, err)
			}
			html = b.applyStaticMode(html)
			dst := filepath.Join(b.OutDir, pathToFile(p))
			if err := b.ensureContained(dst); err != nil {
				return res, err
			}
			if err := writeFile(dst, []byte(html)); err != nil {
				return res, err
			}
			b.log("rendered %s -> %s", p, dst)
			res.Pages = append(res.Pages, p)
		}
	}

	// LLM documentation — per-page llm.md and top-level index.
	if !b.Host.App.NoLLMMD {
		for _, route := range b.Host.App.Routes() {
			screen, _, ok := b.Host.App.Router.Resolve(route.Path)
			if !ok {
				continue
			}
			if screen.NoLLMMD {
				continue
			}
			paths, err := expandRoute(ctx, b.Host.App, route.Path)
			if err != nil {
				return res, fmt.Errorf("static: expand llm.md %q: %w", route.Path, err)
			}
			md := coreapp.ScreenLLMMD(screen)
			for _, p := range paths {
				dst := filepath.Join(b.OutDir, pathToLLMFile(p))
				if err := b.ensureContained(dst); err != nil {
					return res, err
				}
				if err := writeFile(dst, []byte(md)); err != nil {
					return res, err
				}
				res.Assets = append(res.Assets, "/"+filepath.ToSlash(pathToLLMFile(p)))
				b.log("wrote llm.md for %s -> %s", p, dst)
			}
		}
		// Top-level page index
		indexMD := coreapp.AppLLMMD(b.Host.App)
		if err := writeFile(filepath.Join(b.OutDir, "llm-pages.md"), []byte(indexMD)); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, "/llm-pages.md")
		b.log("wrote llm-pages.md index")
	}

	// SEO surface — sitemap.xml and robots.txt, mirroring the live
	// WithSitemap/WithRobots handlers so a static deploy keeps the same
	// crawler contract as the live server. Same bytes as the HTTP
	// handlers (single source: UIHost.SitemapXML / UIHost.RobotsTXT),
	// with BasePath folded into <loc> entries and the derived Sitemap:
	// URL. User-supplied files in the static source win, matching the
	// PWA-surface precedence below.
	if err := b.dumpSEOAssets(&res); err != nil {
		return res, err
	}

	// Generated app icons (uihost.WithAppIcon) — the /__gofastr/icons/
	// PNGs plus the /favicon.ico alias, so a static deploy ships the
	// same icon surface the live server serves. User-supplied files in
	// the static source win, matching the SEO/PWA precedence.
	if err := b.dumpAppIcons(&res); err != nil {
		return res, err
	}

	// /__gofastr/* assets — runtime, compiled actions, theme CSS, custom
	// CSS, route graph script. The injected <link>/<script src> tags in
	// the rendered HTML reference these paths, so SSG output is broken
	// without them.
	type asset struct {
		urlPath string
		body    []byte
	}
	colorScheme, err := runtime.ColorSchemeJS()
	if err != nil {
		return res, fmt.Errorf("static: color-scheme.js: %w", err)
	}
	assets := []asset{
		{urlPath: "/__gofastr/runtime.js", body: []byte(runtime.MustRuntimeJS())},
		// Loaded synchronously at the top of <head>; themeswitch.js
		// early-returns without window.__gofastr_colorScheme, so a
		// missing file silently kills the theme toggle on a static host.
		{urlPath: "/__gofastr/color-scheme.js", body: []byte(colorScheme)},
	}
	if js := b.Host.GetActionJS(); js != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/actions.js", body: []byte(js)})
	}
	// Single app-level CSS asset: theme :root vars + customCSS
	// concatenated. Matches the live server's /__gofastr/app.css.
	if appBody := b.Host.AppCSS(); appBody != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/app.css", body: []byte(appBody)})
	}
	// Route graph + component catalog ship INLINE in each rendered
	// HTML page as <script type="application/json"> blocks. No
	// separate .js files are emitted — the SSG output is fully
	// self-contained per page, no extra round-trips required.
	// Per-component CSS still ships as standalone files because the
	// runtime fetches them on demand.
	for url, css := range b.Host.ComponentCSSFiles() {
		assets = append(assets, asset{urlPath: url, body: []byte(css)})
	}
	// Split runtime modules. runtime.js injects
	// <script src="/__gofastr/runtime/<name>.js?v=<hash>"> on demand for each
	// feature module (themeswitch, shortcut, copy, widgets, sse, toasts, …).
	// A static host ignores the ?v= query and resolves the file by path, so
	// every module must exist on disk query-free — otherwise the dynamic
	// load 404s and the feature silently dies. (This is the regression the
	// wget crawl hit: it baked ?v= into the filename and 404'd every module.)
	for _, name := range runtime.ModuleNames() {
		body, ok := runtime.Module(name)
		if !ok {
			continue
		}
		assets = append(assets, asset{urlPath: "/__gofastr/runtime/" + name + ".js", body: []byte(body)})
	}
	for _, a := range assets {
		dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(a.urlPath, "/")))
		if err := writeFile(dst, b.rewriteJSAsset(a.body)); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, a.urlPath)
		b.log("wrote %s", a.urlPath)
	}

	// Widget catalog + chrome + CSS — dumped as query-free files so the
	// runtime's data-fui-open overlays resolve against the static tree
	// instead of 404'ing against the live widget endpoints. Recorded in
	// res.Assets so the full-site worker precaches them — overlays must
	// keep opening offline.
	if err := b.dumpWidgetAssets(&res); err != nil {
		return res, err
	}

	// Everything recorded so far is framework-generated and precached
	// atomically; user static-dir files below are precached best-effort.
	frameworkAssetCount := len(res.Assets)

	// Static assets — either filesystem dir or embedded FS.
	if dir := b.Host.StaticDir(); dir != "" {
		if err := copyDir(dir, b.OutDir, &res, b.log); err != nil {
			return res, err
		}
	} else if fsys := b.Host.StaticFS(); fsys != nil {
		if err := copyFS(fsys, b.OutDir, &res, b.log); err != nil {
			return res, err
		}
	}

	// PWA surface — manifest, service worker, registration script, and
	// offline fallback, mirroring the live WithPWA routes. Runs LAST so
	// the full-site worker can precache the complete export (pages,
	// runtime assets, AND the static dir copied above) and fingerprint
	// its content. The uihost generators take BasePath directly
	// (manifest start_url/scope/id, worker precache list, and
	// registration target must all resolve under the mount path), so
	// none of these go through rewriteJSAsset.
	if err := b.dumpPWAAssets(&res, frameworkAssetCount); err != nil {
		return res, err
	}

	return res, nil
}

// ensureContained is the last line of defence before any generated file is
// written: it verifies dst resolves inside b.OutDir. applyParams already
// rejects unsafe StaticPaths values, but this guards against any future caller
// that constructs a destination some other way. Fails closed.
func (b *Builder) ensureContained(dst string) error {
	rel, err := filepath.Rel(b.OutDir, dst)
	if err != nil {
		return fmt.Errorf("static: refusing to write outside OutDir: %q: %w", dst, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("static: refusing to write outside OutDir: %q escapes %q", dst, b.OutDir)
	}
	return nil
}

func (b *Builder) log(format string, args ...any) {
	if b.Logger != nil {
		b.Logger(format, args...)
	}
}

// expandRoute returns the concrete URLs to build for a registered route.
// Static routes return themselves. Dynamic routes (with ":param") look up
// their screen, ask for StaticPaths, and substitute each param map. If a
// dynamic route's screen does not implement StaticPathsProvider, the route
// is skipped at build time.
func expandRoute(ctx context.Context, app *coreapp.App, pattern string) ([]string, error) {
	if !strings.Contains(pattern, ":") {
		return []string{pattern}, nil
	}
	screen, _, ok := app.Router.Resolve(pattern)
	if !ok {
		// Pattern is registered but unresolvable — odd, skip safely.
		return nil, nil
	}
	provider, ok := screen.Component.(coreapp.StaticPathsProvider)
	if !ok {
		return nil, nil
	}
	return expandParams(ctx, pattern, provider.StaticPaths(ctx))
}

// expandParams substitutes each StaticPaths param map into the route pattern.
// It fails closed: any param value that would let the generated URL escape its
// route segment — a path separator, an "." / ".." traversal component, a NUL,
// or an empty value — aborts the whole build rather than silently writing a
// file outside OutDir.
func expandParams(_ context.Context, pattern string, sets []map[string]string) ([]string, error) {
	var out []string
	for _, params := range sets {
		url, err := applyParams(pattern, params)
		if err != nil {
			return nil, err
		}
		out = append(out, url)
	}
	return out, nil
}

func applyParams(pattern string, params map[string]string) (string, error) {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			key := strings.TrimPrefix(part, ":")
			if v, ok := params[key]; ok {
				if err := validateParamValue(key, v); err != nil {
					return "", err
				}
				parts[i] = v
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

// validateParamValue rejects StaticPaths values that could break out of their
// single URL segment when written to disk by pathToFile/Build. A param fills
// exactly one path component, so anything that introduces a new component
// (separator) or navigates the tree ("."/"..") or truncates the name (NUL) is
// unsafe.
func validateParamValue(key, v string) error {
	if v == "" {
		return fmt.Errorf("static: empty StaticPaths value for %q", key)
	}
	if strings.ContainsAny(v, "/\\\x00") {
		return fmt.Errorf("static: StaticPaths value for %q contains a path separator: %q", key, v)
	}
	if v == "." || v == ".." {
		return fmt.Errorf("static: StaticPaths value for %q is a traversal component: %q", key, v)
	}
	return nil
}

// applyStaticMode post-processes a rendered page for the serverless
// static export. It does two things:
//
//  1. Stamps <html> with data-fui-static — the runtime's static-mode
//     switch. When present, the runtime skips the widget catalog fetch,
//     no-ops data-fui-rpc dispatch, and short-circuits data-fui-open,
//     so a click on a dead demo does not fire a request that 404s
//     against the host. Live pages never carry the marker.
//
//  2. Injects a dismissible "run locally" notice via the single shared
//     styling surface (framework/ui.Banner) so server-backed demos read
//     as intentionally inactive rather than broken. When BasePath is
//     set, every root-absolute asset/nav URL is rewritten to resolve
//     under the mount path.
func (b *Builder) applyStaticMode(page string) string {
	page = stampStatic(page)
	page = b.rewriteBaseURLs(page)
	notice := string(ui.Banner(ui.BannerConfig{
		Title:       "Static preview",
		Body:        "This is a read-only export. Run the app locally for full interactivity — live search, demos, and server-driven islands need the Go server.",
		Variant:     ui.BannerInfo,
		Dismissible: true,
		DismissID:   "gofastr-static-preview",
	}))
	// Inject immediately after the opening <body ...> tag.
	if j := strings.Index(page, "<body"); j >= 0 {
		if k := strings.IndexByte(page[j:], '>'); k >= 0 {
			at := j + k + 1
			page = page[:at] + notice + page[at:]
		}
	}
	return page
}

// stampStatic marks <html> with data-fui-static — the runtime's
// static-mode switch. The first "<html" in the document is always the
// real root element (it precedes any body content); the marker is
// value-agnostic, so a bare boolean attribute suffices.
func stampStatic(page string) string {
	if i := strings.Index(page, "<html"); i >= 0 {
		return page[:i+len("<html")] + " data-fui-static" + page[i+len("<html"):]
	}
	return page
}

// baseAttrURL matches a whitespace-delimited src="…", href="…", or
// data-fui-push-state="…" whose value is root-absolute (leading "/") but
// NOT protocol-relative ("//"). data-fui-push-state is included so combobox/
// palette selection targets get base-prefixed on subpath deploys (otherwise
// selecting a command navigates to the apex path and 404s). The leading
// ([\s]) anchor ensures only real attributes match — never "data-src" /
// "srcset" — and code samples are safe because core/markdown escapes quotes
// inside <code> to &quot; (so the ="… pattern never appears in rendered code
// text). Group 3 is the first path byte, re-emitted so the prefix is inserted
// after the leading slash.
var baseAttrURL = regexp.MustCompile(`([\s])(src|href|data-fui-push-state)="/([^/])`)

// rewriteBaseURLs prefixes every root-absolute asset and navigation URL in
// the page with b.BasePath. No-op when BasePath is empty (apex deploy /
// live server). Covers /__gofastr/… assets (runtime.js, modules, CSS,
// modulepreload links) and internal nav links (/about → /<base>/about) in a
// single pass. External (https://), protocol-relative (//host), fragment
// (#…), and relative URLs are untouched.
func (b *Builder) rewriteBaseURLs(page string) string {
	if b.BasePath == "" {
		return page
	}
	// 1. Attributes: src="…"/href="…" (assets + nav links).
	page = baseAttrURL.ReplaceAllString(page, "${1}${2}=\""+b.BasePath+"/${3}")
	// 2. Inline JSON values: the component catalog seeds
	// "stylePath":"/__gofastr/comp/<name>.css" entries the runtime
	// lazy-loads at runtime — those aren't attributes, so the regex
	// above misses them. Prefix every quoted root-absolute /__gofastr/
	// path. Safe vs code samples: core/markdown escapes quotes in
	// <code> to &quot;, so the literal "/__gofastr/ (real double-quote
	// + path) only occurs in real JSON, never in rendered code text.
	page = strings.ReplaceAll(page, `"/__gofastr/`, `"`+b.BasePath+`/__gofastr/`)
	return page
}

// dumpWidgetAssets writes the widget catalog JSON and each widget's chrome
// HTML + CSS as query-free static files. Hidden click-to-open widgets
// (command palette, section-menu drawers, modals) are not SSR-inlined, so
// the runtime fetches their chrome from cfg.chromePath on open — which 404s
// on a serverless host. Dumping the same bytes as files lets openWidget
// resolve against the static tree, so every data-fui-open overlay works.
func (b *Builder) dumpWidgetAssets(res *Result) error {
	defs := widget.AllForSSR()
	if len(defs) == 0 {
		return nil
	}
	// Catalog JSON — canonical shape from ServeWidgetList (no page filter =
	// every widget). chromePath/stylePath are root-absolute /core-ui/widget/…
	// values the runtime fetches at open time; prefix them for a subpath deploy.
	rec := httptest.NewRecorder()
	widget.ServeWidgetList(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))
	cat := rec.Body.String()
	if b.BasePath != "" {
		cat = strings.ReplaceAll(cat, `"/core-ui/widget/`, `"`+b.BasePath+`/core-ui/widget/`)
	}
	if err := b.writeRawAsset("/__gofastr/widgets.json", []byte(cat)); err != nil {
		return err
	}
	res.Assets = append(res.Assets, "/__gofastr/widgets.json")
	b.log("wrote %s", "/__gofastr/widgets.json")
	for _, d := range defs {
		// Chrome HTML may carry root-absolute nav links (/docs/…); rewrite
		// them the same way page HTML is rewritten for a subpath deploy.
		chrome := b.rewriteBaseURLs(widget.RenderChrome(d))
		if err := b.writeRawAsset("/core-ui/widget/"+d.Name+"/chrome", []byte(chrome)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/core-ui/widget/"+d.Name+"/chrome")
		if err := b.writeRawAsset("/core-ui/widget/"+d.Name+"/style.css", []byte(widget.RenderCSS(d))); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/core-ui/widget/"+d.Name+"/style.css")
		b.log("wrote widget %s (chrome + css)", d.Name)
	}
	return nil
}

// dumpSEOAssets writes sitemap.xml and robots.txt when the host
// configured WithSitemap/WithRobots. Skips any file the app's static
// source already provides (the user's copy is copied verbatim later and
// must win). No-op when neither option is configured.
func (b *Builder) dumpSEOAssets(res *Result) error {
	type seoFile struct {
		name string
		body string
		ok   bool
	}
	sitemap, sitemapOK := b.Host.SitemapXML(b.BasePath)
	robots, robotsOK := b.Host.RobotsTXT(b.BasePath)
	for _, f := range []seoFile{
		{"sitemap.xml", sitemap, sitemapOK},
		{"robots.txt", robots, robotsOK},
	} {
		if !f.ok {
			continue
		}
		if b.staticSourceHas(f.name) {
			b.log("kept user-supplied /%s", f.name)
			continue
		}
		if err := b.writeRawAsset("/"+f.name, []byte(f.body)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+f.name)
		b.log("wrote /%s", f.name)
	}
	return nil
}

// dumpAppIcons writes the WithAppIcon-generated PNGs (and the
// /favicon.ico alias) to the export tree. No-op when the host has no
// generated icons.
func (b *Builder) dumpAppIcons(res *Result) error {
	icons := b.Host.AppIconAssets()
	if len(icons) == 0 {
		return nil
	}
	paths := make([]string, 0, len(icons))
	for p := range icons {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel := filepath.FromSlash(strings.TrimPrefix(p, "/"))
		if b.staticSourceHas(rel) {
			b.log("kept user-supplied %s", p)
			continue
		}
		if err := b.writeRawAsset(p, icons[p]); err != nil {
			return err
		}
		res.Assets = append(res.Assets, p)
		b.log("wrote %s", p)
	}
	return nil
}

// dumpPWAAssets emits the WithPWA surface for a serverless deploy:
// manifest.webmanifest, service-worker.js, the registration script, and
// the offline fallback page (as offline/index.html so a plain static
// server resolves the extension-less precache URL). No-op when the host
// did not opt into WithPWA.
func (b *Builder) dumpPWAAssets(res *Result, frameworkAssetCount int) error {
	if !b.Host.PWAEnabled() {
		return nil
	}
	manifest, err := b.Host.PWAManifestJSON(b.BasePath)
	if err != nil {
		return fmt.Errorf("static: pwa manifest: %w", err)
	}
	// Static exports get the full-site worker: the page set is closed and
	// immutable, so every exported page and asset is precached at install
	// and the whole site works offline. The tree hash makes a
	// content-only redeploy rotate the cache version.
	contentHash, err := hashExportTree(b.OutDir)
	if err != nil {
		return fmt.Errorf("static: fingerprint export: %w", err)
	}
	if frameworkAssetCount > len(res.Assets) {
		frameworkAssetCount = len(res.Assets)
	}
	sw, err := b.Host.PWAStaticServiceWorkerJS(b.BasePath, uihost.PWAStaticExport{
		Pages:          res.Pages,
		Assets:         res.Assets[:frameworkAssetCount],
		OptionalAssets: res.Assets[frameworkAssetCount:],
		ContentHash:    contentHash,
	})
	if err != nil {
		return fmt.Errorf("static: pwa service worker: %w", err)
	}
	// The offline page is rendered basePath-neutral by the host; its
	// asset links get the same base rewrite as every exported page. It
	// also gets the data-fui-static stamp so the runtime's static-mode
	// guards apply, but not the "run locally" banner — an offline
	// fallback is not a demo page.
	offline := b.rewriteBaseURLs(stampStatic(b.Host.PWAOfflineHTML()))
	files := []struct {
		urlPath string
		relFile string
		body    []byte
	}{
		{"/manifest.webmanifest", "manifest.webmanifest", manifest},
		{"/service-worker.js", "service-worker.js", []byte(sw)},
		{"/__gofastr/pwa/register.js", filepath.Join("__gofastr", "pwa", "register.js"), []byte(b.Host.PWARegisterJS(b.BasePath))},
		{"/__gofastr/pwa/offline", filepath.Join("__gofastr", "pwa", "offline", "index.html"), []byte(offline)},
	}
	for _, f := range files {
		// User-supplied files win: before this surface moved to the end of
		// the build, a manifest/worker shipped in the app's static dir
		// landed after (and therefore over) the generated one. Preserve
		// that precedence by never clobbering a file the static SOURCE
		// provides (the source, not OutDir — a previous build's leftovers
		// in a reused OutDir must not suppress regeneration).
		if b.staticSourceHas(f.relFile) {
			b.log("kept user-supplied %s", f.urlPath)
			continue
		}
		dst := filepath.Join(b.OutDir, f.relFile)
		if err := b.ensureContained(dst); err != nil {
			return err
		}
		if err := writeFile(dst, f.body); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.urlPath)
		b.log("wrote %s", f.urlPath)
	}
	return nil
}

// staticSourceHas reports whether the host's static asset source (dir or
// embedded FS) provides rel — meaning the user shipped their own copy.
func (b *Builder) staticSourceHas(rel string) bool {
	if dir := b.Host.StaticDir(); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return true
		}
		return false
	}
	if fsys := b.Host.StaticFS(); fsys != nil {
		if f, err := fsys.Open(filepath.ToSlash(rel)); err == nil {
			_ = f.Close()
			return true
		}
	}
	return false
}

// hashExportTree fingerprints every file already written to outDir
// (deterministic sorted walk over relative path + streamed bytes). The
// PWA outputs themselves are excluded — they are (re)generated after
// this hash, and a reused OutDir still holds the PREVIOUS build's
// copies; hashing those would make every rebuild byte-different even
// with identical content. A content-only redeploy therefore changes
// the hash → the worker bytes → the cache version, and installed
// clients re-precache; an unchanged rebuild reproduces the same worker.
func hashExportTree(outDir string) (string, error) {
	skip := map[string]bool{
		"manifest.webmanifest": true,
		"service-worker.js":    true,
	}
	h := sha256.New()
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		if skip[slashRel] || strings.HasPrefix(slashRel, "__gofastr/pwa/") {
			return nil
		}
		h.Write([]byte(slashRel))
		h.Write([]byte{0})
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// writeRawAsset writes body to the OutDir path derived from urlPath WITHOUT
// the runtime-JS base bake — callers that need base rewriting apply the
// appropriate rewriter (rewriteBaseURLs for HTML, manual prefixing for JSON)
// before calling. Guards path containment via ensureContained.
func (b *Builder) writeRawAsset(urlPath string, body []byte) error {
	dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
	if err := b.ensureContained(dst); err != nil {
		return err
	}
	return writeFile(dst, body)
}

// rewriteJSAsset bakes b.BasePath into a JS asset's hardcoded /__gofastr/…
// URL literals (runtime.js constructs split-module and endpoint URLs at
// runtime; those aren't in the HTML to rewrite). No-op when BasePath is
// empty. The live server serves the original, unmodified asset.
func (b *Builder) rewriteJSAsset(body []byte) []byte {
	if b.BasePath == "" {
		return body
	}
	return []byte(strings.ReplaceAll(string(body), "/__gofastr/", b.BasePath+"/__gofastr/"))
}

// pathToFile turns a URL path into the relative file path SSG output uses.
//
//	"/"               -> "index.html"
//	"/about"          -> "about/index.html"
//	"/products/abc"   -> "products/abc/index.html"
func pathToFile(p string) string {
	clean := strings.Trim(p, "/")
	if clean == "" {
		return "index.html"
	}
	return filepath.Join(clean, "index.html")
}

// pathToLLMFile turns a URL path into the relative file path for the
// per-page LLM documentation. Dynamic routes are expected to be expanded
// (via expandRoute) before being passed in.
//
//	"/"              -> "llm.md"
//	"/about"         -> "about/llm.md"
//	"/products/abc"  -> "products/abc/llm.md"
func pathToLLMFile(p string) string {
	clean := strings.Trim(p, "/")
	if clean == "" {
		return "llm.md"
	}
	return filepath.Join(clean, "llm.md")
}

func writeFile(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func copyDir(src, dst string, res *Result, log func(string, ...any)) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Refuse to overwrite generated index.html files.
		if filepath.Base(rel) == "index.html" {
			return nil
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := os.Create(out)
		if err != nil {
			return err
		}
		defer w.Close()
		if _, err := io.Copy(w, in); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+filepath.ToSlash(rel))
		log("copied %s -> %s", rel, out)
		return nil
	})
}

func copyFS(fsys fs.FS, dst string, res *Result, log func(string, ...any)) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "index.html" {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+filepath.ToSlash(path))
		log("copied %s -> %s", path, out)
		return nil
	})
}
