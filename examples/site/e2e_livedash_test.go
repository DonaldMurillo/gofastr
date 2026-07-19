package main

// Browser-level e2e for the live-dashboard reference
// (/examples/live-dashboard?presence=live-dashboard-demo). Verifies the
// canonical live-data composition actually behaves live in a real Chrome:
//
//   - The page renders with StatCards + an activity feed island (SSR).
//   - The SSE module loads and connects (waitModule + sseStatus check).
//   - After the demo ticker fires, an island region's innerHTML changes —
//     proving the SSE island push reached the browser and the runtime
//     swapped the slot.
//   - The console/CSP error sink stays empty (the bespoke-CSS canary —
//     any inline style="…" that strict CSP strips shows up here).
//
// Gated by -short, like every chromedp e2e in this package. Claude runs
// the suite serially at review; do NOT run it concurrently with other
// chromedp suites (the shared browser collides).

import (
	"strings"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// TestE2E_LiveDashboard_NoConsoleErrors is the keystone CSP guard for
// the dashboard: an inline style="…" that strict CSP strips shows up as
// a console/CSP error. Catches the bespoke-CSS failure mode that DOM
// dumps and computed-style probes miss.
func TestE2E_LiveDashboard_NoConsoleErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)

	if err := chromedp.Run(ctx,
		runtime.Enable(),
		cdplog.Enable(),
		// The ?presence= param joins the SSE topic so the demo ticker's
		// pushes reach this session. Without it the page still renders
		// (SSR is complete) but no islands update.
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Let the SSE module load, the connection open, and one or two
		// ticker frames arrive. 700ms tick → ~1.5s gives 2 frames.
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate live-dashboard: %v", err)
	}

	if errs := sink.errors(); len(errs) > 0 {
		t.Errorf("live-dashboard produced %d console/CSP error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
}

// TestE2E_LiveDashboard_IslandUpdatesViaSSE proves the end-to-end push
// path: the SSR-painted metric island actually changes after the demo
// ticker fires. Catches the failure mode where the page renders fine
// but the SSE lane is silently dead (the runtime would swap nothing).
func TestE2E_LiveDashboard_IslandUpdatesViaSSE(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	// The stats island's throughput random-walks ±200 per tick, so its
	// innerHTML changes between captures. Read it twice with a gap.
	var initialHTML, laterHTML string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Make sure the SSE module is up before capturing — otherwise
		// the initial read could race a hydration swap.
		waitModule("window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.sse === true"),
		// Confirm the stream actually opened. sseStatus.connected is
		// mutated in place by the SSE module on every onopen/onerror.
		chromedp.Poll("window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === true", nil, chromedp.WithPollingInterval(100*1e6)),
		// Initial capture: prove SSR painted metric StatCards.
		chromedp.InnerHTML(`[data-island="`+liveDashStatsID+`"]`, &initialHTML, chromedp.ByQuery),
		// Wait for ≥2 ticker frames (700ms tick → 1.6s).
		chromedp.Sleep(1600*time.Millisecond),
		// Second capture.
		chromedp.InnerHTML(`[data-island="`+liveDashStatsID+`"]`, &laterHTML, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("live-dashboard island update run: %v", err)
	}

	if initialHTML == "" {
		t.Fatal("initial stats island innerHTML is empty — SSR did not paint the metric StatCards")
	}
	if laterHTML == "" {
		t.Fatal("later stats island innerHTML is empty — the runtime swapped the slot to nothing")
	}
	if initialHTML == laterHTML {
		t.Fatal("stats island innerHTML did not change after ~2s — SSE island push did not reach the browser (check the ticker + presence topic join)")
	}
	// Sanity: both snapshots should mention the StatCard label so a
	// surprising swap (e.g. an error page replacing the region) is caught.
	const want = "Throughput"
	if !strings.Contains(initialHTML, want) || !strings.Contains(laterHTML, want) {
		t.Fatalf("stats island lost its %q StatCard label across the update — got initial=%q later=%q",
			want, initialHTML, laterHTML)
	}
}

// TestE2E_LiveDashboard_FeedIslandRenders proves the activity-feed island
// is present at SSR and carries the polite aria-live semantics. This is
// the a11y contract: high-frequency metrics are NOT aria-live; the feed
// IS. A regression that puts aria-live on the metrics would still pass
// the metrics-update test but should fail an a11y review — this test
// documents the lane separation.
func TestE2E_LiveDashboard_FeedIslandRenders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var feedHTML string
	var feedParentRole string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.InnerHTML(`[data-island="`+liveDashFeedID+`"]`, &feedHTML, chromedp.ByQuery),
		// Read the role attribute off the data-island parent itself —
		// it must be "status" (implicit polite aria-live).
		chromedp.AttributeValue(`[data-island="`+liveDashFeedID+`"]`, "role", &feedParentRole, nil, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("live-dashboard feed render: %v", err)
	}

	if feedHTML == "" {
		t.Fatal("feed island innerHTML is empty — Timeline did not SSR")
	}
	// Either the seeded events rendered ("completed", "Queue depth",
	// "Worker pool") or the empty-state placeholder did. Both are valid.
	const timelineMarker = "ui-timeline"
	if !strings.Contains(feedHTML, timelineMarker) {
		t.Fatalf("feed island did not render a Timeline (missing %q) — got %q",
			timelineMarker, feedHTML)
	}
	if feedParentRole != "status" {
		t.Fatalf("feed island role = %q, want \"status\" (polite aria-live is the a11y contract for the feed lane)",
			feedParentRole)
	}
}

