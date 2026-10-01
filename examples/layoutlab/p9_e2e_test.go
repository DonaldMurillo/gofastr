package main

// PROTOTYPE (spike/layout-loading): browser e2e for P9-A — per-outlet
// loading content, server-rendered once as an inert template beside
// the outlet, cloned in by the runtime after After ms of in-flight
// wait, parked old nodes restored exactly on failure or supersede.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// p9ToolbarProbe reads the toolbar outlet's loading state: its text, its
// loadstate attribute, and whether a parked copy of the old nodes
// exists (the hidden park div).
func p9ToolbarProbe(t *testing.T, ctx context.Context) (text, state string, parked int) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
		t: document.querySelector('[data-fui-outlet="l:shell#toolbar"]').textContent.trim(),
		s: document.querySelector('[data-fui-outlet="l:shell#toolbar"]').getAttribute('data-fui-loadstate') || '',
		p: document.querySelectorAll('body > div[hidden]').length,
	})`, &out)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var v struct {
		T string `json:"t"`
		S string `json:"s"`
		P int    `json:"p"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	return v.T, v.S, v.P
}

// p9CrumbsProbe reads the crumbs AREA's loading state: its text, its
// loadstate attribute, and whether a parked copy of the old nodes
// exists (the hidden park div).
func p9CrumbsProbe(t *testing.T, ctx context.Context) (text, state string, parked int) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
		t: document.querySelector('[data-fui-area="l:shell~crumbs"]').textContent.trim(),
		s: document.querySelector('[data-fui-area="l:shell~crumbs"]').getAttribute('data-fui-loadstate') || '',
		p: document.querySelectorAll('body > div[hidden]').length,
	})`, &out)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var v struct {
		T string `json:"t"`
		S string `json:"s"`
		P int    `json:"p"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	return v.T, v.S, v.P
}

