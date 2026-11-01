package static

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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
	// ExtraDirs maps URL mount paths to filesystems whose files are
	// copied into the export under that path, e.g.
	// {"/docs/api/sdk": os.DirFS("gen/sdk/dist")} so the SDK artifacts a
	// live sdkdocs.Mount serves also ship in the static tree. Files the
	// build (or the user static dir) already produced win: extra files
	// are written only when absent, matching the SEO/PWA precedence.
	ExtraDirs map[string]fs.FS
	// ExcludeRoutes skips routes whose pattern (or a prefix segment of
	// it) matches an entry, e.g. ExcludeRoutes: []string{"/broken"}
	// skips "/broken/load" and every other /broken/* page. A lab or
	// example app uses it to keep routes that fail ON PURPOSE out of
	// the export set; the skip is logged so the gap is never silent.
	// the builder refuses a page whose fill
	// failed and was contained, so routes that fail on purpose must be
	// excluded explicitly or the export errors.
	ExcludeRoutes []string
	// Handler, when set, serves the extra-script rail entries
	// (uihost.WithExtraScripts) that the host does not own: a script an
	// app serves from its own router, e.g. a reducer at
	// /__site/livedash-reducers.js. Each such src is fetched through
	// Handler and written into the export; one that does not answer 200
	// fails the build, since every exported page would 404 on it.
	// App.ExportStatic passes the app's router. A script this build
	// already wrote (a framework asset, the static dir) is not fetched,
	// nor is a src that is not a same-origin path (a CDN URL), nor a
	// document-scoped script whose scope accepts no exported route.
	Handler http.Handler
	// Contributors add files built from the exported pages: each is
	// told a build began (ExportStart), sees every page as it renders
	// (ExportPage), then returns its files (ExportFiles), which the
	// build writes under their root-absolute URL paths. battery/sitesearch writes its index this way.
	// App.ExportStatic passes every registered battery that is one.
	Contributors []Contributor
	// ContentSecurityPolicy is the policy the export carries: a <meta>
	// in every page (one per policy of a comma-separated list, without
	// frame-ancestors, report-uri and sandbox, which a <meta> policy
	// ignores) and the full value in _headers. Empty keeps the
	// built-in policy, the one core/middleware.SecurityHeaders serves by
	// default. App.ExportStatic passes
	// AppConfig.SecurityHeaders.ContentSecurityPolicy. The build refuses
	// a policy with a nonce source (a file every visitor gets publishes
	// it; static pages link every script, so none needs one), a control
	// byte, or a quote or angle bracket.
	ContentSecurityPolicy string
}

// Contributor adds files to an export from the pages it renders: a
// search index, a feed. ExportStart runs when a build begins, so a
// contributor drops what a build that failed partway left behind.
// ExportPage gets each page's live HTML, before the export's static
// rewrites; ExportFiles runs once, after the last page, and returns
// files keyed by root-absolute URL path
// ("/__gofastr/search/manifest.json").
type Contributor interface {
	ExportStart(ctx context.Context) error
	ExportPage(path, html string) error
	ExportFiles(ctx context.Context) (map[string][]byte, error)
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
	if err := checkExportCSP(b.policy()); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(b.OutDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("static: create out dir: %w", err)
	}

	res := Result{}
	// Route patterns with at least one exported page: the identities a
	// document-scoped rail script's scope is asked about.
	var renderedRoutes []string
	// The page files this build writes (rendered pages and redirect
	// pages), as slash paths under OutDir: no static-source or extra
	// file replaces one.
	rendered := newFileSet()
	// The URL path each page file was rendered from, to refuse a second
	// page landing on the same file.
	pageOwner := map[string]string{}
	// The other page files this build writes (404.html, every llm.md and
	// its index), as slash paths under OutDir: with rendered, what the
	// page manifest records (pruneStalePages).
	docs := map[string]bool{}

	for _, c := range b.Contributors {
		if err := c.ExportStart(ctx); err != nil {
			return res, fmt.Errorf("static: contributor start: %w", err)
		}
	}

	// Pages: once, or once per locale for an app that routes by locale
	// prefix (framework.WithLocalePrefixes), each prefixed locale into
	// its own /<tag>/ tree the way the live router serves it.
	locales, byPrefix := b.Host.PathLocales()
	tags := []string{""}
	if byPrefix {
		tags = locales.All()
	}
	for _, tag := range tags {
		lctx, rule := ctx, urlprefix.Rule{}
		if byPrefix {
			lctx = localeContext(ctx, locales, tag)
			rule = urlprefix.Rule{Prefix: i18n.PathPrefix(lctx), Locales: locales.All(), Keep: b.keepUnprefixed}
		}
		err := walk(lctx, b.Host, b.routeExcluded, nil, b.log, nil, func(route, p, page string) error {
			if rule.Prefix != "" {
				p = locales.Path(tag, p)
				page = string(urlprefix.HTML([]byte(page), rule))
			}
			for _, c := range b.Contributors {
				if err := c.ExportPage(p, page); err != nil {
					return fmt.Errorf("static: contributor on %q: %w", p, err)
				}
			}
			// Two pages whose paths map to one file (a StaticPaths key
			// "a/b" beside a route /products/a/b) would leave the later
			// one in place of the earlier: refuse the export instead.
			// Files that differ only in case are one file on a
			// case-insensitive filesystem (APFS, NTFS), so the check
			// compares case-folded paths.
			file := filepath.ToSlash(pathToFile(p))
			if prev, dup := pageOwner[foldPath(file)]; dup {
				return fmt.Errorf("static: pages %q and %q both export to %s; give one of them another path", prev, p, file)
			}
			pageOwner[foldPath(file)] = p
			html := b.applyStaticMode(lctx, page)
			dst := filepath.Join(b.OutDir, pathToFile(p))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, []byte(html)); err != nil {
				return err
			}
			b.log("rendered %s -> %s", p, dst)
			res.Pages = append(res.Pages, p)
			rendered.add(filepath.ToSlash(pathToFile(p)))
			if !slices.Contains(renderedRoutes, route) {
				renderedRoutes = append(renderedRoutes, route)
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}

	// Contributor files (a search index, a feed), written after every
	// page so each saw them all.
	for _, c := range b.Contributors {
		files, err := c.ExportFiles(ctx)
		if err != nil {
			return res, fmt.Errorf("static: contributor files: %w", err)
		}
		for _, urlPath := range slices.Sorted(maps.Keys(files)) {
			if !strings.HasPrefix(urlPath, "/") {
				return res, fmt.Errorf("static: contributor file %q is not a root-absolute path", urlPath)
			}
			// Everything below, the record in res.Assets included, uses
			// the path the write lands on: /about/./index.html is
			// /about/index.html.
			body := files[urlPath]
			urlPath = path.Clean(urlPath)
			// A contributor file on a page's file, in any case, would
			// replace the page; a redirect written later checks it too.
			rel := strings.TrimPrefix(urlPath, "/")
			// The build writes _headers over a contributor's and removes
			// _redirects when the app has no redirects; the static dir's
			// copy is appended instead.
			if f := foldPath(rel); f == foldPath("_headers") || f == foldPath("_redirects") {
				return res, fmt.Errorf("static: contributor file %s: the export writes _headers and _redirects itself; ship extra rules in the static dir, which are appended", urlPath)
			}
			if prev, dup := pageOwner[foldPath(rel)]; dup {
				return res, fmt.Errorf("static: pages %q and %q both export to %s; give one of them another path", prev, urlPath, rel)
			}
			pageOwner[foldPath(rel)] = urlPath
			if err := b.writeRawAsset(urlPath, body); err != nil {
				return res, err
			}
			res.Assets = append(res.Assets, urlPath)
			b.log("wrote contributor file %s", urlPath)
		}
	}

	// Security headers for hosts that read a `_headers` file (Netlify,
	// Cloudflare Pages). Pages also carry the policy as an in-document
	// meta: that enforces the fetch directives on a host that ignores
	// this file, but CSP Level 3 §3.1 ignores `frame-ancestors` in a
	// meta (alongside `sandbox`/`report-uri`), so the clickjacking guard
	// only lands where the header is read.
	if err := writeHeadersFile(b.OutDir, b.policy()); err != nil {
		return res, fmt.Errorf("static: write _headers: %w", err)
	}
	// An ExtraDirs `_headers` must not replace it: that would drop the
	// header-only frame-ancestors guard.
	rendered.add("_headers")

	// Redirect routes: a page at each exact redirect's path, and the
	// _redirects rules the hosts that read _headers also read.
	if err := b.writeRedirects(ctx, pageSet(res.Pages), rendered, pageOwner); err != nil {
		return res, err
	}

	// 404.html at the export root (, GitHub
	// Pages, Netlify, Cloudflare Pages and S3 website hosting all serve
	// it for a miss, rendered from the same not-found page the live
	// server answers with (the root layout's shell, outlets at their
	// defaults, full chrome). The runtime reads a non-OK HTML document
	// through the same swap paths a 200 takes, so a miss on the static
	// host lands inside the shell with the error page in <main>, never
	// a dead toast. A host without a root layout emits none: its bare
	// 404 has no shell to keep.
	if page, ok := b.Host.RenderStaticNotFoundPage(ctx, "/"); ok {
		html := b.applyStaticMode(ctx, page)
		dst := filepath.Join(b.OutDir, "404.html")
		if err := b.ensureContained(dst); err != nil {
			return res, err
		}
		if err := writeUnder(b.OutDir, dst, []byte(html)); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, "/404.html")
		docs["404.html"] = true
		b.log("rendered not-found page -> %s", dst)
	}

	// Page links, once every page is known: see slashPageLinks.
	if err := b.slashLinks(res); err != nil {
		return res, err
	}

	// LLM documentation: per-page llm.md and top-level index, once per
	// locale tree like the pages: the live /es/about/llm.md is the
	// Spanish doc, and the /es/ index links docs under /es/.
	if !b.Host.App.NoLLMMD {
		for _, tag := range tags {
			lctx, at := ctx, func(p string) string { return p }
			if byPrefix {
				// The doc links a copied file at the export root, as the pages do.
				lctx = coreapp.WithLinkKeep(localeContext(ctx, locales, tag), b.keepUnprefixed)
				at = func(p string) string { return locales.Path(tag, p) }
			}
			if err := b.writeLLMMD(lctx, &res, at, docs); err != nil {
				return res, err
			}
		}
	}

	// SEO surface: sitemap.xml and robots.txt, mirroring the live
	// WithSitemap/WithRobots handlers so a static deploy keeps the same
	// crawler contract as the live server. One source with the HTTP
	// handlers (UIHost.SitemapFiles / UIHost.RobotsTXT), listing only the
	// pages this build wrote, with BasePath folded into <loc> entries and
	// the derived Sitemap: URL. User-supplied files in the static source win, matching the
	// PWA-surface precedence below.
	if err := b.dumpSEOAssets(&res); err != nil {
		return res, err
	}

	// Generated app icons (uihost.WithAppIcon): the /__gofastr/icons/
	// PNGs plus the /favicon.ico alias, so a static deploy ships the
	// same icon surface the live server serves. User-supplied files in
	// the static source win, matching the SEO/PWA precedence.
	if err := b.dumpAppIcons(&res); err != nil {
		return res, err
	}

	// /__gofastr/* assets: runtime, compiled actions, theme CSS, custom
	// CSS, route graph script. The injected <link>/<script src> tags in
	// the rendered HTML reference these paths, so SSG output is broken
	// without them.
	type asset struct {
		urlPath string
		body    []byte
		// binary skips the base-path rewrite: it is a byte-level string
		// replace, and a WebAssembly module is not text.
		binary bool
	}
	colorScheme, err := runtime.ColorSchemeJS()
	if err != nil {
		return res, fmt.Errorf("static: color-scheme.js: %w", err)
	}
	assets := []asset{
		{urlPath: "/__gofastr/runtime.js", body: []byte(runtime.MustStaticJS())},
		// Loaded synchronously at the top of <head>: it applies the
		// stored scheme before first paint, so a reload never flashes.
		// Without it headless-navigation still works, but only from
		// its own load onward.
		{urlPath: "/__gofastr/color-scheme.js", body: []byte(colorScheme)},
	}
	if js := b.Host.GetActionJS(); js != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/actions.js", body: []byte(js)})
	}
	// Single app-level CSS asset: theme :root vars + customCSS
	// concatenated. Matches the live server's /__gofastr/app.css.
	if appBody := b.Host.AppCSS(); appBody != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/app.css", body: []byte(minify.ServedCSS(appBody))})
	}
	// Route graph + component catalog ship INLINE in each rendered
	// HTML page as <script type="application/json"> blocks. No
	// separate .js files are emitted. The SSG output is fully
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
	// every module must exist on disk query-free. Otherwise the dynamic
	// load 404s and the feature silently dies. (This is the regression the
	// wget crawl hit: it baked ?v= into the filename and 404'd every module.)
	for _, name := range runtime.ModuleNames() {
		body, ok := runtime.Module(name)
		if !ok {
			continue
		}
		assets = append(assets, asset{urlPath: "/__gofastr/runtime/" + name + ".js", body: []byte(body)})
	}
	// Registered compute assets (core-ui/compute). Every exported page
	// carries the #gofastr-compute-assets block and the compute module
	// builds /__gofastr/compute/<name>.js|.wasm?v=<hash> from it, so
	// each worker and WebAssembly module must exist on disk query-free,
	// as the split modules above do, or every compute task 404s.
	computeAssets := compute.Manifest()
	for _, name := range slices.Sorted(maps.Keys(computeAssets)) {
		var buf bytes.Buffer
		if a, ok := compute.LookupWorker(name); ok {
			_, _ = a.WriteTo(&buf)
			assets = append(assets, asset{urlPath: "/__gofastr/compute/" + name + ".js", body: bytes.Clone(buf.Bytes())})
			buf.Reset()
		}
		if a, ok := compute.LookupWASM(name); ok {
			_, _ = a.WriteTo(&buf)
			assets = append(assets, asset{urlPath: "/__gofastr/compute/" + name + ".wasm", body: bytes.Clone(buf.Bytes()), binary: true})
		}
	}
	for _, a := range assets {
		dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(a.urlPath, "/")))
		body := a.body
		if !a.binary {
			body = b.rewriteJSAsset(body)
		}
		if err := writeUnder(b.OutDir, dst, body); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, a.urlPath)
		b.log("wrote %s", a.urlPath)
	}

	// Widget catalog + chrome + CSS: dumped as query-free files so the
	// runtime's data-cui-open overlays resolve against the static tree
	// instead of 404'ing against the live widget endpoints. Recorded in
	// res.Assets so the full-site worker precaches them. Overlays must
	// keep opening offline.
	if err := b.dumpWidgetAssets(ctx, &res); err != nil {
		return res, err
	}

	// Everything recorded so far is framework-generated and precached
	// atomically; user static-dir files below are precached best-effort.
	frameworkAssetCount := len(res.Assets)

	// Static assets: either filesystem dir or embedded FS.
	if dir := b.Host.StaticDir(); dir != "" {
		if err := copyDir(dir, b.OutDir, rendered, &res, b.log); err != nil {
			return res, err
		}
	} else if fsys := b.Host.StaticFS(); fsys != nil {
		if err := copyFS(fsys, b.OutDir, rendered, &res, b.log); err != nil {
			return res, err
		}
	}

	// Extra mounted directories (SDK artifacts, other host-declared
	// downloads). After the user static dir so its files win; before the
	// PWA surface so the worker fingerprints the complete export.
	if err := b.dumpExtraDirs(&res, rendered); err != nil {
		return res, err
	}
	if err := b.dumpRailScripts(ctx, &res, renderedRoutes, rendered); err != nil {
		return res, err
	}
	// A reused OutDir holds the previous build's pages: drop the ones
	// this build no longer writes, before the PWA worker fingerprints
	// and precaches the tree.
	if err := b.pruneStalePages(res, rendered, docs); err != nil {
		return res, err
	}
	// Before the worker hashes the tree, so a rebuild over leftovers
	// hashes the same names a fresh build does.
	if err := respell(b.OutDir, writtenPaths(&res, rendered, docs)); err != nil {
		return res, err
	}

	// PWA surface: manifest, service worker, registration script, and
	// offline fallback, mirroring the live WithPWA routes. Runs LAST so
	// the full-site worker can precache the complete export (pages,
	// runtime assets, AND the static dir copied above) and fingerprint
	// its content. The uihost generators take BasePath directly
	// (manifest start_url/scope/id, worker precache list, and
	// registration target must all resolve under the mount path), so
	// none of these go through rewriteJSAsset.
	if err := b.dumpPWAAssets(&res, frameworkAssetCount, rendered); err != nil {
		return res, err
	}
	// Last, once every file is written: no two spellings of one file or
	// directory, and each one on disk under the build's spelling.
	if err := checkCaseTwins(&res, rendered, docs); err != nil {
		return res, err
	}
	if err := respell(b.OutDir, writtenPaths(&res, rendered, docs)); err != nil {
		return res, err
	}

	return res, nil
}

