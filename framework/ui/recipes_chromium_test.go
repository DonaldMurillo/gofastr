//go:build chromium

package ui_test

import (
	"context"
	"encoding/json"
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

// The page-frame recipes the examples and the generator compose: the
// tracker's app shell (a page-tall stack, a banner, the viewport
// content row) and the marketing column (banner, reading container,
// footer). The banners here are stand-ins: a real site's header and
// footer are its own packages, each with its own geometry test
// (examples/acme-site/siteheader). Hard rule 9: a frame's guarantees
// are layout, invisible to markup and stylesheet checks — each test
// below renders the recipe and measures the region boxes the retired
// page shell used to guarantee. (The recipes were proven box-equal to
// that shell within 1px at 1280 and 390 before it was deleted; these
// are the invariants that equality stood for.)

// trackerRecipe composes the tracker's app shell (header, sidebar,
// toolbar, main, aside, viewport scrolling) from the pieces — the
// spelling examples/tracker/main.go uses.
func trackerRecipe(header, nav, toolbar, aside, main render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, header),
		ui.ContentRow(ui.ContentRowConfig{
			Viewport:      true,
			PhoneNavFlush: true,
			Sidebar:       nav,
			Toolbar:       toolbar,
			Aside:         aside,
			AsideLabel:    "Context",
		}, main),
	)
}

// meridianRecipe composes the marketing shape (contained header, main,
// footer; a short page keeps the footer at the viewport bottom).
func meridianRecipe(header, main, footer render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Header(html.HeaderConfig{Banner: true}, header),
		main,
		html.Footer(html.FooterConfig{ContentInfo: true}, footer),
	)
}

type recipeBox struct {
	X, Y, W, H float64
}

// UnmarshalJSON reads the [x, y, w, h] arrays the page JS emits.
func (b *recipeBox) UnmarshalJSON(data []byte) error {
	var arr [4]float64
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	b.X, b.Y, b.W, b.H = arr[0], arr[1], arr[2], arr[3]
	return nil
}

type recipeSnapshot struct {
	Regions   map[string]*recipeBox `json:"regions"`
	DocScroll float64               `json:"docScroll"`
	DocView   float64               `json:"docView"`
	HeaderH   float64               `json:"headerH"`
	Token     string                `json:"token"`
	Cols      []bool                `json:"cols"`
}

