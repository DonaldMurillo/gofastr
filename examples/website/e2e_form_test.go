package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Pagination + Breadcrumbs + FormField (framework/ui)
// =============================================================================

func TestE2E_Pagination_FirstPagePrevDisabled(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// We render an example with current=1; the prev item should have
	// the is-disabled class and contain a span with aria-disabled=true.
	var disabledCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pagination"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.pagination .is-disabled [aria-disabled="true"]').length`, &disabledCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if disabledCount < 1 {
		t.Errorf("expected at least one disabled boundary control on the demo page, got %d", disabledCount)
	}
}

// TestE2E_Pagination_IslandMode_NoFullReload pins the island behavior:
// clicking a pagination button must NOT reload the page (the runtime
// hydration state persists), MUST fire an RPC to the island endpoint
// (not a page-nav fetch), MUST update the URL via pushState, and MUST
// swap the rendered active page indicator.
//
// Implementation note: chromedp.Evaluate doesn't await Promises by
// default, so we install the fetch interceptor synchronously, fire
// each click in its own Evaluate, sleep between to let the RPC
// resolve, then read the captured state at the end.
func TestE2E_Pagination_IslandMode_NoFullReload(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Step 1: navigate + install an interceptor that captures every
	// fetch URL onto window.__capturedRequests.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pagination?p=1"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            document.documentElement.setAttribute('data-island-marker', 'before');
            window.__capturedRequests = [];
            window.__partialNavCount = 0;
            const origFetch = window.fetch;
            window.fetch = function(...args) {
                const url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url);
                if (url) window.__capturedRequests.push(url);
                const init = args[1] || {};
                if (init.headers && init.headers['X-Gofastr-Navigate'] === '1') window.__partialNavCount++;
                return origFetch.apply(this, args);
            };
            return true;
        })()`, nil),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Step 2: click each pagination button in sequence, sleeping
	// between clicks so the RPC resolves and the runtime applies the
	// signal + pushState before we move on.
	for _, p := range []int{3, 5, 2} {
		click := fmt.Sprintf(
			`document.querySelector('button[data-fui-rpc*="?p=%d"]').click(); true;`, p)
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(click, nil),
			chromedp.Sleep(300*time.Millisecond),
		); err != nil {
			t.Fatalf("click p=%d: %v", p, err)
		}
	}

	// Step 3: read captured state.
	var captured struct {
		MarkerSurvived  bool     `json:"markerSurvived"`
		Requests        []string `json:"requests"`
		PartialNavCount int      `json:"partialNavCount"`
		FinalURL        string   `json:"finalURL"`
		ActivePage      string   `json:"activePage"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`({
            markerSurvived: document.documentElement.getAttribute('data-island-marker') === 'before',
            requests: (window.__capturedRequests || []).filter(u => u && u.includes('/islands/')),
            partialNavCount: window.__partialNavCount || 0,
            finalURL: location.href,
            activePage: (document.querySelector('.pagination [aria-current="page"]') || {}).textContent || ''
        })`, &captured),
	); err != nil {
		t.Fatalf("read state: %v", err)
	}

	if !captured.MarkerSurvived {
		t.Errorf("page reloaded during island clicks (marker on <html> was wiped)")
	}
	if len(captured.Requests) != 3 {
		t.Errorf("expected 3 island RPCs, got %d: %v", len(captured.Requests), captured.Requests)
	}
	for _, u := range captured.Requests {
		if !strings.Contains(u, "/islands/pagination-demo/page") {
			t.Errorf("unexpected RPC URL: %q", u)
		}
	}
	if captured.PartialNavCount != 0 {
		t.Errorf("island clicks should NOT trigger X-Gofastr-Navigate fetches, got %d", captured.PartialNavCount)
	}
	if !strings.Contains(captured.FinalURL, "?p=2") {
		t.Errorf("expected final URL to end ?p=2 (pushState), got %q", captured.FinalURL)
	}
	if captured.ActivePage != "2" {
		t.Errorf("expected active page = 2, got %q", captured.ActivePage)
	}
}

func TestE2E_Pagination_PageLinkPointsAtCorrectURL(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hrefs []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pagination"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.pagination a[aria-current="page"]')).map(a => a.getAttribute('href'))`, &hrefs),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for _, h := range hrefs {
		if !strings.HasPrefix(h, "?p=") {
			t.Errorf("aria-current href should match pattern '?p=N', got %q", h)
		}
	}
}

func TestE2E_Breadcrumbs_AriaCurrentIsExactlyOne(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var current int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/breadcrumbs"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.breadcrumbs [aria-current="page"]').length`, &current),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if current != 1 {
		t.Errorf("expected exactly 1 aria-current=\"page\" in breadcrumbs, got %d", current)
	}
}

func TestE2E_FormField_ErrorStateChangesBorderColor(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// On the framework-ui page, the email field is rendered with an
	// Error message, which should give .ui-form-field.is-error a
	// red border on the input.
	var errorBorder, normalBorder string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-form-field.is-error input')).borderColor`, &errorBorder),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-form-field:not(.is-error) input')).borderColor`, &normalBorder),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if errorBorder == "" || normalBorder == "" {
		t.Fatalf("computed borderColor empty — CSS may not have loaded (error=%q normal=%q)",
			errorBorder, normalBorder)
	}
	if errorBorder == normalBorder {
		t.Errorf("error-state border should differ from normal; both = %q", errorBorder)
	}
}

func TestE2E_FormField_LabelWiresToInputViaForAttr(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var pairs []map[string]string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-form-field')).map(f => ({
            forAttr: f.querySelector('label').getAttribute('for'),
            inputId: (f.querySelector('input,textarea,select')||{}).id || ''
        }))`, &pairs),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatalf("no form fields found")
	}
	for i, p := range pairs {
		if p["forAttr"] == "" || p["forAttr"] != p["inputId"] {
			t.Errorf("FormField[%d]: label.for=%q != input.id=%q", i, p["forAttr"], p["inputId"])
		}
	}
}
