package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// mixWatch installs observers that record when each /mix region's
// content appears, keyed by marker text. Returns a JS expression to
// read the timestamps.
func mixWatch(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__mix = { fast: 0, slow: 0 };
		const stamp = (sel, key) => {
			const el = document.querySelector(sel);
			const check = () => {
				if (el.textContent.indexOf(key === 'fast' ? 'TOOLBAR-MIX' : 'ASIDE-MIX') >= 0) {
					if (!window.__mix[key]) window.__mix[key] = performance.now();
					return;
				}
				setTimeout(check, 8);
			};
			check();
		};
		stamp('[data-fui-outlet="l:shell#toolbar"]', 'fast');
		stamp('[data-fui-outlet="l:shell#aside"]', 'slow');
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("watch: %v", err)
	}
}

func mixRead(t *testing.T, ctx context.Context) (fast, slow float64) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__mix)`, &out)); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Fast float64 `json:"fast"`
		Slow float64 `json:"slow"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	return v.Fast, v.Slow
}

// TestP10ANoStreamE2E pins variant A: fast and slow fills paint
// together (the response is atomic; the fast fill's paint time equals
// the slow one's within noise, both ≈ the 800ms fill).
func TestP10ANoStreamE2E(t *testing.T) {
	srv := labServe(t) // streaming off by default
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var t0 float64
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
		chromedp.Evaluate(`performance.now()`, &t0),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	mixWatch(t, tctx)
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/mix')`, nil),
		chromedp.WaitVisible(`#lab-ASIDE-MIX`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("nav: %v", err)
	}
	fast, slow := mixRead(t, tctx)
	if fast == 0 || slow == 0 {
		t.Fatalf("timestamps missing (fast=%v slow=%v)", fast, slow)
	}
	dFast, dSlow := fast-t0, slow-t0
	t.Logf("P10-A: fast fill painted at %.0fms, slow fill at %.0fms (both must wait for the 800ms fill)", dFast, dSlow)
	if dFast < 700 {
		t.Errorf("fast fill painted at %.0fms under no streaming; the atomic response must hold it until the 800ms fill lands", dFast)
	}
	if dSlow-dFast > 300 {
		t.Errorf("fast fill painted %.0fms before slow under no streaming; they must land together (got fast=%.0f slow=%.0f)", dSlow-dFast, dFast, dSlow)
	}
	labState(t, tctx, "SCREEN-MIX", "TOOLBAR-MIX", "ASIDE-MIX", "crumbs:/mix")
}

const (
	pAsideSel   = `[data-fui-outlet="l:pshell#aside"]`
	pRailSel    = `[data-fui-outlet="l:pshell#rail"]`
	pToolbarSel = `[data-fui-outlet="l:pshell#toolbar"]`
)

// partsWatch is mixWatch over the /parts* markers (TOOLBAR-PARTS2 is
// the fast region, ASIDE-PARTS2-SLOW the deferred one).
func partsWatch(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__mix = { fast: 0, slow: 0 };
		const stamp = (sel, key, marker) => {
			const el = document.querySelector(sel);
			const check = () => {
				if (el.textContent.indexOf(marker) >= 0) {
					if (!window.__mix[key]) window.__mix[key] = performance.now();
					return;
				}
				requestAnimationFrame(check);
			};
			check();
		};
		stamp('[data-fui-outlet="l:pshell#toolbar"]', 'fast', 'TOOLBAR-PARTS2');
		stamp('[data-fui-outlet="l:pshell#aside"]', 'slow', 'ASIDE-PARTS2-SLOW');
		return true;
	})()`, nil)); err != nil {
		t.Fatalf("arm watchers: %v", err)
	}
}

// TestP10PartsE2E pins the parts twin: on /parts the fast regions
// (primary, toolbar) paint well before the deferred aside's part
// (~800ms) lands; the page still settles complete, and Back replays
// the entry with its landed parts, no request.
func TestP10PartsE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var t0 float64
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/parts"),
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
		chromedp.Evaluate(`performance.now()`, &t0),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	partsWatch(t, tctx)
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/parts2')`, nil),
		chromedp.WaitVisible(`#lab-TOOLBAR-PARTS2`, chromedp.ByQuery),
		chromedp.Sleep(120*time.Millisecond),
	); err != nil {
		t.Fatalf("nav: %v", err)
	}
	// Mid-flight: the fast regions are in, the deferred aside still
	// shows the loading content, and the primary has landed.
	if got := labRead(t, tctx, `main[data-fui-layout-slot="l:pshell"]`); !contains(got, "SCREEN-PARTS2") {
		t.Errorf("primary mid-parts = %q, want SCREEN-PARTS2 applied with the page", got)
	}
	if got := labRead(t, tctx, pAsideSel); contains(got, "ASIDE-PARTS2-SLOW") {
		t.Errorf("slow aside applied early: %q", got)
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-ASIDE-PARTS2-SLOW`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	fast, slow := mixRead(t, tctx)
	dFast, dSlow := fast-t0, slow-t0
	t.Logf("P10-parts: fast region painted at %.0fms, slow part at %.0fms", dFast, dSlow)
	if dFast > 500 {
		t.Errorf("fast region painted at %.0fms; it must not wait for the 800ms part", dFast)
	}
	if dSlow-dFast < 250 {
		t.Errorf("slow part only %.0fms after fast; the part must land late (fast=%.0f slow=%.0f)", dSlow-dFast, dFast, dSlow)
	}
	if got := labRead(t, tctx, pRailSel); !contains(got, "RAIL-PARTS2") {
		t.Errorf("rail part missing after settle: %q", got)
	}

	// Back replays the entry with its landed parts, no request.
	navs := labNavigations(t, srv.URL)
	labBack(t, tctx)
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("back settle: %v", err)
	}
	if got := labRead(t, tctx, pAsideSel); !contains(got, "ASIDE-PARTS-SLOW") {
		t.Errorf("aside after Back = %q, want the /parts aside (ASIDE-PARTS-SLOW)", got)
	}
	if got := labNavigations(t, srv.URL); got != navs {
		t.Errorf("Back after a parts nav re-fetched (%d -> %d); the entry recorded its landed parts", navs, got)
	}
}

// TestP10PartsFailureContainedE2E: a deferred fill that fails must
// degrade its outlet only — the PART answers 200 with the region's
// fallback, even under the PAGE error policy.
func TestP10PartsFailureContainedE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/parts"),
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__gofastr.navigate('/partsfail')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-PARTSFAIL`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("nav: %v", err)
	}
	if got := labRead(t, tctx, pToolbarSel); got != "TOOLBAR-PARTSFAIL" {
		t.Errorf("fast toolbar fill = %q, want TOOLBAR-PARTSFAIL", got)
	}
	if got := labRead(t, tctx, pAsideSel); got != "ASIDE-HELP" {
		t.Errorf("failed aside part = %q, want the contained default ASIDE-HELP (the part answers 200 with the region's fallback)", got)
	}
	var toastVisible bool
	if err := chromedp.Run(tctx, chromedp.Evaluate(
		`!!document.querySelector('.fui-nav-toast.is-visible')`, &toastVisible)); err != nil {
		t.Fatal(err)
	}
	if toastVisible {
		t.Error("a contained part failure must not show the failure toast")
	}
}

