package static

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

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
