package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The showcase's light e2e pass: the lab carries the layout rigour;
// these tests pin the PRODUCT behaviours the showcase exists to show.

const (
	asideSel   = `[data-cui-outlet="l:shell#aside"]`
	crumbsSel  = `[data-cui-area="l:shell~crumbs"]`
	toolbarSel = `[data-cui-outlet="l:shell#toolbar"]`
	filterSel  = `#filter-search-filter`
)

// trackerEnv is kept as the single seam the e2e suite arms before
// building the app; the fill policies it once pinned are the only
// behaviours the core ships now, so there is nothing to set.
func trackerEnv(t *testing.T) {
	t.Helper()
}

// trackerServe builds the app in-process and serves it.
func trackerServe(t *testing.T) string {
	t.Helper()
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

// trackerBrowser opens a browser with the viewport pinned to
// width x height.
func trackerBrowser(t *testing.T, width, height int) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(width, height),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	root, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	if err := chromedp.Run(root); err != nil {
		t.Fatalf("browser failed to start: %v", err)
	}
	// EmulateViewport pins the layout viewport exactly; WindowSize
	// alone leaves the inner width at the browser's whim.
	if err := chromedp.Run(root, chromedp.EmulateViewport(int64(width), int64(height))); err != nil {
		t.Fatalf("viewport: %v", err)
	}
	return root
}

// trackerTab wraps a trackerBrowser context (already viewport-pinned)
// in a deadline.
func trackerTab(t *testing.T, parent context.Context, seconds time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, seconds)
	t.Cleanup(cancel)
	return ctx, cancel
}

// readText reads the trimmed text of sel; a missing element reads as
// "!missing" so failures read clearly.
func readText(t *testing.T, ctx context.Context, sel string) string {
	t.Helper()
	var out string
	expr := `(() => { const el = document.querySelector('` + sel + `'); ` +
		`return el ? el.textContent.trim() : '!missing'; })()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out)); err != nil {
		t.Fatalf("read %s: %v", sel, err)
	}
	return out
}

// waitText waits until sel's text contains want.
func waitText(t *testing.T, ctx context.Context, sel, want string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Poll(fmt.Sprintf(
			`document.querySelector(%q).textContent.includes(%q)`, sel, want), nil),
	); err != nil {
		t.Fatalf("wait %s ~ %q: %v", sel, want, err)
	}
}

// TestTrackerRoutesE2E: every route answers 200 and renders its
// heading inside the shell.
func TestTrackerRoutesE2E(t *testing.T) {
	base := trackerServe(t)

	headings := []struct {
		path, sel, want string
	}{
		{"/", "main h1", "Overview"},
		{"/inbox", "main h1", "Inbox"},
		{"/reports", "main h1", "Reports"},
		{"/settings", "main h1", "Settings"},
		{"/projects/billing", "main h1", "Billing"},
		{"/projects/legacy", "main h1", "Legacy"},
		{"/projects/billing/issues/42", "#issue-detail h2", "Retry card updates"},
		{"/projects/legacy/issues/33", "#issue-detail h2", "Audit log purge"},
	}
	for _, h := range headings {
		res, err := http.Get(base + h.path)
		if err != nil {
			t.Fatalf("%s: %v", h.path, err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Errorf("%s: status %d, want 200", h.path, res.StatusCode)
		}
	}
	// An unknown URL answers 404 and renders its own page. (The 404
	// does not yet travel through the tree-layout shell — a known
	// framework gap another spike owns; the brief says leave it.)
	res, err := http.Get(base + "/no-such-page")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("/no-such-page: status %d, want 404", res.StatusCode)
	}

	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	for _, h := range headings {
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+h.path),
			chromedp.WaitVisible(h.sel, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s: navigate: %v", h.path, err)
		}
		if got := readText(t, ctx, h.sel); !strings.Contains(got, h.want) {
			t.Errorf("%s: heading = %q, want it to contain %q", h.path, got, h.want)
		}
		// The shell stays put: the sidebar and the top bar are part
		// of every page.
		if got := readText(t, ctx, `.fui-content-row__nav`); !strings.Contains(got, "Projects") {
			t.Errorf("%s: shell sidebar missing (read %q)", h.path, got)
		}
	}
}

// TestTrackerIssueFlowE2E: the filter is kept across issue navigations
// (the list pane is kept chrome), and the toolbar breadcrumbs and the
// aside follow the issue.
func TestTrackerIssueFlowE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate project: %v", err)
	}

	// Apply the server-side GET filter; only matching rows are rendered.
	if err := chromedp.Run(ctx, chromedp.SendKeys(filterSel, "retry"),
		chromedp.Click(`[data-cui-comp="ui-filter-toolbar"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#filter-search-filter[value="retry"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("filter: %v", err)
	}
	var visibleRows int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.fui-card[data-key]')).filter(r => r.offsetParent !== null).length`,
		&visibleRows)); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if visibleRows != 1 {
		t.Fatalf("retry filter rendered %d rows, want one", visibleRows)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open BIL-42: %v", err)
	}
	// The layout cell the project layer renders inside the detail pane
	// carries no shell padding: the pane owns its insets (ListDetail's
	// sheet owns the reset; the shell's sheet no longer keys on the
	// pane's class).
	var detailPad string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`getComputedStyle(document.querySelector('.fui-list-detail__detail > .layout-content')).paddingTop`,
		&detailPad)); err != nil {
		t.Fatalf("read detail cell padding: %v", err)
	}
	if detailPad != "0px" {
		t.Fatalf("detail pane layout cell paddingTop = %s, want 0px — the viewport reset stopped applying", detailPad)
	}
	waitText(t, ctx, `#issue-detail h2`, "Retry card updates when the payment service provider times out")

	// The filter input survived the navigation with its value: the
	// list pane is KEPT chrome inside the project layer.
	var filterVal string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('filter-search-filter').value`, &filterVal)); err != nil {
		t.Fatalf("read filter: %v", err)
	}
	// Breadcrumbs follow the issue.
	if got := readText(t, ctx, crumbsSel); !strings.Contains(got, "BIL-42") {
		t.Fatalf("crumbs = %q, want BIL-42", got)
	}
	// The toolbar carries the issue's actions.
	if got := readText(t, ctx, toolbarSel); !strings.Contains(got, "Close issue") {
		t.Fatalf("toolbar = %q, want Close issue", got)
	}
	// The aside carries this issue's activity (it streams in).
	waitText(t, ctx, asideSel, "Otis Vance")

	// Clear and submit the filter, then open two issues from the full list.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const f = document.getElementById('filter-search-filter'); f.value = ''; f.form.requestSubmit(); })()`, nil),
		chromedp.WaitVisible(`a[data-key="BIL-57"]`, chromedp.ByQuery),
		chromedp.Click(`a[data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.Poll(`location.pathname === '/projects/billing/issues/42'`, nil)); err != nil {
		t.Fatalf("clear filter: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-57"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("open BIL-57: %v", err)
	}
	waitText(t, ctx, crumbsSel, "BIL-57")
	waitText(t, ctx, asideSel, "Theo Ito commented")

	// Back: the previous issue is restored with its own crumbs.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatalf("back: %v", err)
	}
	waitText(t, ctx, crumbsSel, "BIL-42")

	if filterVal != "retry" {
		t.Fatalf("kept filter = %q, want retry", filterVal)
	}
}

// TestTrackerLegacyAsideE2E: a Legacy issue's activity aside is
// GUARDED — the archived project's read-only notice renders there
// while the issue itself renders on.
func TestTrackerLegacyAsideE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 60*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/legacy/issues/33"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate legacy issue: %v", err)
	}
	waitText(t, ctx, asideSel, "Archived project, activity is read only in the old system.")
	// And the page itself is intact.
	if got := readText(t, ctx, `#issue-detail h2`); !strings.Contains(got, "Audit log purge") {
		t.Fatalf("issue title = %q", got)
	}
}