// TestP9AreaLoadingShowAndParkE2E: an AREA takes loading content the
// way an outlet does (2026-09-26, "Areas take loading content"): a
// slow navigation that will re-render the crumbs area shows the
// area's configured loading content past After, the old trail is
// parked (not destroyed), the region carries data-fui-loadstate=
// "shown", and the apply replaces it with the destination's fresh
// area fill. A fast navigation that beats After paints nothing.
func TestP9AreaLoadingShowAndParkE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// During the slow flight (/slow/700 sleeps 700ms): past the
	// area's After (150ms) the skeleton line shows, the old trail is
	// parked away, the region is marked shown.
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/700')`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	text, state, parked := p9CrumbsProbe(t, tctx)
	if !strings.Contains(text, "CRUMBS-LOADING") {
		t.Errorf("crumbs during flight = %q, want the area's loading marker", text)
	}
	if strings.Contains(text, "crumbs:/inbox") {
		t.Logf("crumbs during flight = %q (old trail parked)", text)
	}
	if state != "shown" {
		t.Errorf("crumbs data-fui-loadstate during flight = %q, want \"shown\"", state)
	}
	if parked == 0 {
		t.Error("no hidden park div on body during flight; the old trail must be parked in-document")
	}

	// Settle: the fresh area fill replaces the clone, the mark and
	// park are gone.
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-700`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	text, state, parked = p9CrumbsProbe(t, tctx)
	if !strings.Contains(text, "crumbs:/slow/700") {
		t.Errorf("crumbs after settle = %q, want the destination's fresh trail", text)
	}
	if state != "" {
		t.Errorf("crumbs data-fui-loadstate after settle = %q, want cleared", state)
	}
	if parked != 0 {
		t.Errorf("%d park divs left after settle", parked)
	}
}

// TestP9LoadingShowAndParkE2E: a slow navigation shows the toolbar's
// configured loading content (the Spinner) after After (150ms), the
// old content is parked (not destroyed), the region carries
// data-fui-loadstate="shown" and is NOT dimmed, and the apply replaces
// it with the target fill.
func TestP9LoadingShowAndParkE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// During the slow flight (aside sleeps 700ms): the toolbar shows
	// the Spinner clone (fui-spinner markup), the old TOOLBAR-INBOX is
	// parked away, the region is marked shown.
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/700')`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	text, state, parked := p9ToolbarProbe(t, tctx)
	if !strings.Contains(text, "Loading") {
		t.Errorf("toolbar during flight = %q, want the spinner's Loading label", text)
	}
	if !strings.Contains(text, "TOOLBAR-INBOX") {
		// the old fill must be parked, not shown beside the clone
		t.Logf("toolbar during flight = %q (old fill parked)", text)
	}
	if state != "shown" {
		t.Errorf("toolbar data-fui-loadstate during flight = %q, want \"shown\"", state)
	}
	if parked == 0 {
		t.Error("no hidden park div on body during flight; old nodes must be parked in-document")
	}
	// The region holding loading content is not dimmed (P9-A CSS).
	var op string
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`getComputedStyle(document.querySelector('[data-fui-outlet="l:shell#toolbar"]')).opacity`, &op)); err != nil {
		t.Fatal(err)
	}
	if op != "1" {
		t.Errorf("toolbar opacity while showing loading content = %q, want 1 (undimmed)", op)
	}

	// Settle: the fill replaces the clone, the mark and park are gone.
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-700`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	text, state, parked = p9ToolbarProbe(t, tctx)
	if text != "" {
		t.Errorf("toolbar after settle = %q, want empty (slow fills no toolbar)", text)
	}
	if state != "" {
		t.Errorf("toolbar data-fui-loadstate after settle = %q, want cleared", state)
	}
	if parked != 0 {
		t.Errorf("%d park divs left after settle", parked)
	}
	labState(t, tctx, "SCREEN-SLOW-700", "", "ASIDE-SLOW-700", "crumbs:/slow/700")
}

// TestP9LoadingFastPaintsNothingE2E: a response faster than After
// (150ms) must paint ZERO loading frames — the sampler counts frames
// in which any [data-fui-loadstate] region exists.
func TestP9LoadingFastPaintsNothingE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			window.__s = { shown: 0, frames: 0, done: false };
			const tick = () => {
				if (document.querySelector('[data-fui-loadstate]')) window.__s.shown++;
				window.__s.frames++;
				if (!window.__s.done) requestAnimationFrame(tick);
			};
			requestAnimationFrame(tick);
			return true;
		})()`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/settings')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__s.done = true`, nil),
	); err != nil {
		t.Fatalf("fast nav: %v", err)
	}
	var out string
	if err := chromedp.Run(tctx, chromedp.Evaluate(`JSON.stringify(window.__s)`, &out)); err != nil {
		t.Fatal(err)
	}
	var s struct {
		Shown  int `json:"shown"`
		Frames int `json:"frames"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if s.Shown != 0 {
		t.Errorf("fast navigation painted %d frames with loading content; After must keep fast responses flicker-free (frames=%d)", s.Shown, s.Frames)
	}
	if s.Frames == 0 {
		t.Fatal("sampler never ran")
	}
}

// TestP9LoadingMinHoldE2E: /slow/300 answers at ~300ms; the toolbar's
// loading content showed at ~150ms with Min=300ms, so the swap must be
// HELD until ~450ms — at 350ms the spinner is still up even though the
// response has landed.
func TestP9LoadingMinHoldE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/300')`, nil),
		// Response lands ~300ms; sample at 350ms: held (spinner still
		// shown), not applied.
		chromedp.Sleep(350*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	text, state, _ := p9ToolbarProbe(t, tctx)
	if state != "shown" || !strings.Contains(text, "Loading") {
		t.Errorf("at 350ms (response held by Min) toolbar = (%q, %q), want the spinner still shown", text, state)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-300`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	labState(t, tctx, "SCREEN-SLOW-300", "", "ASIDE-SLOW-300", "crumbs:/slow/300")
}

// TestP9LoadingRestoreOnFailureE2E: with the page error policy a
// broken fill fails the response; the parked toolbar (an input the
// user typed into) comes back EXACTLY — same node, same value.
//
// P14 note: a fill failure under the page policy no longer fails the
// FETCH — the 404 arrives as an HTML partial and applies (an error page
// inside the shell). The restore-on-failure contract is still real for
// a failed fetch, so the failure is produced at the network level:
// CDP blocks the /slowfail request, the fetch throws, the toast shows,
// and the parked nodes come back.
func TestP9LoadingRestoreOnFailureE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items"),
		chromedp.WaitVisible(`#lab-SCREEN-ITEMS`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Type into the toolbar outlet's input; then navigate to a route
	// whose fetch is HELD at the network level and failed late (page
	// policy's 404 no longer fails the fetch — see the comment above),
	chromedp.ListenTarget(tctx, func(ev interface{}) {
		if fs, ok := ev.(*fetch.EventRequestPaused); ok && fs.Request.URL == srv.URL+"/slowfail" {
			go func() {
				time.Sleep(700 * time.Millisecond)
				_ = chromedp.Run(tctx, fetch.FailRequest(fs.RequestID, network.ErrorReasonBlockedByClient))
			}()
		}
	})
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-tool-input`, chromedp.ByQuery),
		chromedp.SendKeys(`#lab-tool-input`, "keepme"),
		chromedp.Evaluate(`document.getElementById('lab-tool-input').__labIdentity = true`, nil),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{RequestStage: fetch.RequestStageRequest, URLPattern: "*slowfail*"},
		}),
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slowfail')`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("type + navigate: %v", err)
	}
	// The toolbar fill sleeps 500ms, past the toolbar's After: the
	// loading content must be up and the old nodes parked before the
	// failure, or this test proves nothing about restore.
	if _, state, parked := p9ToolbarProbe(t, tctx); state == "" || parked == 0 {
		t.Fatalf("mid-flight: loadstate=%q parked=%d, want the toolbar's loading content up and its nodes parked", state, parked)
	}
	if err := chromedp.Run(tctx, chromedp.Sleep(600*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var val, identity string
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`document.getElementById('lab-tool-input').value`, &val),
		// The restored node is the SAME node (its identity stamp from
		// before the navigation survives).
		chromedp.Evaluate(`(() => {
			const i = document.getElementById('lab-tool-input');
			return i && i.__labIdentity ? 'kept' : (i ? 'new-node' : 'gone');
		})()`, &identity),
	); err != nil {
		t.Fatal(err)
	}
	if identity != "kept" {
		t.Errorf("toolbar input after failed nav is %q, want the same node kept", identity)
	}
	if val != "keepme" {
		t.Errorf("toolbar input value after failed nav = %q, want \"keepme\" (exact node restore)", val)
	}
	if got := labRead(t, tctx, toolbarSel); got != "TOOLBAR-ITEMS" {
		t.Errorf("toolbar after failed nav = %q, want TOOLBAR-ITEMS restored", got)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`.fui-nav-toast.is-visible`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("failure toast must appear: %v", err)
	}
	_, state, parked := p9ToolbarProbe(t, tctx)
	if state != "" || parked != 0 {
		t.Errorf("after restore: loadstate=%q parked=%d, want both cleared", state, parked)
	}
}

