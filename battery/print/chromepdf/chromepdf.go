// Package chromepdf is the headless-Chromium PDFRenderer for battery/print.
package chromepdf

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Options configures the headless-Chromium PDF renderer.
type Options struct {
	// ExecPath is the path to the Chromium/Chrome binary.
	ExecPath string

	// Timeout bounds one render, Chrome's start included; default 30s.
	Timeout time.Duration

	// ExtraFlags are Chromium flags as "name" or "name=value".
	ExtraFlags []string

	// Scale is the print scale (1 = 100%).
	Scale float64

	// AllowedHosts names the hosts the print document may fetch subresources
	// from; empty means no network. It does not widen the document's CSP:
	// images and fonts stay blocked, only the BaseURL stylesheet is fetched.
	AllowedHosts []string
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
	allocOpts = append(allocOpts, chromedp.WSURLReadTimeout(rd.opts.Timeout))
	if rd.opts.ExecPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(rd.opts.ExecPath))
	}
	allocOpts = append(allocOpts, chromedp.Flag("host-resolver-rules", hostResolverRules(rd.opts.AllowedHosts)))
	for _, f := range rd.opts.ExtraFlags {
		name, val := parseFlag(f)
		allocOpts = append(allocOpts, chromedp.Flag(name, val))
	}

	ctx, cancel := context.WithTimeout(ctx, rd.opts.Timeout)
	defer cancel()
	actx, acancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer acancel()
	bctx, bcancel := chromedp.NewContext(actx,
		chromedp.WithBrowserOption(chromedp.WithDialTimeout(rd.opts.Timeout)))
	defer bcancel()

	w, h := paperInches(p)

	var pdf []byte
	err := chromedp.Run(bctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return setContentAndWaitLoad(ctx, html)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var e error
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

func setContentAndWaitLoad(ctx context.Context, html string) error {
	tree, err := page.GetFrameTree().Do(ctx)
	if err != nil {
		return err
	}
	lctx, stop := context.WithCancel(ctx)
	defer stop()
	loaded := make(chan struct{})
	var once sync.Once
	chromedp.ListenTarget(lctx, func(ev any) {
		if _, ok := ev.(*page.EventLoadEventFired); ok {
			once.Do(func() { close(loaded) })
		}
	})
	if err := page.SetDocumentContent(tree.Frame.ID, html).Do(ctx); err != nil {
		return err
	}
	select {
	case <-loaded:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseFlag(f string) (string, any) {
	f = strings.TrimPrefix(f, "--")
	if i := strings.IndexByte(f, '='); i >= 0 {
		return f[:i], f[i+1:]
	}
	return f, true
}

func paperInches(p print.PageConfig) (float64, float64) {
	var w, h float64
	switch p.Size {
	case print.Letter:
		w, h = 8.5, 11
	case print.Legal:
		w, h = 8.5, 14
	default:
		w, h = 8.27, 11.69
	}
	if p.Orientation == print.Landscape {
		w, h = h, w
	}
	return w, h
}

func hostResolverRules(allowed []string) string {
	rules := []string{"MAP * ~NOTFOUND"}
	for _, h := range allowed {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		rules = append(rules, "EXCLUDE "+h)
	}
	return strings.Join(rules, ",")
}
