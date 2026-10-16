package siteheader_test

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/siteheader"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestPhoneMenuDismisses: the phone menu is a popup over the page. It
// hangs under the bar wherever the bar sits (the static export puts a
// preview banner above it), so its own close button stays reachable,
// and a tap outside it closes it.
func TestPhoneMenuDismisses(t *testing.T) {
	paras := make([]render.HTML, 40)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d of a long page.", i)))
	}
	page := render.Join(
		// Stands in for the static export's banner above the page.
		html.Div(html.DivConfig{ID: "above"}, render.Text("above the bar"), html.Div(html.DivConfig{}, render.Join(paras[:8]...))),
		ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true, Gap: ui.GapNone},
			siteheader.Render(siteheader.Config{
				Version:  "dev",
				Links:    []siteheader.Link{{Label: "Framework", Href: "/framework"}, {Label: "Examples", Href: "/examples"}},
				MenuLead: []siteheader.Link{{Label: "Docs", Href: "/docs/"}},
			}),
			html.Main(html.MainConfig{}, render.Join(paras...)),
		),
	)
	site := app.NewApp("Header")
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(page)), nil)
	host := uihost.New(site)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fw.Router())
	defer srv.Close()
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800), chromedptest.Timeout(60*time.Second))

	const menu = `[data-cui-scope="docsite-header"] details`
	var below, toggleHit, openAfter bool
	var panelBottom float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-disclosure'])`, nil),
		chromedp.Click(menu+` > summary`, chromedp.NodeVisible),
		chromedp.Poll(`document.querySelector('`+menu+`').open && document.getAnimations().every(a => a.playState === 'finished')`, nil),
		chromedp.Evaluate(`(() => { const d = document.querySelector('`+menu+`');
			const bar = d.closest('header').getBoundingClientRect(), p = d.querySelector(':scope > summary + *').getBoundingClientRect();
			return p.top >= bar.bottom - 1; })()`, &below),
		chromedp.Evaluate(`(() => { const s = document.querySelector('`+menu+` > summary'), r = s.getBoundingClientRect();
			return s.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)); })()`, &toggleHit),
		chromedp.Evaluate(`document.querySelector('`+menu+` > summary + *').getBoundingClientRect().bottom`, &panelBottom),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !below {
		t.Error("the open panel starts above the bar's bottom edge: it hangs from the viewport, not the bar")
	}
	if !toggleHit {
		t.Error("the open panel covers the menu's own toggle")
	}
	if panelBottom > 780 {
		t.Fatalf("the panel reaches %v; the test needs page below it to tap", panelBottom)
	}
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(200, (panelBottom+800)/2),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('`+menu+`').open`, &openAfter),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openAfter {
		t.Error("a tap outside the open menu left it open")
	}
}
