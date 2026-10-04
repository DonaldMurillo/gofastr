package appbar_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/tracker/appbar"
	"github.com/DonaldMurillo/gofastr/framework"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// geometry is what the page JS reports, in CSS pixels.
type geometry struct {
	ViewW, ScrollW       float64
	Token                float64
	Header, Brand        []float64 // [x, y, w, h]; w == 0 when not displayed
	Search, Toggle, Menu []float64
}

// The bar's layout promises, drawn in Chrome: the band is exactly
// --size-header-height tall (the height ui.ContentRow's viewport mode
// subtracts — a taller or shorter bar breaks the row's screen math),
// the search field sits beside the brand and folds away below md, the
// actions ride the right edge with the phone trigger beside them, and
// the phone bar never scrolls sideways.
func TestAppBarLayoutPromises(t *testing.T) {
	paras := make([]render.HTML, 30)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d of a long page.", i)))
	}
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		appbar.Render(appbar.Config{
			Name:   "Acme Tracker",
			Href:   "/",
			Search: ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "t", Placeholder: "Search issues and projects"}),
			Actions: ui.ThemeToggle(ui.ThemeToggleConfig{
				Variant: ui.ThemeToggleIcon,
			}),
			// The standalone trigger self-hides at widths where the
			// sidebar's inline column shows; its drawer widget is not
			// part of this layout probe.
			MobileTrigger: ui.SidebarDrawerTrigger(ui.SidebarConfig{}),
		}),
		html.Main(html.MainConfig{}, ui.Container(ui.ContainerConfig{Width: ui.ContainerPage}, render.Join(paras...))),
	)
	site := app.NewApp("Appbar")
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

	const measure = `(()=>{const r=s=>{const e=document.querySelector(s);if(!e)return null;const b=e.getBoundingClientRect();return [b.x,b.y,b.width,b.height]};
const h='[data-cui-scope="appbar"]';
return JSON.stringify({ViewW:innerWidth,ScrollW:document.documentElement.scrollWidth,
Token:parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--size-header-height')),
Header:r(h),Brand:r(h+' a.brand'),Search:r(h+' .search'),
Toggle:r(h+' .fui-theme-toggle'),Menu:r(h+' .fui-sidebar__hamburger')})})()`
	at := func(width int64) geometry {
		t.Helper()
		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(srv.URL+"/"),
			chromedp.Poll(`!!window.__gofastr`, nil),
			chromedp.Evaluate(measure, &raw),
		); err != nil {
			t.Fatalf("chromedp at %d: %v", width, err)
		}
		var g geometry
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return g
	}
	shown := func(b []float64) bool { return b != nil && b[2] > 0 && b[3] > 0 }
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1 }

	t.Run("band", func(t *testing.T) {
		g := at(1440)
		if g.Header == nil || !near(g.Header[2], g.ViewW) {
			t.Fatalf("the bar should span the viewport at 1440: %v", g.Header)
		}
		if !near(g.Header[3], g.Token) {
			t.Errorf("the bar is %vpx tall, the --size-header-height token says %v (ui.ContentRow's viewport mode subtracts exactly this)", g.Header[3], g.Token)
		}
		if !near(g.Brand[0], 16) {
			t.Errorf("the brand should sit on the bar's own padding at 1440, it starts at %v", g.Brand[0])
		}
	})

	t.Run("search-beside-brand", func(t *testing.T) {
		g := at(1440)
		if !shown(g.Search) {
			t.Fatalf("the search field is missing from the desktop bar")
		}
		if g.Search[0] < g.Brand[0]+g.Brand[2] {
			t.Errorf("the search field should sit beside the brand, it overlaps it (brand %v, search %v)", g.Brand, g.Search)
		}
		if !near(g.Search[1]+g.Search[3]/2, g.Header[3]/2) {
			t.Errorf("the search field should center in the band: %v in %v", g.Search, g.Header)
		}
	})

	t.Run("actions-ride-the-right-edge", func(t *testing.T) {
		g := at(1440)
		if !shown(g.Toggle) {
			t.Fatalf("the theme toggle is missing from the bar")
		}
		if !near(g.Toggle[0]+g.Toggle[2], g.ViewW-16) {
			t.Errorf("the last action should end 16px from the right edge at 1440, it ends at %v", g.Toggle[0]+g.Toggle[2])
		}
	})

	t.Run("phone", func(t *testing.T) {
		g := at(390)
		if shown(g.Search) {
			t.Errorf("the search field should fold away at 390: %v", g.Search)
		}
		if !shown(g.Menu) {
			t.Errorf("the sidebar's drawer trigger is missing at 390")
		}
		if !shown(g.Toggle) {
			t.Errorf("the actions should stay in the bar at 390")
		}
		if g.ScrollW > g.ViewW+1 {
			t.Errorf("the page scrolls sideways at 390: scrollWidth %v", g.ScrollW)
		}
	})
}

// TestAppBarSearchWidthToken pins the tokens file: the search field's
// resting width is a typed token, not a stylesheet literal, so the app
// that Extends appbar.Tokens retunes the bar.
func TestAppBarSearchWidthToken(t *testing.T) {
	if got := appbar.Tokens.Sizes.AppbarSearch.Value; got != "26rem" {
		t.Errorf("--size-appbar-search = %q, want 26rem", got)
	}
	if got := appbar.Tokens.Sizes.AppbarSearchMin.Value; got != "20rem" {
		t.Errorf("--size-appbar-search-min = %q, want 20rem", got)
	}
	if css := appbar.Tokens.Sizes.AppbarSearch.CSS(); !strings.Contains(css, "var(--size-appbar-search)") {
		t.Errorf("appbar.Tokens.Sizes.AppbarSearch.CSS() = %q, want it to name var(--size-appbar-search)", css)
	}
}
