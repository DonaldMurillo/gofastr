package main

import (
	"strings"
	"testing"

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
