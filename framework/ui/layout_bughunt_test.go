package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Bug bash 2026-11-01 (layout primitives). Each test asserts the
// intended behaviour and fails on the current code.

// panics reports whether f panicked.
func layoutPanics(f func()) (did bool) {
	defer func() {
		if recover() != nil {
			did = true
		}
	}()
	f()
	return false
}

// B1: the styled-div layout primitives concatenate their enum fields
// into class names with no validation, while Container, HeroSplit,
// FormFrame, Sidebar and Sticky's own ZIndexTier panic on an unknown
// value. A typo renders a class no rule matches: an AspectRatio with an
// empty or unknown Ratio keeps the absolutely-positioned child at
// height:100% of a box with no aspect-ratio, so the media collapses to
// 0px tall; a value with a space smuggles extra classes onto the root.
func TestLayoutEnumsAreValidated(t *testing.T) {
	cases := map[string]func(){
		"AspectRatio Ratio empty (documented required)": func() {
			AspectRatioComponent(AspectRatioConfig{}, render.HTML("<img alt=x src=/a.png>"))
		},
		"AspectRatio Ratio 16:9": func() {
			AspectRatioComponent(AspectRatioConfig{Ratio: "16:9"}, render.HTML("<img alt=x src=/a.png>"))
		},
		"Sticky Edge left":  func() { Sticky(StickyConfig{Edge: "left"}) },
		"Sticky Offset 2xl": func() { Sticky(StickyConfig{Offset: "2xl"}) },
		"Box Pad xs":        func() { Box(BoxConfig{Pad: "xs"}) },
		"Center MinHeight full": func() {
			Center(CenterConfig{MinHeight: "full"})
		},
	}
	for name, f := range cases {
		if !layoutPanics(f) {
			t.Errorf("%s: rendered an unstyled class silently instead of panicking", name)
		}
	}
	// Class smuggling through an enum field.
	if !layoutPanics(func() { Center(CenterConfig{MinHeight: "viewport fui-box--surface"}) }) {
		t.Errorf("Center MinHeight with a space smuggled a second class onto the root")
	}
}

// B4: a sidebar item with MatchPath "/" (the home section) is marked
// current on EVERY page by the server: pathMatches("/customers", "/")
// is true because the prefix plus "/" is "/". The runtime's sweep
// (core-ui/runtime/src/activelink.js) never uses "/" as a prefix, so
// first paint lights two entries and the sweep then clears one — the
// "two lit entries" bug class the sweep's own comment names.
func TestLayoutSidebarRootMatchPathNotEverywhere(t *testing.T) {
	cfg := SidebarConfig{CurrentPath: "/customers", Items: []SidebarItem{
		{Label: "Home", Href: "/", MatchPath: "/"},
		{Label: "Customers", Href: "/customers"},
	}}
	out := string(Sidebar(cfg).Render())
	n := strings.Count(out, `aria-current="page"`)
	if n != 1 {
		t.Errorf("on /customers %d links are current, want 1 (Customers); Home's MatchPath \"/\" matches every path:\n%s", n, out)
	}
}

// B5: a MatchPath written with a trailing slash ("/customers/")
// leaves the section ROOT dark on the server: pathMatches compares the
// current path to the raw prefix for equality, so "/customers" !=
// "/customers/", while the runtime sweep strips the slash (bare()) and
// lights it — first paint and the post-hydration sweep disagree.
func TestLayoutSidebarTrailingSlashMatchPathLightsRoot(t *testing.T) {
	cfg := SidebarConfig{CurrentPath: "/customers", Items: []SidebarItem{
		{Label: "Customers", Href: "/customers/", MatchPath: "/customers/"},
		{Label: "Orders", Href: "/orders"},
	}}
	out := string(Sidebar(cfg).Render())
	if !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("MatchPath \"/customers/\" left /customers dark on first paint:\n%s", out)
	}
}

