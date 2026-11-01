package siteheader_test

import (
	"context"
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
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestPhoneMenuDismisses: the phone menu is a popup over the page. It
// hangs under the bar wherever the bar sits (the static export puts a
// preview banner above it), so its own close button stays reachable,
// and a tap outside it closes it.
func TestPhoneMenuDismisses(t *testing.T) {
	srv := menuSite(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800), chromedptest.Timeout(60*time.Second))

	var below, toggleHit, openAfter bool
	var panelBottom float64
	if err := chromedp.Run(ctx,
		openMenu(srv.URL),
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

// TestPhoneMenuFitsShortScreen: on a phone held sideways the open menu
// is taller than the room below the bar. It stays inside the viewport
// and scrolls, so its last item can still be reached.
func TestPhoneMenuFitsShortScreen(t *testing.T) {
	srv := menuSite(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(740, 360), chromedptest.Timeout(60*time.Second))
	var got struct {
		Bottom  float64 `json:"bottom"`
		Height  float64 `json:"height"`
		Reached bool    `json:"reached"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		// The bar at the top of the viewport, where the sticky header
		// sits once the page scrolls.
		chromedp.Evaluate(`window.scrollTo(0, document.querySelector('header').getBoundingClientRect().top + scrollY); true`, nil),
		openMenu(""),
		chromedp.Evaluate(`(() => { const p = document.querySelector('`+menu+` > summary + *');
			const last = [...p.querySelectorAll('a')].pop();
			last.scrollIntoView({ block: 'nearest' });
			const r = last.getBoundingClientRect();
			return { bottom: p.getBoundingClientRect().bottom, height: innerHeight,
				reached: r.bottom <= innerHeight && r.top >= p.getBoundingClientRect().top - 1 }; })()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got.Bottom > got.Height {
		t.Errorf("the open menu runs to %v, past the %v-pixel viewport", got.Bottom, got.Height)
	}
	if !got.Reached {
		t.Error("the menu's last item cannot be brought into view")
	}
}

// TestPhoneMenuRowsAreTapTargets: each link in the phone menu is a
// full touch target, not the desktop chrome's short row.
func TestPhoneMenuRowsAreTapTargets(t *testing.T) {
	srv := menuSite(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800), chromedptest.Timeout(60*time.Second))
	var rows []struct {
		Text string  `json:"text"`
		H    float64 `json:"h"`
	}
	if err := chromedp.Run(ctx,
		openMenu(srv.URL),
		chromedp.Evaluate(`[...document.querySelectorAll('`+menu+` > summary + * a')].filter((a) => a.offsetParent)
			.map((a) => ({ text: a.textContent.trim(), h: a.getBoundingClientRect().height }))`, &rows),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the open menu shows no links")
	}
	for _, r := range rows {
		if r.H < 44 {
			t.Errorf("menu row %q is %vpx tall; want at least 44px", r.Text, r.H)
		}
	}
}

// TestPhoneMenuOverPinnedChrome: chrome pinned at the sticky layer
// later in the document (a page's floating section pill) stays under
// the open menu instead of covering its last rows.
func TestPhoneMenuOverPinnedChrome(t *testing.T) {
	srv := menuSite(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800), chromedptest.Timeout(60*time.Second))
	var onTop bool
	if err := chromedp.Run(ctx,
		openMenu(srv.URL),
		chromedp.Evaluate(`(() => { const d = document.createElement('div');
			Object.assign(d.style, { position: 'fixed', insetInline: '0', insetBlockEnd: '0', blockSize: '90vh', zIndex: 'var(--z-sticky)' });
			document.body.append(d);
			const last = [...document.querySelectorAll('`+menu+` > summary + * a')].filter((a) => a.offsetParent).pop();
			const r = last.getBoundingClientRect();
			return last.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)); })()`, &onTop),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !onTop {
		t.Error("a fixed element at the sticky layer covers the open menu's last link")
	}
}

const menu = `[data-cui-scope="docsite-header"] details`

// openMenu loads url (when given), waits for the disclosure module and
// opens the phone menu, letting its entrance animation finish.
func openMenu(url string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if url != "" {
			if err := chromedp.Navigate(url + "/").Do(ctx); err != nil {
				return err
			}
		}
		return chromedp.Run(ctx,
			chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-disclosure'])`, nil),
			chromedp.Click(menu+` > summary`, chromedp.NodeVisible),
			chromedp.Poll(`document.querySelector('`+menu+`').open && document.getAnimations().every(a => a.playState === 'finished')`, nil),
		)
	})
}

// menuSite serves the docs-site header over a long page, with a block
// standing in for the static export's banner above the bar.
func menuSite(t *testing.T) *httptest.Server {
	t.Helper()
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
	site := app.NewApp("Header").WithTheme(theme.Default().Extend(siteheader.Tokens))
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(page)), nil)
	host := uihost.New(site)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fw.Router())
	t.Cleanup(srv.Close)
	return srv
}
