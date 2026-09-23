package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdnetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Browser proofs for /examples/headless/{default,dense}/dashboard —
// the form family on a real surface. The journey is the hardest
// transition the family owns: a submit that fails server-side, where
// the island answer must re-render the region, mark the failing
// control and MOVE FOCUS to the summary; nested conditional regions
// that must hide and disable until their watched fields match; and the
// upload's hooks present beside the password shell.

func TestE2E_DashboardSettingsJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// The page renders the form; the module arms and hides the
	// conditional regions whose watched fields do not match (notify
	// defaults to none), and disables what it hides.
	var outerHidden, hookDisabled bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/headless/default/dashboard"),
		pageReady(),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const regions = document.querySelectorAll('#hd-settings [data-hui-when]');
			return regions.length >= 2 && regions[0].hidden && regions[1].hidden;
		})()`, &outerHidden),
		chromedp.Evaluate(`document.querySelector('input[name="webhook"]').disabled`, &hookDisabled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !outerHidden {
		t.Fatal("the conditional regions should be hidden while notify is none (the module hides what does not match)")
	}
	if !hookDisabled {
		t.Error("the webhook control inside the hidden region should be disabled, so nothing hidden submits")
	}

	// Choose webhook: the outer region shows, the inner one stays
	// hidden (its checkbox is unchecked), and its control comes back.
	var outerShown, innerStillHidden, hookEnabled bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('input[name="notify"][value="webhook"]').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(`!document.querySelectorAll('#hd-settings [data-hui-when]')[0].hidden`, &outerShown),
		chromedp.Evaluate(`document.querySelectorAll('#hd-settings [data-hui-when]')[1].hidden`, &innerStillHidden),
		chromedp.Evaluate(`!document.querySelector('input[name="webhook"]').disabled`, &hookEnabled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !outerShown || !innerStillHidden {
		t.Fatalf("after choosing webhook: outer shown=%v inner hidden=%v, want true/true", outerShown, innerStillHidden)
	}
	if !hookEnabled {
		t.Error("the webhook control stayed disabled after its region showed")
	}

	// Check retries: the NESTED region shows.
	var innerShown bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('hd-retries').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(`!document.querySelectorAll('#hd-settings [data-hui-when]')[1].hidden`, &innerShown),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !innerShown {
		t.Fatal("checking retries never showed the nested region")
	}

	// Submit with a blank display name (the form is novalidate, the
	// server owns validation): the island answer re-renders the region
	// with the error, the summary is focused, the control is marked,
	// and the URL never moved.
	var focused, marked, urlAfter, summaryText string
	if err := chromedp.Run(ctx,
		chromedp.Click(`#hd-settings button[type="submit"]`, chromedp.ByQuery),
		waitModule(`!!document.getElementById('hd-settings-errors')`),
		chromedp.Evaluate(`(document.activeElement || {}).id || document.activeElement.tagName`, &focused),
		chromedp.Evaluate(`document.getElementById('hd-display').getAttribute('aria-invalid') || ''`, &marked),
		chromedp.Evaluate(`location.pathname+location.search`, &urlAfter),
		chromedp.Evaluate(`(document.getElementById('hd-settings-errors') || {}).textContent || ''`, &summaryText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(focused, "hd-settings-errors") {
		t.Errorf("focus after the failed submit is %q, want the validation summary", focused)
	}
	if marked != "true" {
		t.Errorf("the failing control carries aria-invalid=%q, want true", marked)
	}
	if urlAfter != "/examples/headless/default/dashboard" {
		t.Errorf("the island submit navigated: url is %q", urlAfter)
	}
	if !strings.Contains(summaryText, "display name") {
		t.Errorf("the summary does not name the display error: %q", summaryText)
	}

	// Fix the name and submit again: the region answers with the
	// success callout.
	var done bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const input = document.getElementById('hd-display');
			input.value = 'Ada';
		})()`, nil),
		chromedp.Click(`#hd-settings button[type="submit"]`, chromedp.ByQuery),
		waitModule(`!!document.getElementById('hd-settings-done')`),
		chromedp.Evaluate(`document.getElementById('hd-settings-done') !== null`, &done),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !done {
		t.Error("the valid submit never rendered the success callout")
	}
}

