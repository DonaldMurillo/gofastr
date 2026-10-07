package ui_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A Sticky row keeps its frame in place while the window scrolls a long
// page: the nav column and the toolbar do not move, the nav column is
// one viewport tall and scrolls its own overflow, and on a phone the
// toolbar still holds the top edge.
func TestContentRowStickyFrame(t *testing.T) {
	site := app.NewApp("Sticky")
	items := make([]ui.SidebarItem, 40)
	for i := range items {
		items[i] = ui.SidebarItem{Label: fmt.Sprintf("Item %d", i), Href: fmt.Sprintf("/i/%d", i)}
	}
	nav, _ := component.SafeRenderCtx(context.Background(), ui.Sidebar(ui.SidebarConfig{NavLabel: "Pages", Items: items}))
	paras := make([]render.HTML, 120)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d", i)))
	}
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, Sidebar: nav, Toolbar: render.Text("Trail")},
			html.Main(html.MainConfig{}, paras...)))
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
	ctx := chromedptest.Context(t)
	check := func(t *testing.T, width int64, js string) {
		t.Helper()
		var result string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 800), chromedp.Navigate(srv.URL+"/"), chromedp.Poll(`!!window.__gofastr`, nil),
			chromedp.Evaluate(`(()=>{const q=s=>document.querySelector(s), r=e=>e.getBoundingClientRect();`+js+`;return ""})()`, &result)); err != nil {
			t.Fatal(err)
		}
		if result != "" {
			t.Fatal(result)
		}
	}
	t.Run("Desktop", func(t *testing.T) {
		check(t, 1280, `
const nav=q('.fui-content-row__nav'), bar=q('.fui-content-row__toolbar');
const n0=r(nav), b0=r(bar);
if(Math.round(n0.height)!==innerHeight)return 'nav column is '+n0.height+'px, not one viewport';
if(!(nav.scrollHeight>nav.clientHeight))return 'nav column does not overflow on its own';
scrollTo(0, 2000);
if(scrollY<1000)return 'the window did not scroll';
const n1=r(nav), b1=r(bar);
if(n1.top!==n0.top||n1.left!==n0.left)return 'nav column moved: '+n0.top+' -> '+n1.top;
if(b1.top!==b0.top)return 'toolbar moved: '+b0.top+' -> '+b1.top;
nav.scrollTop=300;
if(nav.scrollTop<100)return 'nav column does not scroll';
if(getComputedStyle(bar).backgroundColor==='rgba(0, 0, 0, 0)')return 'toolbar is see-through';
if(document.documentElement.scrollWidth>innerWidth)return 'horizontal overflow';
`)
	})
	t.Run("Phone", func(t *testing.T) {
		check(t, 390, `
const bar=q('.fui-content-row__toolbar');
scrollTo(0, 3000);
if(Math.abs(r(bar).top)>1)return 'phone toolbar left the top: '+r(bar).top;
if(document.documentElement.scrollWidth>innerWidth)return 'phone overflow';
`)
	})
}
