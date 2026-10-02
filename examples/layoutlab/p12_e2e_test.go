package main

// PROTOTYPE (spike/layout-motion, P12): browser tests for scroll
// restore on Back as the user's place on the page — the window AND
// every scrolled pane.

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// labPaneScroll reads the list pane's scrollTop.
func labPaneScroll(t *testing.T, ctx context.Context) float64 {
	t.Helper()
	var out float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(document.querySelector('nav:has(> #lab-list)') || {scrollTop: -1}).scrollTop`, &out)); err != nil {
		t.Fatalf("pane scroll: %v", err)
	}
	return out
}

// labWinScroll reads window.scrollY.
func labWinScroll(t *testing.T, ctx context.Context) float64 {
	t.Helper()
	var out float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollY`, &out)); err != nil {
		t.Fatalf("win scroll: %v", err)
	}
	return out
}

// labElTop reads an element's distance from the top of the viewport.
func labElTop(t *testing.T, ctx context.Context, id string) float64 {
	t.Helper()
	var out float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(document.getElementById('`+id+`') || {getBoundingClientRect: () => ({top: -9999})}).getBoundingClientRect().top`, &out)); err != nil {
		t.Fatalf("el top: %v", err)
	}
	return out
}

// labClickNavNoScroll clicks a header link PROGRAMMATICALLY: chromedp's
// coordinate click first scrolls the link into view, and for a header
// link that means scrolling the WINDOW back to 0 — the runtime then
// (correctly) records the last-seen position, 0, as the leave state.
// A programmatic click performs no reveal scroll, so what the user
// (test) left on screen is what gets recorded.
func labClickNavNoScroll(t *testing.T, ctx context.Context, href string) {
	t.Helper()
	labSkipVT(t, ctx)
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('a[data-nav="`+href+`"]').click(); true`, nil)); err != nil {
		t.Fatalf("click %s: %v", href, err)
	}
}

func labSample(t *testing.T, ctx context.Context) (win, pane float64) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
		w: window.scrollY,
		p: (document.querySelector('nav:has(> #lab-list)') || {scrollTop: 0}).scrollTop,
	})`, &raw)); err != nil {
		t.Fatalf("sample: %v", err)
	}
	var v struct {
		W float64 `json:"w"`
		P float64 `json:"p"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("sample json: %v", err)
	}
	return v.W, v.P
}

const labSettle = 350 * time.Millisecond

// TestScrollRestoreBackE2E pins P12-A: Back restores the WINDOW and the
// scrolled LIST PANE (a kept layer's pane re-applied from the cached
// envelope lands at scrollTop 0 and needs its position back), a
// refetched (cache-evicted) Back restores the same pixels, the pane
// survives a KEPT-layer navigation untouched, forward navigations start
// at the top, and a hash link lands on its section.
func TestScrollRestoreBackE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 180*time.Second)
	t.Cleanup(tcancel)

	// scrollRestoration stays manual (the runtime's own contract).
	var sr string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/items"),
		chromedp.WaitVisible(`#lab-SCREEN-ITEMS`, chromedp.ByQuery),
		chromedp.Evaluate(`history.scrollRestoration`, &sr),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if sr != "manual" {
		t.Errorf("history.scrollRestoration = %q, want manual (the browser's own restore fires before the swapped content exists)", sr)
	}

	// Scroll the pane, leave, come Back (cached replay).
	if err := chromedp.Run(tctx, chromedp.Evaluate(`document.querySelector('nav:has(> #lab-list)').scrollTop = 300; true`, nil), chromedp.Sleep(100*time.Millisecond)); err != nil {
		t.Fatalf("scroll pane: %v", err)
	}
	labClickNavNoScroll(t, tctx, "/settings")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if got := labPaneScroll(t, tctx); got > 0 {
		t.Fatalf("pane scrollTop after leaving /items = %v, want none (the settings page has no pane)", got)
	}
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-ITEMS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("back to items: %v", err)
	}
	if got := labPaneScroll(t, tctx); got < 290 || got > 310 {
		t.Errorf("after cached Back: pane scrollTop = %v, want ~300 (the re-applied fill lands at 0; the runtime must restore the recorded pane position)", got)
	}

	// A KEPT-layer navigation does not touch the pane at all. The row
	// click is programmatic: chromedp's coordinate click first scrolls
	// the row into view INSIDE the pane (that reveal scroll is a real
	// user action too — it just is not this assertion's subject).
	labSkipVT(t, tctx)
	if err := chromedp.Run(tctx, page.BringToFront(), chromedp.Evaluate(
		`document.querySelector('a[data-row="7"]').click(); true`, nil)); err != nil {
		t.Fatalf("click row 7: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-DETAIL-7`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("detail 7: %v", err)
	}
	if got := labPaneScroll(t, tctx); got < 290 || got > 310 {
		t.Errorf("after kept-layer nav to /items/7: pane scrollTop = %v, want ~300 (the layer keeps its DOM)", got)
	}
	// Forward click navigation: the WINDOW starts at the top.
	if got := labWinScroll(t, tctx); got > 5 {
		t.Errorf("after a forward click nav the window must start at the top, scrollY = %v", got)
	}

	// Leave for /settings and Back with the cache EVICTED: the pane
	// re-renders from a fresh fetch and must still restore.
	if err := chromedp.Run(tctx, chromedp.Evaluate(`window.scrollTo(0, 200); document.querySelector('nav:has(> #lab-list)').scrollTop = 260; true`, nil), chromedp.Sleep(100*time.Millisecond)); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	w0, _ := labSample(t, tctx)
	labClickNavNoScroll(t, tctx, "/settings")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("settings 2: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.Evaluate(`window.__gofastr.invalidate('*'); true`, nil)); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-DETAIL-7`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("back to detail 7 (refetched): %v", err)
	}
	w1, p1 := labSample(t, tctx)
	if w1 < w0-10 || w1 > w0+10 {
		t.Errorf("after refetched Back: window scrollY = %v, want ~%v", w1, w0)
	}
	if p1 < 250 || p1 > 270 {
		t.Errorf("after refetched Back: pane scrollTop = %v, want ~260", p1)
	}
}