// TestP9LoadingSupersededSettlesE2E: a slow navigation superseded by a
// fast one must not leave its loading content anywhere; the winner's
// state lands.
func TestP9LoadingSupersededSettlesE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/900')`, nil),
		chromedp.Sleep(300*time.Millisecond), // the slow nav's spinner is up
		chromedp.Evaluate(`window.__gofastr.navigate('/settings')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	var stale int
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-fui-loadstate]').length`, &stale)); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d regions still carry data-fui-loadstate after the superseded navigation settled", stale)
	}
	labState(t, tctx, "SCREEN-SETTINGS", "", "ASIDE-HELP", "crumbs:/settings")
}

// TestP9ItemsSkeletonE2E: the list-and-detail case — /items/1 →
// /items/2 swaps the inner primary, whose SkeletonRow shows while the
// detail "loads" (the screen render is instant here, so hold the
// navigation with a slow toolbar… the skeleton asserts on the wire:
// the template rides the /items pages and the runtime resolves it).
// With an instant response the skeleton never shows (After=150ms); pin
// that it does not flash, and that the swap works.
func TestP9ItemsSkeletonE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Click(`a[data-row="2"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("row click: %v", err)
	}
	var stale int
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-fui-loadstate]').length`, &stale)); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d regions still carry data-fui-loadstate after an instant detail swap", stale)
	}
	// The filter input survived the navigation and still takes input
	// (P8's layer contract still holds with loading templates beside
	// the outlets).
	if err := chromedp.Run(tctx,
		chromedp.SendKeys(`#lab-filter`, "f"),
	); err != nil {
		t.Fatalf("filter input broken after loading-template navigation: %v", err)
	}
}