// writeLLMMD writes each exported page's llm.md and the llm-pages.md
// index for one locale tree: ctx carries the locale, at maps a route
// path to its URL in the tree. Each file it writes is marked in docs.
func (b *Builder) writeLLMMD(ctx context.Context, res *Result, at func(string) string, docs map[string]bool) error {
	// The pages whose doc this tree holds, by route pattern: the index
	// links those and no other (coreapp.WithExportedDocs).
	exported := map[string][]string{}
	for _, route := range b.Host.App.Routes() {
		if b.routeExcluded(route.Path) {
			continue
		}
		if route.RedirectTo != "" {
			continue
		}
		screen, ok := b.Host.App.Router.ScreenByPattern(route.Path)
		if !ok {
			continue
		}
		if screen.NoLLMMD {
			continue
		}
		paths, err := expandRoute(ctx, b.Host.App, route.Path)
		if err != nil {
			return fmt.Errorf("static: expand llm.md %q: %w", route.Path, err)
		}
		for _, p := range paths {
			if _, redirected := b.Host.App.ResolveRedirect(p); redirected {
				continue // a redirect page holds the path, as in walk
			}
			// Per-URL doc: a dynamic route expands to N pages, each
			// with its own loaded title + content. The pattern-level
			// ScreenLLMMD would stamp every page with one generic doc
			// (a placeholder, for ScreenLoader screens). Fall back to
			// the pattern doc if the concrete path doesn't resolve.
			var md string
			sctx, recorded := coreapp.TrackStatus(withPageRequest(ctx, p))
			if doc, ok := coreapp.ScreenLLMMDForPath(sctx, b.Host.App, p); ok {
				// A page that answers another status is skipped like
				// its HTML (uihost.StatusPageError): the static host
				// would serve its not-found body as a 200 doc.
				if st := docStatus(doc.Component, recorded()); st != 0 {
					slog.Warn("static: skipped llm.md for a page that answers a status other than 200",
						"path", p, "status", st)
					continue
				}
				md = b.Host.LLMMDWithSEO(sctx, p, screen, doc)
			} else {
				// The URL resolves but its page answers 404 (a resolver
				// or Load naming no row, an outlet nothing fills): no
				// doc, like its skipped HTML.
				if _, _, resolves := b.Host.App.Router.Resolve(p); resolves {
					slog.Warn("static: skipped llm.md for a page that answers a status other than 200",
						"path", p, "status", http.StatusNotFound)
					continue
				}
				// The expanded URL doesn't resolve (e.g. a StaticPaths
				// value that violates the route's constraint). The
				// WITHHELD doc, not the pattern doc: the pattern doc
				// carries the screen's title and rendered content with
				// NO policy evaluation, so falling back to it would
				// write a gated screen's content to disk ungated.
				md = coreapp.ScreenLLMMDWithheld(screen)
				slog.Warn("static: expanded llm.md path does not resolve: writing withheld doc; check StaticPaths values against the route's constraints",
					"route", route.Path, "path", p)
			}
			dst := filepath.Join(b.OutDir, pathToLLMFile(at(p)))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, []byte(md)); err != nil {
				return err
			}
			res.Assets = append(res.Assets, "/"+filepath.ToSlash(pathToLLMFile(at(p))))
			docs[filepath.ToSlash(pathToLLMFile(at(p)))] = true
			exported[route.Path] = append(exported[route.Path], p)
			b.log("wrote llm.md for %s -> %s", p, dst)
		}
	}
	// Top-level page index
	// Build-context policy evaluation: gated screens appear in the
	// exported index metadata-free (no auth at build time).
	indexMD := coreapp.AppLLMMDCtx(coreapp.WithExportedDocs(ctx, exported), b.Host.App)
	index := at("/llm-pages.md")
	if err := writeUnder(b.OutDir, filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(index, "/"))), []byte(indexMD)); err != nil {
		return err
	}
	res.Assets = append(res.Assets, index)
	docs[strings.TrimPrefix(index, "/")] = true
	b.log("wrote %s index", index)
	return nil
}