// TestRecipeShapesKeepTheFrameGuarantees renders each recipe and asserts
// the layout contract the page shell used to carry:
//
//   - tracker at 1280: the document does not scroll, and the nav, main
//     and aside columns each scroll on their own;
//   - meridian at 1280 and 390: a short page's footer bottom equals the
//     viewport bottom, and main is centered at the page measure.
func TestRecipeShapesKeepTheFrameGuarantees(t *testing.T) {
	site := app.NewApp("Frame recipes")

	// ── shared content ─────────────────────────────────────────────
	navCfg := ui.SidebarConfig{
		NavLabel: "Primary", DrawerTitle: "Acme Tracker",
		NativeMobile: true, SuppressDrawerTrigger: true,
		CurrentPath: "/inbox",
	}
	for i := 1; i <= 20; i++ {
		navCfg.Items = append(navCfg.Items, ui.SidebarItem{
			Label: fmt.Sprintf("Issue %d", i), Href: fmt.Sprintf("/inbox/%d", i),
		})
	}
	nav, err := component.SafeRenderCtx(context.Background(), ui.Sidebar(navCfg))
	if err != nil {
		t.Fatalf("sidebar render: %v", err)
	}

	paras := make([]render.HTML, 30)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(
			fmt.Sprintf("Row %d: the quick brown fox jumps over the lazy dog.", i)))
	}
	trackerMain := html.Main(html.MainConfig{}, ui.Stack(ui.StackConfig{Gap: ui.GapMD}, paras...))

	toolbar := ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
		ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Trail"}, ui.Crumb{Text: "Inbox", Current: true}),
		ui.Toolbar(ui.ToolbarConfig{Plain: true, Label: "Actions", Groups: []ui.ToolbarGroup{
			{Children: []render.HTML{ui.Button(ui.ButtonConfig{Label: "New"})}},
		}}))

	asideItems := make([]render.HTML, 25)
	for i := range asideItems {
		asideItems[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Activity %d", i)))
	}
	aside := ui.Stack(ui.StackConfig{Gap: ui.GapMD}, asideItems...)

	trackerHeader := ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
		html.Link(html.LinkConfig{Href: "/", Text: "Acme Tracker"}),
		ui.SidebarDrawerTrigger(navCfg))

	// The brand sits on the page measure, the way a site header package
	// places it, so main's content can be held to its edge.
	meridianHeader := ui.Container(ui.ContainerConfig{Width: ui.ContainerPage},
		html.Link(html.LinkConfig{Href: "/", Text: "Meridian"}))
	meridianFooter := render.Text("Meridian")
	meridianMain := ui.Container(ui.ContainerConfig{As: "main", Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Ship calm software")),
		html.Paragraph(html.TextConfig{}, render.Text("A short page.")),
	))

	// ── the pages ──────────────────────────────────────────────────
	pages := map[string]render.HTML{
		"/tracker": trackerRecipe(trackerHeader, nav, toolbar, aside, trackerMain),
		"/meridian": meridianRecipe(
			meridianHeader, meridianMain, meridianFooter),
	}
	for path, body := range pages {
		site.RegisterScreen(app.NewScreen(path, app.NewStaticComponent(body)), nil)
	}

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

	snapshot := func(path string, width, height int64, sels []string, cols []string) recipeSnapshot {
		t.Helper()
		js := `(()=>{const q=s=>document.querySelector(s);const r=e=>{if(!e)return null;const b=e.getBoundingClientRect();return [b.x,b.y,b.width,b.height]};const out={regions:{`
		for i, s := range sels {
			if i > 0 {
				js += ","
			}
			js += fmt.Sprintf("%q:r(q(%q))", s, s)
		}
		js += `},docScroll:document.documentElement.scrollHeight,docView:document.documentElement.clientHeight,`
		js += `headerH:(()=>{const h=q('header[role="banner"]');return h?h.getBoundingClientRect().height:null})(),`
		js += `token:getComputedStyle(document.documentElement).getPropertyValue('--size-header-height').trim(),cols:[`
		for j, s := range cols {
			if j > 0 {
				js += ","
			}
			js += fmt.Sprintf(`(()=>{const e=q(%q);return e?e.scrollHeight>e.clientHeight:null})()`, s)
		}
		js += `]};return JSON.stringify(out)})()`
		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, height),
			chromedp.Navigate(srv.URL+path),
			chromedp.Poll(`!!window.__gofastr`, nil),
			chromedp.Evaluate(js, &raw),
		); err != nil {
			t.Fatalf("chromedp (%s at %d): %v", path, width, err)
		}
		var snap recipeSnapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			t.Fatalf("snapshot decode (%s): %v", path, err)
		}
		return snap
	}

	// Tracker at 1280: the document does not scroll, each column does,
	// and the header band's height is the token the row subtracts.
	t.Run("tracker/viewport-scroll", func(t *testing.T) {
		cols := []string{`.fui-content-row__nav`, `.fui-content-row main`, `.fui-content-row__aside`}
		s := snapshot("/tracker", 1280, 800, nil, cols)
		if s.DocScroll > s.DocView+1 {
			t.Errorf("document should not scroll at 1280 (scrollHeight=%v clientHeight=%v)", s.DocScroll, s.DocView)
		}
		for i, scrolls := range s.Cols {
			if !scrolls {
				t.Errorf("column %d should scroll on its own at 1280", i)
			}
		}
	})

	// Meridian at both sizes: a short page's footer anchors to the
	// viewport bottom; main's content starts on the header brand's
	// edge (the page container and the bands share one measure and one
	// gutter), and Pad keeps the first block off the header's rule.
	for _, sz := range [][2]int64{{1280, 800}, {390, 844}} {
		w, h := sz[0], sz[1]
		t.Run(fmt.Sprintf("meridian/%d", w), func(t *testing.T) {
			brand := `header[role="banner"] a[href="/"]`
			heading := `#main-content main h1`
			s := snapshot("/meridian", w, h, []string{`#main-content main`, `footer[role="contentinfo"]`, brand, heading, `header[role="banner"]`}, nil)
			foot := s.Regions[`footer[role="contentinfo"]`]
			if foot == nil {
				t.Fatal("footer missing")
			}
			if foot.Y+foot.H < float64(h)-1 {
				t.Errorf("footer should sit at the viewport bottom, got bottom=%v (viewport %d)", foot.Y+foot.H, h)
			}
			b, h1, hdr := s.Regions[brand], s.Regions[heading], s.Regions[`header[role="banner"]`]
			if b == nil || h1 == nil || hdr == nil {
				t.Fatalf("brand=%v h1=%v header=%v", b, h1, hdr)
			}
			if d := h1.X - b.X; d > 1 || d < -1 {
				t.Errorf("main's content should start on the header brand's edge: h1 x=%v, brand x=%v", h1.X, b.X)
			}
			if gap := h1.Y - (hdr.Y + hdr.H); gap < 40 {
				t.Errorf("Pad should keep the first block at least 40px under the header, got %v", gap)
			}
		})
	}

}

