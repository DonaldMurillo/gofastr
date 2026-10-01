//go:build chromium

package ui_test

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// swapScreen calls Responsive at render time, the way a real screen
// does: after the host has built its catalog.
type swapScreen struct{}

func (swapScreen) Render() render.HTML {
	return ui.Responsive(ui.ResponsiveConfig{Below: ui.StackBelowLG},
		render.Text("Desktop variant"), render.Text("Mobile variant"))
}

// A page reached by client-side navigation still gets Responsive's
// sheet. The sheet used to register on first render, after the host
// had frozen its catalog, so a soft navigation to the first page using
// Responsive showed both variants.
func TestResponsiveSheetLoadsAfterSoftNav(t *testing.T) {
	site := app.NewApp("Responsive nav")
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(
		ui.Link(ui.LinkConfig{Text: "Go", Href: "/swap", ID: "go"}))), nil)
	site.RegisterScreen(app.NewScreen("/swap", swapScreen{}), nil)
	host := uihost.New(site)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fw.Router())
	defer srv.Close()
	ctx := chromedptest.Context(t)

	var result string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`window.__softNavMarker = 1`, nil),
		chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.WaitReady(`.fui-responsive`, chromedp.ByQuery),
		chromedp.Poll(`(() => { const m = document.querySelector('.fui-responsive__mobile');
			return m && getComputedStyle(m).display === 'none'; })()`, nil, chromedp.WithPollingTimeout(3e9)),
		chromedp.Evaluate(`(() => {
			if (window.__softNavMarker !== 1) return 'the navigation was a full reload';
			const d = document.querySelector('.fui-responsive__desktop');
			if (getComputedStyle(d).display === 'none') return 'desktop variant hidden at 1280';
			return '';
		})()`, &result),
	); err != nil {
		t.Fatalf("mobile variant still shows at 1280 after a soft navigation: %v", err)
	}
	if result != "" {
		t.Fatal(result)
	}
}
