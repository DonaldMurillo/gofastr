package static

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/rootfs"
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
	// An os.DirFS value is read through os.Root, so a symlink out of
	// its directory is skipped. A symlink in any other FS, a wrapped
	// os.DirFS (fs.Sub) included, is skipped wherever it points.
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

	// extraDirs is ExtraDirs with each os.DirFS re-rooted through
	// os.Root (rootfs.ConfineDirFS), set at the start of Build: a
	// symlink in an extra directory pointing outside it is refused at
	// open, as the UI host's SetStaticFS refuses it, instead of copying
	// whatever it names into the public tree.
	extraDirs map[string]fs.FS
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
	ctx = uihost.WithStaticBasePath(ctx, b.BasePath)
	if b.Host == nil {
		return Result{}, fmt.Errorf("static: Host is required")
	}
	if strings.TrimSpace(b.OutDir) == "" {
		return Result{}, fmt.Errorf("static: OutDir is required")
	}
	if err := checkExportCSP(b.policy()); err != nil {
		return Result{}, err
	}
	// The page manifest lives beside OutDir; an OutDir with no parent is
	// refused here, before anything is written into it.
	if _, _, err := b.pageManifest(); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(b.OutDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("static: create out dir: %w", err)
	}
	b.extraDirs = make(map[string]fs.FS, len(b.ExtraDirs))
	for mount, fsys := range b.ExtraDirs {
		b.extraDirs[mount] = rootfs.ConfineDirFS(fsys)
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

// localeContext is ctx as a request for the locale tag reads it: the
// translator's locale and the path locale the router would set.
func localeContext(ctx context.Context, locales i18n.PathLocales, tag string) context.Context {
	return i18n.WithPathLocale(i18n.WithContext(ctx, i18n.Locale{Tag: tag}), locales, tag)
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