// TestTrackerMobileE2E: at 390px nothing scrolls horizontally, the
// aside stacks below the content, and an issue page leads with the
// detail plus a back link.
func TestTrackerMobileE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	for _, path := range []string{"/", "/projects/billing", "/projects/billing/issues/42"} {
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.WaitVisible("main", chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s: navigate: %v", path, err)
		}
		var scrollW float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`document.documentElement.scrollWidth`, &scrollW)); err != nil {
			t.Fatalf("%s: scrollWidth: %v", path, err)
		}
		if scrollW > 390 {
			t.Errorf("%s: scrollWidth = %.0f, want <= 390 (no horizontal scroll)", path, scrollW)
		}
	}

	// Tracker opts into a single phone pane, with a visible way back.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/42"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate issue: %v", err)
	}
	var panes map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		return {listHidden: document.querySelector('.fui-list-detail__list').offsetParent === null,
			backVisible: document.querySelector('.fui-list-detail__detail a.fui-button').offsetParent !== null};
	})()`, &panes)); err != nil {
		t.Fatalf("pane read: %v", err)
	}
	if !panes["listHidden"].(bool) {
		t.Fatal("mobile issue page: the list still renders beside the detail")
	}
	if !panes["backVisible"].(bool) {
		t.Fatal("mobile issue page: Back to issues link is not visible")
	}
}

// TestTrackerStaticExportE2E: the export builds, every route lands on
// disk, and the Legacy project's issue pages are deliberately absent
// (their activity fill fails on purpose; the builder refuses to bake
// a contained failure into a static page).
func TestTrackerStaticExportE2E(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	dir := t.TempDir()
	if err := exportTo(context.Background(), app, dir); err != nil {
		t.Fatalf("export: %v", err)
	}

	mustExist := []string{
		"index.html",
		filepath.Join("inbox", "index.html"),
		filepath.Join("reports", "index.html"),
		filepath.Join("settings", "index.html"),
		filepath.Join("projects", "billing", "index.html"),
		filepath.Join("projects", "billing", "issues", "42", "index.html"),
		filepath.Join("projects", "search", "issues", "41", "index.html"),
		filepath.Join("projects", "auth", "issues", "3", "index.html"),
	}
	for _, rel := range mustExist {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("export missing %s: %v", rel, err)
		}
	}

	// The exported issue page carries its activity fill inline (SSR
	// complete), and the app script ships beside it.
	data, err := os.ReadFile(filepath.Join(dir, "projects", "billing", "issues", "42", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Otis Vance") {
		t.Error("exported BIL-42 page does not carry the activity feed")
	}

	// Legacy issue pages: absent on purpose.
	legacyDir := filepath.Join(dir, "projects", "legacy", "issues")
	if entries, err := os.ReadDir(legacyDir); err == nil && len(entries) > 0 {
		t.Errorf("legacy issue pages exported (%d entries), want none", len(entries))
	}
	// The legacy project index itself does export.
	if _, err := os.Stat(filepath.Join(dir, "projects", "legacy", "index.html")); err != nil {
		t.Errorf("export missing legacy project index: %v", err)
	}
}

// TestTrackerSidebarCurrentAfterNavE2E: the sidebar's Billing item is
// marked current by the server on /projects/billing, and the runtime's
// active-link sweep must KEEP it lit after a client navigation deeper
// into the section (the item's MatchPath rides the link as
// data-cui-match-prefix).
func TestTrackerSidebarCurrentAfterNavE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate project: %v", err)
	}
	// The sweep that loses the item only runs once the idle-loaded
	// activelink module is live (it stamps .active on the exact match
	// at load; navigating away then clears what it stamped). Wait for
	// it, the way a user who reads the page for a beat does.
	var armed bool
	if err := chromedp.Run(ctx, chromedp.Poll(
		`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.activelink)`,
		&armed, chromedp.WithPollingTimeout(15*time.Second))); err != nil || !armed {
		t.Fatalf("activelink module never idle-loaded (armed=%v): %v", armed, err)
	}
	// The clear lands when the streamed navigation completes (its
	// gofastr:navigate fires at stream end, after the slow activity
	// fill), so gate the read on the event, not on the DOM swap.
	var navd bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { window.__navCount = 0; window.addEventListener('gofastr:navigate', () => window.__navCount++); })()`, nil),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.Poll(`window.__navCount >= 1`, &navd, chromedp.WithPollingTimeout(15*time.Second)),
	); err != nil || !navd {
		t.Fatalf("navigation event never fired (navd=%v): %v", navd, err)
	}
	waitText(t, ctx, `#issue-detail h2`, "Retry card updates")

	var cur string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('.fui-content-row__nav nav a[href="/projects/billing"]').getAttribute('aria-current')`, &cur)); err != nil {
		t.Fatalf("read billing link: %v", err)
	}
	if cur != "page" {
		t.Fatalf(`sidebar Billing link aria-current = %q after navigating into the section, want "page"`, cur)
	}
}

// TestTrackerSwapFocusRingE2E: the runtime moves focus onto the swapped
// region after every client navigation. A pointer-initiated navigation
// must not paint the focus ring (focusVisible: false); a keyboard
// activation must (focus-visible semantics). Computed outline-style is
// the observable: the UA ring draws as "auto" only while the focused
// element matches :focus-visible.
func TestTrackerSwapFocusRingE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	// Pointer: click into an issue; the swapped project-layer cell is
	// focused but shows no ring.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("pointer navigation: %v", err)
	}
	var clickOutline, clickFocused string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const cell = document.querySelector('.fui-list-detail [data-cui-layout-slot]');
		const cs = getComputedStyle(cell);
		return JSON.stringify({
			style: cs.outlineStyle,
			focused: document.activeElement === cell,
			visible: String(cell.matches(':focus-visible')),
		});
	})()`, &clickOutline), chromedp.Evaluate(`document.activeElement && document.activeElement.tagName`, &clickFocused)); err != nil {
		t.Fatalf("read focus state after click: %v", err)
	}
	_ = clickFocused
	var clickState struct {
		Style   string `json:"style"`
		Focused bool   `json:"focused"`
		Visible string `json:"visible"`
	}
	if err := json.Unmarshal([]byte(clickOutline), &clickState); err != nil {
		t.Fatalf("decode click state: %v (raw %q)", err, clickOutline)
	}
	if !clickState.Focused {
		t.Fatalf("the swapped cell did not receive focus (activeElement=%s): screen-reader announcement contract broken", clickFocused)
	}
	if clickState.Style != "none" {
		t.Errorf("pointer navigation painted a focus ring on the swapped region (outline-style=%s, :focus-visible=%s); the ring is keyboard signal, a mouse click must not show it", clickState.Style, clickState.Visible)
	}

	// Keyboard: focus a row link, activate with Enter (a trusted key
	// event synthesizes the activation click with detail 0). The newly
	// swapped cell must now carry the visible ring.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('a.fui-card[data-key][data-key="BIL-57"]').focus()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("keyboard navigation: %v", err)
	}
	waitText(t, ctx, crumbsSel, "BIL-57")
	var keyState struct {
		Style   string `json:"style"`
		Focused bool   `json:"focused"`
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const cell = document.querySelector('.fui-list-detail [data-cui-layout-slot]');
		return JSON.stringify({style: getComputedStyle(cell).outlineStyle, focused: document.activeElement === cell});
	})()`, &raw)); err != nil {
		t.Fatalf("read focus state after Enter: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &keyState); err != nil {
		t.Fatalf("decode key state: %v (raw %q)", err, raw)
	}
	if !keyState.Focused {
		t.Fatal("the swapped cell did not receive focus after the keyboard navigation")
	}
	if keyState.Style == "none" {
		t.Error("keyboard navigation shows no focus ring on the swapped region (outline-style none): a keyboard user cannot see where focus went")
	}
}

// TestTrackerPartFillEventE2E: the runtime dispatches gofastr:fill on
// window after each applied PART. Navigating to the home page fetches
// the slow aside fill as its own part request (~900 ms) — exactly one
// event for l:shell#aside, carrying the destination path and the apply
// time. Streaming's replacement: the same event, a different transport.
// (The DevTools-level contract — one request per deferred outlet, each
// with a readable body — is pinned by TestPartsAreSeparateRequestsWithBodies
// in core-ui/runtime against a controlled server.)
func TestTrackerPartFillEventE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/inbox"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			window.__fillEvents = [];
			window.addEventListener('gofastr:fill', (e) => {
				window.__fillEvents.push(e.detail || {});
			});
		})()`, nil),
	); err != nil {
		t.Fatalf("arm recorder: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-cui-scope="appbar"] a.brand`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click brand: %v", err)
	}
	waitText(t, ctx, asideSel, "Recent activity")
	// The aside part lands after its Load (~900 ms); give the part a
	// moment past the card heading.
	chromedp.Sleep(1500 * time.Millisecond)
	var events []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__fillEvents`, &events)); err != nil {
		t.Fatalf("read events: %v", err)
	}
	var aside int
	for _, ev := range events {
		if ev["addr"] == "l:shell#aside" {
			aside++
			if p, _ := ev["path"].(string); p != "/" {
				t.Errorf("fill event path = %v, want /", ev["path"])
			}
			tf, ok := ev["t"].(float64)
			if !ok || tf <= 0 {
				t.Errorf("fill event t = %v, want a positive performance.now() timestamp", ev["t"])
			}
		}
	}
	if aside != 1 {
		t.Errorf("gofastr:fill fired %d times for l:shell#aside, want exactly 1 (once per applied part)", aside)
	}
}

// jsClick clicks via evaluate: the runtime's view transitions make
// chromedp.Click's stability waits flaky mid-animation, and a JS click
// is exactly what the delegated handlers receive.
func jsClick(ctx context.Context, sel string) error {
	return chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector(`+"`"+sel+"`"+`).click()`, nil))
}