// TestScrollRestoreWindowAnchorE2E pins the long-detail window case:
// scrolled deep into /items/30, Back returns to the same PIXEL offset
// (identical content, so pixel restore is exact). The height-changed
// case lives in TestScrollRestoreHeightChangeE2E.
func TestScrollRestoreWindowAnchorE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/items/30"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-30`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('lab-detail-p20').scrollIntoView(); true`, nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	top0 := labElTop(t, tctx, "lab-detail-p20")
	w0 := labWinScroll(t, tctx)
	if w0 < 300 {
		t.Fatalf("window did not scroll before leaving (scrollY=%v); the test is vacuous", w0)
	}
	labClickNavNoScroll(t, tctx, "/settings")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("settings: %v", err)
	}
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-DETAIL-30`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("back: %v", err)
	}
	if w1 := labWinScroll(t, tctx); w1 < w0-10 || w1 > w0+10 {
		t.Errorf("after Back: window scrollY = %v, want ~%v", w1, w0)
	}
	if top1 := labElTop(t, tctx, "lab-detail-p20"); top1 < top0-10 || top1 > top0+10 {
		t.Errorf("after Back: paragraph 20 sits at %v from the viewport top, want ~%v", top1, top0)
	}
}

// TestScrollRestoreHeightChangeE2E pins P12-A's recorded weakness on
// the height-changed case: Back into a REFETCHED /wobble whose toolbar
// fill (above the content) re-rendered with MORE lines shifts every
// offset below it, and pixel restore lands the user at the same
// absolute offset — which is a DIFFERENT part of the page. The
// paragraph that was at the top of the viewport is off by the toolbar's
// height delta. (Variant B's test asserts the fix.)
func TestScrollRestoreHeightChangeE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	wobbleOverride.Store(0)
	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/wobble"),
		chromedp.WaitVisible(`#lab-SCREEN-WOBBLE`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('lab-wobble-p10').scrollIntoView(); true`, nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var height0 float64
	if err := chromedp.Run(tctx, chromedp.Evaluate(`document.getElementById('lab-wobble-toolbar').getBoundingClientRect().height`, &height0)); err != nil {
		t.Fatalf("toolbar height: %v", err)
	}
	top0 := labElTop(t, tctx, "lab-wobble-p10")
	w0 := labWinScroll(t, tctx)
	if w0 < 300 {
		t.Fatalf("window did not scroll (scrollY=%v); vacuous", w0)
	}
	labClickNavNoScroll(t, tctx, "/settings")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.Evaluate(`window.__gofastr.invalidate('*'); true`, nil)); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	// The Back visit must grow the toolbar by two token-spaced lines:
	// growth is the case where pixel restore misplaces the anchor (a
	// shrink clamps to the new max scroll, which lands the anchor at
	// the same viewport offset by accident — recorded in the report).
	wobbleOverride.Store(2)
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-WOBBLE`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("back: %v", err)
	}
	var height1 float64
	if err := chromedp.Run(tctx, chromedp.Evaluate(`document.getElementById('lab-wobble-toolbar').getBoundingClientRect().height`, &height1)); err != nil {
		t.Fatalf("toolbar height: %v", err)
	}
	if height1 <= height0 {
		t.Fatalf("toolbar did not grow across the refetch (%v -> %v); the height-change case is vacuous", height0, height1)
	}
	w1 := labWinScroll(t, tctx)
	top1 := labElTop(t, tctx, "lab-wobble-p10")
	t.Logf("toolbar height %v -> %v; window %v -> %v; paragraph 10 viewport-top %v -> %v", height0, height1, w0, w1, top0, top1)
	// The anchored paragraph sits where it sat.
	if top1 < top0-12 || top1 > top0+12 {
		t.Errorf("anchor restore: paragraph 10 viewport-top %v -> %v; the anchor must keep its offset", top0, top1)
	}
	// The window scrolled the toolbar delta further to keep it.
	if grown := height1 - height0; w1 < w0+grown-16 || w1 > w0+grown+16 {
		t.Errorf("anchor restore: window %v -> %v; want the toolbar growth (%vpx) absorbed", w0, w1, grown)
	}
}

// TestScrollAnchorGoneFallsBackToPixelsE2E pins P12-B's fallback: when
// the refetched page no longer CONTAINS the anchor (fewer paragraphs),
// the restore falls back to the recorded pixel offset, clamped to the
// shorter page.
func TestScrollAnchorGoneFallsBackToPixelsE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	wobbleOverride.Store(0)
	wobbleParas.Store(20)
	if err := chromedp.Run(tctx,
		chromedp.Navigate(base+"/wobble"),
		chromedp.WaitVisible(`#lab-SCREEN-WOBBLE`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('lab-wobble-p10').scrollIntoView(); true`, nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	w0 := labWinScroll(t, tctx)
	if w0 < 300 {
		t.Fatalf("window did not scroll (scrollY=%v); vacuous", w0)
	}
	labClickNavNoScroll(t, tctx, "/settings")
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := chromedp.Run(tctx, chromedp.Evaluate(`window.__gofastr.invalidate('*'); true`, nil)); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	// The Back visit drops the paragraphs the anchor pointed at.
	wobbleParas.Store(5)
	labBack(t, tctx)
	if err := chromedp.Run(tctx, chromedp.WaitVisible(`#lab-SCREEN-WOBBLE`, chromedp.ByQuery), chromedp.Sleep(labSettle)); err != nil {
		t.Fatalf("back: %v", err)
	}
	var gone bool
	if err := chromedp.Run(tctx, chromedp.Evaluate(`!document.getElementById('lab-wobble-p10')`, &gone)); err != nil || !gone {
		t.Fatalf("p10 must be gone on the refetched page (err %v, gone %v)", err, gone)
	}
	w1 := labWinScroll(t, tctx)
	var maxScroll float64
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`document.documentElement.scrollHeight - innerHeight`, &maxScroll)); err != nil {
		t.Fatalf("max scroll: %v", err)
	}
	want := math.Min(w0, maxScroll)
	if w1 < want-12 || w1 > want+12 {
		t.Errorf("anchor gone: window %v, want the pixel fallback ~%v (recorded %v, max %v)", w1, want, w0, maxScroll)
	}
}

// TestScrollHashLinkE2E pins the hash case: a click navigation to
// /items/30#lab-detail-p2 lands ON the section (scrollToHash), not the
// top and not a recorded position.
func TestScrollHashLinkE2E(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := labServer(t, app.Router())
	base := srv.URL
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx, chromedp.Navigate(base+"/settings"),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery)); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	labClickNav(t, tctx, "/items/30#lab-detail-p2")
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-30`, chromedp.ByQuery),
		chromedp.Sleep(labSettle),
	); err != nil {
		t.Fatalf("detail 30: %v", err)
	}
	if top := labElTop(t, tctx, "lab-detail-p2"); top < -20 || top > 60 {
		t.Errorf("hash navigation: paragraph 2 sits at %v from the viewport top, want ~0 (the section, not the page top)", top)
	}
}
