//go:build chromium

package ui_test

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// A contents list with PhoneSheet in a ContentRow aside, the way a layout
// places it (inside an outlet): on desktop it sticks in the column as
// the window scrolls; on a phone the aside is flush, the pill names the
// section in view, and the open sheet holds the list unpinned and
// without its own label.
func TestTOCPhoneSheetInAside(t *testing.T) {
	items := make([]ui.TOCItem, 6)
	sections := make([]render.HTML, 6)
	for i := range items {
		id := fmt.Sprintf("s%d", i)
		items[i] = ui.TOCItem{ID: id, Label: fmt.Sprintf("Section %d", i)}
		body := []render.HTML{html.Heading(html.HeadingConfig{Level: 2}, render.Text(items[i].Label))}
		for range 30 {
			body = append(body, html.Paragraph(html.TextConfig{}, render.Text("Body")))
		}
		sections[i] = html.Div(html.DivConfig{ID: id}, body...)
	}
	toc := ui.TableOfContents(ui.TOCConfig{Items: items, Target: "main", Sticky: true, PhoneSheet: true})
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{
			Aside:           render.Raw(`<div data-cui-outlet="probe#toc">`) + toc + render.Raw(`</div>`),
			AsideLabel:      "On this page",
			PhoneAsideFlush: true,
		}, html.Main(html.MainConfig{}, sections...)))
	check := tocPage(t, page, "phonesheet")
	t.Run("Desktop", func(t *testing.T) {
		check(t, 1280, `
const nav=q('[data-cui-comp="ui-toc"]');
if(getComputedStyle(q('.cui-sheet-pill')).display!=='none')return 'the pill shows on desktop';
scrollTo(0, 3000); await wait(200);
const n=r(nav);
if(n.height===0)return 'the list is not drawn';
if(n.top<0||n.top>200)return 'the list did not stick: top '+n.top;
`)
	})
	t.Run("Phone", func(t *testing.T) {
		check(t, 390, `
const aside=q('.fui-content-row__aside'), pill=q('.cui-sheet-pill');
if(parseFloat(getComputedStyle(aside).borderTopWidth)!==0)return 'the stacked aside draws a rule';
if(r(aside).height>1)return 'the stacked aside is '+r(aside).height+'px tall';
if(getComputedStyle(pill).display==='none')return 'no pill on a phone';
q('#s3').scrollIntoView(); await wait(400);
const label=q('.cui-sheet-pill__label').textContent;
if(label!=='Section 3')return 'pill names '+JSON.stringify(label)+', not the section in view';
pill.click(); await wait(400);
const nav=q('[data-cui-comp="ui-toc"]');
if(!q('.cui-phone-sheet__sheet').matches(':popover-open'))return 'the pill did not open the sheet';
if(getComputedStyle(nav).position!=='static')return 'the list stays sticky in the sheet';
if(getComputedStyle(nav,'::before').display!=='none')return 'the list repeats its label under the sheet title';
if(!q('[data-cui-comp="ui-toc"] a[aria-current]'))return 'nothing marked in the sheet';
`)
	})
}

// A labelled sticky list that is not a PhoneSheet keeps its label and
// its stickiness inside any other open popover: the sheet overrides
// belong to the phone sheet alone.
func TestTOCInOtherPopoverKeepsLabel(t *testing.T) {
	items := []ui.TOCItem{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	toc := ui.TableOfContents(ui.TOCConfig{Label: "In this file", Items: items, Sticky: true})
	page := render.Raw(`<div popover="manual" id="tools">`) + toc + render.Raw(`</div>`)
	check := tocPage(t, page, "")
	check(t, 1280, `
q('#tools').showPopover();
const nav=q('[data-cui-comp="ui-toc"]');
if(getComputedStyle(nav,'::before').display==='none')return 'the list lost its label in an unrelated popover';
if(getComputedStyle(nav).position!=='sticky')return 'the list lost its stickiness in an unrelated popover';
`)
}

// tocPage serves page through the framework host and returns a checker
// that loads it at a width, waits for the named behaviour (if any), and
// runs js, which returns a failure message or nothing.
func tocPage(t *testing.T, page render.HTML, module string) func(t *testing.T, width int64, js string) {
	t.Helper()
	site := app.NewApp("TOC")
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
	ctx := chromedptest.Context(t)
	ready := `true`
	if module != "" {
		ready = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.` + module + `)`
	}
	return func(t *testing.T, width int64, js string) {
		t.Helper()
		var result string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 800), chromedp.Navigate(srv.URL+"/"),
			chromedp.Poll(ready, nil),
			chromedp.Evaluate(`(async()=>{const q=s=>document.querySelector(s), r=e=>e.getBoundingClientRect(), wait=ms=>new Promise(f=>setTimeout(f,ms));`+js+`;return ""})()`,
				&result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			t.Fatal(err)
		}
		if result != "" {
			t.Fatal(result)
		}
	}
}