// waitForPolls until expr is true (string form "true") or the timeout.
func waitForExpr(t *testing.T, ctx context.Context, expr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got bool
	for time.Now().Before(deadline) {
		got = false
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &got)); err != nil {
			t.Fatalf("poll %s: %v", expr, err)
		}
		if got {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", expr)
}

// TestTrackerBellPopoverE2E (R7): a click on the notification bell
// opens the popover and does NOT navigate, on the home page and on an
// issue page reached by a client navigation. The popover's box stays
// in the viewport with its arrow over the bell's centre (within 4px),
// Escape and an outside click close it and return focus to the bell,
// and the bell icon paints (non-empty box + a resolved stroke).
func TestTrackerBellPopoverE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 860), 180*time.Second)

	// Reach an issue page through a CLIENT navigation (the shell and
	// the bell survive the swap — the wiring must too).
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitForExpr(t, ctx, `!!(window.__gofastr && __gofastr.navigate) &&
		!!document.querySelector('a[href="/projects/billing/issues/42"]')`, 10*time.Second)
	if err := jsClick(ctx, `a[href="/projects/billing/issues/42"]`); err != nil {
		t.Fatalf("click issue: %v", err)
	}
	waitForExpr(t, ctx, `location.pathname === "/projects/billing/issues/42" &&
		!!document.querySelector('.fui-list-detail__detail')`, 10*time.Second)

	for _, tc := range []struct {
		name string
		path string
	}{
		{"issue page (client nav)", "/projects/billing/issues/42"},
		{"home page (full load)", "/"},
	} {
		if tc.path == "/" {
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/"),
				chromedp.WaitVisible("main", chromedp.ByQuery),
				chromedp.Sleep(300*time.Millisecond),
			); err != nil {
				t.Fatalf("navigate home: %v", err)
			}
		}

		// The icon paints: a non-empty bounding box and a stroke that
		// resolves to something visible (not none/transparent).
		var icon map[string]any
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const b = document.querySelector('[data-hui-notification-bell]');
			const svg = b.querySelector('svg');
			const path = svg.querySelector('path');
			const pcs = getComputedStyle(path);
			const r = svg.getBoundingClientRect();
			return {w: r.width, h: r.height, stroke: pcs.stroke};
		})()`, &icon)); err != nil {
			t.Fatalf("%s: icon: %v", tc.name, err)
		}
		if icon["w"].(float64) < 8 || icon["h"].(float64) < 8 {
			t.Errorf("%s: bell icon box = %vx%v, want a painted glyph", tc.name, icon["w"], icon["h"])
		}
		if s, _ := icon["stroke"].(string); s == "none" || s == "transparent" || s == "" {
			t.Errorf("%s: bell icon stroke = %q, want a resolved colour", tc.name, s)
		}

		if err := jsClick(ctx, `[data-hui-notification-bell]`); err != nil {
			t.Fatalf("%s: click bell: %v", tc.name, err)
		}
		waitForExpr(t, ctx, `(() => {
			const w = document.querySelector('[data-cui-widget="tracker-bell"]');
			return !!w && !w.hasAttribute('hidden') &&
				!!w.getAttribute('data-cui-popover-side');
		})()`, 10*time.Second)

		var geo map[string]any
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const w = document.querySelector('[data-cui-widget="tracker-bell"]');
			const b = document.querySelector('[data-hui-notification-bell]');
			const r = w.getBoundingClientRect();
			const br = b.getBoundingClientRect();
			const arrowX = parseFloat(w.style.getPropertyValue('--ui-popover-arrow-x'));
			return {
				url: location.pathname,
				boxX: r.x, boxW: r.width, boxRight: r.right, vw: innerWidth,
				arrowAbs: r.x + arrowX, bellCx: br.x + br.width / 2,
			};
		})()`, &geo)); err != nil {
			t.Fatalf("%s: geometry: %v", tc.name, err)
		}
		if u, _ := geo["url"].(string); u != tc.path {
			t.Errorf("%s: URL = %s after a bell click, want unchanged (%s)", tc.name, u, tc.path)
		}
		if geo["boxX"].(float64) < 0 || geo["boxRight"].(float64) > geo["vw"].(float64) {
			t.Errorf("%s: popover box [%.0f, %.0f] escapes the viewport %.0f",
				tc.name, geo["boxX"], geo["boxRight"], geo["vw"])
		}
		// Arrow over the bell's centre, within 4px (brief R7).
		if d := geo["arrowAbs"].(float64) - geo["bellCx"].(float64); d < -4 || d > 4 {
			t.Errorf("%s: popover arrow sits %.1fpx from the bell centre (want ≤4)", tc.name, d)
		}

		// Escape closes and returns focus to the bell.
		if err := chromedp.Run(ctx,
			chromedp.KeyEvent(kb.Escape),
			chromedp.Sleep(200*time.Millisecond),
			chromedp.Evaluate(`(() => {
				const w = document.querySelector('[data-cui-widget="tracker-bell"]');
				const b = document.querySelector('[data-hui-notification-bell]');
				return { open: !!w && !w.hasAttribute('hidden'),
					focusOnBell: document.activeElement === b };
			})()`, &geo),
		); err != nil {
			t.Fatalf("%s: escape: %v", tc.name, err)
		}
		if geo["open"].(bool) {
			t.Errorf("%s: Escape did not close the popover", tc.name)
		}
		if !geo["focusOnBell"].(bool) {
			t.Errorf("%s: focus did not return to the bell after Escape", tc.name)
		}

		// Reopen, then an outside click closes it too.
		if err := jsClick(ctx, `[data-hui-notification-bell]`); err != nil {
			t.Fatalf("%s: reopen: %v", tc.name, err)
		}
		waitForExpr(t, ctx, `(() => {
			const w = document.querySelector('[data-cui-widget="tracker-bell"]');
			return !!w && !w.hasAttribute('hidden');
		})()`, 10*time.Second)
		if err := jsClick(ctx, `#issue-detail h2, main h1`); err != nil {
			t.Fatalf("%s: outside click: %v", tc.name, err)
		}
		var open bool
		if err := chromedp.Run(ctx,
			chromedp.Sleep(200*time.Millisecond),
			chromedp.Evaluate(`(() => {
				const w = document.querySelector('[data-cui-widget="tracker-bell"]');
				return !!w && !w.hasAttribute('hidden');
			})()`, &open),
		); err != nil {
			t.Fatalf("%s: outside read: %v", tc.name, err)
		}
		if open {
			t.Errorf("%s: an outside click did not close the popover", tc.name)
		}
	}
}

