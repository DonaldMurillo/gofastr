package static

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	coreapp "github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/runtime"
	"github.com/gofastr/gofastr/framework/uihost"
)

// Builder generates a static-site snapshot from a UIHost.
type Builder struct {
	// Host is the UIHost to render through. Must already have its screens,
	// actions, and static assets configured.
	Host *uihost.UIHost
	// OutDir is the destination directory; existing contents are overwritten.
	OutDir string
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
			dst := filepath.Join(b.OutDir, pathToFile(p))
			if err := writeFile(dst, []byte(html)); err != nil {
				return res, err
			}
			b.log("rendered %s -> %s", p, dst)
			res.Pages = append(res.Pages, p)
		}
	}

	// /__gofastr/* assets — runtime, compiled actions, theme CSS, custom
	// CSS, route graph script. The injected <link>/<script src> tags in
	// the rendered HTML reference these paths, so SSG output is broken
	// without them.
	type asset struct {
		urlPath string
		body    []byte
	}
	assets := []asset{
		{urlPath: "/__gofastr/runtime.js", body: []byte(runtime.MustRuntimeJS())},
	}
	if js := b.Host.GetActionJS(); js != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/actions.js", body: []byte(js)})
	}
	if app := b.Host.App; app != nil && app.Theme != nil {
		assets = append(assets, asset{urlPath: "/__gofastr/theme.css", body: []byte(app.Theme.CSSCustomProperties())})
	}
	if css := b.Host.CustomCSS(); css != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/styles.css", body: []byte(css)})
	}
	if rg := b.Host.RouteGraphJS(); rg != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/routes.js", body: []byte(rg)})
	}
	// Component catalog + per-component scoped sheets. The rendered
	// HTML references /__gofastr/comp/<name>.css for each registered
	// component (static export sets bundle=false so the rendered HTML
	// never references the dynamic comp-bundle.css?names=… URL).
	if cat := b.Host.CatalogJS(); cat != "" {
		assets = append(assets, asset{urlPath: "/__gofastr/catalog.js", body: []byte(cat)})
	}
	for url, css := range b.Host.ComponentCSSFiles() {
		assets = append(assets, asset{urlPath: url, body: []byte(css)})
	}
	for _, a := range assets {
		dst := filepath.Join(b.OutDir, filepath.FromSlash(strings.TrimPrefix(a.urlPath, "/")))
		if err := writeFile(dst, a.body); err != nil {
			return res, err
		}
		res.Assets = append(res.Assets, a.urlPath)
		b.log("wrote %s", a.urlPath)
	}

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

	return res, nil
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
	var out []string
	for _, params := range provider.StaticPaths(ctx) {
		out = append(out, applyParams(pattern, params))
	}
	return out, nil
}

func applyParams(pattern string, params map[string]string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			key := strings.TrimPrefix(part, ":")
			if v, ok := params[key]; ok {
				parts[i] = v
			}
		}
	}
	return strings.Join(parts, "/")
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
