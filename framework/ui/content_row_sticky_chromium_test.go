package ui_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
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
// The column scrolls its own overflow: itself, or (a sidebar filling
// it) the sidebar's nav region inside it.
const sc=[nav,...nav.querySelectorAll('*')].find(e=>e.scrollHeight>e.clientHeight+1&&/(auto|scroll)/.test(getComputedStyle(e).overflowY));
if(!sc)return 'nav column does not overflow on its own';
scrollTo(0, 2000);
if(scrollY<1000)return 'the window did not scroll';
const n1=r(nav), b1=r(bar);
if(n1.top!==n0.top||n1.left!==n0.left)return 'nav column moved: '+n0.top+' -> '+n1.top;
if(b1.top!==b0.top)return 'toolbar moved: '+b0.top+' -> '+b1.top;
sc.scrollTop=300;
if(sc.scrollTop<100)return 'nav column does not scroll';
if(scrollY<1000)return 'scrolling the nav column moved the window';
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

// A sticky panel inside a Sticky row (a FormFrame's side rail) stops
// under the row's toolbar, not under the window's top edge where the
// toolbar would paint over it.
func TestContentRowStickyPanelClearsTheToolbar(t *testing.T) {
	site := app.NewApp("Sticky panel")
	fields := make([]render.HTML, 80)
	for i := range fields {
		fields[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Field %d", i)))
	}
	frame := ui.FormFrame(ui.FormFrameConfig{SidePanel: true, Main: fields,
		Side: []render.HTML{html.Paragraph(html.TextConfig{}, render.Text("Details"))}})
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, Sidebar: render.Text("Nav"), Toolbar: render.Text("Trail")},
			html.Main(html.MainConfig{}, frame)))
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
	var result string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(srv.URL+"/"), chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`(()=>{const q=s=>document.querySelector(s), r=e=>e.getBoundingClientRect();
const bar=q('.fui-content-row__toolbar'), side=q('.fui-form-frame__side');
if(getComputedStyle(side).position!=='sticky')return 'the side panel is not sticky';
scrollTo(0, 1500);
if(scrollY<1000)return 'the window did not scroll';
const b=r(bar), s=r(side);
if(s.top<b.bottom)return 'the side panel ('+s.top+') sits under the toolbar (bottom '+b.bottom+')';
if(s.top>b.bottom+40)return 'the side panel ('+s.top+') floats far below the toolbar (bottom '+b.bottom+')';
return ""})()`, &result)); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatal(result)
	}
}

// A collapsible sidebar's Collapse row sits under its links, never over
// them: in a Sticky row whose nav overflows, the links scroll in their
// own region above the foot row, so no link is half hidden behind it
// (axe's target-size found exactly that once a nav grew one row).
func TestContentRowStickySidebarFootCoversNoLink(t *testing.T) {
	site := app.NewApp("Sticky foot")
	items := make([]ui.SidebarItem, 30)
	for i := range items {
		items[i] = ui.SidebarItem{Label: fmt.Sprintf("Item %d", i), Href: fmt.Sprintf("/i/%d", i)}
	}
	nav, _ := component.SafeRenderCtx(context.Background(), ui.Sidebar(ui.SidebarConfig{NavLabel: "Pages", Items: items, Variant: ui.SidebarCollapsible}))
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, Sidebar: nav, Toolbar: render.Text("Trail")},
			html.Main(html.MainConfig{}, html.Paragraph(html.TextConfig{}, render.Text("Body")))))
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
	var result string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(srv.URL+"/"), chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`(()=>{
const foot=document.querySelector('.fui-sidebar__collapse');
if(!foot)return 'no collapse row';
const f=foot.getBoundingClientRect();
if(f.bottom>innerHeight+1)return 'the collapse row is below the screen: '+f.bottom;
// A link's visible part is its box clipped to the nav's scroll
// region; none of it may lie under the collapse row.
const region=document.querySelector('.fui-sidebar__inline > .fui-sidebar__nav').getBoundingClientRect();
for(const a of document.querySelectorAll('.fui-sidebar__inline a.fui-sidebar__link')){
  const r=a.getBoundingClientRect();
  const top=Math.max(r.top,region.top), bottom=Math.min(r.bottom,region.bottom);
  if(bottom<=top)continue;
  if(bottom>f.top+0.5&&top<f.bottom-0.5)return 'link '+a.textContent.trim()+' ('+top+'..'+bottom+' visible) lies under the collapse row ('+f.top+')';
}
const last=[...document.querySelectorAll('.fui-sidebar__inline a.fui-sidebar__link')].pop();
last.scrollIntoView({block:'nearest'});
const lr=last.getBoundingClientRect(), f2=foot.getBoundingClientRect();
if(lr.bottom>f2.top+0.5)return 'the last link cannot be scrolled clear of the collapse row';
if(scrollY!==0)return 'scrolling the nav moved the window';
return ""})()`, &result)); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatal(result)
	}
}

