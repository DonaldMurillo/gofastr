package ui_test

// Menu and dropdown panels on the top layer (#706): where the browser has
// anchor positioning, the kit's sheet anchors a popup's panel to its
// trigger and headless-disclosure opens it as a manual popover, so a
// clipping box cannot cut it and the browser flips it when its side has
// no room. Elsewhere today's measured shift and float place it.

import (
	"context"
	"math"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func popupItems() []ui.MenuItem {
	return []ui.MenuItem{{Label: "Profile", Href: "/me"}, {Label: "Settings", Href: "/settings"}, {Label: "Sign out", Href: "/out"}}
}

// popupPage serves a dropdown inside a box that clips it, a menu at the
// bottom of the viewport, an end-aligned menu with room on both sides
// and a text colour of its own, a menu
// on a caller's own trigger and a bare headless disclosure that asks for
// light dismiss.
func popupPage(t *testing.T, anchorPositioning bool) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	dd := ui.Dropdown(ui.DropdownConfig{ID: "dd", Label: "Filters",
		Content: render.HTML(`<div style="height: 160px">Filter fields</div>`)})
	body := `<div id="clip" style="overflow: hidden; height: 60px; width: 300px; margin: 20px">` + string(dd) + `</div>` +
		`<div style="position: fixed; bottom: 10px; left: 300px">` + string(ui.Menu(ui.MenuConfig{ID: "mb", Label: "Bottom", Items: popupItems()})) + `</div>` +
		`<div style="position: fixed; top: 10px; left: 400px; color: rgb(160, 30, 40)">` + string(ui.Menu(ui.MenuConfig{ID: "me", Label: "End", Position: ui.MenuBottomEnd, Items: popupItems()})) + `</div>` +
		`<div style="position: fixed; top: 10px; left: 600px">` + string(ui.Dropdown(ui.DropdownConfig{ID: "de", Label: "Columns", Align: ui.DropdownEnd,
		Content: render.HTML(`<div style="width: 220px">Column toggles</div>`)})) + `</div>` +
		`<div style="position: fixed; top: 200px; left: 500px">` + string(ui.Menu(ui.MenuConfig{ID: "mt",
		TriggerElement: render.HTML(`<button type="button" id="mtb">Open</button>`), Items: popupItems()})) + `</div>` +
		`<div style="position: fixed; top: 300px; left: 20px">` + string(headless.Disclosure(headless.DisclosureProps{ID: "bare", Dismiss: true,
		Summary: render.Text("Bare"), Content: render.HTML(`<p>Bare panel</p>`)}, nil)) + `</div>` +
		`<div style="position: fixed; top: 400px; left: 600px">` + string(ui.Menu(ui.MenuConfig{ID: "mu", Label: "Up", Position: ui.MenuTopEnd, Items: popupItems()})) + `</div>` +
		`<p id="outside" style="position: fixed; top: 400px; left: 400px">Outside</p>`
	srv := themeTestPageWithHead(t, kitHead(t, anchorPositioning), body)
	ctx := moduleTestCtxURL(t, srv.URL, emulateFocus(), chromedp.EmulateViewport(900, 600))
	if !pollJS(ctx, moduleLoaded("headless-disclosure")) || !pollJS(ctx, moduleLoaded("headless-menu")) {
		t.Fatal("headless-disclosure or headless-menu did not load")
	}
	return ctx
}

type box struct{ Top, Bottom, Left, Right float64 }

func boxOf(t *testing.T, ctx context.Context, sel string) box {
	t.Helper()
	var b box
	evalJS(t, ctx, `(() => { const r = document.querySelector('`+sel+`').getBoundingClientRect(); return {Top: r.top, Bottom: r.bottom, Left: r.left, Right: r.right}; })()`, &b)
	return b
}

func near(a, b float64) bool { return math.Abs(a-b) <= 2 }

func lifted(t *testing.T, ctx context.Context, sel string) bool {
	t.Helper()
	var v bool
	evalJS(t, ctx, `document.querySelector('`+sel+`').matches('[popover]:popover-open')`, &v)
	return v
}

