//go:build chromium

package sitefooter_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/sitefooter"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// front is the first action every measurement tab runs: axetest tabs
// are background targets (visibilityState hidden), and a hidden tab
// throttles the page clock, which starves polls and settles.
var front = page.BringToFront()

// geometry is what the page JS reports, in CSS pixels.
type geometry struct {
	ViewW, ScrollW            float64
	Inner, Brand, Note        []float64
	Lead, Columns, Col1, Col2 []float64
	Column                    []float64 // main's paragraph, the page column
	Border                    string    // the footer root's top border colour
	BorderW                   float64   // and its width, the divider itself
}

// The footer's layout promises, drawn in Chrome: titled link columns
// beside the lead on wide screens, content on the page measure main
// uses, the lead stacked over the columns with no sideways scroll on
// a phone, and both colour schemes holding that layout.
func TestFooterLayoutPromises(t *testing.T) {
	paras := make([]render.HTML, 20)
	for i := range paras {
		paras[i] = html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d of a page.", i)))
	}
	columns := make([]sitefooter.Column, 2)
	for i := range columns {
		links := make([]sitefooter.Link, 4)
		for j := range links {
			links[j] = sitefooter.Link{Label: fmt.Sprintf("Link %d", j+1), Href: "/"}
		}
		columns[i] = sitefooter.Column{Title: fmt.Sprintf("Column %d", i+1), Links: links}
	}
	pageHTML := ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
		html.Main(html.MainConfig{}, ui.Container(ui.ContainerConfig{Width: ui.ContainerPage}, render.Join(paras...))),
		sitefooter.Render(sitefooter.Config{
			Name:    "Example",
			Tagline: "One line about the app.",
			Columns: columns,
			Note:    "© 2026 Example",
		}),
	)
	site := app.NewApp("Footer")
	// A theme set the way an app sets one, so the dark palette (and
	// the scheme flip below) is in play.
	site.WithTheme(theme.Default())
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(pageHTML)), nil)
	host := uihost.New(site)
	fw := framework.NewApp()
	fw.Use(host.RouteMatchMiddleware())
	fw.Mount(host)
	if err := fw.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fw.Router())
	defer srv.Close()
	browser := axetest.NewBrowser(t)

	const measure = `(()=>{const r=s=>{const e=document.querySelector(s);if(!e)return null;const b=e.getBoundingClientRect();return [b.x,b.y,b.width,b.height]};
const f='[data-cui-scope="sitefooter"]';
return JSON.stringify({ViewW:innerWidth,ScrollW:document.documentElement.scrollWidth,
Inner:r(f+' .inner'),Brand:r(f+' .brand'),Note:r(f+' .note'),
Lead:r(f+' .top > div:first-child'),Columns:r(f+' .columns'),
Col1:r(f+' .columns > div:nth-child(1)'),Col2:r(f+' .columns > div:nth-child(2)'),
Column:r('main p'),Border:(()=>{const s=getComputedStyle(document.querySelector(f));return s.borderTopColor})(),BorderW:parseFloat(getComputedStyle(document.querySelector(f)).borderTopWidth)})})()`
	// at opens a fresh tab and measures the page at the given width,
	// optionally under a forced colour scheme (axetest.Prepare).
	at := func(width int64, scheme string, act ...chromedp.Action) geometry {
		t.Helper()
		ctx, cancel := axetest.NewTab(t, browser)
		defer cancel()
		actions := []chromedp.Action{
			front,
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(srv.URL + "/"),
			chromedp.Poll(`!!window.__gofastr`, nil),
		}
		if scheme != "" {
			actions = append(actions, axetest.Prepare(scheme))
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

	t.Run("columns-beside-lead", func(t *testing.T) {
		g := at(1280, "")
		if !shown(g.Col1) || !shown(g.Col2) {
			t.Fatalf("the link columns should draw at 1280: %v %v", g.Col1, g.Col2)
		}
		if !near(g.Col1[1], g.Col2[1]) || g.Col1[0]+g.Col1[2] > g.Col2[0] {
			t.Errorf("the columns should lay out side by side: %v %v", g.Col1, g.Col2)
		}
		if !shown(g.Lead) || g.Lead[0]+g.Lead[2] > g.Columns[0] {
			t.Errorf("the lead should take the first third, left of the columns: lead %v, columns %v", g.Lead, g.Columns)
		}
	})

	// The footer's content sits on the same page measure main's
	// ContainerPage uses: the brand starts on main's text edge and the
	// note ends on its far edge. A dropped max-inline-size or padding
	// (an invalid calc()) spreads the footer to the viewport edges.
	t.Run("on-the-page-column", func(t *testing.T) {
		g := at(1440, "")
		if !near(g.Brand[0], g.Column[0]) || !near(g.Note[0]+g.Note[2], g.Column[0]+g.Column[2]) {
			t.Errorf("the footer's content should span main's column %v: brand %v, note %v", g.Column, g.Brand, g.Note)
		}
	})

	t.Run("phone-stacks-and-fits", func(t *testing.T) {
		g := at(375, "")
		if g.ScrollW > g.ViewW+1 {
			t.Errorf("the page scrolls sideways at 375: scrollWidth %v", g.ScrollW)
		}
		if !near(g.Lead[0], g.Columns[0]) || g.Columns[1] < g.Lead[1]+g.Lead[3] {
			t.Errorf("the lead should stack over the columns at 375: lead %v, columns %v", g.Lead, g.Columns)
		}
	})

	t.Run("light-and-dark", func(t *testing.T) {
		light, dark := at(1280, "light"), at(1280, "dark")
		if light.Border == dark.Border {
			t.Errorf("the footer's rule should ride the theme (light %q, dark %q)", light.Border, dark.Border)
		}
		if light.BorderW != 1 || dark.BorderW != 1 {
			t.Errorf("the footer should draw its 1px divider (light %v, dark %v)", light.BorderW, dark.BorderW)
		}
		for name, g := range map[string]geometry{"light": light, "dark": dark} {
			if !near(g.Col1[1], g.Col2[1]) || g.Col1[0]+g.Col1[2] > g.Col2[0] {
				t.Errorf("%s: the columns should still lay out side by side: %v %v", name, g.Col1, g.Col2)
			}
			if !near(g.Brand[0], g.Column[0]) {
				t.Errorf("%s: the footer should stay on the page column %v: brand %v", name, g.Column, g.Brand)
			}
		}
	})
}