// TestTrackerViewportFitE2E (R8): the app fills exactly the viewport —
// the document never scrolls on / or an issue page at 1280x860 — and
// the last paragraph of the long issue remains reachable in its scroll region.
func TestTrackerViewportFitE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 860), 120*time.Second)

	var m map[string]any
	for _, path := range []string{"/", "/projects/billing/issues/104"} {
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.WaitVisible("main", chromedp.ByQuery),
			// Let the streamed fills land so their height counts.
			chromedp.Sleep(1500*time.Millisecond),
			chromedp.Evaluate(`(() => ({
				path: location.pathname,
				scrollH: document.documentElement.scrollHeight,
				innerH: innerHeight,
			}))()`, &m),
		); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if m["scrollH"].(float64) > m["innerH"].(float64) {
			t.Errorf("%s: scrollHeight %.0f > innerHeight %.0f — the document scrolls",
				path, m["scrollH"], m["innerH"])
		}
	}

	// The issue's last paragraph can scroll fully into view.
	var last map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/104"),
		chromedp.WaitVisible(`#issue-detail`, chromedp.ByQuery),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const main = document.querySelector('.fui-content-row__workspace > main');
			main.scrollTop = main.scrollHeight;
			const paras = main.querySelectorAll('.fui-list-detail__detail p');
			const last = paras[paras.length - 1];
			const r = last.getBoundingClientRect();
			return {bottom: r.bottom, barTop: innerHeight};
		})()`, &last),
	); err != nil {
		t.Fatalf("last paragraph: %v", err)
	}
	if last["bottom"].(float64) > last["barTop"].(float64) {
		t.Errorf("the issue's last line (%.0f) is clipped below the viewport (%.0f)",
			last["bottom"], last["barTop"])
	}
}

// TestTrackerBackTimingE2E measures cached Back from popstate to the
// runtime's navigation event, without an application HUD.
func TestTrackerBackTimingE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 860), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitForExpr(t, ctx, `!!(window.__gofastr && __gofastr.navigate) &&
		!!document.querySelector('a[href="/projects/billing/issues/31"]')`, 10*time.Second)
	for _, issue := range []string{`a[href="/projects/billing/issues/31"]`, `a[href="/projects/billing/issues/42"]`} {
		if err := jsClick(ctx, issue); err != nil {
			t.Fatalf("click %s: %v", issue, err)
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(1100*time.Millisecond)); err != nil {
			t.Fatalf("settle: %v", err)
		}
	}
	var ms float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			window.__backMs = -1;
			window.addEventListener('popstate', () => {
				const start = performance.now();
				window.addEventListener('gofastr:navigate', () => {
					window.__backMs = performance.now() - start;
				}, {once:true});
			}, {once:true});
			history.back();
		})()`, nil),
		chromedp.Poll(`window.__backMs >= 0`, nil),
		chromedp.Evaluate(`window.__backMs`, &ms),
	); err != nil {
		t.Fatalf("back: %v", err)
	}
	waitText(t, ctx, crumbsSel, "BIL-31")
	if ms >= 200 {
		t.Errorf("cached Back took %.0f ms, want < 200", ms)
	}
}

// TestTrackerAsideCollapseE2E (R3): an aside that renders nothing
// gives its column back (the outlet arrives empty), and a slow
// navigation to one of those pages never reserves the column with its
// loading skeleton.
func TestTrackerAsideCollapseE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 860), 120*time.Second)

	// At rest: the column is gone and the content takes the width.
	var wideMain float64
	for _, path := range []string{"/settings", "/inbox"} {
		var m map[string]any
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.WaitVisible("main", chromedp.ByQuery),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Evaluate(`(() => {
				const a = document.querySelector('.fui-content-row__aside');
				const m = document.querySelector('.fui-content-row__workspace > main');
				return {display: getComputedStyle(a).display,
					mainW: Math.round(m.getBoundingClientRect().width), vw: innerWidth};
			})()`, &m),
		); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if m["display"] != "none" {
			t.Errorf("%s: the empty aside still displays (%s)", path, m["display"])
		}
		if wideMain == 0 {
			wideMain = m["mainW"].(float64)
		}
	}
	// The freed column goes to the content: a filled page's main is
	// ~320px (the aside's basis) narrower than the empty-aside pages.
	var filledMain float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`Math.round(document.querySelector('.fui-content-row__workspace > main').getBoundingClientRect().width)`, &filledMain),
	); err != nil {
		t.Fatalf("filled page: %v", err)
	}
	if wideMain <= filledMain {
		t.Errorf("empty-aside main width %.0f vs filled %.0f — the freed column did not go to the content",
			wideMain, filledMain)
	}

	// Navigation to a slow, empty-aside page must release the column once
	// its response arrives; no application script predicts route contents.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	waitForExpr(t, ctx, `!!(window.__gofastr && __gofastr.navigate)`, 10*time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.fui-content-row__nav a[href="/reports"], a[href="/reports"]').click()`, nil),
	); err != nil {
		t.Fatalf("click reports: %v", err)
	}
	var mid map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Poll(`location.pathname === '/reports' && document.querySelector('[data-cui-outlet="l:shell#aside"]').textContent === ''`, nil),
		chromedp.Evaluate(`(() => {
			const a = document.querySelector('.fui-content-row__aside');
			return {display: getComputedStyle(a).display};
		})()`, &mid),
	); err != nil {
		t.Fatalf("empty aside after navigation: %v", err)
	}
	if mid["display"] != "none" {
		t.Errorf("reports after navigation: the empty aside still reserves a column (display=%s)", mid["display"])
	}
	// And the reports charts hug their bars (R4): the fitted svg is
	// shorter than the old fixed 200 and no band pads above the caps.
	var chart map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const svg = document.querySelector('#tracker-report-billing.fui-card svg.fui-bar-chart, #tracker-report-billing svg');
		const h = parseFloat(svg.getAttribute('height'));
		let topBar = Infinity, firstLabel = Infinity;
		for (const t of svg.querySelectorAll('text.fui-bar-chart__value')) {
			topBar = Math.min(topBar, parseFloat(t.getAttribute('y')));
		}
		return {h, topBar};
	})()`, &chart)); err != nil {
		t.Fatalf("reports chart: %v", err)
	}
	if h := chart["h"].(float64); h >= 200 {
		t.Errorf("reports chart height %.0f — the empty band above the bars survived", h)
	}
}

// TestTrackerMobileFactsTwoUpE2E (R5): at 390 the four fact boxes sit
// two by two, not one per row.
func TestTrackerMobileFactsTwoUpE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	var m map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
		chromedp.Sleep(2000*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const boxes = document.querySelectorAll('.fui-metric-band__item');
			const rows = new Set();
			for (const b of boxes) rows.add(Math.round(b.getBoundingClientRect().y));
			return {count: boxes.length, rows: rows.size};
		})()`, &m),
	); err != nil {
		t.Fatalf("facts: %v", err)
	}
	if m["count"].(float64) != 4 {
		t.Fatalf("fact boxes = %.0f, want 4", m["count"].(float64))
	}
	if m["rows"].(float64) != 2 {
		t.Errorf("fact boxes occupy %.0f rows at 390, want 2 (two by two)", m["rows"].(float64))
	}
}