// Walk renders every page an export of host would contain, in route
// order, and hands each to fn with its path and its live HTML (before
// the export's static rewrites). Excluded routes, redirects, gated
// screens and 404-outlet routes are skipped the way Build skips them.
// A search index or a feed built at runtime reads the same pages an
// export writes. skip, when set, is asked about each route pattern and
// each expanded path before it renders, so a skipped page that cannot
// render is never an error.
//
// A page that fails to render, or whose fill failed and was contained,
// reaches fn with an empty html and the error; so does a dynamic route
// whose StaticPaths failed, under its pattern. As with fs.WalkDir, fn
// returning nil walks on without the page and returning an error stops
// the walk with it.
func Walk(ctx context.Context, host *uihost.UIHost, skip func(path string) bool, fn func(path, html string, err error) error) error {
	if host == nil {
		return fmt.Errorf("static: Walk needs a host")
	}
	if skip == nil {
		skip = func(string) bool { return false }
	}
	return walk(ctx, host, skip, skip, nil,
		func(path string, err error) error { return fn(path, "", err) },
		func(_, p, page string) error { return fn(p, page, nil) })
}

// walk is the page loop Build and Walk share: excluded is asked about
// route patterns, skipPath (when set) about expanded paths, and fn gets
// the route pattern, the concrete path and the rendered page. fail,
// when set, gets each route or page that could not render and decides
// whether the walk goes on (nil) or stops; unset, the first one stops
// it.
func walk(ctx context.Context, host *uihost.UIHost, excluded, skipPath func(string) bool, logf func(string, ...any), fail func(path string, err error) error, fn func(route, path, page string) error) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if fail == nil {
		fail = func(_ string, err error) error { return err }
	}
	for _, route := range host.App.Routes() {
		if excluded(route.Path) {
			logf("skipped excluded route %s", route.Path)
			continue
		}
		if route.RedirectTo != "" {
			continue // redirects render no page
		}
		paths, err := expandRoute(ctx, host.App, route.Path)
		if err != nil {
			if err := fail(route.Path, fmt.Errorf("static: expand %q: %w", route.Path, err)); err != nil {
				return err
			}
			continue
		}
		for _, p := range paths {
			if skipPath != nil && skipPath(p) {
				continue
			}
			// An exact redirect may take one concrete path of a dynamic
			// screen; the live router answers it with the redirect, and
			// so does the export (writeRedirects).
			if _, redirected := host.App.ResolveRedirect(p); redirected {
				logf("skipped %s: a redirect takes its path", p)
				continue
			}
			page, fillFailures, err := host.RenderStaticPageResult(ctx, p)
			if err != nil {
				if blocked, ok := errors.AsType[*uihost.PolicyBlockedError](err); ok {
					slog.Warn("static: skipped gated screen: policy refused the static render; the route stays reachable via the live server. Use a policy that RenderAlts (e.g. a login prompt) to keep the page in the export.",
						"path", p, "decision", blocked.Decision)
					continue
				}
				// The 404-outlet outcome (FallbackNotFound, Decided 5):
				// the route's own answer is 404, so there is no page to
				// bake in — skip with a warning naming the route, the way
				// a dynamic route without StaticPaths is skipped.
				if nf, ok := errors.AsType[*uihost.NotFoundOutletError](err); ok {
					slog.Warn("static: skipped 404-outlet route: a FallbackNotFound outlet had no fill at build time, the route's own answer is 404; it stays reachable via the live server",
						"path", nf.Path)
					continue
				}
				// A page that answers another status (app.SetStatus, a
				// ScreenStatusCode): a static host would serve it 200.
				if st, ok := errors.AsType[*uihost.StatusPageError](err); ok {
					slog.Warn("static: skipped page that answers a status other than 200; a static host would serve it as 200, so it stays reachable via the live server",
						"path", st.Path, "status", st.Status)
					continue
				}
				if err := fail(p, fmt.Errorf("static: render %q: %w", p, err)); err != nil {
					return err
				}
				continue
			}
			// a fill that failed and was
			// contained degrades its outlet on a LIVE server, but an
			// export would freeze the degraded bytes into the page for
			// every visitor — refuse, naming the route and each outlet
			// address, so the operator fixes the fill or excludes the
			// route on purpose.
			if len(fillFailures) > 0 {
				parts := make([]string, 0, len(fillFailures))
				for _, f := range fillFailures {
					// The error text can carry hostile bytes (the lab's
					// broken fills do on purpose); a build log is a
					// sink, so it reaches it scrubbed.
					parts = append(parts, fmt.Sprintf("%s (%v)", f.Addr, textsafe.StripUnsafe(f.Err.Error())))
				}
				if err := fail(p, fmt.Errorf(
					"static: render %q refused: %d fill(s) failed and were contained (a live server degrades the outlet; the export will not bake it): %s",
					p, len(fillFailures), strings.Join(parts, "; "))); err != nil {
					return err
				}
				continue
			}
			if err := fn(route.Path, p, page); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeRedirects gives every exact redirect route (app.Redirect) a page
// at its path, so a link or bookmark to the old URL still lands: a meta
// refresh to the chain's final target, with a plain link as the
// fallback, no script and no style, under the export's own CSP. It also
// writes a `_redirects` rule per redirect (`/old /new 308`, the live
// status) for Netlify and Cloudflare Pages, the hosts that read the
// `_headers` file beside it, which then answer with a real redirect. A
// `_redirects` in the static source is appended after these rules.
//
// A pattern redirect (app.RedirectPattern) matches URLs no file can
// stand for; it is logged as skipped and its URLs answer 404.html. A
// redirect chain that never ends (the live router fails it closed) is
// skipped too. With path locales each locale tree gets its own page, its
// target under the same prefix.
func (b *Builder) writeRedirects(ctx context.Context, pages map[string]bool, rendered *fileSet, owner map[string]string) error {
	locales, byPrefix := b.Host.PathLocales()
	tags := []string{""}
	if byPrefix {
		tags = locales.All()
	}
	var rules strings.Builder
	for _, route := range b.Host.App.Routes() {
		if route.RedirectTo == "" {
			continue
		}
		if b.routeExcluded(route.Path) {
			b.log("skipped excluded redirect %s", route.Path)
			continue
		}
		if strings.Contains(route.Path, ":") {
			slog.Warn("static: skipped pattern redirect: a static host cannot match a pattern, so its URLs answer the export's 404 page; the live server keeps redirecting them",
				"from", route.Path, "to", route.RedirectTo)
			continue
		}
		target, ok := b.Host.App.ResolveRedirect(route.Path)
		if !ok {
			slog.Warn("static: skipped redirect whose chain does not end", "from", route.Path)
			continue
		}
		for _, tag := range tags {
			from, to, lctx := route.Path, target, ctx
			if byPrefix {
				from, to = locales.Path(tag, from), b.localeTarget(locales, tag, to)
				lctx = localeContext(ctx, locales, tag)
			}
			href := b.redirectHref(to, pages)
			rel := filepath.ToSlash(pathToFile(from))
			// A redirect file that differs from a page's, or another
			// redirect's, only in case is the same file on a
			// case-insensitive filesystem; it would replace that page.
			if prev, dup := owner[foldPath(rel)]; dup {
				return fmt.Errorf("static: pages %q and %q both export to %s; give one of them another path", prev, from, rel)
			}
			owner[foldPath(rel)] = from
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, []byte(redirectPage(lctx, href, b.policy()))); err != nil {
				return err
			}
			rendered.add(rel)
			fmt.Fprintf(&rules, "%s %s 308\n", urlBytesEscaper(b.BasePath+from), href)
			b.log("redirect %s -> %s", from, href)
		}
	}
	dst := filepath.Join(b.OutDir, "_redirects")
	if rules.Len() == 0 {
		// A reused OutDir may hold an earlier build's rules for
		// redirects the app has since dropped.
		if err := removeUnder(b.OutDir, dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("static: remove stale _redirects: %w", err)
		}
		return nil
	}
	if err := writeUnder(b.OutDir, dst, []byte(rules.String())); err != nil {
		return err
	}
	rendered.add("_redirects")
	return nil
}

// localeTarget is a redirect target as locale tag's tree links it, by the
// rule its page links follow: a target already naming a locale, or one
// keepUnprefixed keeps (a static file, a path no route resolves), stays
// as written; any other gets tag's prefix.
func (b *Builder) localeTarget(locales i18n.PathLocales, tag, target string) string {
	p := target
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		p = target[:i]
	}
	if t, _ := locales.Split(p); t != "" || b.keepUnprefixed(p) {
		return target
	}
	return locales.Path(tag, target)
}