// A dropdown in a box that clips it opens in the top layer under its
// trigger, whole, and leaves the top layer when it closes.
func TestDropdownPanelEscapesAClippingBox(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `#dd > .fui-dropmenu__panel`
	evalJS(t, ctx, `document.querySelector('#dd > summary').click()`, nil)
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("the open dropdown's panel is not an open popover")
	}
	settle()
	p, trig, clip := boxOf(t, ctx, panel), boxOf(t, ctx, `#dd > summary`), boxOf(t, ctx, `#clip`)
	if !near(p.Top, trig.Bottom+4) || !near(p.Left, trig.Left) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want it 4px under the trigger at (%v, %v)", p.Left, p.Top, p.Right, p.Bottom, trig.Left, trig.Bottom)
	}
	if p.Bottom <= clip.Bottom {
		t.Fatalf("the panel ends at %v inside the clipping box (bottom %v); the test needs one that overhangs it", p.Bottom, clip.Bottom)
	}
	var hit bool
	evalJS(t, ctx, `!!document.elementFromPoint(`+strconv.FormatFloat((p.Left+p.Right)/2, 'f', 1, 64)+`, `+strconv.FormatFloat((clip.Bottom+p.Bottom)/2, 'f', 1, 64)+`)?.closest('`+panel+`')`, &hit)
	if !hit {
		t.Fatal("the part of the panel past the clipping box is not drawn: the box still clips it")
	}
	evalJS(t, ctx, `document.getElementById('outside').dispatchEvent(new PointerEvent('pointerdown', {bubbles: true})); document.getElementById('outside').click()`, nil)
	if !pollJS(ctx, `!document.getElementById('dd').open && !document.querySelector('`+panel+`').hasAttribute('popover')`) {
		t.Fatal("a press outside did not close the dropdown and take its panel out of the top layer")
	}
}

// A menu at the bottom of the viewport opens above its trigger, the
// keyboard lands inside the raised panel, and Escape closes it with
// focus back on the trigger.
func TestMenuPanelFlipsAboveNearTheBottom(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `[data-hui-menu="mb"] > .fui-menu__panel`
	evalJS(t, ctx, `document.querySelector('[data-hui-menu="mb"] > summary').focus()`, nil)
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\r")); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open') && document.querySelector('`+panel+`').contains(document.activeElement)`) {
		t.Fatal("Enter on the trigger did not open the menu in the top layer with focus on a row")
	}
	settle()
	p, trig := boxOf(t, ctx, panel), boxOf(t, ctx, `[data-hui-menu="mb"] > summary`)
	if !near(p.Bottom, trig.Top-4) || !near(p.Left, trig.Left) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want it flipped 4px above the trigger at (%v, %v)", p.Left, p.Top, p.Right, p.Bottom, trig.Left, trig.Top)
	}
	pressEscape(t, ctx)
	if !pollJS(ctx, `!document.querySelector('[data-hui-menu="mb"]').open && document.activeElement === document.querySelector('[data-hui-menu="mb"] > summary') && !document.querySelector('`+panel+`').hasAttribute('popover')`) {
		t.Fatal("Escape did not close the raised menu with focus back on its trigger")
	}
}

// An end-aligned menu lines its panel up with the trigger's end edge.
func TestMenuEndPanelLinesUpWithTheTriggerEnd(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `[data-hui-menu="me"] > .fui-menu__panel`
	evalJS(t, ctx, `document.querySelector('[data-hui-menu="me"] > summary').click()`, nil)
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("the end-aligned menu did not open in the top layer")
	}
	settle()
	p, trig := boxOf(t, ctx, panel), boxOf(t, ctx, `[data-hui-menu="me"] > summary`)
	if !near(p.Right, trig.Right) || !near(p.Top, trig.Bottom+4) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want its end at the trigger's (%v) 4px below it (%v)", p.Left, p.Top, p.Right, p.Bottom, trig.Right, trig.Bottom)
	}
	var colours []string
	evalJS(t, ctx, `[getComputedStyle(document.querySelector('`+panel+`')).color, getComputedStyle(document.querySelector('[data-hui-menu="me"]')).color]`, &colours)
	if colours[0] != colours[1] {
		t.Fatalf("the raised panel's text is %s, want the menu's %s: the popover default must not repaint it", colours[0], colours[1])
	}
}

// A top-end menu with room everywhere opens above its trigger, its end
// at the trigger's.
func TestMenuTopEndPanelOpensAboveTheTriggerEnd(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `[data-hui-menu="mu"] > .fui-menu__panel`
	evalJS(t, ctx, `document.querySelector('[data-hui-menu="mu"] > summary').click()`, nil)
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("the top-end menu did not open in the top layer")
	}
	settle()
	p, trig := boxOf(t, ctx, panel), boxOf(t, ctx, `[data-hui-menu="mu"] > summary`)
	if !near(p.Right, trig.Right) || !near(p.Bottom, trig.Top-4) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want it 4px above the trigger (%v) with its end at the trigger's (%v)", p.Left, p.Top, p.Right, p.Bottom, trig.Top, trig.Right)
	}
}

