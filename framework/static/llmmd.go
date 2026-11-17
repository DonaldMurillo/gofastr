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

func (b *Builder) writeLLMMD(ctx context.Context, res *Result, at func(string) string, docs map[string]bool) error {
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
				continue
			}
			var md string
			sctx, recorded := coreapp.TrackStatus(withPageRequest(ctx, p))
			if doc, ok := coreapp.ScreenLLMMDForPath(sctx, b.Host.App, p); ok {
				if st := docStatus(doc.Component, recorded()); st != 0 {
					slog.Warn("static: skipped llm.md for a page that answers a status other than 200",
						"path", p, "status", st)
					continue
				}
				md = b.Host.LLMMDWithSEO(sctx, p, screen, doc)
			} else {
				if _, _, resolves := b.Host.App.Router.Resolve(p); resolves {
					slog.Warn("static: skipped llm.md for a page that answers a status other than 200",
						"path", p, "status", http.StatusNotFound)
					continue
				}
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

func pathToLLMFile(p string) string {
	clean := strings.Trim(hostPath(p), "/")
	if clean == "" {
		return "llm.md"
	}
	return filepath.Join(clean, "llm.md")
}