// redirectHref is a redirect target as the export links it: the page
// form its host serves without a 301 (slashed) when the target is an
// exported page, under BasePath, query and fragment kept.
func (b *Builder) redirectHref(target string, pages map[string]bool) string {
	p, rest := target, ""
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		p, rest = target[:i], target[i:]
	}
	if pages[p] || (!strings.HasSuffix(p, "/") && pages[p+"/"]) {
		p = slashed(p)
	}
	return urlBytesEscaper(b.BasePath + p + rest)
}

// urlBytesEscaper percent-encodes the bytes that would end a URL early
// where the export writes one: a control byte (a live redirect's
// Location gets the same treatment) and a space, which separates the
// fields of a _redirects line.
func urlBytesEscaper(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= 0x20 || c == 0x7f {
			fmt.Fprintf(&out, "%%%02X", c)
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// redirectPage is the document an exported redirect leaves at its old
// path. It carries no script or style: the refresh is a meta, the
// fallback a plain link, so the export's CSP holds for it like any
// page.
func redirectPage(ctx context.Context, href, policy string) string {
	lang := i18n.FromContext(ctx).Tag
	if lang == "" {
		lang = "en"
	}
	// The target lands in a meta refresh, a canonical link and an
	// anchor. HTML escaping leaves javascript: and data: intact, so the
	// scheme goes through the anchor allow-list first; a refused target
	// degrades to an inert "#".
	safe := urlsafe.CleanAnchor(href)
	if safe == "" {
		safe = "#"
	}
	h := stdhtml.EscapeString(safe)
	page := "<!doctype html>\n" +
		"<html lang=\"" + stdhtml.EscapeString(lang) + "\">\n" +
		"<head>\n" +
		"<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<meta http-equiv=\"refresh\" content=\"0; url=" + h + "\">\n" +
		"<meta name=\"robots\" content=\"noindex\">\n" +
		"<link rel=\"canonical\" href=\"" + h + "\">\n" +
		"<title>" + stdhtml.EscapeString(pageRedirectTitle.Text(ctx)) + "</title>\n" +
		"</head>\n" +
		"<body>\n" +
		"<p><a href=\"" + h + "\">" + stdhtml.EscapeString(pageRedirectLink.Text(ctx)) + "</a></p>\n" +
		"</body>\n" +
		"</html>\n"
	return applyStaticCSP(page, policy)
}

// routeExcluded reports whether a route pattern is covered by an
// ExcludeRoutes entry: exact match, or the pattern sits under the
// entry as a whole path segment ("/broken" covers "/broken/load" but
// not "/brokenly"). Entries normalize their trailing slash away.
func (b *Builder) routeExcluded(pattern string) bool {
	for _, ex := range b.ExcludeRoutes {
		ex = strings.TrimSuffix(ex, "/")
		if ex == "" {
			continue
		}
		if pattern == ex || strings.HasPrefix(pattern, ex+"/") {
			return true
		}
	}
	return false
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
// their screen, ask for StaticPaths, and substitute each param map. A
// dynamic route whose screen does not implement StaticPathsProvider (or
// returns zero paths) is skipped at build time, but the builder now warns
// naming the route and the fix, instead of failing silently. The route
// remains reachable via SSR if the server is running.
func expandRoute(ctx context.Context, app *coreapp.App, pattern string) ([]string, error) {
	if !strings.Contains(pattern, ":") {
		return []string{pattern}, nil
	}
	screen, ok := app.Router.ScreenByPattern(pattern)
	if !ok {
		// Pattern is registered but unresolvable. Odd, skip safely.
		return nil, nil
	}
	provider, hasPaths := screen.Component.(coreapp.StaticPathsProvider)
	if !hasPaths {
		slog.Warn("static: dynamic route has no StaticPaths: skipping; implement StaticPaths on the screen to export its pages",
			"route", pattern)
		return nil, nil
	}
	paths := provider.StaticPaths(ctx)
	if len(paths) == 0 {
		slog.Warn("static: dynamic route's StaticPaths returned no paths: skipping; return at least one concrete path to export its pages",
			"route", pattern)
		return nil, nil
	}
	return expandParams(ctx, pattern, paths)
}

// expandParams substitutes each StaticPaths param map into the route pattern.
// It fails closed: any param value whose page file would leave its route's
// directory, an "." / ".." or empty component on disk, a NUL, a backslash,
// or an empty value, aborts the whole build rather than silently writing a
// file outside OutDir (validateDiskValue).
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
	for _, part := range strings.Split(pattern, "/") {
		if !strings.HasPrefix(part, ":") || len(part) <= 1 {
			continue
		}
		key := coreapp.ParamName(part)
		v, ok := params[key]
		if !ok {
			continue
		}
		if err := validateDiskValue(key, v, part); err != nil {
			return "", err
		}
	}
	// Routing form, as the sitemap writes it: substituted raw, the
	// router decoded "a%2Fb" to key "a/b", another record's page.
	return coreapp.PatternPath(pattern, params), nil
}

// validateDiskValue rejects a StaticPaths value whose page file would
// leave its route's directory. It checks what Build writes, not the raw
// value: the segment is encoded the way the sitemap links it
// (PatternPath), then decoded the way a static host maps that URL to a
// file (hostPath). A "/" in a one-segment key ("a/b", linked as a%2Fb)
// is a directory boundary on disk, so that key exports to
// a/b/index.html; a catch-all's "/" separates segments the same way.
// Every component on disk must be non-empty and not "." or "..", and
// the value may not hold a NUL or a backslash (a separator on Windows
// that the encoding does not escape).
func validateDiskValue(key, v, part string) error {
	if v == "" {
		return fmt.Errorf("static: empty StaticPaths value for %q", key)
	}
	if strings.ContainsAny(v, "\\\x00") {
		return fmt.Errorf("static: StaticPaths value for %q contains a NUL or a backslash: %q", key, v)
	}
	disk := hostPath(coreapp.PatternPath(part, map[string]string{key: v}))
	for comp := range strings.SplitSeq(disk, "/") {
		if comp == "" || comp == "." || comp == ".." {
			return fmt.Errorf("static: StaticPaths value for %q writes an empty or traversal path component: %q", key, v)
		}
	}
	return nil
}

// applyStaticMode post-processes a rendered page for the serverless
// static export. It does two things:
//
//  1. Stamps <html> with data-cui-static: the runtime's static-mode
//     switch. When present, the runtime skips the widget catalog fetch,
//     no-ops data-cui-rpc dispatch, and short-circuits data-cui-open,
//     so a click on a dead demo does not fire a request that 404s
//     against the host. Live pages never carry the marker.
//
//  2. Injects a dismissible "run locally" notice via the single shared
//     styling surface (framework/ui.Banner) so server-backed demos read
//     as intentionally inactive rather than broken. When BasePath is
//     set, every root-absolute asset/nav URL is rewritten to resolve
//     under the mount path.
func (b *Builder) applyStaticMode(ctx context.Context, page string) string {
	page = stampStatic(page)
	page = applyStaticCSP(page, b.policy())
	page = b.rewriteBaseURLs(page)
	notice := string(ui.Banner(ui.BannerConfig{
		Ctx:         ctx,
		Title:       pageStaticTitle.Text(ctx),
		Body:        pageStaticBody.Text(ctx),
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

// stampStatic marks <html> with data-cui-static: the runtime's
// static-mode switch. The first "<html" in the document is always the
// real root element (it precedes any body content); the marker is
// value-agnostic, so a bare boolean attribute suffices.
func stampStatic(page string) string {
	if i := strings.Index(page, "<html"); i >= 0 {
		return page[:i+len("<html")] + " data-cui-static" + page[i+len("<html"):]
	}
	return page
}

// baseAttrURL matches a whitespace-delimited src="…", href="…", action="…",
// data-cui-push-state="…" or data-cui-match-prefix="…" whose value is
// root-absolute (leading "/") but NOT protocol-relative ("//").
// data-cui-push-state is included so combobox/palette selection targets get
// base-prefixed on subpath deploys (otherwise selecting a command navigates
// to the apex path and 404s), action so a GET form (the search page's)
// submits under the base, and data-cui-match-prefix because the runtime
// compares the section it names with the page path, which carries the base.
// The leading ([\s]) anchor ensures only real attributes match, never "data-src" /
// "srcset", and code samples are safe because core/markdown escapes quotes
// inside <code> to &quot; (so the ="… pattern never appears in rendered code
// text). Group 3 is the first path byte, re-emitted so the prefix is inserted
// after the leading slash.
var baseAttrURL = regexp.MustCompile(`([\s])(src|href|action|data-cui-push-state|data-cui-match-prefix)="/([^/])`)

// pageLinkAttr matches a root-absolute href, action or
// data-cui-push-state value (the attributes baseAttrURL prefixes), split
// into the path and its optional query or fragment.
var pageLinkAttr = regexp.MustCompile(`([\s](?:href|action|data-cui-push-state)=")(/[^"?#]*)([?#][^"]*)?"`)

// slashPageLinks writes every link to an exported page in the form its
// host serves without a redirect. A page is written as <path>/index.html,
// and GitHub Pages, S3 website hosting, Netlify, Cloudflare Pages and the
// common file servers serve a directory index at <path>/ and answer
// <path> with a 301, so a link spelled /docs cost every click, client
// navigation included, an extra round trip. pages holds the exported
// route paths ("/docs"); a link to anything else (an asset, a contributor
// file, a route the export skipped) is left as written.
func (b *Builder) slashPageLinks(page string, pages map[string]bool) string {
	return pageLinkAttr.ReplaceAllStringFunc(page, func(m string) string {
		sub := pageLinkAttr.FindStringSubmatch(m)
		p := sub[2]
		rel := p
		if b.BasePath != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(p, b.BasePath); !ok || (rel != "" && rel[0] != '/') {
				return m
			}
		}
		if rel == "" {
			rel = "/" // the base path itself, the exported root
		} else if strings.HasSuffix(rel, "/") {
			return m
		}
		// A route registered with its slash (/components/) is linked
		// without one too. The page set holds routing form and an href
		// is escaped: /items/caf%C3%A9 names the page /items/café.
		if key := coreapp.RoutePath(rel); !pages[key] && !pages[key+"/"] {
			return m
		}
		return sub[1] + slashed(p) + sub[3] + `"`
	})
}

// slashed is an exported page's URL path in the form its host serves
// without a redirect: "/" stays, "/about" becomes "/about/".
func slashed(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// pageSet is the exported route paths, as slashPageLinks looks them up.
func pageSet(paths []string) map[string]bool {
	pages := make(map[string]bool, len(paths))
	for _, p := range paths {
		pages[p] = true
	}
	return pages
}

// slashLinks applies slashPageLinks to every written page and to
// 404.html. dumpWidgetAssets applies it to each widget's chrome.
func (b *Builder) slashLinks(res Result) error {
	pages := pageSet(res.Pages)
	files := make([]string, 0, len(res.Pages)+1)
	for _, p := range res.Pages {
		files = append(files, filepath.Join(b.OutDir, pathToFile(p)))
	}
	if slices.Contains(res.Assets, "/404.html") {
		files = append(files, filepath.Join(b.OutDir, "404.html"))
	}
	for _, f := range files {
		page, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("static: slash page links: %w", err)
		}
		if out := b.slashPageLinks(string(page), pages); out != string(page) {
			if err := writeUnder(b.OutDir, f, []byte(out)); err != nil {
				return err
			}
		}
	}
	return nil
}

// rewriteBaseURLs prefixes every root-absolute asset and navigation URL in
// the page with b.BasePath. No-op when BasePath is empty (apex deploy /
// live server). Covers /__gofastr/… assets (runtime.js, modules, CSS,
// module preload links) and internal nav links (/about → /<base>/about) in a
// single pass. External (https://), protocol-relative (//host), fragment
// (#…), and relative URLs are untouched.
func (b *Builder) rewriteBaseURLs(page string) string {
	if b.BasePath == "" {
		return page
	}
	// 1. Attributes: src="…"/href="…"/action="…" (assets, nav links, forms).
	page = baseAttrURL.ReplaceAllString(page, "${1}${2}=\""+b.BasePath+"/${3}")
	// 2. Inline JSON values: the component catalog seeds
	// "stylePath":"/__gofastr/comp/<name>.css" entries the runtime
	// lazy-loads at runtime. Those aren't attributes, so the regex
	// above misses them. Prefix every quoted root-absolute /__gofastr/
	// path. Safe vs code samples: core/markdown escapes quotes in
	// <code> to &quot;, so the literal "/__gofastr/ (real double-quote
	// + path) only occurs in real JSON, never in rendered code text.
	page = strings.ReplaceAll(page, `"/__gofastr/`, `"`+b.BasePath+`/__gofastr/`)
	// 3. The route graph. The runtime takes the client-side path only for
	// a link it finds in the graph, looking the link's pathname up by
	// route path. Step 1 rewrote every href for the base while the graph
	// kept root-relative paths, so on a project page no link was ever
	// recognised and every click was a full page load.
	page = baseRouteGraph.ReplaceAllStringFunc(page, func(block string) string {
		m := baseRouteGraph.FindStringSubmatch(block)
		return m[1] + strings.ReplaceAll(m[2], `"path":"/`, `"path":"`+b.BasePath+`/`) + m[3]
	})
	// 4. The locale set: the runtime finds a link's locale prefix behind
	// the base, so it has to know the base.
	// json.Marshal escapes < > &, so the value cannot close the script.
	base, _ := json.Marshal(b.BasePath)
	page = baseLocales.ReplaceAllStringFunc(page, func(m string) string {
		return baseLocales.FindStringSubmatch(m)[1] + `"base":` + string(base)
	})
	return page
}

// baseLocales matches the base field of the locale set a page of an app
// that routes by locale prefix embeds.
var baseLocales = regexp.MustCompile(`(<script type="application/json" id="gofastr-locales">[^<]*?)"base":""`)

// baseRouteGraph matches the route graph the host embeds in every page,
// whose "path" values are the keys the runtime's navigation looks links
// up by.
var baseRouteGraph = regexp.MustCompile(`(?s)(<script type="application/json" id="gofastr-routes">)(.*?)(</script>)`)

// jsonString is s as the widget catalog encodes it: a quoted JSON string
// with HTML escaping off (ServeWidgetList's encoder setting).
func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(sb.String(), "\n")
}

// dumpWidgetAssets writes the widget catalog JSON and each widget's chrome
// HTML + CSS as query-free static files. Hidden click-to-open widgets
// (command palette, section-menu drawers, modals) are not SSR-inlined, so
// the runtime fetches their chrome from cfg.chromePath on open, which 404s
// on a serverless host. Dumping the same bytes as files lets openWidget
// resolve against the static tree, so every data-cui-open overlay works.
func (b *Builder) dumpWidgetAssets(ctx context.Context, res *Result) error {
	defs := widget.AllForSSR()
	if len(defs) == 0 {
		return nil
	}
	// Catalog JSON: canonical shape from ServeWidgetList (no page filter =
	// every widget). Its style, chrome and state URLs are root-absolute
	// values the runtime fetches at open time; prefix each one for a
	// subpath deploy. They are matched whole and quoted, so a custom
	// StylePath outside /core-ui/widget/ is prefixed too.
	rec := httptest.NewRecorder()
	widget.ServeWidgetList(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))
	cat := rec.Body.String()
	if b.BasePath != "" {
		for _, d := range defs {
			for _, u := range []string{d.StyleURL(), d.ChromeURL(), d.StateURL()} {
				cat = strings.ReplaceAll(cat, jsonString(u), jsonString(b.BasePath+u))
			}
		}
	}
	if err := b.writeRawAsset("/__gofastr/widgets.json", []byte(cat)); err != nil {
		return err
	}
	res.Assets = append(res.Assets, "/__gofastr/widgets.json")
	b.log("wrote %s", "/__gofastr/widgets.json")
	pages := pageSet(res.Pages)
	locales, byPrefix := b.Host.PathLocales()
	// Every file in the export is public. A widget whose live /chrome
	// endpoint refuses an anonymous caller (RequireSession,
	// RequireAuthenticated) gets its style sheet, which the live host
	// serves ungated, and no chrome.
	anon := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, d := range defs {
		if err := b.writeRawAsset(d.StyleURL(), []byte(widget.RenderCSS(d))); err != nil {
			return err
		}
		res.Assets = append(res.Assets, d.StyleURL())
		if !widget.GateSatisfied(d, anon) {
			b.log("wrote widget %s (css only: its chrome is gated)", d.Name)
			continue
		}
		// Chrome HTML may carry root-absolute nav links (/docs/…); rewrite
		// them the same way page HTML is rewritten for a subpath deploy,
		// and slash its page links the way slashLinks does a page's (a
		// section menu's links live here). Its slots render in the
		// locale of the tree it lands in, like the pages beside it.
		dctx := ctx
		if byPrefix {
			dctx = localeContext(ctx, locales, locales.Default)
		}
		chrome := b.slashPageLinks(b.rewriteBaseURLs(widget.RenderChromeCtx(dctx, d)), pages)
		if err := b.writeRawAsset(d.ChromeURL(), []byte(chrome)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, d.ChromeURL())
		// On a /<tag>/ page the runtime asks for the chrome under the
		// same prefix, the way the live router serves it.
		if byPrefix {
			for _, tag := range locales.Prefixed {
				rule := urlprefix.Rule{Prefix: "/" + tag, Locales: locales.All(), Keep: b.keepUnprefixed}
				raw := widget.RenderChromeCtx(localeContext(ctx, locales, tag), d)
				local := b.slashPageLinks(b.rewriteBaseURLs(string(urlprefix.HTML([]byte(raw), rule))), pages)
				at := "/" + tag + d.ChromeURL()
				if err := b.writeRawAsset(at, []byte(local)); err != nil {
					return err
				}
				res.Assets = append(res.Assets, at)
			}
		}
		b.log("wrote widget %s (chrome + css)", d.Name)
	}
	return nil
}

// keepUnprefixed is the locale rule's Keep: a link a locale tree must
// leave unprefixed. The live router serves every path under each locale
// prefix, but the export copies the static source and ExtraDirs once,
// to its root, and writes pages only for routes, so a link to a copied
// file, or to any path no route or redirect takes, keeps its root path.
// A copied file wins over a route at the same path, as it does live.
func (b *Builder) keepUnprefixed(p string) bool {
	dec, err := url.PathUnescape(p)
	if err != nil {
		dec = p
	}
	if rel := strings.TrimPrefix(dec, "/"); fs.ValidPath(rel) && rel != "." {
		if b.staticSourceFile(rel) {
			return true
		}
		for mount, fsys := range b.ExtraDirs {
			sub, ok := strings.CutPrefix(rel, strings.Trim(mount, "/")+"/")
			if strings.Trim(mount, "/") == "" {
				sub, ok = rel, true
			}
			if ok && fsys != nil && isFile(fsys, sub) {
				return true
			}
		}
	}
	// The router and redirect table key on routing form: decoded, a
	// "/" or "%" inside one segment still %2F or %25. An href is
	// escaped, so /caf%C3%A9 must be read as /café.
	rp := coreapp.RoutePath(p)
	if _, _, ok := b.Host.App.Router.Resolve(rp); ok {
		return false
	}
	if _, ok := b.Host.App.ResolveRedirect(rp); ok {
		return false
	}
	return true
}

// staticSourceFile reports whether the host's static source holds a
// regular file at rel, a valid fs path (a directory named like a page
// does not count).
func (b *Builder) staticSourceFile(rel string) bool {
	if dir := b.Host.StaticDir(); dir != "" {
		return isFile(os.DirFS(dir), rel)
	}
	if fsys := b.Host.StaticFS(); fsys != nil {
		return isFile(fsys, rel)
	}
	return false
}

// isFile reports whether fsys holds a regular file at name.
func isFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// localeContext is ctx as a request for the locale tag reads it: the
// translator's locale and the path locale the router would set.
func localeContext(ctx context.Context, locales i18n.PathLocales, tag string) context.Context {
	return i18n.WithPathLocale(i18n.WithContext(ctx, i18n.Locale{Tag: tag}), locales, tag)
}

// dumpSEOAssets writes sitemap.xml (with the parts it indexes, past one
// file's limits) and robots.txt when the host configured
// WithSitemap/WithRobots. Skips any file the app's static source already
// provides (the user's copy is copied verbatim later and must win); a
// user-supplied sitemap.xml replaces the generated parts too. No-op when
// neither option is configured.
func (b *Builder) dumpSEOAssets(res *Result) error {
	var files []uihost.SitemapFile
	if sitemaps, ok := b.exportedSitemap(pageSet(res.Pages)); ok {
		if b.staticSourceHas("sitemap.xml") {
			b.log("kept user-supplied /sitemap.xml")
		} else {
			files = append(files, sitemaps...)
		}
	}
	if robots, ok := b.Host.RobotsTXT(b.BasePath); ok {
		if b.staticSourceHas("robots.txt") {
			b.log("kept user-supplied /robots.txt")
		} else {
			files = append(files, uihost.SitemapFile{Path: "/robots.txt", Body: robots})
		}
	}
	for _, f := range files {
		if err := b.writeRawAsset(f.Path, []byte(f.Body)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.Path)
		b.log("wrote %s", f.Path)
	}
	return nil
}

// sitemapLoc matches one sitemap URL.
var sitemapLoc = regexp.MustCompile(`<loc>([^<]*)</loc>`)

// exportedSitemap is the host's sitemap files with only the pages this
// export wrote: a page it skipped (an excluded route, a 404-outlet
// route, one that answers a status other than 200, one a redirect
// takes) has no file on the static host, so a crawler sent there finds
// 404.html. The host asks about each page by the routing path the
// export wrote it under, so a StaticPaths that answers differently
// per call can only drop an entry, never list an unwritten page. Page
// URLs then take the slash their links carry (slashSitemapLocs); a
// sitemap index names part files, not pages, and is left alone.
func (b *Builder) exportedSitemap(pages map[string]bool) ([]uihost.SitemapFile, bool) {
	files, ok := b.Host.SitemapFiles(b.BasePath, func(p string) bool { return pages[p] })
	if !ok {
		return nil, false
	}
	escaped := escapedPageSet(pages)
	for i := range files {
		if len(files) == 1 || i > 0 {
			files[i].Body = slashSitemapLocs(files[i].Body, escaped)
		}
	}
	return files, true
}

// escapedPageSet is pages keyed by the escaped path a sitemap <loc>
// spells (app.EscapeRoutePath), the one spelling every ID has: a
// routing-form key keeps "%" as %25 but Unicode raw, so an ID holding
// both ("café%") matched neither the loc nor its plain unescape.
func escapedPageSet(pages map[string]bool) map[string]bool {
	out := make(map[string]bool, len(pages))
	for p := range pages {
		out[coreapp.EscapeRoutePath(p)] = true
	}
	return out
}

// slashSitemapLocs gives every sitemap URL that names an exported page
// the trailing slash its links carry (see slashPageLinks), so a crawler
// is not sent through the host's 301 for every page. A <loc> is the
// site URL followed by the page path, so the page is found as a
// segment-aligned suffix of the URL's escaped path; escaped holds the
// pages in that spelling (escapedPageSet).
func slashSitemapLocs(doc string, escaped map[string]bool) string {
	return sitemapLoc.ReplaceAllStringFunc(doc, func(m string) string {
		loc := sitemapLoc.FindStringSubmatch(m)[1]
		u, err := url.Parse(stdhtml.UnescapeString(loc))
		if err != nil || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Path, "/") {
			return m
		}
		ep := u.EscapedPath()
		for i := 0; i < len(ep); i++ {
			if ep[i] == '/' && (escaped[ep[i:]] || escaped[ep[i:]+"/"]) {
				return "<loc>" + loc + "/</loc>"
			}
		}
		return m
	})
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
// Each file it writes is added to written under its file path (the
// offline page's URL is a directory).
func (b *Builder) dumpPWAAssets(res *Result, frameworkAssetCount int, written *fileSet) error {
	if !b.Host.PWAEnabled() {
		return nil
	}
	manifest, err := b.Host.PWAManifestJSON(context.Background(), b.BasePath)
	if err != nil {
		return fmt.Errorf("static: pwa manifest: %w", err)
	}
	// Static exports get the full-site worker: the page set is closed and
	// immutable, so every exported page and asset is precached at install
	// and the whole site works offline. The tree hash makes a
	// content-only redeploy rotate the cache version.
	type pwaFile struct {
		urlPath string
		relFile string
		body    []byte
	}
	// A locale tree's pages link their own manifest, whose start_url
	// launches into that tree.
	var localeManifests []pwaFile
	if locales, byPrefix := b.Host.PathLocales(); byPrefix {
		for _, tag := range locales.Prefixed {
			body, err := b.Host.PWAManifestJSON(localeContext(context.Background(), locales, tag), b.BasePath)
			if err != nil {
				return fmt.Errorf("static: pwa manifest %s: %w", tag, err)
			}
			localeManifests = append(localeManifests, pwaFile{locales.Path(tag, "/manifest.webmanifest"), filepath.Join(tag, "manifest.webmanifest"), body})
		}
	}
	// The hash leaves out every file this surface writes: a rebuild
	// finds them from the last build and a fresh build does not. They
	// are respelled first, so a leftover in another case that is the
	// same file on this disk (MANIFEST.webmanifest on APFS) takes the
	// generated name and drops out, and one that is a file of its own
	// on a case-sensitive disk stays in the hash like any other file.
	generated := map[string]bool{"manifest.webmanifest": true, "service-worker.js": true}
	for _, f := range localeManifests {
		generated[filepath.ToSlash(f.relFile)] = true
	}
	const pwaDir = "__gofastr/pwa"
	genPaths := append(slices.Sorted(maps.Keys(generated)), pwaDir+"/register.js", pwaDir+"/offline/index.html")
	if err := respell(b.OutDir, genPaths); err != nil {
		return err
	}
	// An alias of a generated file (a symlink or hard link under any
	// name) would hash the last build's copy, and change with every
	// build: it is left out like the file it names. A file the static
	// source supplies is the user's, not generated, and its aliases
	// stay in the hash.
	var genFiles []fs.FileInfo
	for _, g := range genPaths {
		if b.staticSourceHas(g) {
			continue
		}
		if fi, err := os.Stat(filepath.Join(b.OutDir, filepath.FromSlash(g))); err == nil {
			genFiles = append(genFiles, fi)
		}
	}
	contentHash, err := hashExportTree(b.OutDir, func(rel string) bool {
		if generated[rel] || strings.HasPrefix(rel, pwaDir+"/") {
			return true
		}
		fi, err := os.Stat(filepath.Join(b.OutDir, filepath.FromSlash(rel)))
		return err == nil && slices.ContainsFunc(genFiles, func(g fs.FileInfo) bool { return os.SameFile(fi, g) })
	})
	if err != nil {
		return fmt.Errorf("static: fingerprint export: %w", err)
	}
	if frameworkAssetCount > len(res.Assets) {
		frameworkAssetCount = len(res.Assets)
	}
	// Pages are precached under the URL their links name (see
	// slashPageLinks): a client navigation is a plain fetch, which the
	// worker matches exactly, so /about would never answer /about/.
	// The worker fetches and matches escaped URLs: a page or file named
	// "a?b" precached raw is fetched as /a with a query, its 404 fails
	// the whole install, and a navigation never matches it.
	pages := make([]string, len(res.Pages))
	for i, p := range res.Pages {
		pages[i] = slashed(coreapp.EscapeRoutePath(p))
	}
	sw, err := b.Host.PWAStaticServiceWorkerJS(b.BasePath, uihost.PWAStaticExport{
		Pages:          pages,
		Assets:         escapedURLs(res.Assets[:frameworkAssetCount]),
		OptionalAssets: escapedURLs(res.Assets[frameworkAssetCount:]),
		ContentHash:    contentHash,
	})
	if err != nil {
		return fmt.Errorf("static: pwa service worker: %w", err)
	}
	// The offline page is rendered basePath-neutral by the host; its
	// asset links get the same base rewrite as every exported page. It
	// also gets the data-cui-static stamp so the runtime's static-mode
	// guards apply, but not the "run locally" banner. An offline
	// fallback is not a demo page. Its page links take the slash the
	// pages' own do (slashPageLinks), and it carries the export's CSP
	// meta like every page: a host that ignores _headers serves it with
	// no other policy.
	offline := b.slashPageLinks(b.rewriteBaseURLs(stampStatic(b.Host.PWAOfflineHTML())), pageSet(res.Pages))
	offline = applyStaticCSP(offline, b.policy())
	files := []pwaFile{
		{"/manifest.webmanifest", "manifest.webmanifest", manifest},
		{"/service-worker.js", "service-worker.js", []byte(sw)},
		{pwaRegisterJS, filepath.Join("__gofastr", "pwa", "register.js"), []byte(b.Host.PWARegisterJS(b.BasePath))},
		{"/__gofastr/pwa/offline", filepath.Join("__gofastr", "pwa", "offline", "index.html"), []byte(offline)},
	}
	files = append(files, localeManifests...)
	for _, f := range files {
		// User-supplied files win: before this surface moved to the end of
		// the build, a manifest/worker shipped in the app's static dir
		// landed after (and therefore over) the generated one. Preserve
		// that precedence by never clobbering a file the static SOURCE
		// provides (the source, not OutDir: a previous build's leftovers
		// in a reused OutDir must not suppress regeneration).
		if b.staticSourceHas(f.relFile) {
			b.log("kept user-supplied %s", f.urlPath)
			continue
		}
		dst := filepath.Join(b.OutDir, f.relFile)
		if err := b.ensureContained(dst); err != nil {
			return err
		}
		if err := writeUnder(b.OutDir, dst, f.body); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.urlPath)
		written.add(filepath.ToSlash(f.relFile))
		b.log("wrote %s", f.urlPath)
	}
	return nil
}

// escapedURLs is the request URL of each written file: Result.Assets
// holds the decoded path a static host resolves to the file.
func escapedURLs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = (&url.URL{Path: p}).EscapedPath()
	}
	return out
}

