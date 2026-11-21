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

const (
	envExport     = "GOFASTR_EXPORT"
	envExportBase = "GOFASTR_EXPORT_BASE"
	envExportRun  = "GOFASTR_EXPORT_RUN"
)

// ExportReceiptFile is the receipt Start writes into the export
// directory when GOFASTR_EXPORT_RUN is set: {"run": <id>, "pages": <n>}.
const ExportReceiptFile = ".gofastr-export.json"

type exportReceipt struct {
	Run   string `json:"run"`
	Pages int    `json:"pages"`
}

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

// ExportStatic renders every route of the mounted uihost.UIHost, and the
// assets the runtime needs, to dir as static files. basePath is the URL
// subpath the site is served under, "" for an apex deploy. It returns an
// error when no UIHost is mounted.
func (a *App) ExportStatic(ctx context.Context, dir, basePath string) error {
	_, err := a.exportStatic(ctx, dir, basePath)
	return err
}

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
				TrustedTypesPolicies:  a.Config.SecurityHeaders.TrustedTypesPolicies,
				NoTrustedTypes:        a.Config.SecurityHeaders.NoTrustedTypes,
			}).Build(ctx)
		}
	}
	return static.Result{}, fmt.Errorf("framework: ExportStatic requires a mounted uihost.UIHost")
}
