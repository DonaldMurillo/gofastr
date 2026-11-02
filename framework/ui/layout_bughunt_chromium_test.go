//go:build chromium

package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// layoutHuntCSS is the sheets the bug-bash layout probes render under.
func layoutHuntCSS() string {
	th := theme.Default()
	var b strings.Builder
	for _, s := range []string{
		layoutStyle.Entry().CSSFor(th), stickyStyle.Entry().CSSFor(th),
		aspectRatioStyle.Entry().CSSFor(th), formFrameStyle.Entry().CSSFor(th),
		contentRowStyle.Entry().CSSFor(th), containerStyle.Entry().CSSFor(th),
		sidebarStyle.Entry().CSSFor(th),
	} {
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// B2: a bottom-edge Sticky with an Offset gets the offset as `top`
// (every offset rule sets top), so it is pinned to BOTH edges — it
// sticks to the top of the viewport once scrolled past — and its
// bottom distance stays 0, never the offset asked for.
func TestLayoutStickyBottomOffset(t *testing.T) {
	page := render.HTML(`<div style="height:1500px"></div>`) +
		Sticky(StickyConfig{Edge: StickyBottom, Offset: StickyOffsetLg, ID: "bar"}, render.HTML("<p>toolbar</p>")) +
		render.HTML(`<div style="height:1500px"></div>`)
	m := geometryOf(t, layoutHuntCSS(), page, `(() => {
		const s = getComputedStyle(document.getElementById('bar'));
		return {top: s.top === 'auto' ? -1 : parseFloat(s.top), bottom: s.bottom === 'auto' ? -1 : parseFloat(s.bottom)};
	})()`)
	if m["top"] != -1 || m["bottom"] != 16 {
		t.Errorf("bottom Sticky with Offset lg: top=%v bottom=%v, want top auto (-1) and bottom 16px", m["top"], m["bottom"])
	}
}

// B3: a FormFrame with only Side content arms the two-column grid, so
// on a 900px frame the lone side column takes the 1fr track (900 - 16rem
// - gap) instead of the full width.
func TestLayoutFormFrameSideOnlyFullWidth(t *testing.T) {
	page := FormFrame(FormFrameConfig{ID: "ff", Side: []render.HTML{render.HTML(`<p>status</p>`)}})
	m := geometryOf(t, layoutHuntCSS(), page, `(() => ({
		frame: document.getElementById('ff').getBoundingClientRect().width,
		side: document.querySelector('.fui-form-frame__side').getBoundingClientRect().width}))()`)
	if m["side"] != m["frame"] {
		t.Errorf("side-only FormFrame: side column %vpx of a %vpx frame, want the full width", m["side"], m["frame"])
	}
}

// B9: inside a has-nav ContentRow, the app-frame padding rule is a
// DESCENDANT selector (.fui-content-row--has-nav .layout-content), so a
// nested layout layer's .layout-content cell inside <main> is padded a
// second time (and made a flex column with a 24px gap). The viewport
// arm zeroes `.layout-content .layout-content` inside a workspace, so
// a nested cell is meant to carry no padding of its own.
func TestLayoutContentRowNestedLayerNotPaddedTwice(t *testing.T) {
	page := ContentRow(ContentRowConfig{Sidebar: render.HTML(`<p>nav</p>`)},
		render.HTML(`<main id="outer"><div class="layout-content" id="inner"><p>x</p></div></main>`))
	m := geometryOf(t, layoutHuntCSS(), page, `(() => ({
		outer: parseFloat(getComputedStyle(document.getElementById('outer')).paddingTop),
		inner: parseFloat(getComputedStyle(document.getElementById('inner')).paddingTop)}))()`)
	if m["outer"] == 0 {
		t.Fatalf("probe: main got no frame padding: %v", m)
	}
	if m["inner"] != 0 {
		t.Errorf("nested layer cell padded again: main %vpx, inner .layout-content %vpx (want 0)", m["outer"], m["inner"])
	}
}

// B10: the sidebar's two direction-bearing drawings have no :dir(rtl)
// mirror. The Compact sub-list's guide line is `box-shadow: inset 1px 0`
// (always the LEFT edge) while the list indents with
// margin-inline-start and the current marker is border-inline-start:
// in RTL the guide sits on the inline END. And a closed SectionLabels
// group's chevron rotates -90deg, pointing right (inline end in LTR,
// inline START in RTL). The kit mirrors the same thing elsewhere
// (menu.go's submenu arrow) and GOFASTR1824 is the reading-direction
// contract.
func TestLayoutSidebarRTLMirrors(t *testing.T) {
	compact := Sidebar(SidebarConfig{Compact: true, CurrentPath: "/a/1", Items: []SidebarItem{
		{Label: "Group", Children: []SidebarItem{{Label: "One", Href: "/a/1"}}},
	}}).Render()
	labels := Sidebar(SidebarConfig{SectionLabels: true, DrawerName: "sl", Items: []SidebarItem{
		{Label: "Billing", Children: []SidebarItem{{Label: "Invoices", Href: "/inv"}}},
	}}).Render()
	page := render.HTML(`<div dir="rtl">`) + compact + labels + render.HTML(`</div>`)
	// The guide line's colour is a theme token; without it the
	// shadow computes to none on both sides and the probe is vacuous.
	m := geometryOf(t, ":root{--color-border:#ccc}\n"+layoutHuntCSS(), page, `(() => {
		const sub = document.querySelector('.fui-sidebar--compact .fui-sidebar__sublist');
		const r = sub.getBoundingClientRect();
		const cs = getComputedStyle(sub);
		// Guide on the inline-start (right) edge in RTL: a negative x
		// inset shadow, or a right border.
		const guideRight = /-1px 0px/.test(cs.boxShadow) || parseFloat(cs.borderRightWidth) > 0;
		const sum = document.querySelector('.fui-sidebar--section-labels .fui-sidebar__group:not([open]) > summary');
		const rot = getComputedStyle(sum, '::after').rotate;
		return {guideRight: guideRight ? 1 : 0, chevronMinus90: rot === '-90deg' ? 1 : 0, w: r.width};
	})()`)
	if m["guideRight"] != 1 {
		t.Errorf("RTL compact sub-list draws its guide line on the left (inline end)")
	}
	if m["chevronMinus90"] == 1 {
		t.Errorf("RTL closed section-label chevron rotates -90deg, pointing to the inline start")
	}
}

// B11: the Sticky sheet says its bottom hairline shows "when the element
// is stuck", but the rule is `.fui-sticky--top:not(:first-child)::after
// { opacity: 1 }`: any top Sticky that is not its parent's first child
// draws the line at rest, unscrolled, mid-page.
func TestLayoutStickyHairlineOnlyWhenStuck(t *testing.T) {
	page := render.HTML(`<div><p>intro</p>`) +
		Sticky(StickyConfig{ID: "st"}, render.HTML("<p>bar</p>")) +
		render.HTML(`<div style="height:3000px"></div></div>`)
	m := geometryOf(t, layoutHuntCSS(), page, `(() => ({
		scrolled: window.scrollY,
		op: parseFloat(getComputedStyle(document.getElementById('st'), '::after').opacity)}))()`)
	if m["scrolled"] == 0 && m["op"] != 0 {
		t.Errorf("unstuck top Sticky draws its stuck hairline at rest (opacity %v)", m["op"])
	}
}

// B12: Start drops the container's gutter (padding-inline: 0) but the
// page measure's cap still adds two gutters (calc(page-width + 2 *
// --ui-layout-gutter)), so a Start page column's content box is 64px
// wider than the 66rem page measure the header and footer bands use.
// The has-nav rule zeroes --ui-layout-gutter for exactly this reason;
// Start does not.
func TestLayoutContainerStartPageKeepsMeasure(t *testing.T) {
	page := render.HTML(`<div style="width:1600px">`) +
		Container(ContainerConfig{Width: ContainerPage, Start: true, ID: "c"}, render.HTML("<p>x</p>")) +
		render.HTML(`</div>`)
	m := geometryOf(t, layoutHuntCSS(), page, `(() => {
		const c = document.getElementById('c'), s = getComputedStyle(c);
		return {content: c.getBoundingClientRect().width - parseFloat(s.paddingLeft) - parseFloat(s.paddingRight),
			rem: parseFloat(getComputedStyle(document.documentElement).fontSize)};
	})()`)
	if want := 66 * m["rem"]; m["content"] != want {
		t.Errorf("Start page container content box %vpx, want the %vpx page measure", m["content"], want)
	}
}