// The settings handler's two faces, at the router level: the island
// POST answers 200 with the re-rendered region (the errors ARE the
// answer), and the no-script POST redirects 303 with the outcome alone
// in the query — never a typed value.
func TestHeadlessSettingsHandlerAnswersBothWays(t *testing.T) {
	island := httptest.NewRequest(http.MethodPost, dashboardSettingsPath,
		strings.NewReader(`{"display":"","notify":"none","theme":"default"}`))
	island.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	serveHeadlessSettings(rec, island)
	if rec.Code != http.StatusOK {
		t.Fatalf("island POST answered %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hd-settings-errors") {
		t.Errorf("the island answer carries no validation summary:\n%s", body)
	}
	if !strings.Contains(body, `aria-invalid="true"`) {
		t.Errorf("the island answer does not mark the failing control:\n%s", body)
	}

	native := httptest.NewRequest(http.MethodPost, dashboardSettingsPath,
		strings.NewReader("display=&notify=none&theme=dense"))
	native.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	serveHeadlessSettings(rec, native)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("native POST answered %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), dashboardRoutePath("dense")+"?settings=invalid-name"; got != want {
		t.Fatalf("redirect is %q, want %q", got, want)
	}
}

// TestHeadlessDashboardComposesOnEveryRoute pins the dashboard's
// composition at the SSR level, per registered theme: the
// RecordSummary leads, its MetricBand carries four signals, the usage
// chart is named by its visible heading and echoed by the text
// alternative, the invoice table renders five rows with a sortable
// column, and the settings form is still the last section, unchanged.
func TestHeadlessDashboardComposesOnEveryRoute(t *testing.T) {
	for _, r := range landingRoutes {
		page := body(t, dashboardRoutePath(r.Segment))
		for _, probe := range []struct {
			id   string
			what string
		}{
			{"hd-summary", "the RecordSummary"},
			{"hd-summary-metrics", "the MetricBand"},
			{"hd-usage-chart", "the usage chart"},
			{"hd-invoices-table", "the invoice table"},
			{"hd-settings", "the settings form"},
		} {
			if !strings.Contains(page, `id="`+probe.id+`"`) {
				t.Errorf("%s: %s (%s) missing from the render", r.Segment, probe.what, probe.id)
			}
		}
		// The chart is named by its visible heading, not left unnamed.
		if !strings.Contains(page, `aria-labelledby="hd-usage-title"`) {
			t.Errorf("%s: the chart is not named through the section heading's id", r.Segment)
		}
		// The text alternative repeats all twelve values.
		for _, month := range dashboardUsageMonths {
			if !strings.Contains(page, month+" requests") {
				t.Errorf("%s: the chart's text alternative misses %s", r.Segment, month)
			}
		}
		// Five invoice rows, all five fixture numbers.
		for _, inv := range dashboardInvoices {
			if !strings.Contains(page, inv.Number) {
				t.Errorf("%s: invoice %s missing from the table", r.Segment, inv.Number)
			}
		}
		// The settings section is still headed "Account settings".
		if !strings.Contains(page, "Account settings") {
			t.Errorf("%s: the Account settings heading is gone", r.Segment)
		}
	}
}

// TestHeadlessDashboardInvoiceSorting pins the sort at the router
// level, both directions of both sortable columns, plus the island
// endpoint's face and its unknown-theme refusal.
func TestHeadlessDashboardInvoiceSorting(t *testing.T) {
	cases := []struct {
		query       string
		first, last string
	}{
		{"", "INV‑018", "INV‑014"},                      // default: newest first
		{"?sort=issued&dir=asc", "INV‑014", "INV‑018"},  // oldest first
		{"?sort=issued&dir=desc", "INV‑018", "INV‑014"}, // newest first
		{"?sort=amount&dir=asc", "INV‑014", "INV‑018"},  // $147 first, $196 last
		{"?sort=amount&dir=desc", "INV‑018", "INV‑014"}, // $196 first
	}
	for _, c := range cases {
		page := body(t, dashboardRoutePath("soft")+c.query)
		first := strings.Index(page, c.first)
		last := strings.Index(page, c.last)
		if first == -1 || last == -1 {
			t.Fatalf("query %q: invoice numbers missing from the page", c.query)
		}
		if first > last {
			t.Errorf("query %q: %s renders before %s — the sort did not apply", c.query, c.first, c.last)
		}
		// The active column announces its direction.
		if c.query != "" && !strings.Contains(page, `aria-sort="ascending"`) && !strings.Contains(page, `aria-sort="descending"`) {
			t.Errorf("query %q: no th carries aria-sort", c.query)
		}
	}

	// The island endpoint answers the re-rendered table, sorted.
	// Through the site's router: the {theme} path value is the
	// router's to set, and this also proves the endpoint is mounted.
	rec := serve(t, http.MethodGet, "/__site/headless/invoices/editorial?sort=amount&dir=asc")
	if rec.Code != http.StatusOK {
		t.Fatalf("island GET answered %d, want 200", rec.Code)
	}
	if i, j := strings.Index(rec.Body.String(), "INV‑014"), strings.Index(rec.Body.String(), "INV‑018"); i == -1 || i > j {
		t.Errorf("island GET did not answer the ascending-amount table:\n%s", rec.Body.String())
	}
	// An unknown theme is refused, the same line the sibling handlers hold.
	if got := serve(t, http.MethodGet, "/__site/headless/invoices/retro?sort=amount&dir=asc").Code; got != http.StatusBadRequest {
		t.Errorf("island GET with unknown theme answered %d, want 400", got)
	}
}

// TestE2E_HeadlessDashboard_SortLinkRoundTripsNoScript blocks the
// runtime the way a reader without script experiences the page, clicks
// the Amount sort anchor, and watches the native navigation land back
// on the same page carrying ?sort=amount&dir=asc with the table
// re-rendered in that order.
func TestE2E_HeadlessDashboard_SortLinkRoundTripsNoScript(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var afterURL, firstRow string
	err := chromedp.Run(ctx,
		cdnetwork.Enable(),
		cdnetwork.SetBlockedURLs().WithURLPatterns([]*cdnetwork.BlockPattern{
			{URLPattern: "*://*:*/*runtime.js*", Block: true},
			{URLPattern: "*://*:*/*__gofastr/runtime/*", Block: true},
		}),
		chromedp.Navigate(base+dashboardRoutePath("contrast")),
		pageReady(),
		// The inactive Amount column's first click sorts ascending.
		chromedp.Click(`#hd-invoices-table th a[href*="sort=amount"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#hd-invoices-table`, chromedp.ByQuery),
		chromedp.Location(&afterURL),
		chromedp.Evaluate(`document.querySelector('#hd-invoices-table tbody tr td').textContent.trim()`, &firstRow),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	wantPrefix := base + dashboardRoutePath("contrast") + "?"
	if !strings.HasPrefix(afterURL, wantPrefix) || !strings.Contains(afterURL, "sort=amount") || !strings.Contains(afterURL, "dir=asc") {
		t.Fatalf("after the sort click the browser is at %q, want %s…sort=amount&dir=asc — without the runtime the anchor must navigate", afterURL, wantPrefix)
	}
	if !strings.HasPrefix(firstRow, "INV‑014") {
		t.Errorf("ascending amount puts %q in the first row, want INV‑014 ($147.00)", firstRow)
	}
}