// An end-aligned dropdown lines its panel up with the trigger's end
// edge.
func TestDropdownEndPanelLinesUpWithTheTriggerEnd(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `#de > .fui-dropmenu__panel`
	evalJS(t, ctx, `document.querySelector('#de > summary').click()`, nil)
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("the end-aligned dropdown did not open in the top layer")
	}
	settle()
	p, trig := boxOf(t, ctx, panel), boxOf(t, ctx, `#de > summary`)
	if !near(p.Right, trig.Right) || !near(p.Top, trig.Bottom+4) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want its end at the trigger's (%v) 4px below it (%v)", p.Left, p.Top, p.Right, p.Bottom, trig.Right, trig.Bottom)
	}
}

// A menu on a caller's own trigger hangs its raised panel from that
// element.
func TestCallerTriggerMenuPanelHangsFromTheElement(t *testing.T) {
	ctx := popupPage(t, true)
	const panel = `details[data-hui-menu="mt"] > .fui-menu__panel`
	evalJS(t, ctx, `document.getElementById('mtb').click()`, nil)
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("the caller-trigger menu did not open in the top layer")
	}
	settle()
	p, trig := boxOf(t, ctx, panel), boxOf(t, ctx, `#mtb`)
	if !near(p.Top, trig.Bottom+4) || !near(p.Left, trig.Left) {
		t.Fatalf("the panel spans (%v, %v)–(%v, %v), want it 4px under the caller's button at (%v, %v)", p.Left, p.Top, p.Right, p.Bottom, trig.Left, trig.Bottom)
	}
}

// Without anchor positioning the panel is not raised: the measured
// float takes it out of the clipping box as before.
func TestPopupPanelFloatsWithoutAnchorPositioning(t *testing.T) {
	ctx := popupPage(t, false)
	const panel = `#dd > .fui-dropmenu__panel`
	evalJS(t, ctx, `document.querySelector('#dd > summary').click()`, nil)
	if !pollJS(ctx, `document.getElementById('dd').open && document.querySelector('`+panel+`').style.position === 'fixed'`) {
		t.Fatal("without anchor positioning the clipped panel did not float")
	}
	if lifted(t, ctx, panel) {
		t.Fatal("without anchor positioning the panel was raised into the top layer, where nothing places it")
	}
}

// A headless disclosure the kit does not style stays in its flow: its
// host's own CSS places it.
func TestBareDismissDisclosureStaysInPlace(t *testing.T) {
	ctx := popupPage(t, true)
	evalJS(t, ctx, `document.querySelector('#bare > summary').click()`, nil)
	if !pollJS(ctx, `document.getElementById('bare').open`) {
		t.Fatal("the bare disclosure did not open")
	}
	settle()
	var has bool
	evalJS(t, ctx, `document.querySelector('#bare > :not(summary)').hasAttribute('popover')`, &has)
	if has {
		t.Fatal("a disclosure the kit does not anchor was made a popover")
	}
}

// A dropdown rendered open is raised when the module arrives, with no
// toggle for it to hear, and stays raised when the module scans again.
func TestDropdownRenderedOpenIsRaisedOnArrival(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	dd := ui.Dropdown(ui.DropdownConfig{ID: "dd", Label: "Filters", Open: true,
		Content: render.HTML(`<div style="height: 160px">Filter fields</div>`)})
	srv := themeTestPageWithHead(t, kitHead(t, true), `<div style="overflow: hidden; height: 60px; width: 300px; margin: 20px">`+string(dd)+`</div>`)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(900, 600))
	if !pollJS(ctx, moduleLoaded("headless-disclosure")) {
		t.Fatal("headless-disclosure did not load")
	}
	const panel = `#dd > .fui-dropmenu__panel`
	if !pollJS(ctx, `document.querySelector('`+panel+`').matches(':popover-open')`) {
		t.Fatal("a dropdown rendered open was not raised when the module arrived")
	}
	evalJS(t, ctx, `window.__gofastr._moduleScanners['headless-disclosure'](document)`, nil)
	settle()
	if !lifted(t, ctx, panel) {
		t.Fatal("a second scan took the open panel out of the top layer")
	}
}
