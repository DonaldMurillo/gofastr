package main

// The resolution-driven tracker cases (spike/layout-resolve): the ONE
// param group and its project resolver, the per-project transition
// pick, the Legacy archived guard's alt (covered by the updated
// TestTrackerLegacyAsideE2E), the malformed-record activity boundary
// through the deferred part, the unavailable-store 500 page inside the
// root layout, and the down exporter's 503 toast.

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTrackerProjectResolverGatesSlug: the group's project resolver is
// eager — an unknown slug is the not-found page (through the shell),
// a known one renders.
func TestTrackerProjectResolverGatesSlug(t *testing.T) {
	base := trackerServe(t)
	for _, tc := range []struct {
		path   string
		status int
		want   string
	}{
		{"/projects/billing", 200, "Billing"},
		{"/projects/unknown-project", 404, "Page not found"},
		{"/projects/unknown-project/issues/1", 404, "Page not found"},
	} {
		res, err := http.Get(base + tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		body := readAll(t, res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, res.StatusCode, tc.status)
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: body misses %q", tc.path, tc.want)
		}
	}
}

// TestTrackerProjectLayerKeyedByProject: ONE group at
// /projects/{project} — between one project's pages the layer is KEPT
// (the typed filter survives), between projects it RE-RENDERS (the
// new project's header and list replace it).
func TestTrackerProjectLayerKeyedByProject(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate billing: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.SendKeys(filterSel, "retry")); err != nil {
		t.Fatalf("filter: %v", err)
	}
	// Same project, deeper: kept.
	if err := chromedp.Run(ctx,
		chromedp.Click(`a.fui-card[data-key][data-key="BIL-42"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open BIL-42: %v", err)
	}
	var kept string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('filter-search-filter').value`, &kept)); err != nil {
		t.Fatal(err)
	}
	if kept != "retry" {
		t.Fatalf("same project must keep the layer (filter = %q, want retry)", kept)
	}
	// Another project: the layer re-renders under the new key.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-content-row__nav a[href="/projects/search"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`main h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate search: %v", err)
	}
	waitText(t, ctx, `main h1`, "Search")
	var reset string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('filter-search-filter').value`, &reset)); err != nil {
		t.Fatal(err)
	}
	if reset != "" {
		t.Fatalf("another project must re-render the layer (filter = %q, want empty)", reset)
	}
	var key string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.querySelector('[data-cui-layout-key^="g:/projects/"]');
		return el ? el.getAttribute('data-cui-layout-key') : '!missing';
	})()`, &key)); err != nil {
		t.Fatal(err)
	}
	if key != "g:/projects/search/:project" {
		t.Fatalf("layer key = %q, want the search-resolved key", key)
	}
}

// TestTrackerProjectTransitionsFollowProject: the issue detail's move
// is picked from the resolved project — the page answer's
// X-Gofastr-Transition names it, and the gofastr:transition event's
// types carry it beside the direction.
func TestTrackerProjectTransitionsFollowProject(t *testing.T) {
	base := trackerServe(t)

	// The header, per project (deterministic, no animation needed).
	for _, tc := range []struct{ path, want string }{
		{"/projects/billing/issues/42", "slide"},
		{"/projects/search/issues/15", "fade-through"},
		{"/projects/auth/issues/3", "crossfade"},
		{"/projects/legacy/issues/12", "fade"},
	} {
		req, _ := http.NewRequest(http.MethodGet, base+tc.path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-From", "/projects/billing")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		readAll(t, res.Body)
		res.Body.Close()
		if got := res.Header.Get("X-Gofastr-Transition"); got != tc.want {
			t.Errorf("%s: X-Gofastr-Transition = %q, want %q", tc.path, got, tc.want)
		}
	}

	// And through the browser: a cross-project click carries the
	// destination's pick beside 'forward'.
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 120*time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate billing: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__vtTypes = [];
		document.addEventListener('gofastr:transition', (e) => window.__vtTypes.push(e.detail.types.slice()));
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-content-row__nav a[href="/projects/search"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`main h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate search: %v", err)
	}
	waitText(t, ctx, `main h1`, "Search")
	var types [][]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__vtTypes || []`, &types)); err != nil {
		t.Fatal(err)
	}
	if len(types) == 0 || strings.Join(types[len(types)-1], ",") != "forward,fade-through" {
		t.Fatalf("transition types = %v, want the last to be [forward fade-through]", types)
	}
}

// TestTrackerMalformedActivityBoundary: BIL-63's activity loader
// panics on the one malformed record — the deferred PART answers 200
// with the panel's ErrorBoundary notice, and the page around it is
// intact.
func TestTrackerMalformedActivityBoundary(t *testing.T) {
	base := trackerServe(t)

	// The part request itself: 200, the boundary's short notice. A
	// session cookie rides along (the page request mints it; the part
	// never does — a cookie-less part is the 409 reset).
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	if page, err := client.Get(base + "/projects/billing"); err != nil {
		t.Fatal(err)
	} else {
		page.Body.Close()
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/projects/billing/issues/63", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Part", "l:shell#aside")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("part status = %d, want 200 (the boundary IS the answer)", res.StatusCode)
	}
	if !strings.Contains(body, "Activity could not load") {
		t.Fatalf("part body misses the boundary notice: %s", body)
	}
	if strings.Contains(body, "malformed event payload") {
		t.Fatalf("the panic text must not reach the body: %s", body)
	}

	// In the browser: the page renders, the aside lands the notice.
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 60*time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/billing/issues/63"),
		chromedp.WaitVisible(`#issue-detail h2`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate BIL-63: %v", err)
	}
	waitText(t, ctx, asideSel, "Activity could not load")
	if got := readText(t, ctx, `#issue-detail h2`); !strings.Contains(got, "Dunning emails stop") {
		t.Fatalf("the issue itself renders (title = %q)", got)
	}
}

