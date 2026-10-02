package headless

// Browser coverage for headless-tabs: the real registered module
// loads on the strip's marker, the keyboard contract holds (one tab is
// roving-tabindex 0, ArrowLeft/ArrowRight are RTL-aware and select on
// focus, Home/End jump, Tab leaves the strip), markup that arrives
// after load is armed without double-binding, and there is exactly one
// document keydown listener installed by the module.

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

const tabsLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-tabs'])`

func tabsStrip(name string, labels ...string) render.HTML {
	tabs := make([]Tab, 0, len(labels))
	for i, l := range labels {
		tabs = append(tabs, Tab{Label: l, Panel: render.Text(l + " panel")})
		_ = i
	}
	return Tabs(TabsProps{Name: name, Tabs: tabs, StateAttrs: true}, nil)
}

// tabKey dispatches a keydown on the focused tab.
func tabKey(key string) chromedp.Action {
	return chromedp.Evaluate(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'`+key+`',bubbles:true,cancelable:true}))`, nil)
}

// The keyboard contract: roving tabindex follows selection, the arrows
// move and select, Home/End jump, and the aria-selected mirror tracks.
func TestE2E_TabsKeyboardContract(t *testing.T) {
	b := startBehaviorServer(t, string(tabsStrip("kb", "General", "Secrets", "Advanced")))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the strip marker never loaded headless-tabs")
	}
	var sel string
	readSel := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent`, &sel)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-tabs] [role="tab"][tabindex="0"]').focus()`, nil)); err != nil {
		t.Fatal(err)
	}
	// ArrowRight selects on focus: General → Secrets.
	if err := chromedp.Run(ctx, tabKey("ArrowRight")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent === 'Secrets' &&
		document.activeElement.textContent === 'Secrets'`) {
		t.Fatal("ArrowRight did not move selection and focus together")
	}
	readSel()
	if sel != "Secrets" {
		t.Fatalf("ArrowRight selected %q, want Secrets", sel)
	}
	// Home jumps to the first, End to the last.
	if err := chromedp.Run(ctx, tabKey("End")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent === 'Advanced'`) {
		t.Fatal("End did not select the last tab")
	}
	if err := chromedp.Run(ctx, tabKey("Home")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent === 'General'`) {
		t.Fatal("Home did not select the first tab")
	}
	// The roving tabindex followed: General is 0, the rest are -1.
	var tabIndexes []int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify(Array.from(document.querySelectorAll('[data-hui-tabs] [role="tab"]')).map(t => t.tabIndex))`, &sel)); err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshalInts(sel, &tabIndexes); err != nil {
		t.Fatal(err)
	}
	if len(tabIndexes) != 3 || tabIndexes[0] != 0 || tabIndexes[1] != -1 || tabIndexes[2] != -1 {
		t.Fatalf("roving tabindex = %v, want [0 -1 -1]", tabIndexes)
	}
}

// Under dir=rtl the arrows swap: ArrowLeft moves forward (selects the
// next tab), ArrowRight moves back.
func TestE2E_TabsArrowsSwapInRTL(t *testing.T) {
	b := startBehaviorServer(t, `<div dir="rtl">`+string(tabsStrip("rtl", "One", "Two"))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-tabs] [role="tab"][tabindex="0"]').focus()`, nil),
		tabKey("ArrowLeft"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent === 'Two'`) {
		t.Fatal("ArrowLeft (RTL) did not select the next tab")
	}
	if err := chromedp.Run(ctx, tabKey("ArrowRight")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tabs] [aria-selected="true"]').textContent === 'One'`) {
		t.Fatal("ArrowRight (RTL) did not select the previous tab")
	}
}