// TestTrackerPhoneCrumbsSeparatorE2E (R9): the hidden leading crumbs
// take their separator with them — the first visible crumb starts the
// trail (no leading "/").
func TestTrackerPhoneCrumbsSeparatorE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	var m map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/42"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const items = document.querySelectorAll('[data-cui-area="l:shell~crumbs"] .fui-breadcrumbs__item');
			let firstVisible = null, visibleCount = 0;
			for (const it of items) {
				if (getComputedStyle(it).display !== 'none') {
					if (!firstVisible) firstVisible = it;
					visibleCount++;
				}
			}
			const sep = firstVisible.querySelector('.fui-breadcrumbs__sep');
			return {visibleCount, firstSepDisplay: sep ? getComputedStyle(sep).display : 'no-sep'};
		})()`, &m),
	); err != nil {
		t.Fatalf("crumbs: %v", err)
	}
	if m["visibleCount"].(float64) != 2 {
		t.Errorf("visible crumbs = %.0f, want the last two", m["visibleCount"].(float64))
	}
	if m["firstSepDisplay"] != "none" {
		t.Errorf("the first visible crumb still shows its separator (%s)", m["firstSepDisplay"])
	}
}

// TestTrackerSlideTransitionSequentialE2E (S2): mid-transition the
// old and new issue snapshots used to cross at near-full opacity —
// two paragraphs of text read as one garbled block, and on a phone
// the new detail drew over the list and its search box. The generated
// Slide keyframes are sequential: the old snapshot reaches opacity 0
// by the midpoint, the new one holds 0 until then. The running
// animations are read with document.getAnimations() — the keyframes
// the browser actually parsed, not the CSS text — with the snapshot
// animations stretched to 30s (adopted stylesheet, CSP-clean) so the
// read lands mid-transition deterministically.
func TestTrackerSlideTransitionSequentialE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
		// Wait for the runtime so the click is a client navigation.
		chromedp.Poll(`!!(window.__gofastr && __gofastr.navigate)`, nil,
			chromedp.WithPollingTimeout(15*time.Second)),
		// Stretch every snapshot animation to 30s (the lab's labVTArm
		// trick). Constructable stylesheets are CSSOM, not inline style.
		chromedp.Evaluate(`(() => {
			const sheet = new CSSStyleSheet();
			// !important is load-bearing: a named ::view-transition-old(<name>)
			// selector carries pseudo-class specificity and beats a bare (*)
			// rule, so without it the generated legs run at their real
			// 150-220ms and are over before any read lands.
			sheet.replaceSync('::view-transition-group(*), ::view-transition-image-pair(*), ::view-transition-old(*), ::view-transition-new(*) { animation-duration: 30s !important; }');
			document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
		})()`, nil),
		// Real mouse click on the first issue (BIL-31).
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
		// A second issue: the project layer's primary slides.
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// The pseudo-element animations carry the keyframes the browser
	// parsed. This Chrome exposes them on document.getAnimations()
	// with the pseudo's ANIMATION NAME (vt-…-in / -out) and an empty
	// pseudoElement field, so the legs are matched by name.
	var frames map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Poll(`(() => {
			const vt = document.querySelector('.fui-list-detail [data-cui-vt]');
			if (!vt) return false;
			const name = vt.getAttribute('data-cui-vt');
			const anims = document.getAnimations().filter((a) =>
				a.animationName === name + '-in' || a.animationName === name + '-out');
			return anims.length === 2;
		})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`(() => {
			const name = document.querySelector('.fui-list-detail [data-cui-vt]').getAttribute('data-cui-vt');
			const out = {};
			for (const a of document.getAnimations()) {
				if (a.animationName !== name + '-in' && a.animationName !== name + '-out') continue;
				const leg = a.animationName === name + '-out' ? 'old' : 'new';
				out[leg] = a.effect.getKeyframes()
					.filter((k) => k.offset !== null && k.offset !== undefined && 'opacity' in k)
					.map((k) => ({offset: k.offset, opacity: parseFloat(k.opacity)}));
			}
			return out;
		})()`, &frames),
	); err != nil {
		t.Fatalf("read animations: %v", err)
	}

	opAt := func(leg string, off float64) (float64, bool) {
		for _, k := range frames[leg].([]any) {
			kf := k.(map[string]any)
			if o, _ := kf["offset"].(float64); o == off {
				v, _ := kf["opacity"].(float64)
				return v, true
			}
		}
		return 0, false
	}
	for _, leg := range []string{"old", "new"} {
		if _, ok := frames[leg]; !ok {
			t.Fatalf("no running %s snapshot animation for the project slide (frames=%v)", leg, frames)
		}
	}
	// Old: fully faded BY the midpoint and still 0 after it.
	if o, ok := opAt("old", 0.5); !ok || o > 0.05 {
		t.Errorf("old snapshot opacity at offset 0.5 = %v (found=%v), want 0 — it must be gone by the midpoint", o, ok)
	}
	if o, ok := opAt("old", 1); !ok || o > 0.05 {
		t.Errorf("old snapshot opacity at offset 1 = %v (found=%v), want 0", o, ok)
	}
	// New: 0 at the start and still ≤0.2 at the midpoint — between two
	// equal keyframes the value is constant, so it never shows early.
	if o, ok := opAt("new", 0); !ok || o > 0.2 {
		t.Errorf("new snapshot opacity at offset 0 = %v (found=%v), want 0", o, ok)
	}
	if o, ok := opAt("new", 0.5); !ok || o > 0.2 {
		t.Errorf("new snapshot opacity at offset 0.5 = %v (found=%v), want ≤ 0.2 until the midpoint", o, ok)
	}
	// And the nudge survives: the new leg's first keyframe carries the
	// 24px translate (the unit test pins the token; this proves the
	// slide leg still animates a transform at all).
	var hasTransform bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const name = document.querySelector('.fui-list-detail [data-cui-vt]').getAttribute('data-cui-vt');
		for (const a of document.getAnimations()) {
			if (a.animationName !== name + '-in') continue;
			return a.effect.getKeyframes().some((k) => k.offset === 0 && typeof k.transform === 'string' && k.transform !== 'none');
		}
		return false;
	})()`, &hasTransform)); err != nil {
		t.Fatalf("transform probe: %v", err)
	}
	if !hasTransform {
		t.Error("the new snapshot's first keyframe carries no translate — the slide nudge vanished")
	}
}

// TestTrackerActiveLinkMarksAtCommitE2E (S3, re-pinned for parts):
// during a navigation from BIL-31 to BIL-42 the kept list must mark
// the new row by the time ANY late content lands — under the parts
// transport that is the first gofastr:fill for the deferred aside
// part, which arrives after the page commit (streaming's old unit-0
// fill event no longer exists; gofastr:navigate now fires with the
// commit itself).
func TestTrackerActiveLinkMarksAtCommitE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/31"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
		// The idle module owns the sweep; wait for it so the test
		// cannot pass vacuously through a not-yet-loaded module.
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.activelink)`,
			nil, chromedp.WithPollingTimeout(15*time.Second)),
		// At the first fill for the deferred aside PART (it lands
		// after the commit, the "activity arrived later" case),
		// snapshot which list row carries aria-current.
		chromedp.Evaluate(`(() => {
			window.__marks = [];
			window.addEventListener('gofastr:fill', (e) => {
				if (!window.__marks.length && e.detail && e.detail.addr === 'l:shell#aside') {
					window.__marks = Array.from(document.querySelectorAll('.fui-list-detail__list a[aria-current="page"]'))
						.map((a) => a.getAttribute('href'));
				}
			});
		})()`, nil),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.Poll(`window.__marks.length > 0`, nil, chromedp.WithPollingTimeout(15*time.Second)),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	var marks []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__marks`, &marks)); err != nil {
		t.Fatalf("read marks: %v", err)
	}
	if len(marks) != 1 || marks[0] != "/projects/billing/issues/42" {
		t.Errorf("at the aside part's first fill the list marked %v, want exactly [/projects/billing/issues/42]", marks)
	}
}

// TestTrackerDrawerHeaderE2E (S8): at 390 the mobile drawer opens
// with a header row — the app's brand and a visible close button —
// instead of a bare link list pressed against the top edge. The close
// button closes the drawer and returns focus to the menu button;
// Escape closes it too.
func TestTrackerDrawerHeaderE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	const (
		triggerSel = `[data-cui-scope="appbar"] .fui-sidebar__hamburger`
		drawerSel  = `[data-cui-widget="ui-sidebar-drawer"]`
		closeSel   = drawerSel + ` .fui-sidebar__drawer-close`
	)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible("main", chromedp.ByQuery),
		// Open with a real click on the header's menu button.
		chromedp.Click(triggerSel, chromedp.ByQuery),
		chromedp.WaitVisible(drawerSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open drawer: %v", err)
	}

	// The header row: the brand the top bar shows, a visible 44px close
	// button, and the brand's left edge lining up with the first nav
	// row's icon (U4: the header used to start 16px from the edge
	// while the rows' icons start at 8px).
	var head map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const drawer = document.querySelector('`+drawerSel+`');
		const brand = drawer.querySelector('.fui-sidebar__drawer-brand');
		const close = drawer.querySelector('.fui-sidebar__drawer-close');
		const r = close.getBoundingClientRect();
		const icon = drawer.querySelector('.fui-sidebar__link svg');
		return {
			brand: brand ? brand.textContent.trim() : '',
			closeVisible: close.offsetParent !== null,
			closeSize: Math.round(r.width) + 'x' + Math.round(r.height),
			closeLabel: close.getAttribute('aria-label'),
			brandLeft: brand ? brand.getBoundingClientRect().left : -1,
			iconLeft: icon ? icon.getBoundingClientRect().left : -1,
		};
	})()`, &head)); err != nil {
		t.Fatalf("header read: %v", err)
	}
	if d := head["brandLeft"].(float64) - head["iconLeft"].(float64); math.Abs(d) > 1 {
		t.Errorf("drawer brand starts %.1fpx from the first nav row's icon (brand %.1f, icon %.1f) — one inline inset must line them up",
			d, head["brandLeft"].(float64), head["iconLeft"].(float64))
	}

	// The close button closes the drawer and returns focus to the
	// trigger. A real click, so the trigger held focus at open.
	var afterClose map[string]any
	if err := chromedp.Run(ctx,
		// Let the drawer's enter animation settle before the click.
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(closeSel, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const drawer = document.querySelector('`+drawerSel+`');
			const trigger = document.querySelector('`+triggerSel+`');
			return {open: !!drawer && !drawer.hasAttribute('hidden'),
				focusOnTrigger: document.activeElement === trigger};
		})()`, &afterClose),
	); err != nil {
		t.Fatalf("close click: %v", err)
	}
	if afterClose["open"].(bool) {
		t.Error("the close button did not close the drawer")
	}
	if !afterClose["focusOnTrigger"].(bool) {
		t.Error("focus did not return to the menu button after closing")
	}

	// Escape closes it too, with the same focus return.
	if err := chromedp.Run(ctx,
		chromedp.Click(triggerSel, chromedp.ByQuery),
		chromedp.WaitVisible(drawerSel, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const drawer = document.querySelector('`+drawerSel+`');
			const trigger = document.querySelector('`+triggerSel+`');
			return {open: !!drawer && !drawer.hasAttribute('hidden'),
				focusOnTrigger: document.activeElement === trigger};
		})()`, &afterClose),
	); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if afterClose["open"].(bool) {
		t.Error("Escape did not close the drawer")
	}
	if !afterClose["focusOnTrigger"].(bool) {
		t.Error("focus did not return to the menu button after Escape")
	}
}