// pwaRegisterJS is the WithPWA registration script, on the rail and
// written by dumpPWAAssets.
const pwaRegisterJS = "/__gofastr/pwa/register.js"

// staticSourceHas reports whether the host's static asset source (dir or
// embedded FS) provides rel, meaning the user shipped their own copy.
//
// The spelling must match: on a case-insensitive filesystem a static
// MANIFEST.webmanifest opened as manifest.webmanifest, the generated
// file was skipped, and a case-sensitive host had no manifest. The other
// spelling is copied as itself and refused as a case twin.
func (b *Builder) staticSourceHas(rel string) bool {
	if dir := b.Host.StaticDir(); dir != "" {
		return spelledIn(os.DirFS(dir), filepath.ToSlash(rel))
	}
	if fsys := b.Host.StaticFS(); fsys != nil {
		return spelledIn(fsys, filepath.ToSlash(rel))
	}
	return false
}

// spelledIn reports whether fsys holds rel, a slash path, under exactly
// that spelling: each directory listing names each segment.
func spelledIn(fsys fs.FS, rel string) bool {
	// Absent in any spelling is the common answer, and one Stat gives it.
	// Any other Stat error (an adapter whose files read but do not stat)
	// leaves the answer to the listings.
	if _, err := fs.Stat(fsys, rel); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	dir := "."
	for _, name := range strings.Split(rel, "/") {
		ents, err := fs.ReadDir(fsys, dir)
		if err != nil || !slices.ContainsFunc(ents, func(e fs.DirEntry) bool { return e.Name() == name }) {
			return false
		}
		dir = path.Join(dir, name)
	}
	return true
}