// TestTrackerLegacyStoreUnavailable500: LEG-40's issue store is down —
// the page answer is the 500 error page THROUGH the root layout (no
// error text in the body), reached by clicking the issue in the list.
func TestTrackerLegacyStoreUnavailable500(t *testing.T) {
	base := trackerServe(t)

	// The full load: status 500, the shell around the error body.
	res, err := http.Get(base + "/projects/legacy/issues/40")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res.Body)
	res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
	for _, want := range []string{"Something went wrong", "Back to overview", "fui-sidebar__inline"} {
		if !strings.Contains(body, want) {
			t.Errorf("500 page misses %q (the root layout must wrap the tracker's error screen)", want)
		}
	}
	if strings.Contains(body, "issue store is unavailable") {
		t.Errorf("the error text must not reach the body")
	}

	// By click: the Legacy list's LEG-40 row, a soft navigation — the
	// partial's 500 body applies inside the live shell.
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 60*time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/projects/legacy"),
		chromedp.WaitVisible(filterSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate legacy: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`a.fui-card[data-key][data-key="LEG-40"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click LEG-40: %v", err)
	}
	// The click launched the route's deferred-aside part beside the
	// page request; BOTH see the unavailable store, so the part answers
	// the 409 reset and the runtime reloads the URL as a whole document
	// (once) — the wait must tolerate that document swap.
	waitPageText(t, ctx, `main h1`, "Something went wrong")
	// The tracker's own error screen, not the framework default: the
	// same empty-state card the not-found page uses, with its
	// "Back to overview" action.
	if got := readText(t, ctx, `main a.fui-button`); !strings.Contains(got, "Back to overview") {
		t.Errorf("the error screen's action = %q, want \"Back to overview\"", got)
	}
	if got := readText(t, ctx, `main`); strings.Contains(got, "issue store is unavailable") {
		t.Errorf("the error text must not reach the rendered page (main = %.200q)", got)
	}
	if got := readText(t, ctx, `.fui-content-row__nav`); !strings.Contains(got, "Projects") {
		t.Fatalf("the shell must survive the error page (sidebar = %q)", got)
	}
}

// waitPageText polls an element's text across document navigations
// (WaitVisible errors with -32000 when the tab navigates mid-wait).
func waitPageText(t *testing.T, ctx context.Context, sel, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var txt string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(() => { const el = document.querySelector('`+sel+`'); return el ? el.textContent.trim() : ''; })()`, &txt)); err == nil && strings.Contains(txt, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting %s ~ %q (last %q)", sel, want, readText(t, ctx, sel))
}

// TestTrackerExportDownToasts: Reports' Export link while the exporter
// is down — the soft navigation's 503 text/plain answer surfaces the
// runtime's toast naming the status, and the URL stays on Reports.
func TestTrackerExportDownToasts(t *testing.T) {
	base := trackerServe(t)
	ctx, _ := trackerTab(t, trackerBrowser(t, 1280, 800), 60*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/reports"),
		chromedp.WaitVisible(`main h1`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate reports: %v", err)
	}
	// A settle beat: the reports page's own view transition must finish
	// before the click — the runtime re-delivers a click that lands
	// mid-transition, and the re-delivery is not what we are testing.
	time.Sleep(700 * time.Millisecond)
	// The no-reload marker: a document reload wipes it (nothing on the
	// reports page sets it back), so reading it after the toast proves
	// the blocked navigation never reloaded — the 409 part-reset race.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__noReload = true`, nil)); err != nil {
		t.Fatalf("set no-reload marker: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`a[href="/reports/export"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click export: %v", err)
	}
	// The toast shows for four seconds; poll its text (WaitVisible can
	// miss the whole window under suite load).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var toast string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(() => { const t = document.getElementById('cui-nav-toast'); return t ? t.textContent : ''; })()`, &toast)); err == nil && strings.Contains(toast, "503") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := readText(t, ctx, `#cui-nav-toast`); !strings.Contains(got, "503") {
		t.Fatalf("the toast never named the 503 (last read %q)", got)
	}
	if got := readText(t, ctx, `#cui-nav-toast`); !strings.Contains(got, "/reports/export") {
		t.Fatalf("the toast names the target: %q", got)
	}
	var path string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &path)); err != nil {
		t.Fatal(err)
	}
	if path != "/reports" {
		t.Fatalf("a failed navigation reverts the URL (at %q)", path)
	}
	// Hard rule 11: prove the assertion bites — the part's 409 reset
	// racing the toast would reload AFTER the reads above, so give the
	// race its window, then require the marker to have survived.
	time.Sleep(1500 * time.Millisecond)
	var noReload bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__noReload === true`, &noReload)); err != nil {
		t.Fatalf("read no-reload marker: %v", err)
	}
	if !noReload {
		t.Fatal("the blocked navigation reloaded the document (the no-reload marker was wiped)")
	}
}

// readAll drains and closes-free reads a body (the caller closes).
func readAll(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
