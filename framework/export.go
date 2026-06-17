package framework

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// ExportStatic renders the app to a folder of static HTML + assets
// using the SSG Builder (framework/static). It locates the mounted
// UIHost and renders every declared route to a directory-style file,
// plus all /__gofastr assets the runtime needs to boot — runtime.js,
// the split runtime modules (themeswitch, shortcut, copy, widgets, …),
// app.css, and per-component CSS — with query-free filenames so the
// output serves correctly from any static host (GitHub Pages, S3, …).
//
// basePath is the URL subpath the site is served under (e.g. "/gofastr"
// for a GitHub Pages project site at https://user.github.io/gofastr/);
// pass "" for an apex deploy. When set, the builder prefixes every
// root-absolute asset/nav URL in the HTML and bakes the prefix into
// runtime.js so dynamically-loaded modules resolve under the mount path.
//
// This is the native replacement for the wget mirror the Pages deploy
// used to ship: declaration-driven (no crawling), and it dumps the
// split modules the crawl baked a "?v=" into and 404'd.
//
// Returns an error if no uihost.UIHost is mounted.
func (a *App) ExportStatic(ctx context.Context, dir, basePath string) error {
	for _, m := range a.Mountables() {
		if host, ok := m.(*uihost.UIHost); ok {
			_, err := (&static.Builder{Host: host, OutDir: dir, BasePath: basePath}).Build(ctx)
			return err
		}
	}
	return fmt.Errorf("framework: ExportStatic requires a mounted uihost.UIHost")
}
