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

// Builder writes a static snapshot of a UIHost's routes to a directory.
type Builder struct {
	// Host renders the pages. Its screens, actions and static assets must be configured.
	Host *uihost.UIHost
	// OutDir is the export directory. The files a build writes replace what is there.
	OutDir string
	// BasePath is the URL subpath the export is served under, such as
	// "/gofastr". Empty serves it at the root.
	BasePath string
	// Logger, when set, gets one line per file written, copied or skipped.
	Logger func(format string, args ...any)
	// ExtraDirs maps URL mount paths to filesystems copied into the export.
	// A file the build wrote or the static source provides wins over an
	// extra file at the same path.
	ExtraDirs map[string]fs.FS
	// ExcludeRoutes skips every route pattern equal to or under an entry,
	// by whole path segments. A page whose fill failed and was contained
	// fails the build unless its route is excluded.
	ExcludeRoutes []string
	// Handler, when set, serves the extra-script rail entries the host does
	// not own. Each is fetched and written, and one that does not answer
	// 200 fails the build. App.ExportStatic passes the app's router.
	Handler http.Handler
	// Contributors add files built from the exported pages, such as a search index.
	Contributors []Contributor
	// ContentSecurityPolicy is the policy every page carries as a meta and
	// _headers carries in full. Empty uses the built-in policy. Build
	// refuses one with a nonce source, a control byte, a quote or an angle
	// bracket.
	ContentSecurityPolicy string

	extraDirs map[string]fs.FS
}

// Contributor adds files to an export from its pages. ExportStart runs
// when a build begins, ExportPage gets each page's HTML before the static
// rewrites, and ExportFiles runs after the last page and returns files
// keyed by root-absolute URL path.
type Contributor interface {
	ExportStart(ctx context.Context) error
	ExportPage(path, html string) error
	ExportFiles(ctx context.Context) (map[string][]byte, error)
}

// Result lists what a Build wrote.
type Result struct {
	Pages []string // route paths rendered, dynamic routes expanded
	// Assets holds the URL paths of the generated assets, docs,
	// contributor files and copied files written. Redirect pages,
	// _headers and _redirects are not listed.
	Assets []string
}