// Markup that arrives after the module is armed by the arrival pass,
// once — and the module installs exactly one document keydown listener
// (counted by instrumenting addEventListener before and after an
// insertion).
func TestE2E_TabsInsertedStripArmedOnceSingleListener(t *testing.T) {
	b := startBehaviorServer(t, string(tabsStrip("first", "A", "B")))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the module never loaded")
	}
	var listeners int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { window.__kdCount = 0; const orig = document.addEventListener.bind(document);
			document.addEventListener = function (t) { if (t === 'keydown') window.__kdCount++; return orig.apply(this, arguments); };
			const d = document.createElement('div');
			d.innerHTML = `+jsSecondStrip()+`;
			document.body.appendChild(d.firstElementChild);
			return true; })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.querySelector('[data-hui-tabs] [aria-selected="true"][data-fui-signal-set^="second"]') ||
		document.querySelectorAll('[data-hui-tabs]').length === 2`) {
		t.Fatal("the inserted strip never arrived")
	}
	// The inserted strip is armed: its tabs answer the keyboard.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-hui-tabs]')[1].querySelector('[role="tab"][tabindex="0"]').focus()`, nil),
		tabKey("ArrowRight"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `Array.from(document.querySelectorAll('[data-hui-tabs]')[1].querySelectorAll('[role="tab"]'))
		.some(t => t.textContent === 'Y' && t.getAttribute('aria-selected') === 'true')`) {
		t.Fatal("the inserted strip's tabs were never armed by the arrival pass")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__kdCount`, &listeners)); err != nil {
		t.Fatal(err)
	}
	if listeners != 0 {
		t.Fatalf("%d document keydown listeners were installed by the insertion — the module must install exactly one, at load", listeners)
	}
}

func jsSecondStrip() string {
	return "`" + string(tabsStrip("second", "X", "Y")) + "`"
}

// jsonUnmarshalInts decodes a JSON array of numbers.
func jsonUnmarshalInts(src string, out *[]int) error {
	var arr []int
	if err := json.Unmarshal([]byte(src), &arr); err != nil {
		return err
	}
	*out = arr
	return nil
}

// The vacate restore-on-show round trip (family 4's flagged gap): the
// hidden panel ships empty with its content in the stash; switching to
// it by keyboard restores the live nodes and consumes the stash;
// switching away and back reuses the same nodes — no duplicates.
func TestE2E_TabsVacateRestoreOnShowRoundTrip(t *testing.T) {
	strip := Tabs(TabsProps{Name: "vac", VacateHidden: true, StateAttrs: true, Tabs: []Tab{
		{Label: "One", Panel: render.HTML(`<p id="vac-p1">First panel body</p>`)},
		{Label: "Two", Panel: render.HTML(`<p id="vac-p2">Second panel body</p>`)},
	}}, nil)
	b := startBehaviorServer(t, string(strip))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the module never loaded")
	}
	// SSR: panel 1 (inactive) is empty, the stash is present.
	if !pollTrue(ctx, `document.getElementById('vac-panel-1') &&
		document.getElementById('vac-panel-1').children.length === 0 &&
		document.querySelector('script[data-hui-tabs-stash]') !== null`) {
		t.Fatal("the hidden panel did not ship empty with its stash present")
	}
	// Switch to tab 1 by keyboard: the panel's content is back and the
	// stash is consumed.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-tabs] [role="tab"][tabindex="0"]').focus()`, nil),
		tabKey("ArrowRight"),
		chromedp.Sleep(200*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('vac-panel-1').querySelector('#vac-p2') !== null &&
		document.querySelector('script[data-hui-tabs-stash]') === null`) {
		t.Fatal("switching to the vacated panel did not restore its content and consume the stash")
	}
	// Switch away and back: the same node (by id), exactly one — no
	// duplicates from a re-restore or a double move.
	if err := chromedp.Run(ctx,
		tabKey("ArrowLeft"),
		chromedp.Sleep(150*1e6),
		tabKey("ArrowRight"),
		chromedp.Sleep(150*1e6),
	); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('#vac-panel-1 #vac-p2').length`, &n))
	if n != 1 {
		t.Fatalf("the restored panel carries %d copies of its content after the round trip, want 1", n)
	}
}