// hashExportTree fingerprints every file already written to outDir
// (deterministic sorted walk over relative path + streamed bytes). The
// PWA outputs themselves are excluded: they are (re)generated after
// this hash, and a reused OutDir still holds the PREVIOUS build's
// copies; hashing those would make every rebuild byte-different even
// with identical content. A content-only redeploy therefore changes
// the hash → the worker bytes → the cache version, and installed
// clients re-precache; an unchanged rebuild reproduces the same worker.
//
// skip reports, for a slash path under outDir, whether it is left out.
func hashExportTree(outDir string, skip func(string) bool) (string, error) {
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
		if skip(slashRel) {
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
// the runtime-JS base bake. Callers that need base rewriting apply the
// appropriate rewriter (rewriteBaseURLs for HTML, manual prefixing for JSON)
// before calling. Guards path containment via ensureContained.
func (b *Builder) writeRawAsset(urlPath string, body []byte) error {
	if err := refuseSeparatorName(urlPath); err != nil {
		return err
	}
	dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
	if err := b.ensureContained(dst); err != nil {
		return err
	}
	return writeUnder(b.OutDir, dst, body)
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

// hostPath is the decoded path a static host resolves a routing-form
// page path's URL to: /items/a%252Fb is requested as /items/a%252Fb
// and read from items/a%2Fb, not items/a/b.
func hostPath(p string) string {
	if d, err := url.PathUnescape(coreapp.EscapeRoutePath(p)); err == nil {
		return d
	}
	return p
}

// pathToFile turns a URL path into the relative file path SSG output uses.
//
//	"/"               -> "index.html"
//	"/about"          -> "about/index.html"
//	"/products/abc"   -> "products/abc/index.html"
func pathToFile(p string) string {
	clean := strings.Trim(hostPath(p), "/")
	if clean == "" {
		return "index.html"
	}
	return filepath.Join(clean, "index.html")
}

// docStatus is the status a page's render answers when it is not 200:
// the screen's ScreenStatusCode first, then what the render set through
// app.SetStatus. Zero means 200.
func docStatus(c any, recorded int) int {
	if sc, ok := c.(uihost.ScreenStatusCode); ok {
		if code := sc.ScreenStatusCode(); code != 0 && code != http.StatusOK {
			return code
		}
	}
	if recorded == http.StatusOK {
		return 0
	}
	return recorded
}

// withPageRequest gives ctx the GET the page at routing path p arrives
// as, the request its exported HTML renders under
// (uihost.RenderStaticPageResult) and its live /llm.md rebases onto: a
// screen that reads the request (entityui.List's record links) renders
// its doc instead of panicking into an error placeholder. The path is
// escaped the way the live URL is, so a "?" or "#" a param holds stays
// in the path.
func withPageRequest(ctx context.Context, p string) context.Context {
	if coreapp.RequestFromContext(ctx) != nil {
		return ctx
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreapp.EscapeRoutePath(p), nil)
	if err != nil {
		return ctx
	}
	return coreapp.WithRequest(ctx, req)
}

// pathToLLMFile turns a URL path into the relative file path for the
// per-page LLM documentation. Dynamic routes are expected to be expanded
// (via expandRoute) before being passed in.
//
//	"/"              -> "llm.md"
//	"/about"         -> "about/llm.md"
//	"/products/abc"  -> "products/abc/llm.md"
func pathToLLMFile(p string) string {
	clean := strings.Trim(hostPath(p), "/")
	if clean == "" {
		return "llm.md"
	}
	return filepath.Join(clean, "llm.md")
}

// dumpRailScripts fetches each extra-script rail entry through b.Handler
// and writes it into the export. A file this build already wrote (a
// framework asset, the static dir, ExtraDirs) wins; a leftover from an
// earlier build in a reused OutDir does not. The PWA registration script
// is skipped because dumpPWAAssets writes its base-path-aware copy, and
// a src that is not a same-origin path (a CDN URL, a relative src) is
// not the app's to serve and is left to the browser.
func (b *Builder) dumpRailScripts(ctx context.Context, res *Result, routes []string, rendered *fileSet) error {
	if b.Handler == nil {
		return nil
	}
	// Keyed folded: a script in another case is the same file on a
	// case-insensitive filesystem.
	written := newFileSet()
	for _, p := range res.Assets {
		written.add(p)
	}
	for _, src := range b.Host.ExtraScriptSrcs(routes) {
		u, err := url.Parse(src)
		if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "/\\") {
			continue
		}
		// The file goes under the decoded path, the name a static host
		// resolves the request to.
		urlPath := path.Clean(u.Path)
		// rendered holds every page and redirect page, compared in any
		// case.
		if rendered.has(strings.TrimPrefix(urlPath, "/")) {
			return fmt.Errorf("static: extra script %s would overwrite the exported page at the same path", urlPath)
		}
		// The same spelling is the file the build wrote, which wins.
		// Another spelling would be skipped while every page still asks
		// for it: a 404 on a case-sensitive host.
		if own, ok := written.spelling(urlPath); ok {
			if own != urlPath {
				return caseTwinError(own, urlPath)
			}
			continue
		}
		if urlPath == pwaRegisterJS && b.Host.PWAEnabled() {
			continue
		}
		target := u.EscapedPath()
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		b.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return fmt.Errorf("static: extra script %s answered %d; every exported page loads it", urlPath, rec.Code)
		}
		if err := b.writeRawAsset(urlPath, b.rewriteJSAsset(rec.Body.Bytes())); err != nil {
			return err
		}
		res.Assets = append(res.Assets, urlPath)
		written.add(urlPath)
		b.log("script %s", urlPath)
	}
	return nil
}

// dumpExtraDirs copies every Builder.ExtraDirs filesystem under its mount
// path. Deterministic (sorted mounts, sorted walk) and non-clobbering:
// a file this build already wrote (a page, a framework asset, an earlier
// mount's file) or one the static SOURCE provides wins. What decides is
// this build and the source, never what sits in OutDir: Watch rebuilds
// into the same directory, and the previous build's copy of an extra
// file must not keep the new one out.
func (b *Builder) dumpExtraDirs(res *Result, rendered *fileSet) error {
	// Keyed folded, like every lookup below.
	written := newFileSet()
	for _, p := range res.Assets {
		written.add(p)
	}
	mounts := make([]string, 0, len(b.ExtraDirs))
	for m := range b.ExtraDirs {
		mounts = append(mounts, m)
	}
	sort.Strings(mounts)
	for _, mount := range mounts {
		fsys := b.ExtraDirs[mount]
		if fsys == nil {
			continue
		}
		if err := refuseSeparatorName(mount); err != nil {
			return fmt.Errorf("static: extra dir: %w", err)
		}
		cleanMount := "/" + strings.Trim(mount, "/")
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if err := refuseSeparatorName(p); err != nil {
				return err
			}
			urlPath := path.Join(cleanMount, p)
			rel := strings.TrimPrefix(urlPath, "/")
			if !publishable(rel) {
				b.log("skipped %s (not published)", urlPath)
				return nil
			}
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if own, ok := written.spelling(urlPath); ok && own != urlPath {
				return caseTwinError(own, urlPath)
			}
			if written.has(urlPath) || rendered.has(rel) || b.staticSourceHas(filepath.FromSlash(rel)) {
				b.log("kept %s (extra dir %s skipped)", urlPath, mount)
				return nil
			}
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, data); err != nil {
				return err
			}
			res.Assets = append(res.Assets, urlPath)
			written.add(urlPath)
			b.log("copied extra %s", urlPath)
			return nil
		})
		if err != nil {
			return fmt.Errorf("static: extra dir %q: %w", mount, err)
		}
	}
	return nil
}

