package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestDocLayoutThreeColumnWithToc(t *testing.T) {
	h := string(DocLayout(DocLayoutConfig{
		Nav: render.Raw(`<nav class="rail">nav</nav>`),
		Toc: render.Raw(`<aside class="toc"></aside>`),
	}, render.Text("BODY")))
	mustContain(t, render.HTML(h), `data-fui-comp="ui-doc-layout"`)
	mustContain(t, render.HTML(h), "ui-doc-layout__content")
	mustContain(t, render.HTML(h), "BODY")
	if strings.Contains(h, "ui-doc-layout--notoc") || strings.Contains(h, "ui-doc-layout--narrow") {
		t.Errorf("nav+content+toc should be the default 3-col shape:\n%s", h)
	}
}

func TestDocLayoutNotocWhenNoToc(t *testing.T) {
	h := string(DocLayout(DocLayoutConfig{
		Nav: render.Raw(`<nav class="rail">nav</nav>`),
	}, render.Text("BODY")))
	mustContain(t, render.HTML(h), "ui-doc-layout--notoc")
}

func TestDocLayoutNarrowWhenNoNav(t *testing.T) {
	h := string(DocLayout(DocLayoutConfig{}, render.Text("BODY")))
	mustContain(t, render.HTML(h), "ui-doc-layout--narrow")
}

func TestDocLayoutCrumbsAndCurrent(t *testing.T) {
	h := string(DocLayout(DocLayoutConfig{
		Crumbs: []DocCrumb{{Label: "Docs", Href: "/docs/"}, {Label: "Here"}},
	}, render.Text("BODY")))
	mustContain(t, render.HTML(h), `aria-label="Breadcrumb"`)
	mustContain(t, render.HTML(h), `href="/docs/"`)
	mustContain(t, render.HTML(h), "ui-doc-layout__crumb-current")
	if !strings.Contains(h, "ui-doc-layout__crumb-sep") {
		t.Errorf("multi-crumb trail should have a separator:\n%s", h)
	}
}

func TestDocPrevNextOmitsNextWhenEmpty(t *testing.T) {
	withNext := string(DocPrevNext(DocPager{PrevHref: "/p", PrevLabel: "Prev", NextHref: "/n", NextLabel: "Next"}))
	mustContain(t, render.HTML(withNext), "ui-doc-layout__next")
	last := string(DocPrevNext(DocPager{PrevHref: "/p", PrevLabel: "Prev"}))
	if strings.Contains(last, "ui-doc-layout__next") {
		t.Errorf("no NextHref should omit the next card:\n%s", last)
	}
	mustContain(t, render.HTML(last), "ui-doc-layout__prev")
}

func TestDocLayoutPagerAfterBody(t *testing.T) {
	h := string(DocLayout(DocLayoutConfig{
		Pager: &DocPager{PrevHref: "/p", PrevLabel: "Prev"},
	}, render.Text("BODY")))
	bodyIdx := strings.Index(h, "BODY")
	footIdx := strings.Index(h, "ui-doc-layout__foot")
	if bodyIdx == -1 || footIdx == -1 || footIdx < bodyIdx {
		t.Errorf("pager should render after the body:\n%s", h)
	}
}

func TestDocLayoutCSSCollapsesOnMobile(t *testing.T) {
	css := docLayoutCSS(style.Theme{})
	mq := strings.Index(css, "max-width: 900px")
	if mq < 0 {
		t.Fatal("DocLayout CSS missing its mobile breakpoint")
	}
	// On mobile the grid must collapse to block — a grid track resolves to
	// the content's min-content and overflows narrow viewports otherwise.
	if !strings.Contains(css[mq:], "display: block") {
		t.Fatal("DocLayout must collapse to display:block on mobile")
	}
}
