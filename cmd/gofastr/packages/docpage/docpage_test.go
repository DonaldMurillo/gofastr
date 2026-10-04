package docpage_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/docpage"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// full is a representative page: a nav rail, crumbs, an article with a
// pager, and a filled TOC rail.
func full() docpage.Config {
	nav := html.Nav(html.NavConfig{Label: "Browse the help"},
		html.Link(html.LinkConfig{Href: "/help/getting-started", Text: "Getting started"}))
	crumbs := html.Nav(html.NavConfig{Label: "Breadcrumb"},
		html.Link(html.LinkConfig{Href: "/help", Text: "Help"}))
	body := html.Heading(html.HeadingConfig{Level: 1}, render.Text("Getting started"))
	toc := html.Nav(html.NavConfig{Label: "On this page"},
		html.Link(html.LinkConfig{Href: "#first", Text: "First steps"}))
	return docpage.Config{
		Nav:    nav,
		Crumbs: crumbs,
		Body:   body,
		Pager:  docpage.Pager(docpage.PagerConfig{NextHref: "/help/next", NextLabel: "What's next"}),
		Toc:    toc,
	}
}

func TestRenderPageRegions(t *testing.T) {
	html := string(docpage.Render(full()))
	for _, want := range []string{
		// The owned root and its three regions.
		`data-fui-scope="docpage"`, `class="nav"`, `class="article"`, `class="toc"`,
		// The crumbs sit inside the article, above the body.
		`class="crumbs"`, "Getting started",
		// The TOC content lands in its rail.
		"On this page",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered page missing %q:\n%s", want, html)
		}
	}
	if strings.Index(html, `class="crumbs"`) > strings.Index(html, "<h1") {
		t.Errorf("crumbs should render above the article body:\n%s", html)
	}
}

func TestRenderTocRegionCollapse(t *testing.T) {
	filled := string(docpage.Render(full()))
	if !strings.Contains(filled, "On this page") {
		t.Fatalf("the filled TOC should render its content")
	}
	// An empty region element (an outlet with no fill) is the shape the
	// sheet's :has(.toc > :empty) collapse keys on: the rail stays in
	// the markup, holding an empty child.
	cfg := full()
	cfg.Toc = html.Div(html.DivConfig{}, render.HTML(""))
	empty := string(docpage.Render(cfg))
	want := `<div class="toc"><div></div></div>`
	if !strings.Contains(empty, want) {
		t.Errorf("an empty TOC region should render the collapse shape %q:\n%s", want, empty)
	}
}

func TestRenderNavRegionEmpty(t *testing.T) {
	// An unset Nav renders an empty .nav element, no child at all:
	// the shape the sheet's .nav:empty collapse keys on.
	cfg := full()
	cfg.Nav = render.HTML("")
	got := string(docpage.Render(cfg))
	want := `<div class="nav"></div>`
	if !strings.Contains(got, want) {
		t.Errorf("an unset nav should render the empty element %q:\n%s", want, got)
	}
}

func TestPager(t *testing.T) {
	both := string(docpage.Pager(docpage.PagerConfig{
		PrevHref: "/help/a", PrevLabel: "Setup",
		NextHref: "/help/c", NextLabel: "Billing",
	}))
	for _, want := range []string{
		`class="pager"`, `aria-label="More articles"`,
		`class="page"`, `class="page next"`, "Previous", "Next", "Setup", "Billing",
		`href="/help/a"`, `href="/help/c"`,
	} {
		if !strings.Contains(both, want) {
			t.Errorf("pager missing %q:\n%s", want, both)
		}
	}

	// Direction labels default but can be overridden.
	custom := string(docpage.Pager(docpage.PagerConfig{
		PrevHref: "/a", PrevLabel: "A", PrevDir: "Newer",
		NextHref: "/c", NextLabel: "C", NextDir: "Older",
	}))
	for _, want := range []string{"Newer", "Older"} {
		if !strings.Contains(custom, want) {
			t.Errorf("pager direction labels should be configurable (%q missing):\n%s", want, custom)
		}
	}

	// A next-only pager puts its card in the second column.
	nextOnly := string(docpage.Pager(docpage.PagerConfig{NextHref: "/c", NextLabel: "Billing"}))
	if !strings.Contains(nextOnly, `class="page next"`) || strings.Contains(nextOnly, "Previous") {
		t.Errorf("a next-only pager should draw one card:\n%s", nextOnly)
	}

	// No neighbours, no pager at all.
	if got := string(docpage.Pager(docpage.PagerConfig{})); got != "" {
		t.Errorf("a pager with no hrefs should render nothing, got %q", got)
	}
}
