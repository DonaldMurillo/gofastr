package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
