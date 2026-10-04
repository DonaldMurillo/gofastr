//go:build chromium

package docpage_test

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

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/docpage"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// front is the first action every measurement tab runs: axetest tabs
// are background targets (visibilityState hidden), and a hidden tab
// throttles the page clock, which starves polls and settles.
var front = page.BringToFront()

// geometry is what the page JS reports, in CSS pixels.
type geometry struct {
	ViewW, ScrollW    float64
	Scope             []float64
	ScopePadLeft      float64 // the scope's resolved inline padding
	Nav, Article, Toc []float64
	NavDisplay        string // the computed display of the nav rail
	TocDisplay        string // the computed display of the toc rail
	Pager, PageNext   []float64
	ProseMeasure      float64 // --size-prose-measure resolved in px
}

// The docs page's layout promises, drawn in Chrome: three columns at
// lg and up, a rail collapsing when its region renders empty or is
// left unset (the contents rail dropping below lg too), no rails at
// all drawing one centred column at the prose measure, and one column
// with no sideways scroll on a phone.
func TestDocPageLayoutPromises(t *testing.T) {
	// article builds a body with sections a TOC can watch.
	article := func(paras int) []render.HTML {
		out := []render.HTML{
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("Getting started")),
			html.Paragraph(html.TextConfig{}, render.Text("The first paragraph of the article.")),
		}
		for i := range paras {
			out = append(out,
				html.Heading(html.HeadingConfig{Level: 2}, render.Text(fmt.Sprintf("Section %d", i+1))),
				html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("Paragraph %d of a long article that wraps onto several lines at every width the page is read at.", i))),
			)
		}
		return out
	}
	navRail := html.Nav(html.NavConfig{Label: "Browse the help"},
		html.UnorderedList(html.ListConfig{},
			html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/help/start", Text: "Getting started"})),
			html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/help/billing", Text: "Billing"})),
		))
	tocRail := html.Nav(html.NavConfig{Label: "On this page"},
		html.UnorderedList(html.ListConfig{},
			html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "#s1", Text: "Section 1"})),
			html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "#s2", Text: "Section 2"})),
		))
	crumbs := html.Nav(html.NavConfig{Label: "Breadcrumb"},
		html.Link(html.LinkConfig{Href: "/help", Text: "Help"}))

	shell := func(cfg docpage.Config) render.HTML {
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			html.Main(html.MainConfig{}, docpage.Render(cfg)))
	}
	// full: every region filled, prev/next pager under the article.
	full := shell(docpage.Config{
		Nav:    navRail,
		Crumbs: crumbs,
		Body:   render.Join(article(4)...),
		Pager:  docpage.Pager(docpage.PagerConfig{PrevHref: "/help/a", PrevLabel: "Setup", NextHref: "/help/c", NextLabel: "Billing"}),
		Toc:    tocRail,
	})
	// short: an article with no headings fills no TOC (an outlet with
	// no fill renders the empty element the collapse keys on).
	short := shell(docpage.Config{
		Nav:  navRail,
		Body: render.Join(article(0)...),
		Toc:  html.Div(html.DivConfig{}, render.HTML("")),
	})
	// plain: neither rail set, the old DocLayout narrow shape a blog
	// post or a standalone page uses. An unset region renders the
	// empty element itself (no child at all).
	plain := shell(docpage.Config{Body: render.Join(article(2)...)})
	// noNav: the left rail unset, the contents rail filled.
	noNav := shell(docpage.Config{
		Body: render.Join(article(2)...),
		Toc:  tocRail,
	})
	// noToc: the left rail filled, the contents rail unset (the empty
	// element, not the empty child the /short page uses).
	noToc := shell(docpage.Config{
		Nav:  navRail,
		Body: render.Join(article(2)...),
	})

	site := app.NewApp("Docs")
	// A theme set the way an app sets one; docpage's prose-measure
	// token joins it the way the site's Extend would.
	site.WithTheme(theme.Default().Extend(docpage.Tokens))
	site.RegisterScreen(app.NewScreen("/", app.NewStaticComponent(full)), nil)
	site.RegisterScreen(app.NewScreen("/short", app.NewStaticComponent(short)), nil)
	site.RegisterScreen(app.NewScreen("/plain", app.NewStaticComponent(plain)), nil)
	site.RegisterScreen(app.NewScreen("/no-nav", app.NewStaticComponent(noNav)), nil)
	site.RegisterScreen(app.NewScreen("/no-toc", app.NewStaticComponent(noToc)), nil)
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
const d='[data-fui-scope="docpage"]';
const root=document.querySelector(d);
const nav=document.querySelector(d+' .nav');
const toc=document.querySelector(d+' .toc');
const probe=document.createElement('div');
probe.style.cssText='position:absolute;inline-size:var(--size-prose-measure)';
document.body.appendChild(probe);
const prose=probe.getBoundingClientRect().width;
probe.remove();
return JSON.stringify({ViewW:innerWidth,ScrollW:document.documentElement.scrollWidth,
Scope:r(d),ScopePadLeft:root?parseFloat(getComputedStyle(root).paddingLeft):null,
Nav:r(d+' .nav'),NavDisplay:nav?getComputedStyle(nav).display:null,
Article:r(d+' .article'),Toc:r(d+' .toc'),TocDisplay:toc?getComputedStyle(toc).display:null,
Pager:r(d+' .pager'),PageNext:r(d+' .page.next'),ProseMeasure:prose})})()`
	// at opens a fresh tab and measures the page at the given width.
	at := func(width int64, path string) geometry {
		t.Helper()
		ctx, cancel := axetest.NewTab(t, browser)
		defer cancel()
		actions := []chromedp.Action{
			front,
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(srv.URL + path),
			chromedp.Poll(`!!window.__gofastr`, nil),
		}
		var raw string
		actions = append(actions, chromedp.Evaluate(measure, &raw))
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("chromedp at %d %s: %v", width, path, err)
		}
		var g geometry
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return g
	}
	shown := func(b []float64) bool { return b != nil && b[2] > 0 && b[3] > 0 }
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1 }

	t.Run("three-columns-at-lg", func(t *testing.T) {
		g := at(1280, "/")
		if !shown(g.Nav) || !shown(g.Article) || !shown(g.Toc) {
			t.Fatalf("the three regions should draw at 1280: nav %v, article %v, toc %v", g.Nav, g.Article, g.Toc)
		}
		if !(g.Nav[0] < g.Article[0] && g.Article[0] < g.Toc[0]) {
			t.Errorf("the rails should flank the article: nav %v, article %v, toc %v", g.Nav, g.Article, g.Toc)
		}
		// The rails are sticky under where the pinned header sits, so
		// they share THEIR top, below the article's first block.
		if !near(g.Nav[1], g.Toc[1]) {
			t.Errorf("the rails should sit level: nav %v, toc %v", g.Nav, g.Toc)
		}
		if g.Nav[2] < 100 || g.Toc[2] < 100 {
			t.Errorf("the rails should hold their measured columns: nav %v, toc %v", g.Nav, g.Toc)
		}
		if !shown(g.Pager) || !shown(g.PageNext) {
			t.Errorf("the pager and its next card should draw under the article: %v %v", g.Pager, g.PageNext)
		}
	})

	t.Run("empty-toc-collapses", func(t *testing.T) {
		fullPage := at(1280, "/")
		shortPage := at(1280, "/short")
		if shortPage.TocDisplay != "none" {
			t.Fatalf("an empty TOC region should hide the rail (display %q)", shortPage.TocDisplay)
		}
		// The article takes the freed column: it is measurably wider
		// than on the three-column page.
		if grew := shortPage.Article[2] - fullPage.Article[2]; grew < 150 {
			t.Errorf("the article should take the collapsed rail's column (grew only %vpx): %v vs %v", grew, shortPage.Article, fullPage.Article)
		}
	})

	t.Run("no-rails-centred-column", func(t *testing.T) {
		g := at(1280, "/plain")
		if g.NavDisplay != "none" || g.TocDisplay != "none" {
			t.Fatalf("unset rails should hide (nav display %q, toc display %q)", g.NavDisplay, g.TocDisplay)
		}
		if !shown(g.Article) {
			t.Fatalf("the article should draw with no rails: %v", g.Article)
		}
		// The old DocLayout narrow mode: one column at the prose
		// measure, centred between equal gaps.
		left, right := g.Article[0], g.ViewW-(g.Article[0]+g.Article[2])
		if d := math.Abs(left - right); d > 2 {
			t.Errorf("the article should sit centred between equal gaps (left %v, right %v)", left, right)
		}
		if g.Article[2] > g.ProseMeasure+1 {
			t.Errorf("the article should stop at the prose measure (%vpx), width %v", g.ProseMeasure, g.Article[2])
		}
	})

	t.Run("no-rails-one-column-on-phone", func(t *testing.T) {
		g := at(375, "/plain")
		if g.ScrollW > g.ViewW+1 {
			t.Errorf("the page scrolls sideways at 375: scrollWidth %v", g.ScrollW)
		}
		if !shown(g.Article) || g.NavDisplay != "none" {
			t.Errorf("the article alone should draw at 375: article %v, nav display %q", g.Article, g.NavDisplay)
		}
	})

	t.Run("unset-nav-drops-left-rail", func(t *testing.T) {
		g := at(1280, "/no-nav")
		if g.NavDisplay != "none" {
			t.Fatalf("an unset nav should hide the rail (display %q)", g.NavDisplay)
		}
		if !shown(g.Article) || !shown(g.Toc) || !(g.Article[0] < g.Toc[0]) {
			t.Fatalf("the article and the filled toc should flank: article %v, toc %v", g.Article, g.Toc)
		}
		// The article starts at the page's own left padding, not one
		// rail in.
		if !near(g.Article[0], g.Scope[0]+g.ScopePadLeft) {
			t.Errorf("the article should start at the page padding: article x %v, scope %v + %v", g.Article[0], g.Scope[0], g.ScopePadLeft)
		}
	})

	t.Run("unset-toc-collapses-too", func(t *testing.T) {
		fullPage := at(1280, "/")
		g := at(1280, "/no-toc")
		if g.TocDisplay != "none" {
			t.Fatalf("an unset toc should hide the rail (display %q)", g.TocDisplay)
		}
		if !shown(g.Nav) || !shown(g.Article) || !(g.Nav[0] < g.Article[0]) {
			t.Fatalf("the nav should stay beside the article: nav %v, article %v", g.Nav, g.Article)
		}
		// The unset rail frees its column exactly like the empty
		// child on /short does.
		if grew := g.Article[2] - fullPage.Article[2]; grew < 150 {
			t.Errorf("the article should take the collapsed rail's column (grew only %vpx): %v vs %v", grew, g.Article, fullPage.Article)
		}
	})

	t.Run("below-lg-drops-toc", func(t *testing.T) {
		g := at(900, "/")
		if g.TocDisplay != "none" {
			t.Errorf("the contents rail should be gone below lg (display %q)", g.TocDisplay)
		}
		if !shown(g.Nav) || !shown(g.Article) || !(g.Nav[0] < g.Article[0]) {
			t.Errorf("the nav should stay beside the article at 900: nav %v, article %v", g.Nav, g.Article)
		}
	})

	t.Run("one-column-on-phone", func(t *testing.T) {
		g := at(375, "/")
		if g.ScrollW > g.ViewW+1 {
			t.Errorf("the page scrolls sideways at 375: scrollWidth %v", g.ScrollW)
		}
		if !shown(g.Nav) || !shown(g.Article) {
			t.Fatalf("the nav and article should draw at 375: nav %v, article %v", g.Nav, g.Article)
		}
		if !near(g.Nav[0], g.Article[0]) || g.Article[1] < g.Nav[1]+g.Nav[3] {
			t.Errorf("the nav should stack above the article at 375: nav %v, article %v", g.Nav, g.Article)
		}
		if !shown(g.Pager) || !near(g.PageNext[0], g.Pager[0]) {
			t.Errorf("the pager cards should stack at 375: pager %v, next %v", g.Pager, g.PageNext)
		}
	})
}