// B6: RaisedCurrent rides only the inline sidebar's root. The body
// paths (SidebarBody, the MountSidebar drawer slot, the NativeMobile
// disclosure) carry Compact and SectionLabels but drop RaisedCurrent,
// and its rule keys on the root class, so the phone drawer of an app
// that asked for the raised pill (battery/admin) draws the grey fill.
func TestLayoutSidebarRaisedCurrentOnBodyPaths(t *testing.T) {
	cfg := SidebarConfig{RaisedCurrent: true, CurrentPath: "/a", Items: []SidebarItem{{Label: "A", Href: "/a"}}}
	if out := string(SidebarBody(cfg)); !strings.Contains(out, "fui-sidebar--raised-current") {
		t.Errorf("SidebarBody dropped RaisedCurrent:\n%s", out)
	}
	cfg.DrawerName = "ui-sidebar-drawer"
	cfg.Variant = SidebarPersistent
	if out := string(sidebarDrawerSlot{cfg: cfg}.Render()); !strings.Contains(out, "fui-sidebar--raised-current") {
		t.Errorf("drawer body dropped RaisedCurrent:\n%s", out)
	}
	cfg.NativeMobile = true
	out := string(Sidebar(cfg).Render())
	if strings.Count(out, "fui-sidebar--raised-current") < 2 {
		t.Errorf("NativeMobile disclosure body dropped RaisedCurrent:\n%s", out)
	}
}

var bbIDRe = regexp.MustCompile(` id="([^"]*)"`)

// B7: Section's auto-anchor slug keeps only ASCII a-z0-9, so a heading
// in any non-Latin script slugs to "": the root gets no id (the
// documented "auto-slugs the heading as its id" anchor is gone), and
// headless falls back to slugID's "section-" for the heading, so two
// DIFFERENT headings ("概要", "料金") both render id="section--title"
// and the second section's aria-labelledby names the first heading.
func TestLayoutSectionNonLatinHeadingIDs(t *testing.T) {
	a := string(Section(SectionConfig{Heading: "概要"}, render.HTML("<p>a</p>")))
	b := string(Section(SectionConfig{Heading: "料金"}, render.HTML("<p>b</p>")))
	if !strings.Contains(a, `<section`) || bbIDRe.FindStringSubmatch(a) == nil {
		t.Fatalf("no ids at all: %s", a)
	}
	rootID := func(s string) string {
		i := strings.Index(s, "<section")
		j := strings.Index(s[i:], ">")
		m := bbIDRe.FindStringSubmatch(s[i : i+j])
		if m == nil {
			return ""
		}
		return m[1]
	}
	if rootID(a) == "" {
		t.Errorf("a non-Latin heading gets no anchor id on its section: %s", a)
	}
	seen := map[string]bool{}
	for _, m := range bbIDRe.FindAllStringSubmatch(a, -1) {
		seen[m[1]] = true
	}
	for _, m := range bbIDRe.FindAllStringSubmatch(b, -1) {
		if seen[m[1]] {
			t.Errorf("two different headings share id %q:\n%s\n%s", m[1], a, b)
		}
	}
	// Even with an id scope the anchor is lost.
	ctx := render.WithIDScope(context.Background())
	if c := string(Section(SectionConfig{Heading: "概要", Ctx: ctx})); rootID(c) == "" {
		t.Errorf("with an id scope a non-Latin heading still gets no anchor id: %s", c)
	}
}

// B8: HeroSplitConfig.AriaLabel says it is "required unless an h1
// inside Copy provides the accessible name", but a <section> is never
// named by a heading it contains (only aria-label / aria-labelledby /
// title name it), and HeroSplit wires no aria-labelledby. A hero with
// an h1 and no AriaLabel is an unnamed section: no region landmark.
func TestLayoutHeroSplitNamedByItsHeading(t *testing.T) {
	out := string(HeroSplit(HeroSplitConfig{
		Copy:      render.HTML(`<h1 id="hero-title">Ship faster</h1>`),
		Media:     render.HTML(`<p>media</p>`),
		HeadingID: "hero-title",
	}))
	i := strings.Index(out, "<section")
	root := out[i : i+strings.Index(out[i:], ">")]
	if !strings.Contains(root, `aria-labelledby="hero-title"`) {
		t.Errorf("hero section with an h1 and no AriaLabel has no accessible name: %s", root)
	}
}

// B3 (markup half): with only Side content, the frame still arms the
// two-column modifier, so above 48rem the lone side column sits in the
// 1fr track beside an empty 16rem track instead of taking the full
// width the doc promises ("When empty the column is not rendered and
// Side takes the full width").
func TestLayoutFormFrameSideOnlyIsOneColumn(t *testing.T) {
	out := string(FormFrame(FormFrameConfig{Side: []render.HTML{render.HTML("<p>status</p>")}}))
	if strings.Contains(out, "fui-form-frame--with-side") {
		t.Errorf("a side-only frame armed the two-column grid: %s", out)
	}
}