// writeUnder writes data to dst, a path under outDir, through an
// os.Root opened on outDir: every component below outDir is resolved in
// the kernel, so a symlink planted inside the output tree fails the
// write instead of redirecting it outside (2026-10-27 I4). The export
// is public output, so files are 0644 and directories 0755.
func writeUnder(outDir, dst string, data []byte) error {
	rel, err := relUnder(outDir, dst)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	return root.WriteFile(rel, data, 0o644)
}

// removeUnder removes dst, a path under outDir, through an os.Root.
func removeUnder(outDir, dst string) error {
	rel, err := relUnder(outDir, dst)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(rel)
}

// relUnder is dst relative to outDir, refused when it leaves outDir.
func relUnder(outDir, dst string) (string, error) {
	rel, err := filepath.Rel(outDir, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("static: refusing to write outside OutDir: %q", dst)
	}
	return rel, nil
}

// keepRendered reports whether the static source's file at rel (a slash
// path) is refused because the build wrote a file there, in any case,
// and logs the refusal. Any other file, an index.html under a path no
// page takes included, is copied.
func keepRendered(rel string, rendered *fileSet, log func(string, ...any)) bool {
	own, ok := rendered.spelling(rel)
	if !ok {
		return false
	}
	if own == rel {
		log("kept the rendered page /%s (static source copy refused)", rel)
	} else {
		log("kept the rendered page /%s (static source copy /%s refused)", own, rel)
	}
	return true
}

