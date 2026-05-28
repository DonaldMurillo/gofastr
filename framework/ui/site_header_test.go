package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSiteHeaderRendersBrandPrimaryAndRight(t *testing.T) {
	h := string(SiteHeader(SiteHeaderConfig{
		Brand: render.Raw(`<a class="brand" href="/">gofastr</a>`),
		NavItems: []SiteHeaderLink{
			{Label: "Docs", Href: "/docs/", MatchPrefix: true},
			{Label: "Examples", Href: "/examples"},
		},
		Actions: render.Raw(`<button>Search</button>`),
	}))

	for _, want := range []string{
		`data-fui-comp="ui-site-header"`,
		`class="brand"`,
		`aria-label="Primary"`,
		`href="/docs/"`,
		`data-fui-match-prefix=""`,
		`href="/examples"`,
		`<button>Search</button>`,
		`data-fui-disclosure=""`,
		`aria-label="Mobile primary"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("SiteHeader missing %q\nhtml=%s", want, h)
		}
	}
}

func TestSiteHeaderDesktopNavOmitsExternalAttrs(t *testing.T) {
	// External flag should only affect the mobile drawer copy, not
	// the desktop bar — keeps "primary" links semantically internal.
	h := string(SiteHeader(SiteHeaderConfig{
		Brand: render.Raw(`<a>x</a>`),
		NavItems: []SiteHeaderLink{
			{Label: "GitHub", Href: "https://gh", External: true},
		},
	}))
	desktopBlock := h[:strings.Index(h, `data-fui-disclosure`)]
	if strings.Contains(desktopBlock, `target="_blank"`) {
		t.Errorf("desktop nav must not open external links in new tabs:\n%s", desktopBlock)
	}
}

func TestSiteHeaderEmitsBothMenuAndCloseIcons(t *testing.T) {
	h := string(SiteHeader(SiteHeaderConfig{
		Brand:    render.Raw(`<a>x</a>`),
		NavItems: []SiteHeaderLink{{Label: "Docs", Href: "/docs/"}},
	}))
	// Both SVG icons present in source so the open/close CSS swap
	// works without runtime JS.
	if !strings.Contains(h, "ui-site-header__icon--menu") {
		t.Errorf("missing menu icon (closed-state visual):\n%s", h)
	}
	if !strings.Contains(h, "ui-site-header__icon--close") {
		t.Errorf("missing close icon (open-state visual):\n%s", h)
	}
	// Both are aria-hidden (decorative) — the summary's aria-label
	// carries the AT name.
	if strings.Count(h, `aria-hidden="true"`) < 2 {
		t.Errorf("both SVG icons must be aria-hidden — let the summary aria-label own the AT name:\n%s", h)
	}
}

func TestSiteHeaderMobileDrawerHasFocusTrapOptIn(t *testing.T) {
	h := string(SiteHeader(SiteHeaderConfig{
		Brand:    render.Raw(`<a>x</a>`),
		NavItems: []SiteHeaderLink{{Label: "Docs", Href: "/docs/"}},
	}))
	if !strings.Contains(h, "data-fui-disclosure-trap") {
		t.Errorf("mobile drawer must opt into the runtime's focus trap "+
			"so Tab doesn't walk into hidden main content:\n%s", h)
	}
}

func TestSiteHeaderMatchPrefixAppliesToBothDesktopAndMobile(t *testing.T) {
	h := string(SiteHeader(SiteHeaderConfig{
		Brand:    render.Raw(`<a>x</a>`),
		NavItems: []SiteHeaderLink{{Label: "Docs", Href: "/docs/", MatchPrefix: true}},
	}))
	// data-fui-match-prefix must appear at least twice — once on the
	// desktop nav copy, once on the mobile drawer copy. Otherwise
	// active-route highlighting only works on one viewport.
	if strings.Count(h, `data-fui-match-prefix=""`) < 2 {
		t.Errorf("MatchPrefix must wire both desktop and mobile copies of the link:\n%s", h)
	}
}

func TestSiteHeaderMobileExtraLinksRenderOnlyInDrawer(t *testing.T) {
	h := string(SiteHeader(SiteHeaderConfig{
		Brand: render.Raw(`<a>x</a>`),
		NavItems: []SiteHeaderLink{{Label: "Docs", Href: "/docs/"}},
		MobileExtraLinks: []SiteHeaderLink{
			{Label: "Home", Href: "/"},
			{Label: "GitHub ↗", Href: "https://gh", External: true},
		},
	}))
	idx := strings.Index(h, `data-fui-disclosure`)
	if idx == -1 {
		t.Fatal("missing mobile drawer")
	}
	desktopBlock := h[:idx]
	mobileBlock := h[idx:]
	if strings.Contains(desktopBlock, `Home`) || strings.Contains(desktopBlock, `GitHub`) {
		t.Errorf("desktop nav leaked mobile-extra links:\n%s", desktopBlock)
	}
	for _, want := range []string{`Home`, `GitHub ↗`, `target="_blank"`} {
		if !strings.Contains(mobileBlock, want) {
			t.Errorf("mobile drawer missing %q\n%s", want, mobileBlock)
		}
	}
}