// TestP10PartsSupersededE2E: a navigation superseded while its parts
// are in flight must leave no late part in the winner's page.
func TestP10PartsSupersededE2E(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/parts"),
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Run(tctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/parts2')`, nil),
		chromedp.Sleep(200*time.Millisecond), // page landed, slow part still pending
		chromedp.Evaluate(`window.__gofastr.navigate('/parts')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
		chromedp.Sleep(900*time.Millisecond), // let the late /parts2 part arrive
	); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	// The late part must NOT have landed in the winner's page.
	if got := labRead(t, tctx, pAsideSel); contains(got, "ASIDE-PARTS2") {
		t.Errorf("aside after superseded parts nav = %q; the late /parts2 part leaked into the winner's page", got)
	}
	if got := labRead(t, tctx, pRailSel); contains(got, "RAIL-PARTS2") {
		t.Errorf("rail after superseded parts nav = %q; the late part leaked", got)
	}
	if got := labRead(t, tctx, pAsideSel); !contains(got, "ASIDE-PARTS-SLOW") {
		t.Errorf("aside after supersede = %q, want the winner's own part (ASIDE-PARTS-SLOW)", got)
	}
}

// TestP10CFirstLoadCarriesDeferredRegions settles the SSR-first
// question at the HTTP layer (streaming's replacement): a first load —
// no navigate headers — waits for every fill, DEFERRED ones included,
// so a client without JavaScript sees the aside's own content inline in
// the outlet cell. Nothing travels as an inert template anymore.
func TestP10CFirstLoadCarriesDeferredRegions(t *testing.T) {
	srv := labServe(t)
	res, err := http.Get(srv.URL + "/parts")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if !strings.Contains(page, `data-fui-outlet="l:pshell#aside"`) {
		t.Fatal("no aside outlet in the document")
	}
	// The outlet cell carries the FILL's own content inline (the
	// JavaScript-off guarantee), not loading content.
	i := strings.Index(page, `data-fui-outlet="l:pshell#aside"`)
	cell := page[i:]
	if j := strings.Index(cell, ">"); j >= 0 {
		cell = cell[j+1:]
	}
	if k := strings.Index(cell, "</div>"); k >= 0 {
		cell = cell[:k]
	}
	if !strings.Contains(cell, "ASIDE-PARTS-SLOW") {
		t.Errorf("aside outlet cell = %q; a first load must carry the deferred fill's own content inline", cell)
	}
	if strings.Contains(page, `<template data-fui-fill=`) {
		t.Error("a first load must not ship fills as inert templates")
	}
}

// TestP10PartsBackKeepsGroupLayer: Back to a page reached by a parts
// navigation must replay at the layer the entry swapped, keeping every
// layer above it (the P8 rule holds under the parts transport).
func TestP10PartsBackKeepsGroupLayer(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var filter, detail string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/items/2')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.SetValue(`#lab-filter`, "still here", chromedp.ByQuery),
		chromedp.Evaluate(`window.__gofastr.navigate('/items/3')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-3`, chromedp.ByQuery),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Value(`#lab-filter`, &filter, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('#lab-filter').length + ''`, &detail),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if filter != "still here" {
		t.Errorf("filter after Back = %q, want \"still here\" (the list layer was replaced)", filter)
	}
	if detail != "1" {
		t.Errorf("%s filter inputs after Back, want 1", detail)
	}
}