// A BelowHeader row under a sticky page header pins its nav column
// below the header (never behind it), the rest of the viewport tall,
// while the window scrolls the page; the page footer below the row
// keeps the full width.
func TestContentRowStickyBelowHeader(t *testing.T) {
	site := app.NewApp("Below header")
	items := make([]ui.SidebarItem, 40)
	for i := range items {
		items[i] = ui.SidebarItem{Label: fmt.Sprintf("Item %d", i), Href: fmt.Sprintf("/i/%d", i)}
	}
	nav, _ := component.SafeRenderCtx(context.Background(), ui.Sidebar(ui.SidebarConfig{NavLabel: "Pages", Items: items}))
	paras := make([]render.HTML, 120)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d", i)))
	}
	header := ui.Sticky(ui.StickyConfig{}, html.Header(html.HeaderConfig{}, render.Text("Header")))
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		header,
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, BelowHeader: true, Sidebar: nav},
			html.Main(html.MainConfig{}, paras...)),
		html.Footer(html.FooterConfig{ContentInfo: true}, render.Text("Colophon")))
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
	var result string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(srv.URL+"/"), chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`(()=>{const q=s=>document.querySelector(s), r=e=>e.getBoundingClientRect();
const hh=parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--size-header-height'))||56;
const nav=q('.fui-content-row__nav'), foot=q('footer');
if(Math.round(r(nav).height)!==Math.round(innerHeight-hh))return 'nav column is '+r(nav).height+'px, not the viewport less the '+hh+'px header';
scrollTo(0, 1500);
if(scrollY<1000)return 'the window did not scroll';
if(Math.round(r(nav).top)!==Math.round(hh))return 'nav column top is '+r(nav).top+', not under the '+hh+'px header';
if(Math.round(r(nav).height)!==Math.round(innerHeight-hh))return 'nav column is '+r(nav).height+'px once scrolled';
if(Math.round(r(foot).left)!==0||Math.round(r(foot).width)!==innerWidth)return 'the footer is not full width: '+r(foot).left+' / '+r(foot).width;
return ""})()`, &result)); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatal(result)
	}
}

// An interactive.SectionMenu with FillColumn in a BelowHeader row fills
// the pinned nav column to the viewport's bottom edge and scrolls its
// rail there, with no dead strip under the rail.
func TestContentRowBelowHeaderSectionMenuFillsColumn(t *testing.T) {
	site := app.NewApp("Fill column")
	items := make([]interactive.SectionItem, 60)
	for i := range items {
		items[i] = interactive.SectionItem{Label: fmt.Sprintf("Item %d", i), Href: fmt.Sprintf("/i/%d", i)}
	}
	menu := interactive.SectionMenu(interactive.SectionMenuConfig{FillColumn: true,
		Groups: []interactive.SectionGroup{{Label: "All", Items: items}}})
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		ui.Sticky(ui.StickyConfig{}, html.Header(html.HeaderConfig{}, render.Text("Header"))),
		ui.ContentRow(ui.ContentRowConfig{Sticky: true, BelowHeader: true, Sidebar: menu},
			html.Main(html.MainConfig{}, render.Text("Body"))))
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
	var result string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 800), chromedp.Navigate(srv.URL+"/"), chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`(()=>{const r=e=>e.getBoundingClientRect();
const nav=document.querySelector('.fui-content-row__nav'), rail=document.querySelector('.cui-section-menu__rail');
if(Math.abs(r(nav).bottom-innerHeight)>1)return 'nav column ends at '+r(nav).bottom+', not the viewport bottom '+innerHeight;
if(Math.abs(r(rail).bottom-r(nav).bottom)>1)return 'rail ends at '+r(rail).bottom+', short of the column bottom '+r(nav).bottom;
if(rail.scrollHeight<=rail.clientHeight)return 'rail does not overflow, so the check proves nothing';
rail.scrollTop=400;
if(rail.scrollTop<100)return 'rail does not scroll';
if(nav.scrollHeight>nav.clientHeight+1)return 'the column scrolls too: a second scroller around the rail';
return ""})()`, &result)); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatal(result)
	}
}
