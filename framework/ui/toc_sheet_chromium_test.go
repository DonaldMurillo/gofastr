//go:build chromium

package ui_test

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
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

// An AnchoredRail with PhoneSheet folds the same way: on a phone the
// pill names the section in view by its text alone, and the open sheet
// holds the rail unpinned, without its own label under the sheet title.
func TestAnchoredRailFoldsOnPhone(t *testing.T) {
	items := make([]ui.RailItem, 4)
	sections := make([]render.HTML, 4)
	for i := range items {
		items[i] = ui.RailItem{Eyebrow: fmt.Sprintf("%02d", i), Text: fmt.Sprintf("Intent %d", i), Anchor: fmt.Sprintf("i%d", i), Count: 3}
		body := []render.HTML{}
		for range 30 {
			body = append(body, html.Paragraph(html.TextConfig{}, render.Text("Body")))
		}
		sections[i] = ui.Section(ui.SectionConfig{ID: items[i].Anchor, Heading: items[i].Text}, body...)
	}
	rail := ui.AnchoredRail(ui.AnchoredRailConfig{Label: "By intent", Items: items, ObserveSelector: "#sections", PhoneSheet: true})
	page := rail + html.Div(html.DivConfig{ID: "sections"}, sections...)
	check := tocPage(t, page, "phonesheet")
	check(t, 390, `
q('#i2').scrollIntoView(); await wait(400);
const label=q('.cui-sheet-pill__label').textContent;
if(label!=='Intent 2')return 'pill names '+JSON.stringify(label)+', not the intent in view';
q('.cui-sheet-pill').click(); await wait(400);
const aside=q('[data-cui-comp="ui-anchored-rail"]');
if(!q('.cui-phone-sheet__sheet').matches(':popover-open'))return 'the pill did not open the sheet';
if(getComputedStyle(aside).position!=='static')return 'the rail stays sticky in the sheet';
if(getComputedStyle(q('.fui-anchored-rail__label')).display!=='none')return 'the rail repeats its label under the sheet title';
`)
}

// With PhoneSheetTop the rail's pill is a bar pinned at the top: a
// section reached from the open sheet lands below the bar, not under
// it, and the bar names that section, not the one whose tail is left
// behind the bar.
func TestAnchoredRailBarClearsSections(t *testing.T) {
	items := make([]ui.RailItem, 4)
	sections := make([]render.HTML, 4)
	for i := range items {
		items[i] = ui.RailItem{Text: fmt.Sprintf("Intent %d", i), Anchor: fmt.Sprintf("i%d", i)}
		body := []render.HTML{}
		for range 30 {
			body = append(body, html.Paragraph(html.TextConfig{}, render.Text("Body")))
		}
		sections[i] = ui.Section(ui.SectionConfig{ID: items[i].Anchor, Heading: items[i].Text}, body...)
	}
	rail := ui.AnchoredRail(ui.AnchoredRailConfig{Label: "On this page", Items: items, ObserveSelector: "#sections", PhoneSheet: true, PhoneSheetTop: true})
	page := rail + html.Div(html.DivConfig{ID: "sections"}, sections...)
	check := tocPage(t, page, "phonesheet")
	check(t, 390, `
const bar=q('.cui-sheet-pill--bar');
bar.click(); await wait(400);
q('.cui-phone-sheet__sheet a[href="#i2"]').click(); await wait(600);
const b=r(bar), s=r(q('#i2'));
if(Math.round(b.top)!==0)return 'the bar is not pinned: top '+b.top;
if(s.top<b.bottom-1)return 'section top '+s.top+' sits under the bar (bottom '+b.bottom+')';
const label=q('.cui-sheet-pill__label').textContent;
if(label!=='Intent 2')return 'the bar names '+JSON.stringify(label)+', not the section below it';
`)
}

