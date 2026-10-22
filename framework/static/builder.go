package static

import (
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
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
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
	if err := os.MkdirAll(b.OutDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("static: create out dir: %w", err)
	}

	res := Result{}
	// Route patterns with at least one exported page: the identities a
	// document-scoped rail script's scope is asked about.
	var renderedRoutes []string

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
			lctx = i18n.WithPathLocale(i18n.WithContext(ctx, i18n.Locale{Tag: tag}), locales, tag)
			rule = urlprefix.Rule{Prefix: i18n.PathPrefix(lctx), Locales: locales.All()}
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
			html := b.applyStaticMode(lctx, page)
			dst := filepath.Join(b.OutDir, pathToFile(p))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if err := writeFile(dst, []byte(html)); err != nil {
				return err
			}
			b.log("rendered %s -> %s", p, dst)
			res.Pages = append(res.Pages, p)
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
			if err := b.writeRawAsset(urlPath, files[urlPath]); err != nil {
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
	if err := writeHeadersFile(b.OutDir); err != nil {
		return res, fmt.Errorf("static: write _headers: %w", err)
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
		if err := writeFile(dst, []byte(html)); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, "/404.html")
		b.log("rendered not-found page -> %s", dst)
	}

	// Page links, once every page is known: see slashPageLinks.
	if err := b.slashLinks(res); err != nil {
		return res, err
	}

	// LLM documentation: per-page llm.md and top-level index.
	if !b.Host.App.NoLLMMD {
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
				return res, fmt.Errorf("static: expand llm.md %q: %w", route.Path, err)
			}
			for _, p := range paths {
				// Per-URL doc: a dynamic route expands to N pages, each
				// with its own loaded title + content. The pattern-level
				// ScreenLLMMD would stamp every page with one generic doc
				// (a placeholder, for ScreenLoader screens). Fall back to
				// the pattern doc if the concrete path doesn't resolve.
				var md string
				if doc, ok := coreapp.ScreenLLMMDForPath(ctx, b.Host.App, p); ok {
					md = doc.MD
				} else {
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
		// Build-context policy evaluation: gated screens appear in the
		// exported index metadata-free (no auth at build time).
		indexMD := coreapp.AppLLMMDCtx(ctx, b.Host.App)
		if err := writeFile(filepath.Join(b.OutDir, "llm-pages.md"), []byte(indexMD)); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, "/llm-pages.md")
		b.log("wrote llm-pages.md index")
	}

	// SEO surface: sitemap.xml and robots.txt, mirroring the live
	// WithSitemap/WithRobots handlers so a static deploy keeps the same
	// crawler contract as the live server. Same bytes as the HTTP
	// handlers (single source: UIHost.SitemapXML / UIHost.RobotsTXT),
	// with BasePath folded into <loc> entries and the derived Sitemap:
	// URL. User-supplied files in the static source win, matching the
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
	for _, a := range assets {
		dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(a.urlPath, "/")))
		if err := writeFile(dst, b.rewriteJSAsset(a.body)); err != nil {
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
	if err := b.dumpWidgetAssets(&res); err != nil {
		return res, err
	}

	// Everything recorded so far is framework-generated and precached
	// atomically; user static-dir files below are precached best-effort.
	frameworkAssetCount := len(res.Assets)

	// Static assets: either filesystem dir or embedded FS.
	if dir := b.Host.StaticDir(); dir != "" {
		if err := copyDir(dir, b.OutDir, &res, b.log); err != nil {
			return res, err
		}
	} else if fsys := b.Host.StaticFS(); fsys != nil {
		if err := copyFS(fsys, b.OutDir, &res, b.log); err != nil {
			return res, err
		}
	}

	// Extra mounted directories (SDK artifacts, other host-declared
	// downloads). After the user static dir so its files win; before the
	// PWA surface so the worker fingerprints the complete export.
	if err := b.dumpExtraDirs(&res); err != nil {
		return res, err
	}
	if err := b.dumpRailScripts(ctx, &res, renderedRoutes); err != nil {
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
	if err := b.dumpPWAAssets(&res, frameworkAssetCount); err != nil {
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
// It fails closed: any param value that would let the generated URL escape its
// route segment, a path separator, an "." / ".." traversal component, a NUL,
// or an empty value, aborts the whole build rather than silently writing a
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
		if !strings.HasPrefix(part, ":") || len(part) <= 1 {
			continue
		}
		key := coreapp.ParamName(part)
		v, ok := params[key]
		if !ok {
			continue
		}
		// A catch-all segment (":name*") may legitimately carry a
		// multi-segment remainder; any other param fills exactly one
		// path component and is validated as such.
		if strings.HasSuffix(part, "*") {
			if err := validateCatchAllValue(key, v); err != nil {
				return "", err
			}
		} else {
			if err := validateParamValue(key, v); err != nil {
				return "", err
			}
		}
		parts[i] = v
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

// validateCatchAllValue validates a catch-all ("name*") StaticPaths value,
// which may span several path components joined by "/". Each component must
// still be non-empty and free of "." / ".." traversal and NUL.
func validateCatchAllValue(key, v string) error {
	if v == "" {
		return fmt.Errorf("static: empty StaticPaths value for %q", key)
	}
	if strings.ContainsRune(v, '\x00') {
		return fmt.Errorf("static: catch-all StaticPaths value for %q contains NUL: %q", key, v)
	}
	for comp := range strings.SplitSeq(v, "/") {
		if comp == "" || comp == "." || comp == ".." {
			return fmt.Errorf("static: catch-all StaticPaths value for %q has an empty or traversal component: %q", key, v)
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
	page = applyStaticCSP(page)
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

// baseAttrURL matches a whitespace-delimited src="…", href="…", action="…"
// or data-cui-push-state="…" whose value is root-absolute (leading "/") but
// NOT protocol-relative ("//"). data-cui-push-state is included so combobox/
// palette selection targets get base-prefixed on subpath deploys (otherwise
// selecting a command navigates to the apex path and 404s), and action so
// a GET form (the search page's) submits under the base. The leading
// ([\s]) anchor ensures only real attributes match, never "data-src" /
// "srcset", and code samples are safe because core/markdown escapes quotes
// inside <code> to &quot; (so the ="… pattern never appears in rendered code
// text). Group 3 is the first path byte, re-emitted so the prefix is inserted
// after the leading slash.
var baseAttrURL = regexp.MustCompile(`([\s])(src|href|action|data-cui-push-state)="/([^/])`)

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
		// without one too.
		if !pages[rel] && !pages[rel+"/"] {
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
			if err := writeFile(f, []byte(out)); err != nil {
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

// dumpWidgetAssets writes the widget catalog JSON and each widget's chrome
// HTML + CSS as query-free static files. Hidden click-to-open widgets
// (command palette, section-menu drawers, modals) are not SSR-inlined, so
// the runtime fetches their chrome from cfg.chromePath on open, which 404s
// on a serverless host. Dumping the same bytes as files lets openWidget
// resolve against the static tree, so every data-cui-open overlay works.
func (b *Builder) dumpWidgetAssets(res *Result) error {
	defs := widget.AllForSSR()
	if len(defs) == 0 {
		return nil
	}
	// Catalog JSON: canonical shape from ServeWidgetList (no page filter =
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
	pages := pageSet(res.Pages)
	for _, d := range defs {
		// Chrome HTML may carry root-absolute nav links (/docs/…); rewrite
		// them the same way page HTML is rewritten for a subpath deploy,
		// and slash its page links the way slashLinks does a page's (a
		// section menu's links live here).
		chrome := b.slashPageLinks(b.rewriteBaseURLs(widget.RenderChrome(d)), pages)
		if err := b.writeRawAsset("/core-ui/widget/"+d.Name+"/chrome", []byte(chrome)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, "/core-ui/widget/"+d.Name+"/chrome")
		// On a /<tag>/ page the runtime asks for the chrome under the
		// same prefix, the way the live router serves it.
		if locales, ok := b.Host.PathLocales(); ok {
			for _, tag := range locales.Prefixed {
				rule := urlprefix.Rule{Prefix: "/" + tag, Locales: locales.All()}
				local := b.slashPageLinks(b.rewriteBaseURLs(string(urlprefix.HTML([]byte(widget.RenderChrome(d)), rule))), pages)
				at := "/" + tag + "/core-ui/widget/" + d.Name + "/chrome"
				if err := b.writeRawAsset(at, []byte(local)); err != nil {
					return err
				}
				res.Assets = append(res.Assets, at)
			}
		}
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
	sitemap = slashSitemapLocs(sitemap, pageSet(res.Pages))
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

// sitemapLoc matches one sitemap URL.
var sitemapLoc = regexp.MustCompile(`<loc>([^<]*)</loc>`)

// slashSitemapLocs gives every sitemap URL that names an exported page
// the trailing slash its links carry (see slashPageLinks), so a crawler
// is not sent through the host's 301 for every page. A <loc> is the
// site URL followed by the page path, so the page is found as a
// segment-aligned suffix of the URL's path.
func slashSitemapLocs(doc string, pages map[string]bool) string {
	return sitemapLoc.ReplaceAllStringFunc(doc, func(m string) string {
		loc := sitemapLoc.FindStringSubmatch(m)[1]
		u, err := url.Parse(stdhtml.UnescapeString(loc))
		if err != nil || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Path, "/") {
			return m
		}
		for i := 0; i < len(u.Path); i++ {
			if u.Path[i] == '/' && (pages[u.Path[i:]] || pages[u.Path[i:]+"/"]) {
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
	// Pages are precached under the URL their links name (see
	// slashPageLinks): a client navigation is a plain fetch, which the
	// worker matches exactly, so /about would never answer /about/.
	pages := make([]string, len(res.Pages))
	for i, p := range res.Pages {
		pages[i] = slashed(p)
	}
	sw, err := b.Host.PWAStaticServiceWorkerJS(b.BasePath, uihost.PWAStaticExport{
		Pages:          pages,
		Assets:         res.Assets[:frameworkAssetCount],
		OptionalAssets: res.Assets[frameworkAssetCount:],
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
	// pages' own do (slashPageLinks).
	offline := b.slashPageLinks(b.rewriteBaseURLs(stampStatic(b.Host.PWAOfflineHTML())), pageSet(res.Pages))
	files := []struct {
		urlPath string
		relFile string
		body    []byte
	}{
		{"/manifest.webmanifest", "manifest.webmanifest", manifest},
		{"/service-worker.js", "service-worker.js", []byte(sw)},
		{pwaRegisterJS, filepath.Join("__gofastr", "pwa", "register.js"), []byte(b.Host.PWARegisterJS(b.BasePath))},
		{"/__gofastr/pwa/offline", filepath.Join("__gofastr", "pwa", "offline", "index.html"), []byte(offline)},
	}
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
		if err := writeFile(dst, f.body); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.urlPath)
		b.log("wrote %s", f.urlPath)
	}
	return nil
}

// pwaRegisterJS is the WithPWA registration script, on the rail and
// written by dumpPWAAssets.
const pwaRegisterJS = "/__gofastr/pwa/register.js"

// staticSourceHas reports whether the host's static asset source (dir or
// embedded FS) provides rel, meaning the user shipped their own copy.
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
// PWA outputs themselves are excluded: they are (re)generated after
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
// the runtime-JS base bake. Callers that need base rewriting apply the
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

// dumpRailScripts fetches each extra-script rail entry through b.Handler
// and writes it into the export. A file this build already wrote (a
// framework asset, the static dir, ExtraDirs) wins; a leftover from an
// earlier build in a reused OutDir does not. The PWA registration script
// is skipped because dumpPWAAssets writes its base-path-aware copy, and
// a src that is not a same-origin path (a CDN URL, a relative src) is
// not the app's to serve and is left to the browser.
func (b *Builder) dumpRailScripts(ctx context.Context, res *Result, routes []string) error {
	if b.Handler == nil {
		return nil
	}
	written := make(map[string]bool, len(res.Assets))
	for _, p := range res.Assets {
		written[p] = true
	}
	pages := make(map[string]bool, len(res.Pages))
	for _, p := range res.Pages {
		pages["/"+filepath.ToSlash(pathToFile(p))] = true
	}
	for _, src := range b.Host.ExtraScriptSrcs(routes) {
		u, err := url.Parse(src)
		if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "/\\") {
			continue
		}
		// The file goes under the decoded path, the name a static host
		// resolves the request to.
		urlPath := u.Path
		if pages[urlPath] {
			return fmt.Errorf("static: extra script %s would overwrite the exported page at the same path", urlPath)
		}
		if written[urlPath] || (urlPath == pwaRegisterJS && b.Host.PWAEnabled()) {
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
		written[urlPath] = true
		b.log("script %s", urlPath)
	}
	return nil
}

// dumpExtraDirs copies every Builder.ExtraDirs filesystem under its mount
// path. Deterministic (sorted mounts, sorted walk) and non-clobbering:
// files already in the export tree win.
func (b *Builder) dumpExtraDirs(res *Result) error {
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
		cleanMount := "/" + strings.Trim(mount, "/")
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			urlPath := cleanMount + "/" + filepath.ToSlash(path)
			dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if _, statErr := os.Stat(dst); statErr == nil {
				b.log("kept existing %s (extra dir %s skipped)", urlPath, mount)
				return nil
			}
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			if err := writeFile(dst, data); err != nil {
				return err
			}
			res.Assets = append(res.Assets, urlPath)
			b.log("copied extra %s", urlPath)
			return nil
		})
		if err != nil {
			return fmt.Errorf("static: extra dir %q: %w", mount, err)
		}
	}
	return nil
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

// applyStaticCSP inserts the policy as an in-document meta, immediately
// after <head> so it precedes anything that can trigger a fetch. A page
// that already carries one (the print shell, say) is left alone.
func applyStaticCSP(page string) string {
	if strings.Contains(page, "http-equiv=\"Content-Security-Policy\"") {
		return page
	}
	meta := "\n<meta http-equiv=\"Content-Security-Policy\" content=\"" + staticExportCSP + "\">"
	if i := strings.Index(page, "<head"); i >= 0 {
		if j := strings.IndexByte(page[i:], '>'); j >= 0 {
			at := i + j + 1
			return page[:at] + meta + page[at:]
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
func writeHeadersFile(outDir string) error {
	body := "/*\n" +
		"  Content-Security-Policy: " + staticExportCSP + "\n" +
		"  X-Content-Type-Options: nosniff\n" +
		"  Referrer-Policy: strict-origin-when-cross-origin\n"
	return writeFile(filepath.Join(outDir, "_headers"), []byte(body))
}