// The aside releases its width when it holds only an empty outlet —
// the behaviour that lets an unfilled context column take no space.
func TestAsideReleasesEmptyOutlet(t *testing.T) {
	emptyOutlet := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-outlet": "app#aside"}})
	site := app.NewApp("Aside release")
	site.RegisterScreen(app.NewScreen("/recipe", app.NewStaticComponent(
		meridianRecipe(render.Text("h"),
			ui.ContentRow(ui.ContentRowConfig{Aside: emptyOutlet, AsideLabel: "Context"},
				html.Main(html.MainConfig{}, render.Text("m"))),
			render.Text("f")),
	)), nil)
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
	var display string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/recipe"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('aside[aria-label="Context"]')).display`, &display),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if display != "none" {
		t.Errorf("an aside holding only an empty outlet must release its column, got display=%q", display)
	}
}

// A non-viewport row under a header and above a footer, in the
// page-tall stack: a short page must not scroll, the footer sits at the
// viewport bottom, and the nav column's surface reaches the footer. A
// row that keeps a bare 100vh minimum scrolls by the header's height
// and pushes the footer below the fold.
func TestRowUnderHeaderHasNoDeadScroll(t *testing.T) {
	nav, err := component.SafeRenderCtx(context.Background(),
		ui.Sidebar(ui.SidebarConfig{NavLabel: "Primary", Items: []ui.SidebarItem{{Label: "Inbox", Href: "/inbox"}}}))
	if err != nil {
		t.Fatal(err)
	}
	site := app.NewApp("Row dead scroll")
	site.RegisterScreen(app.NewScreen("/row", app.NewStaticComponent(
		ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			html.Header(html.HeaderConfig{Banner: true}, html.Link(html.LinkConfig{Href: "/", Text: "Acme"})),
			ui.ContentRow(ui.ContentRowConfig{Sidebar: nav},
				html.Main(html.MainConfig{}, render.Text("short page"))),
			html.Footer(html.FooterConfig{ContentInfo: true}, render.Text("Acme")),
		),
	)), nil)
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
	var got struct {
		Scroll, View, FooterTop, FooterBottom, NavBottom float64
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/row"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`(() => {
			const f = document.querySelector('footer[role="contentinfo"]').getBoundingClientRect();
			const n = document.querySelector('.fui-content-row__nav').getBoundingClientRect();
			return {Scroll: document.documentElement.scrollHeight, View: document.documentElement.clientHeight,
				FooterTop: f.top, FooterBottom: f.bottom, NavBottom: n.bottom};
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got.Scroll > got.View+1 {
		t.Errorf("short page under a header must not scroll: scrollHeight=%v clientHeight=%v", got.Scroll, got.View)
	}
	if d := got.FooterBottom - got.View; d < -1 || d > 1 {
		t.Errorf("footer should sit at the viewport bottom, got bottom=%v (viewport %v)", got.FooterBottom, got.View)
	}
	if d := got.NavBottom - got.FooterTop; d < -1 || d > 1 {
		t.Errorf("nav column should reach the footer, got nav bottom=%v footer top=%v", got.NavBottom, got.FooterTop)
	}
}
