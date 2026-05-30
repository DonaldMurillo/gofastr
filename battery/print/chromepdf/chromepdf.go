// Package chromepdf is the headless-Chromium PDF backend for the
// [github.com/DonaldMurillo/gofastr/battery/print] battery. It is the
// ONLY package in the print feature that imports chromedp; hosts that
// don't need PDF never pull it in.
//
// Wire it into the battery:
//
//	pb := print.New(print.Config{
//		PDFRenderer: chromepdf.New(chromepdf.Options{}),
//	})
//
// The renderer feeds the battery's already-shelled print HTML to a
// headless Chromium via an in-memory data: URL (no temp file — per-user
// document bytes never touch disk) and prints it to PDF, honoring the
// shell's CSS @page size and margins (WithPreferCSSPageSize).
package chromepdf

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Options configures the headless-Chromium PDF renderer.
type Options struct {
	// ExecPath is the path to the Chromium/Chrome binary. Empty =
	// chromedp auto-detects.
	ExecPath string

	// Timeout bounds a single render. Default 30s.
	Timeout time.Duration

	// ExtraFlags are extra Chromium command-line flags, each either
	// "name" (boolean true) or "name=value". A leading "--" is optional.
	// Common in containers: "no-sandbox".
	ExtraFlags []string

	// Scale is the print scale (1 = 100%). Default 1.
	Scale float64
}

type renderer struct{ opts Options }

// New returns a print.PDFRenderer backed by headless Chromium.
func New(o Options) print.PDFRenderer {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Scale <= 0 {
		o.Scale = 1
	}
	return &renderer{opts: o}
}

// RenderPDF implements print.PDFRenderer.
func (rd *renderer) RenderPDF(ctx context.Context, html string, p print.PageConfig, _ string) ([]byte, error) {
	allocOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if rd.opts.ExecPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(rd.opts.ExecPath))
	}
	for _, f := range rd.opts.ExtraFlags {
		name, val := parseFlag(f)
		allocOpts = append(allocOpts, chromedp.Flag(name, val))
	}

	ctx, cancel := context.WithTimeout(ctx, rd.opts.Timeout)
	defer cancel()
	actx, acancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer acancel()
	bctx, bcancel := chromedp.NewContext(actx)
	defer bcancel()

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	w, h := paperInches(p)

	var pdf []byte
	err := chromedp.Run(bctx,
		chromedp.Navigate(dataURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var e error
			// Margins are 0 here: the shell's CSS @page margin is the
			// single source of truth, so PrintToPDF must not add its own.
			pdf, _, e = page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithPaperWidth(w).
				WithPaperHeight(h).
				WithMarginTop(0).
				WithMarginRight(0).
				WithMarginBottom(0).
				WithMarginLeft(0).
				WithScale(rd.opts.Scale).
				Do(ctx)
			return e
		}),
	)
	if err != nil {
		return nil, err
	}
	return pdf, nil
}

// parseFlag splits an ExtraFlags entry into a chromedp flag name + value.
// "no-sandbox" -> ("no-sandbox", true); "window-size=1280,720" ->
// ("window-size", "1280,720"). A leading "--" is stripped.
func parseFlag(f string) (string, any) {
	f = strings.TrimPrefix(f, "--")
	if i := strings.IndexByte(f, '='); i >= 0 {
		return f[:i], f[i+1:]
	}
	return f, true
}

// paperInches returns the paper width/height in inches for a PageConfig's
// named size (used as a fallback; WithPreferCSSPageSize means the shell's
// @page size normally wins). Custom sizes fall back to A4.
func paperInches(p print.PageConfig) (float64, float64) {
	var w, h float64
	switch p.Size {
	case print.Letter:
		w, h = 8.5, 11
	case print.Legal:
		w, h = 8.5, 14
	default: // A4 and Custom
		w, h = 8.27, 11.69
	}
	if p.Orientation == print.Landscape {
		w, h = h, w
	}
	return w, h
}