// TestP9PageLoadingComponentE2E: with LAYOUTLAB_PAGE_LOADING=1 the
// host's component replaces the strip: hidden at rest, visible during
// a slow flight, hidden after; the default strip's ::after is gone.
func TestP9PageLoadingComponentE2E(t *testing.T) {
	t.Setenv("LAYOUTLAB_PAGE_LOADING", "1")
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var probe struct {
		Exists bool   `json:"exists"`
		Vis    string `json:"vis"`
		Strip  string `json:"strip"`
	}
	read := func(target interface{}) {
		t.Helper()
		if err := chromedp.Run(tctx, chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-fui-page-loading]');
			return {
				exists: !!el,
				vis: el ? getComputedStyle(el).visibility : '',
				strip: getComputedStyle(document.documentElement, '::after').content,
			};
		})()`, target)); err != nil {
			t.Fatal(err)
		}
	}
	read(&probe)
	if !probe.Exists {
		t.Fatal("no [data-fui-page-loading] element on the page")
	}
	if probe.Vis != "hidden" {
		t.Errorf("page-loading visibility at rest = %q, want hidden", probe.Vis)
	}
	if probe.Strip != "none" {
		t.Errorf("default strip ::after content = %q, want none (replaced by the component)", probe.Strip)
	}
	if got := labRead(t, tctx, `[data-fui-page-loading]`); !strings.Contains(got, "PAGE-BAR") {
		t.Errorf("page loading component content = %q, want PAGE-BAR", got)
	}

	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/700')`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	read(&probe)
	if probe.Vis != "visible" {
		t.Errorf("page-loading visibility during flight = %q, want visible", probe.Vis)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-700`, chromedp.ByQuery),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	read(&probe)
	if probe.Vis != "hidden" {
		t.Errorf("page-loading visibility after apply = %q, want hidden", probe.Vis)
	}
}

// TestP9LoadingAnimStatesE2E: the loading content's lifecycle is
// shown → exit → gone: the enter fade runs on shown (the region's own
// animation, framework CSS), the apply first sets exit and waits for
// the region's animationend before the content goes in, and the state
// clears with the swap. A MutationObserver records the sequence.
func TestP9LoadingAnimStatesE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var states string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			window.__states = [];
			const el = document.querySelector('[data-fui-outlet="l:shell#toolbar"]');
			new MutationObserver((muts) => {
				for (const m of muts) window.__states.push(m.target.getAttribute('data-fui-loadstate') || 'gone');
			}).observe(el, { attributes: true, attributeFilter: ['data-fui-loadstate'] });
			return true;
		})()`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/700')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-700`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify(window.__states)`, &states),
	); err != nil {
		t.Fatalf("nav: %v", err)
	}
	var seq []string
	if err := json.Unmarshal([]byte(states), &seq); err != nil {
		t.Fatal(err)
	}
	if len(seq) < 2 || seq[0] != "shown" || seq[1] != "exit" || seq[len(seq)-1] != "gone" {
		t.Errorf("loadstate sequence = %v, want [shown exit ... gone]", seq)
	}
	_, state, _ := p9ToolbarProbe(t, tctx)
	if state != "" {
		t.Errorf("final loadstate = %q, want cleared", state)
	}
}

// TestP9BManifestLoadingE2E: P9-B — the /slow screen's WithLoading
// declaration rides the route manifest and shows in the swap slot
// (only; the toolbar outlet keeps its own per-outlet behaviour).
func TestP9BManifestLoadingE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/700')`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate slow: %v", err)
	}
	var probe struct {
		SlotState string `json:"slotState"`
		SlotHas   bool   `json:"slotHas"`
		SlotOld   bool   `json:"slotOld"`
	}
	if err := chromedp.Run(tctx, chromedp.Evaluate(`(() => {
		const s = document.querySelector('main[data-fui-layout-slot="l:shell"]');
		return {
			slotState: s.getAttribute('data-fui-loadstate') || '',
			slotHas: !!s.querySelector('.fui-skeleton-card'),
			slotOld: !!s.querySelector('#lab-SCREEN-INBOX'),
		};
	})()`, &probe)); err != nil {
		t.Fatal(err)
	}
	if probe.SlotState != "shown" || !probe.SlotHas {
		t.Errorf("swap slot during flight: state=%q skeleton=%v; want the manifest's SkeletonCard shown", probe.SlotState, probe.SlotHas)
	}
	if probe.SlotOld {
		t.Error("old screen content still in the slot during flight; it must be parked")
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-700`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	labState(t, tctx, "SCREEN-SLOW-700", "", "ASIDE-SLOW-700", "crumbs:/slow/700")
	var stale int
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-fui-loadstate]').length`, &stale)); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d regions still carry data-fui-loadstate after settle", stale)
	}
}