// Opened before the page scrolls, while the bar still sits in flow
// below the content above it, the sheet hangs from where the bar is
// drawn, not from where it would pin, and the dim starts there too.
func TestBarSheetHangsBeforeScroll(t *testing.T) {
	items := []ui.RailItem{{Text: "Intent 0", Anchor: "i0"}, {Text: "Intent 1", Anchor: "i1"}}
	sections := []render.HTML{
		ui.Section(ui.SectionConfig{ID: "i0", Heading: "Intent 0"}, html.Paragraph(html.TextConfig{}, render.Text("Body"))),
		ui.Section(ui.SectionConfig{ID: "i1", Heading: "Intent 1"}, html.Paragraph(html.TextConfig{}, render.Text("Body"))),
	}
	rail := ui.AnchoredRail(ui.AnchoredRailConfig{Label: "On this page", Items: items, ObserveSelector: "#sections", PhoneSheet: true, PhoneSheetTop: true})
	intro := make([]render.HTML, 8)
	for i := range intro {
		intro[i] = html.Paragraph(html.TextConfig{}, render.Text("Intro"))
	}
	page := html.Div(html.DivConfig{}, intro...) + rail + html.Div(html.DivConfig{ID: "sections"}, sections...)
	check := tocPage(t, page, "phonesheet")
	check(t, 390, `
const bar=q('.cui-sheet-pill--bar'), sheet=q('.cui-phone-sheet__sheet');
const b=r(bar);
if(b.top<100)return 'fixture: the bar should start in flow, top '+b.top;
bar.click(); await wait(400);
if(!sheet.matches(':popover-open'))return 'the bar did not open the sheet';
const s=r(sheet);
if(Math.abs(s.top-b.bottom)>1)return 'the sheet hangs at '+s.top+', the bar ends at '+b.bottom;
const dim=parseFloat(getComputedStyle(sheet,'::backdrop').insetBlockStart);
if(Math.abs(dim-b.bottom)>1)return 'the dim starts at '+dim+', the bar ends at '+b.bottom;
`)
}

// A PhoneSheetTop contents list in a ContentRow aside: on a phone the
// stacked aside moves above the main column and sticks, so the bar
// pins at the top through the whole page instead of trailing the
// article, and the sheet hangs from it.
func TestTOCBarInAsidePinsOnPhone(t *testing.T) {
	items := make([]ui.TOCItem, 4)
	sections := make([]render.HTML, 4)
	for i := range items {
		id := fmt.Sprintf("s%d", i)
		items[i] = ui.TOCItem{ID: id, Label: fmt.Sprintf("Section %d", i)}
		body := []render.HTML{html.Heading(html.HeadingConfig{Level: 2}, render.Text(items[i].Label))}
		for range 30 {
			body = append(body, html.Paragraph(html.TextConfig{}, render.Text("Body")))
		}
		sections[i] = html.Div(html.DivConfig{ID: id}, body...)
	}
	toc := ui.TableOfContents(ui.TOCConfig{Items: items, Target: "main", Sticky: true, PhoneSheet: true, PhoneSheetTop: true})
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{
			Aside:      render.Raw(`<div data-cui-outlet="probe#toc">`) + toc + render.Raw(`</div>`),
			AsideLabel: "On this page",
		}, html.Main(html.MainConfig{}, sections...)))
	check := tocPage(t, page, "phonesheet")
	t.Run("Phone", func(t *testing.T) {
		check(t, 390, `
const bar=q('.cui-sheet-pill--bar'), main=q('main'), aside=q('.fui-content-row__aside');
if(r(bar).top>r(main).top)return 'the bar trails the article: bar '+r(bar).top+', main '+r(main).top;
if(parseFloat(getComputedStyle(aside).borderTopWidth)!==0)return 'the stacked aside draws a rule';
for(const y of [1500, 3500, document.documentElement.scrollHeight]){
  scrollTo(0, y); await wait(200);
  if(Math.round(r(bar).top)!==0)return 'at scroll '+y+' the bar is not pinned: top '+r(bar).top;
}
bar.click(); await wait(400);
const s=r(q('.cui-phone-sheet__sheet'));
if(Math.abs(s.top-r(bar).bottom)>1)return 'the sheet hangs at '+s.top+', the bar ends at '+r(bar).bottom;
`)
	})
	t.Run("Desktop", func(t *testing.T) {
		check(t, 1280, `
const main=q('main'), aside=q('.fui-content-row__aside');
if(Math.abs(r(aside).top-r(main).top)>1||r(aside).left<r(main).left+100)return 'the aside left the side column: aside '+JSON.stringify(r(aside))+', main '+JSON.stringify(r(main));
`)
	})
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