// TestTrackerPhonePageFitE2E (S5): at 390 the stacked layout's
// min-height floor used to be a full 100dvh while the body also pads
// 40px for the fixed bottom bar — exactly one bar of empty scroll
// under short pages (/settings, /nope read scrollHeight 884). The
// floor now subtracts the bar; long pages still scroll to their last
// card, clear of the bar.
func TestTrackerPhonePageFitE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	for _, path := range []string{"/settings", "/nope"} {
		var m map[string]any
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.WaitVisible("main", chromedp.ByQuery),
			chromedp.Sleep(400*time.Millisecond),
			chromedp.Evaluate(`(() => ({
				path: location.pathname,
				scrollH: document.documentElement.scrollHeight,
				innerH: innerHeight,
			}))()`, &m),
		); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if m["scrollH"].(float64) != m["innerH"].(float64) {
			t.Errorf("%s: scrollHeight %.0f != innerHeight %.0f — a short page scrolls",
				path, m["scrollH"].(float64), m["innerH"].(float64))
		}
	}

	// A long page still scrolls, and its last card lands fully above
	// the fixed bar once scrolled to the end.
	var m map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/reports"),
		chromedp.WaitVisible("main h1", chromedp.ByQuery),
		chromedp.Sleep(2200*time.Millisecond), // the slow reports Load (~1.2s)
		chromedp.Evaluate(`(() => {
			const doc = document.documentElement;
			const scrolls = doc.scrollHeight > innerHeight;
			window.scrollTo(0, doc.scrollHeight);
			const cards = document.querySelectorAll('main .fui-card');
			const last = cards[cards.length - 1].getBoundingClientRect();
			return {scrolls, cards: cards.length, lastBottom: last.bottom, barTop: innerHeight,
				scrollH: doc.scrollHeight, innerH: innerHeight};
		})()`, &m),
	); err != nil {
		t.Fatalf("reports: %v", err)
	}
	if !m["scrolls"].(bool) {
		t.Errorf("/reports does not scroll at 390 (scrollHeight %.0f, innerHeight %.0f)",
			m["scrollH"].(float64), m["innerH"].(float64))
	}
	if m["cards"].(float64) < 2 {
		t.Fatalf("/reports rendered %.0f cards — the page did not load", m["cards"].(float64))
	}
	if m["lastBottom"].(float64) > m["barTop"].(float64) {
		t.Errorf("/reports last card bottom %.0f is under the bar (bar top %.0f) after scrolling to the end",
			m["lastBottom"].(float64), m["barTop"].(float64))
	}
}

// vtSnapshotOpacities reads the LIVE computed opacity of one name's
// old/new view-transition snapshots off the document element's
// pseudos (NaN when a leg is absent — the name has no such snapshot
// this navigation). Used by the round-4 mid-transition reads.
const vtSnapshotOpacities = `((name) => {
	const cs = (p) => parseFloat(getComputedStyle(document.documentElement, p).opacity);
	return {old: cs('::view-transition-old(' + name + ')'),
		fresh: cs('::view-transition-new(' + name + ')')};
})`

