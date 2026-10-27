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
// The renderer opens about:blank in a headless Chromium, sets the
// battery's already-shelled print HTML as the page's content over the
// DevTools protocol (no temp file, no URL: per-user document bytes never
// touch disk, and no URL length cap applies), and prints it to PDF,
// honoring the shell's CSS @page size and margins
// (WithPreferCSSPageSize).
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
	// ExecPath is the path to the Chromium/Chrome binary. Empty =
	// chromedp auto-detects.
	ExecPath string

	// Timeout bounds a single render, Chrome's start included: chromedp's
	// own 20s DevTools-URL wait and 10s websocket dial take it too.
	// Default 30s.
	Timeout time.Duration

	// ExtraFlags are extra Chromium command-line flags, each either
	// "name" (boolean true) or "name=value". A leading "--" is optional.
	// Common in containers: "no-sandbox".
	ExtraFlags []string

	// Scale is the print scale (1 = 100%). Default 1.
	Scale float64

	// AllowedHosts names the hosts the print document may fetch
	// subresources from. Empty, the default, means NO network at all:
	// every hostname is resolved to NOTFOUND before Chrome can connect.
	//
	// This is the network half of the SSRF gate. The print document is
	// set as the content of about:blank, which has no response headers,
	// so the print route's Content-Security-Policy does not reach it (the
	// shell also emits the policy as an in-document meta). Without a
	// resolver rule, a caller-supplied URL that reaches a URL attribute,
	// ui.Avatar{Src} on a receipt, say, is fetched by a browser sitting
	// inside the trust boundary, and the response is rendered into the
	// downloaded PDF: a readable channel to 169.254.169.254, loopback
	// admin panels, and RFC1918.
	//
	// A document whose CSS and images are inlined (what the shell
	// produces) needs no network, so the default costs nothing. Name a
	// host here only when a PDF genuinely must pull a remote asset, and
	// note that an allowed host's DNS still decides which IP it reaches,
	// so allow only hosts you control.
	//
	// print.Config.BaseURL is one such case: the app.css link it makes
	// absolute is fetched from BaseURL's host, so name that host here
	// (AllowedHosts: []string{"app.example.com"}) for the PDF to carry
	// the app's design tokens.
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
	// Timeout bounds the whole render, Chrome's start included, so
	// chromedp's own shorter start waits (20s for the DevTools URL, 10s
	// for the websocket dial) do not fail a slow start first.
	allocOpts = append(allocOpts, chromedp.WSURLReadTimeout(rd.opts.Timeout))
	if rd.opts.ExecPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(rd.opts.ExecPath))
	}
	// Network gate, installed BEFORE ExtraFlags so a host can still
	// override it deliberately if they must.
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
		// The document goes in as content, never as a URL: Chrome caps
		// URLs at 2 MiB, so a data: URL of the whole document failed
		// any print HTML past about 1.5 MB with net::ERR_ABORTED.
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return setContentAndWaitLoad(ctx, html)
		}),
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

// setContentAndWaitLoad replaces the current page's document with html
// and returns once that document's load event fires, the point a URL
// navigation waits for: its stylesheets, images and fonts have loaded or
// failed. document.readyState is no substitute here, since it can still
// read "complete" from about:blank after the content is set while a
// stylesheet is in flight. The caller's deadline bounds the wait.
func setContentAndWaitLoad(ctx context.Context, html string) error {
	tree, err := page.GetFrameTree().Do(ctx)
	if err != nil {
		return err
	}
	// Listen before setting the content so the load event cannot slip
	// past. Navigate already waited out about:blank's own load event.
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

// hostResolverRules builds Chrome's --host-resolver-rules value: map
// every hostname to NOTFOUND, then EXCLUDE the allow-listed ones so they
// resolve normally. With an empty allow-list the document cannot open a
// connection to anything.
//
// A resolver rule rather than request interception because it fails
// closed at the lowest layer: there is no request to intercept if the
// name never resolves, so no code path, image, stylesheet, font, XHR,
// navigation, can slip past by taking a different route.
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
