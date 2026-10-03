package siteheader_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/siteheader"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// testMark stands in for the app's logo: a monochrome currentColor SVG,
// the shape .mark colours with the theme's primary.
const testMark = `<svg width="24" height="24" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" fill="currentColor"/></svg>`

// full is a representative Config: brand with a mark, a section link,
// a call to action and a bar action.
func full() siteheader.Config {
	return siteheader.Config{
		Name: "Example",
		Mark: render.Raw(testMark),
		Links: []siteheader.Link{
			{Label: "Overview", Href: "/"},
			{Label: "Help", Href: "/help", Section: true},
		},
		CTA:     siteheader.Link{Label: "Get started", Href: "/pricing"},
		Actions: render.Raw(`<button type="button" data-test-action>Toggle theme</button>`),
	}
}

func TestRenderBarLandmarks(t *testing.T) {
	html := string(siteheader.Render(full()))
	for _, want := range []string{
		// The banner landmark, owned by this package's style.
		"<header", `role="banner"`, `data-fui-scope="siteheader"`,
		// The bar and its regions.
		`class="bar"`, `class="brand"`, `class="links"`, `class="end"`,
		`aria-label="Primary"`,
		// The brand link carries the name and links home.
		`href="/"`, "Example",
		// The action stays in the bar.
		`data-test-action`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered bar missing %q:\n%s", want, html)
		}
	}
}

func TestRenderMarkOptional(t *testing.T) {
	with := string(siteheader.Render(full()))
	if !strings.Contains(with, `class="mark"`) {
		t.Errorf("Mark should draw inside the brand link:\n%s", with)
	}
	// The mark precedes the wordmark inside the brand link.
	if strings.Index(with, `class="mark"`) > strings.Index(with, "Example") {
		t.Errorf("Mark should be drawn before Name:\n%s", with)
	}

	cfg := full()
	cfg.Mark = ""
	without := string(siteheader.Render(cfg))
	if strings.Contains(without, `class="mark"`) {
		t.Errorf("zero Mark should draw nothing:\n%s", without)
	}
	if !strings.Contains(without, "Example") {
		t.Errorf("the wordmark survives without a mark:\n%s", without)
	}
}

func TestRenderSectionLinkMarking(t *testing.T) {
	html := string(siteheader.Render(full()))
	// Only the Section link carries the prefix-match attribute the
	// runtime's active-link pass reads, in the bar's nav and again in
	// the phone menu's.
	i := strings.Index(html, `href="/help"`)
	if i < 0 || !strings.Contains(html[:i], "data-fui-match-prefix") {
		t.Errorf("the section link should carry data-fui-match-prefix before its href:\n%s", html)
	}
	if n := strings.Count(html, "data-fui-match-prefix"); n != 2 {
		t.Errorf("the section link should be marked in the bar and the phone menu, found %d markers", n)
	}
}

func TestRenderCTAOptional(t *testing.T) {
	with := string(siteheader.Render(full()))
	for _, want := range []string{`class="bar-cta"`, `class="panel-cta"`, "Get started"} {
		if !strings.Contains(with, want) {
			t.Errorf("the call to action should draw in the bar and the phone menu (%q missing):\n%s", want, with)
		}
	}

	cfg := full()
	cfg.CTA = siteheader.Link{}
	without := string(siteheader.Render(cfg))
	for _, banned := range []string{`class="bar-cta"`, `class="panel-cta"`, "Get started"} {
		if strings.Contains(without, banned) {
			t.Errorf("a zero CTA should draw neither button (%q present):\n%s", banned, without)
		}
	}
}

func TestRenderPhoneMenu(t *testing.T) {
	html := string(siteheader.Render(full()))
	for _, want := range []string{
		"<details", `class="menu"`, "<summary", `class="toggle"`, `class="panel"`, `class="panel-links"`,
		// The disclosure traps focus while open (headless.Disclosure).
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the phone menu missing %q:\n%s", want, html)
		}
	}

	cfg := full()
	cfg.Links = nil
	// With no links the CTA still builds the menu; with neither, none.
	cfg.CTA = siteheader.Link{}
	none := string(siteheader.Render(cfg))
	if strings.Contains(none, "<details") {
		t.Errorf("no links and no CTA should draw no phone menu:\n%s", none)
	}
}
