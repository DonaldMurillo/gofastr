//go:build chromium

package siteheader_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/meridian/siteheader"
	"github.com/DonaldMurillo/gofastr/framework"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// geometry is what the page JS reports, in CSS pixels.
type geometry struct {
	ViewW, ViewH, ScrollW float64
	Header, Nav, Toggle   []float64 // [x, y, w, h]; w == 0 when not displayed
	Panel, PanelToggle    []float64
	BarToggle, Persistent []float64
	Brand, Column         []float64
	PanelSignIn           []float64
}

// The header's layout promises, drawn in Chrome: links and actions in
// the bar on wide screens, both folded into a menu on phones while the
// persistent sign-in stays in the bar (with no duplicate inside the
// menu), a phone menu that covers the page under the bar, no sideways
// scroll, and a bar that stays pinned while the page scrolls under it.
func TestHeaderLayoutPromises(t *testing.T) {
	paras := make([]render.HTML, 60)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d of a long page.", i)))
	}
	page := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		siteheader.Render(siteheader.Config{
			Name: "Meridian",
			Links: []siteheader.Link{
				{Label: "Pricing", Href: "/pricing"},
				{Label: "About", Href: "/about"},
			},
			Persistent: ui.LinkButton(ui.LinkButtonConfig{Label: "Sign in", Href: "/login", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}),
			Actions:    ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
		}),
		html.Main(html.MainConfig{}, ui.Container(ui.ContainerConfig{Width: ui.ContainerPage}, render.Join(paras...))),
	)
	site := app.NewApp("Header")
	site.WithTheme(theme.Default(theme.Overrides{}).Extend(siteheader.Tokens))
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
const h='[data-fui-scope="meridian-siteheader"]';
return JSON.stringify({ViewW:innerWidth,ViewH:innerHeight,ScrollW:document.documentElement.scrollWidth,
Header:r(h),Nav:r(h+' nav[aria-label="Primary"]'),Toggle:r(h+' summary'),
Panel:r(h+' details[open] > summary + *'),PanelToggle:r(h+' details[open] .panel-actions [data-fui-comp="ui-theme-toggle"]'),
BarToggle:r(h+' .bar-actions [data-fui-comp="ui-theme-toggle"]'),Persistent:r(h+' .end a[href="/login"]'),
Brand:r(h+' .brand'),Column:r('main p'),
PanelSignIn:r(h+' details[open] a[href="/login"]')})})()`
	at := func(width int64, act ...chromedp.Action) geometry {
		t.Helper()
		actions := []chromedp.Action{
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(srv.URL + "/"),
			chromedp.Poll(`!!window.__gofastr`, nil),
		}
		actions = append(actions, act...)
		var raw string
		actions = append(actions, chromedp.Evaluate(measure, &raw))
		if err := chromedp.Run(ctx, actions...); err != nil {
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

	t.Run("wide", func(t *testing.T) {
		g := at(1280)
		if !shown(g.Nav) || !shown(g.BarToggle) {
			t.Errorf("links and actions belong in the bar at 1280: nav %v, toggle %v", g.Nav, g.BarToggle)
		}
		if !shown(g.Persistent) {
			t.Errorf("the persistent sign-in belongs in the bar at 1280: %v", g.Persistent)
		}
		if shown(g.Toggle) {
			t.Errorf("the phone menu's toggle shows at 1280: %v", g.Toggle)
		}
	})

	// The bar's content sits on the page measure main's ContainerPage
	// uses: the brand starts on main's text edge and the sign-in CTA
	// ends before its far edge. A dropped max-inline-size or padding
	// (an invalid calc()) spreads the bar to the viewport edges.
	t.Run("on-the-page-column", func(t *testing.T) {
		g := at(1440)
		if !near(g.Brand[0], g.Column[0]) {
			t.Errorf("the bar's content should start on main's column %v: brand %v", g.Column, g.Brand)
		}
		if g.Persistent[0]+g.Persistent[2] > g.Column[0]+g.Column[2]+1 {
			t.Errorf("the sign-in CTA should end inside main's column %v: %v", g.Column, g.Persistent)
		}
		g = at(390)
		if !near(g.Brand[0], g.Column[0]) {
			t.Errorf("at 390 the brand should start on main's text edge %v: %v", g.Column[0], g.Brand)
		}
	})

	t.Run("phone", func(t *testing.T) {
		g := at(390)
		if shown(g.Nav) || shown(g.BarToggle) {
			t.Errorf("links and bar actions should fold into the menu at 390: nav %v, toggle %v", g.Nav, g.BarToggle)
		}
		if !shown(g.Toggle) {
			t.Errorf("the menu toggle is missing at 390")
		}
		if !shown(g.Persistent) || g.Persistent[0]+g.Persistent[2] > 390 {
			t.Errorf("the persistent sign-in should stay in the bar at 390 without overflowing: %v", g.Persistent)
		}
		if g.ScrollW > g.ViewW+1 {
			t.Errorf("the page scrolls sideways at 390: scrollWidth %v", g.ScrollW)
		}
	})

	t.Run("phone-menu-open", func(t *testing.T) {
		g := at(390, chromedp.Click(`[data-fui-scope="meridian-siteheader"] summary`, chromedp.NodeVisible),
			chromedp.Poll(`!!document.querySelector('[data-fui-scope="meridian-siteheader"] details[open]')`, nil),
			// The panel slides in; measure where it settles.
			chromedp.Poll(`document.getAnimations().every(a => a.playState === 'finished')`, nil))
		if !shown(g.Panel) {
			t.Fatalf("the open menu draws no panel")
		}
		top := g.Header[1] + g.Header[3]
		if !near(g.Panel[1], top) || !near(g.Panel[0], 0) || !near(g.Panel[2], g.ViewW) || !near(g.Panel[1]+g.Panel[3], g.ViewH) {
			t.Errorf("the panel should cover the viewport under the bar (top %v, viewport %vx%v): %v", top, g.ViewW, g.ViewH, g.Panel)
		}
		if !shown(g.PanelToggle) {
			t.Errorf("the theme toggle is missing from the open menu: %v", g.PanelToggle)
		}
		if g.PanelSignIn != nil {
			t.Errorf("the persistent sign-in must have no duplicate in the menu: %v", g.PanelSignIn)
		}
	})

	// Opening the menu moves, visibly, and closing runs it back. motion
	// reports the transitions running on the panel (and inside it) just
	// after a click: the longest delay+duration in ms, the properties,
	// and whether the panel is still drawn.
	type motion struct {
		Longest float64
		Props   []string
		Drawn   bool
	}
	const probe = `(()=>{const p=document.querySelector('[data-fui-scope="meridian-siteheader"] details > summary + *');
const as=p.getAnimations({subtree:true});
return JSON.stringify({Longest:Math.max(0,...as.map(a=>{const t=a.effect.getComputedTiming();return t.delay+t.duration})),
Props:as.filter(a=>a.effect.target===p).map(a=>a.transitionProperty),Drawn:p.getBoundingClientRect().height>0})})()`
	toggle := chromedp.Click(`[data-fui-scope="meridian-siteheader"] summary`, chromedp.NodeVisible)
	settled := chromedp.Poll(`document.getAnimations().every(a => a.playState === 'finished')`, nil)
	run := func(reduce, closing bool) motion {
		t.Helper()
		pref := "no-preference"
		if reduce {
			pref = "reduce"
		}
		actions := []chromedp.Action{
			chromedp.EmulateViewport(390, 800),
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: pref}}),
			chromedp.Navigate(srv.URL + "/"),
			chromedp.Poll(`!!window.__gofastr`, nil),
			toggle,
		}
		if closing {
			actions = append(actions, settled, toggle)
		}
		var raw string
		actions = append(actions, chromedp.Evaluate(probe, &raw))
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("chromedp: %v", err)
		}
		var m motion
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return m
	}
	has := func(props []string, want string) bool {
		for _, p := range props {
			if p == want {
				return true
			}
		}
		return false
	}
	t.Run("menu-animates-open", func(t *testing.T) {
		m := run(false, false)
		// A 150ms nudge read as no motion at all.
		if m.Longest < 300 {
			t.Errorf("opening should run visible motion for at least 300ms, the longest runs %vms", m.Longest)
		}
		if !has(m.Props, "clip-path") {
			t.Errorf("the panel should unroll (a clip-path transition), it runs %v", m.Props)
		}
	})
	t.Run("menu-animates-closed", func(t *testing.T) {
		m := run(false, true)
		if !m.Drawn || !has(m.Props, "clip-path") {
			t.Errorf("closing should keep the panel drawn while it rolls back up: drawn %v, running %v", m.Drawn, m.Props)
		}
	})
	t.Run("menu-still-under-reduced-motion", func(t *testing.T) {
		if m := run(true, false); m.Longest != 0 {
			t.Errorf("reduced motion should open the menu with no transitions, one runs %vms", m.Longest)
		}
	})

	t.Run("pinned", func(t *testing.T) {
		g := at(1280, chromedp.Evaluate(`window.scrollTo(0, 1200)`, nil), chromedp.Poll(`scrollY >= 1200`, nil))
		if !near(g.Header[1], 0) {
			t.Errorf("the bar should stay at the top after scrolling 1200px, it is at y=%v", g.Header[1])
		}
	})
}