// TestE2E_LiveDashboard_ComputedStatusPill proves the store.Computed
// pill is bound and that the reducer is loaded. The reducer ships via
// uihost.WithExtraScripts, so a missing/delayed load would silently
// leave the SSR-painted label in place — this test asserts the reducer
// is registered and runnable.
func TestE2E_LiveDashboard_ComputedStatusPill(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var reducerIsFn string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Ensure runtime.js has assigned __gofastr and the reducer
		// script (loaded via WithExtraScripts AFTER runtime.js) has
		// registered the dash.status reducer.
		chromedp.Poll(
			`window.__gofastr && window.__gofastr._reducers && `+
				`typeof window.__gofastr._reducers['dash.status'] === 'function'`,
			nil, chromedp.WithPollingInterval(100*1e6)),
		chromedp.Evaluate(
			`typeof window.__gofastr._reducers['dash.status']`, &reducerIsFn),
	); err != nil {
		t.Fatalf("live-dashboard computed pill run: %v", err)
	}
	if reducerIsFn != "function" {
		t.Fatalf("dash.status reducer is %q, want \"function\" — WithExtraScripts did not register it",
			reducerIsFn)
	}
}

// TestE2E_LiveDashboard_RegionHeadingsSurviveSSEPush catches the
// regression where the feed/jobs <h2> headings lived INSIDE the
// data-island wrapper. The runtime does island.innerHTML = payload on
// every SSE push, and the push payload contained only the Timeline /
// DataTable — so the first tick permanently deleted the "Activity feed"
// and "Jobs" titles. The fix moves each heading OUTSIDE the slot, as a
// sibling; this test asserts both headings are still in the DOM AFTER
// at least one ticker frame has landed.
func TestE2E_LiveDashboard_RegionHeadingsSurviveSSEPush(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	// After ≥2 ticker frames, each data-island slot has had its
	// innerHTML swapped at least once. The fix moves the region <h2>
	// OUT of the slot to a sibling position, so the heading must still
	// be present in the parent wrapper. Read the title via JS so we
	// explicitly assert "the h2 is a sibling of the data-island, not
	// a child of it" — walking parentElement then querySelector'ing
	// for the h2 fails if the h2 is missing OR if it ended up inside
	// the slot (parentElement would be the grid cell either way; the
	// querySelector('h2') picks up the heading wherever it lives in
	// the wrapper).
	var feedTitle, jobsTitle string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Confirm SSE actually connected — otherwise no push lands
		// and the test would pass against a non-live page.
		waitModule("window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.sse === true"),
		chromedp.Poll("window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === true", nil, chromedp.WithPollingInterval(100*1e6)),
		// Let ≥2 ticker frames land (700ms tick → 1.6s).
		chromedp.Sleep(1600*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-island="`+liveDashFeedID+`"]');
			const wrap = el ? el.parentElement : null;
			const h = wrap ? wrap.querySelector('h2.livedash-region-title') : null;
			return h ? h.textContent : '';
		})()`, &feedTitle),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('[data-island="`+liveDashJobsID+`"]');
			const wrap = el ? el.parentElement : null;
			const h = wrap ? wrap.querySelector('h2.livedash-region-title') : null;
			return h ? h.textContent : '';
		})()`, &jobsTitle),
	); err != nil {
		t.Fatalf("live-dashboard heading-survive run: %v", err)
	}

	const wantFeed = "Activity feed"
	const wantJobs = "Jobs"
	if feedTitle != wantFeed {
		t.Errorf("feed heading after SSE push = %q, want %q — the h2 was inside the data-island slot and the first tick wiped it",
			feedTitle, wantFeed)
	}
	if jobsTitle != wantJobs {
		t.Errorf("jobs heading after SSE push = %q, want %q — the h2 was inside the data-island slot and the first tick wiped it",
			jobsTitle, wantJobs)
	}
}