// TestTrackerMobilePaneSlideE2E (U1): at 390 the list and the detail
// are ONE pane, so the panes region carries the project slide's
// view-transition name and the detail cell drops its own. Pre-fix the
// detail cell carried the name at every width: its old "No issue
// selected" snapshot morphed its group geometry from the old box
// below the list to the new detail box, drawing over the list rows
// while the list itself was half faded through the root crossfade.
// The snapshot animations are stretched to 30s so the read lands
// mid-transition deterministically.
func TestTrackerMobilePaneSlideE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	// Name placement on first paint: the region owns the name below
	// the breakpoint, the placed cell does not. (Pre-fix the cell carried
	// the name at 390 — this read fails there.)
	var place map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const region = document.querySelector('.fui-list-detail');
			const cell = document.querySelector('.fui-list-detail [data-cui-layout-slot]');
			return {region: getComputedStyle(region).viewTransitionName,
				cell: getComputedStyle(cell).viewTransitionName,
				regionWhen: region.getAttribute('data-cui-vt-when')};
		})()`, &place),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if place["region"] != "vt-project-primary-slide" {
		t.Errorf("at 390 the panes region must carry the picked slide's keyed name, got %v", place["region"])
	}
	if place["cell"] != "none" {
		t.Errorf("at 390 the detail cell must drop the name (the old \"No issue selected\" snapshot would morph across the list), got %v", place["cell"])
	}

	// Mid-transition: stretch every snapshot animation to 30s, tap the
	// first issue with a real click, and read the RUNNING pair.
	var mid map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr && __gofastr.navigate)`, nil,
			chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`(() => {
			const sheet = new CSSStyleSheet();
			// !important is load-bearing: a named ::view-transition-old(<name>)
			// selector carries pseudo-class specificity and beats a bare (*)
			// rule, so without it the generated legs run at their real
			// 150-220ms and are over before any read lands.
			sheet.replaceSync('::view-transition-group(*), ::view-transition-image-pair(*), ::view-transition-old(*), ::view-transition-new(*) { animation-duration: 30s !important; }');
			document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
		})()`, nil),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.Poll(`(() => {
			const r = `+vtSnapshotOpacities+`('vt-project-primary-slide');
			return !Number.isNaN(r.old) && !Number.isNaN(r.fresh);
		})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		// Sample at ~40% progress (12s of the 30s stretch), where a
		// simultaneous crossfade has BOTH legs near half opacity while
		// a sequential handover still holds the new leg at 0 — reading
		// at the first frame would pass vacuously.
		chromedp.Sleep(12*time.Second),
		chromedp.Evaluate(`(() => {
			// Every framework-GENERATED transition must hand over
			// sequentially: while one leg of a pair is legible (>0.2)
			// the other must not be. Scoped to names with running
			// generated keyframes (…-in/…-out): a name-only escape
			// hatch (tracker-page) and the ROOT ride the plain
			// crossfade on purpose — the unchanged pixels behind them
			// would blink behind a sequential one.
			const declared = new Set();
			for (const el of document.querySelectorAll('[data-cui-vt]')) declared.add(el.getAttribute('data-cui-vt'));
			const names = new Set();
			for (const a of document.getAnimations()) {
				const m = a.animationName && a.animationName.match(/^(.*)-(in|out)$/);
				if (m && declared.has(m[1])) names.add(m[1]);
			}
			const cs = (p) => parseFloat(getComputedStyle(document.documentElement, p).opacity);
			const pairs = {};
			for (const n of names) pairs[n] = {old: cs('::view-transition-old(' + n + ')'), fresh: cs('::view-transition-new(' + n + ')')};
			const cell = document.querySelector('.fui-list-detail [data-cui-layout-slot]');
			return {pairs, cellName: getComputedStyle(cell).viewTransitionName,
				regionName: getComputedStyle(document.querySelector('.fui-list-detail')).viewTransitionName};
		})()`, &mid),
	); err != nil {
		t.Fatalf("tap + mid-read: %v", err)
	}
	for name, raw := range mid["pairs"].(map[string]any) {
		p := raw.(map[string]any)
		old, fresh := p["old"].(float64), p["fresh"].(float64)
		if math.IsNaN(old) || math.IsNaN(fresh) {
			t.Fatalf("group %s has no running snapshot pair mid-transition (%v/%v)", name, old, fresh)
		}
		if old > 0.2 && fresh > 0.2 {
			t.Errorf("group %s draws its old and new snapshots together mid-transition (old %.2f, new %.2f) — the handover must be sequential",
				name, old, fresh)
		}
	}
	// The mirror kept the name on the region only (a duplicate name
	// would make the browser skip the whole transition).
	if mid["cellName"] != "none" {
		t.Errorf("mid-transition the detail cell carries name %v at 390; only the region may", mid["cellName"])
	}
	if mid["regionName"] != "vt-project-primary-slide" {
		t.Errorf("mid-transition the region name = %v, want vt-project-primary-slide", mid["regionName"])
	}
}

// TestTrackerCrumbsFadeThroughE2E (U2): the crumb area carries its own
// SEQUENTIAL fade at every width, so the outgoing and incoming trails
// never draw at once (the root crossfade used to ghost "Billing /
// BIL-31" over "Projects / Billing" for a beat on every navigation).
// The root itself must stay a plain crossfade — its unchanged pixels
// would blink behind a sequential one.
func TestTrackerCrumbsFadeThroughE2E(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"desktop", 1280, 800},
		{"phone", 390, 844},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := trackerServe(t)
			ctx, _ := trackerTab(t, trackerBrowser(t, tc.width, tc.height), 120*time.Second)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/projects/billing"),
				chromedp.WaitVisible(crumbsSel, chromedp.ByQuery),
				chromedp.Poll(`!!(window.__gofastr && __gofastr.navigate)`, nil,
					chromedp.WithPollingTimeout(15*time.Second)),
				chromedp.Evaluate(`(() => {
					const sheet = new CSSStyleSheet();
					sheet.replaceSync('::view-transition-group(*), ::view-transition-image-pair(*), ::view-transition-old(*), ::view-transition-new(*) { animation-duration: 30s; }');
					document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
				})()`, nil),
				// A real click on the first issue changes the trail
				// (…/Billing → …/Billing / BIL-31).
				chromedp.Click(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
				chromedp.Poll(`(() => {
					const anims = document.getAnimations().filter((a) =>
						a.animationName === 'vt-shell-crumbs-in' || a.animationName === 'vt-shell-crumbs-out');
					return anims.length === 2;
				})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
			); err != nil {
				t.Fatalf("navigate/click: %v", err)
			}
			var frames map[string]any
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const out = {};
				for (const a of document.getAnimations()) {
					if (a.animationName !== 'vt-shell-crumbs-in' && a.animationName !== 'vt-shell-crumbs-out') continue;
					const leg = a.animationName === 'vt-shell-crumbs-out' ? 'old' : 'new';
					out[leg] = a.effect.getKeyframes()
						.filter((k) => k.offset !== null && k.offset !== undefined)
						.map((k) => ({offset: k.offset, opacity: parseFloat(k.opacity), transform: k.transform}));
				}
				return out;
			})()`, &frames)); err != nil {
				t.Fatalf("read crumb animations: %v", err)
			}
			opAt := func(leg string, off float64) (float64, bool) {
				for _, k := range frames[leg].([]any) {
					kf := k.(map[string]any)
					if o, _ := kf["offset"].(float64); o == off {
						v, _ := kf["opacity"].(float64)
						return v, true
					}
				}
				return 0, false
			}
			for _, leg := range []string{"old", "new"} {
				if _, ok := frames[leg]; !ok {
					t.Fatalf("no running %s crumb snapshot animation (frames=%v)", leg, frames)
				}
			}
			if o, ok := opAt("old", 0.5); !ok || o > 0.05 {
				t.Errorf("old crumb trail opacity at offset 0.5 = %v (found=%v), want 0 — gone by the midpoint", o, ok)
			}
			if o, ok := opAt("new", 0); !ok || o > 0.2 {
				t.Errorf("new crumb trail opacity at offset 0 = %v (found=%v), want 0", o, ok)
			}
			if o, ok := opAt("new", 0.5); !ok || o > 0.2 {
				t.Errorf("new crumb trail opacity at offset 0.5 = %v (found=%v), want ≤ 0.2 until the midpoint", o, ok)
			}
			// No nudge: a sequential FADE, not Slide.
			for _, leg := range []string{"old", "new"} {
				for _, k := range frames[leg].([]any) {
					if kf := k.(map[string]any); kf["transform"] != nil {
						t.Errorf("crumb %s leg carries a transform (%v) — the fade must not nudge", leg, kf["transform"])
					}
				}
			}
		})
	}
}

// TestTrackerPhoneOnePaneE2E preserves the tracker's single-pane
// phone view, including the unknown-issue screen and desktop empty detail.
func TestTrackerPhoneOnePaneE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 390, 844), 120*time.Second)

	var idx map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const panes = document.querySelector('.fui-list-detail');
			const cell = document.querySelector('.fui-list-detail__detail');
			const list = document.querySelector('.fui-list-detail__list');
			return {cellHidden: cell !== null && cell.offsetParent === null,
				listBottomGap: Math.abs(list.getBoundingClientRect().bottom - panes.getBoundingClientRect().bottom)};
		})()`, &idx),
	); err != nil {
		t.Fatalf("phone index: %v", err)
	}
	if !idx["cellHidden"].(bool) {
		t.Error("at 390 the unselected detail must not take space")
	}
	if gap, _ := idx["listBottomGap"].(float64); gap > 2 {
		t.Errorf("at 390 the list is not the last content block (bottom gap %.1fpx below it)", gap)
	}

	// An open issue hides the list; the back link is the way out.
	var issue map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr && __gofastr.navigate)`, nil,
			chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-31"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const list = document.querySelector('.fui-list-detail__list');
			const back = document.querySelector('.fui-list-detail__detail a.fui-button');
			return {listHidden: list.offsetParent === null,
				backVisible: back !== null && back.offsetParent !== null};
		})()`, &issue),
	); err != nil {
		t.Fatalf("phone issue: %v", err)
	}
	if !issue["listHidden"].(bool) {
		t.Error("at 390 an open issue must hide the list")
	}
	if !issue["backVisible"].(bool) {
		t.Error("the Back to issues link is not visible at 390")
	}

	// An unknown issue id still shows its pane.
	var missing map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/999"),
		chromedp.WaitVisible(`.fui-list-detail`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const list = document.querySelector('.fui-list-detail__list');
			const cell = document.querySelector('.fui-list-detail__detail');
			return {listHidden: list.offsetParent === null,
				detailVisible: cell !== null && cell.offsetParent !== null};
		})()`, &missing),
	); err != nil {
		t.Fatalf("phone unknown issue: %v", err)
	}
	if !missing["listHidden"].(bool) || !missing["detailVisible"].(bool) {
		t.Errorf("at 390 an unknown issue must show its detail pane (listHidden=%v detailVisible=%v)",
			missing["listHidden"], missing["detailVisible"])
	}

	// Desktop keeps the empty detail beside the list.
	dctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)
	var desk map[string]any
	if err := chromedp.Run(dctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const cell = document.querySelector('.fui-list-detail__detail');
			return {cellVisible: cell !== null && cell.offsetParent !== null,
				listVisible: document.querySelector('.fui-list-detail__list').offsetParent !== null};
		})()`, &desk),
	); err != nil {
		t.Fatalf("desktop index: %v", err)
	}
	if !desk["cellVisible"].(bool) {
		t.Error("at 1280 the empty detail must stay beside the list")
	}
	if !desk["listVisible"].(bool) {
		t.Error("at 1280 the issue list must be visible")
	}
	var desktopBack bool
	if err := chromedp.Run(dctx,
		chromedp.Navigate(base+"/projects/billing/issues/31"),
		chromedp.WaitVisible("#issue-detail", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('.fui-list-detail__back a.fui-button').getBoundingClientRect().height > 0`, &desktopBack),
	); err != nil {
		t.Fatalf("desktop issue: %v", err)
	}
	if desktopBack {
		t.Error("the phone Back link must not take space beside the desktop list")
	}
}

// crumbsProbe reads the crumbs area's loading state: its loadstate
// attribute, whether the skeleton line shows inside it, and its text.
func crumbsProbe(t *testing.T, ctx context.Context) (state string, skeleton bool, text string) {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		return JSON.stringify({
			s: el.getAttribute('data-cui-loadstate') || '',
			k: !!el.querySelector('.fui-skeleton-line'),
			t: el.textContent.trim(),
		});
	})()`, crumbsSel), &out)); err != nil {
		t.Fatalf("probe crumbs: %v", err)
	}
	var v struct {
		S string `json:"s"`
		K bool   `json:"k"`
		T string `json:"t"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	return v.S, v.K, v.T
}

// TestTrackerCrumbSkeletonE2E: the crumbs area declares loading
// content (one SkeletonLine, After 150ms). A slow navigation (Reports
// takes ~1.2s in Load) must show the skeleton line in place of the old
// trail past After and land the destination's fresh trail after; a
// fast navigation that beats After must never show it.
func TestTrackerCrumbSkeletonE2E(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`main h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate home: %v", err)
	}

	// Slow leg: mid-flight (600ms of the 1.2s Load) the trail is the
	// skeleton line, the region is marked shown.
	if err := chromedp.Run(ctx,
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/reports')`, nil),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("navigate reports: %v", err)
	}
	state, skeleton, _ := crumbsProbe(t, ctx)
	if state != "shown" {
		t.Errorf("crumbs data-cui-loadstate during slow flight = %q, want \"shown\"", state)
	}
	if !skeleton {
		t.Error("crumbs during slow flight show no skeleton line; the area's loading content must clone in past After")
	}

	// Settle: the fresh trail replaces the skeleton, the mark clears.
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('main h1') && document.querySelector('main h1').textContent.includes('Reports')`, nil),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("settle reports: %v", err)
	}
	state, skeleton, text := crumbsProbe(t, ctx)
	if state != "" || skeleton {
		t.Errorf("crumbs after settle: state=%q skeleton=%v, want both cleared", state, skeleton)
	}
	if !strings.Contains(text, "Reports") {
		t.Errorf("crumbs after settle = %q, want the Reports trail", text)
	}

	var out2 string
	// Fast leg: /settings answers well inside After; a rAF sampler
	// must see ZERO frames with the crumbs area showing loading
	// content. (The deferred aside's own loadstate mark is out of
	// scope: its placeholder travels inside the applied page.)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`(() => {
			window.__cs = { shown: 0, frames: 0, done: false };
			const tick = () => {
				const el = document.querySelector(%q);
				if (el && (el.getAttribute('data-cui-loadstate') || el.querySelector('.fui-skeleton-line'))) window.__cs.shown++;
				window.__cs.frames++;
				if (!window.__cs.done) requestAnimationFrame(tick);
			};
			requestAnimationFrame(tick);
			return true;
		})()`, crumbsSel), nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/settings')`, nil),
		chromedp.Poll(`document.querySelector('main h1') && document.querySelector('main h1').textContent.includes('Settings')`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__cs.done = true`, nil),
		chromedp.Evaluate(`JSON.stringify(window.__cs)`, &out2),
	); err != nil {
		t.Fatalf("fast nav: %v", err)
	}
	var s struct {
		Shown  int `json:"shown"`
		Frames int `json:"frames"`
	}
	if err := json.Unmarshal([]byte(out2), &s); err != nil {
		t.Fatal(err)
	}
	if s.Frames == 0 {
		t.Fatal("sampler never ran")
	}
	if s.Shown != 0 {
		t.Errorf("fast navigation painted %d frames of crumbs loading content; After must keep fast responses flicker-free (frames=%d)", s.Shown, s.Frames)
	}
}
