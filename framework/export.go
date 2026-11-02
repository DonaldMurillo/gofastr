package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// envExport and envExportBase ask Start for an export instead of a
// server: the output directory and the deploy's URL subpath. `gofastr
// export` sets them.
const (
	envExport     = "GOFASTR_EXPORT"
	envExportBase = "GOFASTR_EXPORT_BASE"
	// envExportRun is the run id `gofastr export` mints for one export.
	// Start writes it back in ExportReceiptFile once the export is on
	// disk, so the CLI can tell a finished export from a main that
	// exited 0 without reaching Start.
	envExportRun = "GOFASTR_EXPORT_RUN"
)

// ExportReceiptFile is the receipt Start writes into the export
// directory when GOFASTR_EXPORT_RUN is set: {"run": <id>, "pages": <n>}.
// `gofastr export` reads it, checks the run id, and removes it, so it
// never ships with the site.
const ExportReceiptFile = ".gofastr-export.json"

// exportReceipt is the body of ExportReceiptFile.
type exportReceipt struct {
	Run   string `json:"run"`
	Pages int    `json:"pages"`
}

// writeExportReceipt records that the export for run finished with
// pages pages. An empty run (no `gofastr export` asked) writes nothing.
func writeExportReceipt(dir, run string, pages int) error {
	if run == "" {
		return nil
	}
	body, err := json.Marshal(exportReceipt{Run: run, Pages: pages})
	if err != nil {
		return err
	}
	return fileperm.WriteOwnerOnly(filepath.Join(dir, ExportReceiptFile), body)
}

// ExportStatic renders the app to a folder of static HTML + assets
// using the SSG Builder (framework/static). It locates the mounted
// UIHost and renders every declared route to a directory-style file,
// plus all /__gofastr assets the runtime needs to boot, runtime.js,
// the split runtime modules (themeswitch, shortcut, copy, widgets, …),
// app.css, and per-component CSS, with query-free filenames so the
// output serves correctly from any static host (GitHub Pages, S3, …).
//
// basePath is the URL subpath the site is served under (e.g. "/gofastr"
// for a GitHub Pages project site at https://user.github.io/gofastr/);
// pass "" for an apex deploy. When set, the builder prefixes every
// root-absolute asset/nav URL in the HTML and bakes the prefix into
// runtime.js so dynamically-loaded modules resolve under the mount path.
//
// Scripts on the extra-script rail that the app serves from its own
// router (uihost.WithExtraScripts("/__site/x.js") plus a router.Get for
// that path) are fetched through the router and exported too.
//
// A registered battery that implements static.Contributor (the
// battery/sitesearch index) sees every page and adds its files.
//
// The export carries the Content-Security-Policy the app serves live
// (AppConfig.SecurityHeaders.ContentSecurityPolicy, or the built-in
// default when unset) in each page's <meta> and in _headers.
//
// This is the native replacement for the wget mirror the Pages deploy
// used to ship: declaration-driven (no crawling), and it dumps the
// split modules the crawl baked a "?v=" into and 404'd.
//
// Returns an error if no uihost.UIHost is mounted.
func (a *App) ExportStatic(ctx context.Context, dir, basePath string) error {
	_, err := a.exportStatic(ctx, dir, basePath)
	return err
}

// exportStatic is ExportStatic returning what the builder produced.
func (a *App) exportStatic(ctx context.Context, dir, basePath string) (static.Result, error) {
	for _, m := range a.Mountables() {
		if host, ok := m.(*uihost.UIHost); ok {
			var contributors []static.Contributor
			for _, b := range a.Batteries.registered() {
				if c, ok := b.(static.Contributor); ok {
					contributors = append(contributors, c)
				}
			}
			return (&static.Builder{
				Host: host, OutDir: dir, BasePath: basePath, Handler: a.router, Contributors: contributors,
				ContentSecurityPolicy: a.Config.SecurityHeaders.ContentSecurityPolicy,
			}).Build(ctx)
		}
	}
	return static.Result{}, fmt.Errorf("framework: ExportStatic requires a mounted uihost.UIHost")
}