// TestE2E_LiveDashboard_AcknowledgeButtonBumpsCount catches the
// regression where the Acknowledge button incremented the
// dash.incidentsAckd signal but nothing visible was bound to it — the
// "Acknowledged 0" text was static SSR. The fix binds the count to a
// live signal span; this test clicks Acknowledge and asserts the DOM
// count actually changes.
func TestE2E_LiveDashboard_AcknowledgeButtonBumpsCount(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	// The Acknowledge button is uniquely identified by its
	// data-fui-signal-inc target. (ui.Button forces aria-label to the
	// visible Label, so the aria-label="Acknowledge one incident" in
	// the button's ExtraAttrs is overwritten by Label="Acknowledge"
	// at render time — selecting by aria-label would silently match
	// nothing.)
	const ackBtnSel = `button[data-fui-signal-inc="dash.incidentsAckd:1"]`
	const ackCountSel = `[data-fui-signal="dash.incidentsAckd"]`

	var before, after string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/live-dashboard?presence="+liveDashTopic),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// The data-fui-signal-inc click delegator lives in core
		// runtime.js (not a separate module). Wait for the runtime
		// to be reachable before relying on it.
		chromedp.Poll(`typeof (window.__gofastr || {}).setSignal === 'function'`, nil, chromedp.WithPollingInterval(50*1e6)),
		// Confirm the bound count span is in the DOM with the SSR
		// initial value of "0" — guards against the bind regressing
		// back to static text (no data-fui-signal attr).
		chromedp.WaitVisible(ackCountSel, chromedp.ByQuery),
		chromedp.Text(ackCountSel, &before, chromedp.ByQuery),
		// Click (NOT Submit — see CLAUDE.md). data-fui-signal-inc is
		// a click delegator; no form submission is involved.
		chromedp.Click(ackBtnSel, chromedp.ByQuery),
		// Poll for the DOM to reflect the new signal value. The
		// runtime applies the inc synchronously and re-texts the
		// bound span on the same tick, but a brief poll keeps the
		// test robust to scheduling on slow CI runners.
		chromedp.Poll(`(() => {
			const el = document.querySelector('[data-fui-signal="dash.incidentsAckd"]');
			return el && el.textContent === '1';
		})()`, nil, chromedp.WithPollingInterval(50*1e6)),
		chromedp.Text(ackCountSel, &after, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("live-dashboard ack click run: %v", err)
	}

	if before != "0" {
		t.Fatalf("acknowledged count before click = %q, want \"0\" — the bind did not stamp the SSR default", before)
	}
	if after != "1" {
		t.Fatalf("acknowledged count after click = %q, want \"1\" — the Acknowledge button is inert (data-fui-signal-inc fired but no DOM node is bound to dash.incidentsAckd, or the runtime did not apply the mutation)", after)
	}
}