// Build renders every route of b.Host into b.OutDir with the assets its
// pages load, and returns what it wrote. A page with no static answer
// (gated, redirected, a 404 outlet or a status other than 200) is skipped
// with a warning; any other render failure fails the build.
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
	var renderedRoutes []string
	rendered := newFileSet()
	pageOwner := map[string]string{}
	docs := map[string]bool{}

	for _, c := range b.Contributors {
		if err := c.ExportStart(ctx); err != nil {
			return res, fmt.Errorf("static: contributor start: %w", err)
		}
	}

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

	for _, c := range b.Contributors {
		files, err := c.ExportFiles(ctx)
		if err != nil {
			return res, fmt.Errorf("static: contributor files: %w", err)
		}
		for _, urlPath := range slices.Sorted(maps.Keys(files)) {
			if !strings.HasPrefix(urlPath, "/") {
				return res, fmt.Errorf("static: contributor file %q is not a root-absolute path", urlPath)
			}
			body := files[urlPath]
			urlPath = path.Clean(urlPath)
			rel := strings.TrimPrefix(urlPath, "/")
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

	if err := writeHeadersFile(b.OutDir, b.policy()); err != nil {
		return res, fmt.Errorf("static: write _headers: %w", err)
	}
	rendered.add("_headers")

	if err := b.writeRedirects(ctx, pageSet(res.Pages), rendered, pageOwner); err != nil {
		return res, err
	}

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

	if err := b.slashLinks(res); err != nil {
		return res, err
	}

	if !b.Host.App.NoLLMMD {
		for _, tag := range tags {
			lctx, at := ctx, func(p string) string { return p }
			if byPrefix {
				lctx = coreapp.WithLinkKeep(localeContext(ctx, locales, tag), b.keepUnprefixed)
				at = func(p string) string { return locales.Path(tag, p) }
			}
			if err := b.writeLLMMD(lctx, &res, at, docs); err != nil {
				return res, err
			}
		}
	}

	if err := b.dumpSEOAssets(&res); err != nil {
		return res, err
	}

	if err := b.dumpAppIcons(&res); err != nil {
		return res, err
	}

	type asset struct {
		urlPath string
		body    []byte
		binary  bool
	}
	colorScheme, err := runtime.ColorSchemeJS()
	if err != nil {
		return res, fmt.Errorf("static: color-scheme.js: %w", err)
	}
	assets := []asset{
		{urlPath: "/__gofastr/runtime.js", body: []byte(runtime.MustStaticJS())},
		{urlPath: "/__gofastr/color-scheme.js", body: []byte(colorScheme)},
	}
	if js := b.Host.GetActionJS(); js != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/actions.js", body: []byte(js)})
	}
	if appBody := b.Host.AppCSS(); appBody != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/app.css", body: []byte(minify.ServedCSS(appBody))})
	}
	for url, css := range b.Host.ComponentCSSFiles() {
		assets = append(assets, asset{urlPath: url, body: []byte(css)})
	}
	for _, name := range runtime.ModuleNames() {
		body, ok := runtime.Module(name)
		if !ok {
			continue
		}
		assets = append(assets, asset{urlPath: "/__gofastr/runtime/" + name + ".js", body: []byte(body)})
	}
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

	if err := b.dumpWidgetAssets(ctx, &res); err != nil {
		return res, err
	}

	frameworkAssetCount := len(res.Assets)

	if dir := b.Host.StaticDir(); dir != "" {
		if err := copyDir(dir, b.OutDir, rendered, &res, b.log); err != nil {
			return res, err
		}
	} else if fsys := b.Host.StaticFS(); fsys != nil {
		if err := copyFS(fsys, b.OutDir, rendered, &res, b.log); err != nil {
			return res, err
		}
	}

	if err := b.dumpExtraDirs(&res, rendered); err != nil {
		return res, err
	}
	if err := b.dumpRailScripts(ctx, &res, renderedRoutes, rendered); err != nil {
		return res, err
	}
	if err := b.pruneStalePages(res, rendered, docs); err != nil {
		return res, err
	}
	if err := respell(b.OutDir, writtenPaths(&res, rendered, docs)); err != nil {
		return res, err
	}

	if err := b.dumpPWAAssets(&res, frameworkAssetCount, rendered); err != nil {
		return res, err
	}
	if err := checkCaseTwins(&res, rendered, docs); err != nil {
		return res, err
	}
	if err := respell(b.OutDir, writtenPaths(&res, rendered, docs)); err != nil {
		return res, err
	}

	return res, nil
}

// Walk renders every page an export of host would write, in route order,
// and passes each to fn with its live HTML. skip, when set, is asked about
// each route pattern and expanded path before it renders. A page that
// fails to render reaches fn with an empty html and the error; fn
// returning nil walks on, and an error stops the walk.
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
			continue
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
				if nf, ok := errors.AsType[*uihost.NotFoundOutletError](err); ok {
					slog.Warn("static: skipped 404-outlet route: a FallbackNotFound outlet had no fill at build time, the route's own answer is 404; it stays reachable via the live server",
						"path", nf.Path)
					continue
				}
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
			if len(fillFailures) > 0 {
				parts := make([]string, 0, len(fillFailures))
				for _, f := range fillFailures {
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

func expandRoute(ctx context.Context, app *coreapp.App, pattern string) ([]string, error) {
	if !strings.Contains(pattern, ":") {
		return []string{pattern}, nil
	}
	screen, ok := app.Router.ScreenByPattern(pattern)
	if !ok {
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
	return coreapp.PatternPath(pattern, params), nil
}

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

func localeContext(ctx context.Context, locales i18n.PathLocales, tag string) context.Context {
	return i18n.WithPathLocale(i18n.WithContext(ctx, i18n.Locale{Tag: tag}), locales, tag)
}

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

func hostPath(p string) string {
	if d, err := url.PathUnescape(coreapp.EscapeRoutePath(p)); err == nil {
		return d
	}
	return p
}

func pathToFile(p string) string {
	clean := strings.Trim(hostPath(p), "/")
	if clean == "" {
		return "index.html"
	}
	return filepath.Join(clean, "index.html")
}