// publishable reports whether rel, a slash path in the export, is copied
// into the public tree: not a path core/static refuses (a dot segment
// such as .env or .git/config, or a server config file), the same set
// the UI host refuses to serve. A leading .well-known/ is the public
// namespace RFC 8615 reserves, and the root .nojekyll is the marker
// GitHub Pages reads; both are copied.
func publishable(rel string) bool {
	if rel == ".nojekyll" {
		return true
	}
	return !corestatic.Refuses(strings.TrimPrefix(rel, ".well-known/"))
}

// generatedHostRules reports whether rel (a slash path) is a host rule
// file the build wrote: the root _headers, always, and the root
// _redirects when the app has redirects. A static source's copy of one is
// appended to it rather than refused, so the app's rules (Cache-Control,
// HSTS, legacy redirects) apply and the generated ones, the header-only
// frame-ancestors guard among them, stay first.
func generatedHostRules(rel string, rendered *fileSet) bool {
	return (rel == "_headers" || rel == "_redirects") && rendered.hasExact(rel)
}

// appendHostRules appends the static source's rules to the generated
// file at rel. Both generated files end in a newline, so the first
// appended line starts a line of its own.
func appendHostRules(root *os.Root, rel string, data []byte, log func(string, ...any)) error {
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	log("appended the static source's %s to the generated one", rel)
	return nil
}

func copyDir(src, dst string, rendered *fileSet, res *Result, log func(string, ...any)) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
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
		// ToSlash first: on Windows the separators are the path's own.
		if err := refuseSeparatorName(filepath.ToSlash(rel)); err != nil {
			return err
		}
		if !publishable(filepath.ToSlash(rel)) {
			log("skipped %s (not published)", rel)
			return nil
		}
		if generatedHostRules(filepath.ToSlash(rel), rendered) {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return appendHostRules(root, rel, data, log)
		}
		if keepRendered(filepath.ToSlash(rel), rendered, log) {
			return nil
		}
		out := filepath.Join(dst, rel)
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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

func copyFS(fsys fs.FS, dst string, rendered *fileSet, res *Result, log func(string, ...any)) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if err := refuseSeparatorName(path); err != nil {
			return err
		}
		if !publishable(path) {
			log("skipped %s (not published)", path)
			return nil
		}
		if generatedHostRules(path, rendered) {
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			return appendHostRules(root, filepath.FromSlash(path), data, log)
		}
		if keepRendered(path, rendered, log) {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, path)
		rel := filepath.FromSlash(path)
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		if err := root.WriteFile(rel, data, 0o644); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/"+filepath.ToSlash(path))
		log("copied %s -> %s", path, out)
		return nil
	})
}

// staticExportCSP is the Content-Security-Policy a static export carries
// with it.
//
// Every browser gadget the runtime can be pushed into, an injected
// data-behavior <script src>, a signal-driven URL attribute, a fetch
// aimed off-origin, is mitigated in production by the default
// `default-src 'self'` that core/middleware.SecurityHeaders sets as a
// response HEADER. A static export is a directory of files: S3, GitHub
// Pages and friends set no such header, so the export was the one
// deployment target where the mitigation simply was not there.
//
// It mirrors the served default and adds the two directives the audit
// recommended for both: object-src 'none' (plugin content is a
// script-execution surface with no legitimate use here) and form-action
// 'self' (an injected form cannot post the page's data elsewhere).
const staticExportCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"

// policy is the Content-Security-Policy this build ships: the
// configured one, or the built-in default.
func (b *Builder) policy() string {
	if p := strings.TrimSpace(b.ContentSecurityPolicy); p != "" {
		return p
	}
	return staticExportCSP
}

// checkExportCSP refuses a policy the export cannot write safely. A
// nonce source is per response on a server; baked into files every
// visitor downloads, it is a published value that any injected script
// can carry, and the export links every script, so none needs one. A
// control byte would end the _headers line and start a header (or a
// path block) of the policy's choosing. A quote or angle bracket is no
// CSP token (RFC 9110 field values and CSP's grammar keep them out) and
// would end the <meta> attribute.
func checkExportCSP(policy string) error {
	for i := 0; i < len(policy); i++ {
		if c := policy[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("static: Content-Security-Policy contains control byte %#x; a header value is one line", c)
		}
	}
	if strings.ContainsAny(policy, "\"<>") {
		return fmt.Errorf("static: Content-Security-Policy contains a quote or angle bracket, which no CSP source uses: %q", policy)
	}
	for src := range strings.FieldsSeq(strings.NewReplacer(";", " ", ",", " ").Replace(policy)) {
		if strings.HasPrefix(strings.ToLower(src), "'nonce-") {
			return fmt.Errorf("static: Content-Security-Policy source %s is a nonce: a static file gives every visitor the same one, so it protects nothing; drop it (exported pages link every script)", src)
		}
	}
	return nil
}

// metaIgnored names the directives a <meta>-delivered policy ignores
// (CSP3 §3.1, "The <meta> element"): browsers log a console warning for
// each and enforce nothing, so the meta leaves them out. _headers keeps
// them.
var metaIgnored = map[string]bool{"frame-ancestors": true, "report-uri": true, "sandbox": true}

// metaPolicies splits a header value into the policies a <meta> can
// carry: one per comma-separated policy, since a <meta> parses a single
// one, with the directives a meta ignores dropped. A policy left with no
// directive is dropped.
func metaPolicies(policy string) []string {
	var out []string
	for part := range strings.SplitSeq(policy, ",") {
		var kept []string
		for d := range strings.SplitSeq(part, ";") {
			d = strings.Join(strings.Fields(d), " ")
			if d == "" {
				continue
			}
			name, _, _ := strings.Cut(d, " ")
			if metaIgnored[strings.ToLower(name)] {
				continue
			}
			kept = append(kept, d)
		}
		if len(kept) > 0 {
			out = append(out, strings.Join(kept, "; "))
		}
	}
	return out
}

// applyStaticCSP inserts the policy as in-document metas, immediately
// after <head> so they precede anything that can trigger a fetch. A
// page that already carries one (the print shell, say) is left alone.
// policy has passed checkExportCSP, so it holds no quote or bracket to
// escape; & is escaped all the same.
func applyStaticCSP(page, policy string) string {
	if strings.Contains(page, "http-equiv=\"Content-Security-Policy\"") {
		return page
	}
	var meta strings.Builder
	for _, p := range metaPolicies(policy) {
		meta.WriteString("\n<meta http-equiv=\"Content-Security-Policy\" content=\"" + strings.ReplaceAll(p, "&", "&amp;") + "\">")
	}
	return insertAfterHead(page, meta.String())
}

// insertAfterHead puts s right after the document's opening <head> tag.
func insertAfterHead(page, s string) string {
	if s == "" {
		return page
	}
	if i := strings.Index(page, "<head"); i >= 0 {
		if j := strings.IndexByte(page[i:], '>'); j >= 0 {
			at := i + j + 1
			return page[:at] + s + page[at:]
		}
	}
	return page
}

// writeHeadersFile emits a Netlify / Cloudflare Pages `_headers` file so
// the policy arrives as a REAL header on hosts that read one. Both ship
// because neither alone covers the field, but they do NOT cover the same
// directives: per CSP L3 §3.1 a <meta>-delivered policy ignores
// frame-ancestors (as well as report-uri and sandbox), so the meta covers
// the fetch directives everywhere while the clickjacking guard lands only
// on hosts that read this file.
func writeHeadersFile(outDir, policy string) error {
	body := "/*\n" +
		"  Content-Security-Policy: " + policy + "\n" +
		"  X-Content-Type-Options: nosniff\n" +
		"  Referrer-Policy: strict-origin-when-cross-origin\n"
	return writeUnder(outDir, filepath.Join(outDir, "_headers"), []byte(body))
}

// foldPath maps every rune of p to the smallest rune in its Unicode
// simple case-fold orbit, the relation strings.EqualFold uses, so two
// paths a case-insensitive filesystem treats as one fold to one key.
// p is decomposed first (NFD): APFS also treats a precomposed "café"
// and "cafe" with a combining accent as one name.
func foldPath(p string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, norm.NFD.String(p))
}
