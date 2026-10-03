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

// The back link shows only while the detail pane is alone: a phone
// with a selected detail. The placeholder screen shows the list, and a
// desktop shows both panes, so neither needs a way back.
func TestListDetailBackLinkOnlyOnPhoneDetail(t *testing.T) {
	site := app.NewApp("List detail back")
	page := func(detail render.HTML) render.HTML {
		return ui.ListDetail(ui.ListDetailConfig{
			ListLabel: "Issues", MobileSinglePane: true,
			BackHref: "/issues", BackLabel: "Back to issues",
			List: render.Text("Issue rows"), Detail: detail,
		})
	}
	site.RegisterScreen(app.NewScreen("/issues/1", app.NewStaticComponent(page(render.Text("Issue one")))), nil)
	site.RegisterScreen(app.NewScreen("/issues", app.NewStaticComponent(page(ui.ListDetailPlaceholder(render.Text("Pick an issue"))))), nil)
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

	check := func(t *testing.T, path string, width int64, js string) {
		t.Helper()
		var result string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 844), chromedp.Navigate(srv.URL+path),
			chromedp.Poll(`!!window.__gofastr`, nil),
			chromedp.Evaluate(`(()=>{const q=s=>document.querySelector(s), shown=e=>!!e&&getComputedStyle(e).display!=='none'&&e.getBoundingClientRect().height>0;`+js+`;return ""})()`, &result)); err != nil {
			t.Fatal(err)
		}
		if result != "" {
			t.Fatal(result)
		}
	}
	t.Run("PhoneDetail", func(t *testing.T) {
		check(t, "/issues/1", 390, `
const back=q('.fui-list-detail__back');
if(!shown(back))return 'back link hidden on a phone detail';
if(shown(q('.fui-list-detail__list')))return 'list still shown beside the phone detail';
if(back.getBoundingClientRect().bottom>q('.fui-list-detail__detail').getBoundingClientRect().top+80)return 'back link not at the top of the pane';
`)
	})
	t.Run("PhonePlaceholder", func(t *testing.T) {
		check(t, "/issues", 390, `if(shown(q('.fui-list-detail__back')))return 'back link shown with the placeholder'`)
	})
	t.Run("Desktop", func(t *testing.T) {
		check(t, "/issues/1", 1280, `if(shown(q('.fui-list-detail__back')))return 'back link shown on desktop'`)
	})
}
