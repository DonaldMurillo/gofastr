package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Pagination + Breadcrumbs
// =============================================================================
//
// DROPPED: TestE2E_FormField_ErrorStateChangesBorderColor and
//          TestE2E_FormField_LabelWiresToInputViaForAttr
// Both targeted /framework-ui/ which does NOT exist in the site. No
// equivalent page renders standalone FormField error demos.
//
// DROPPED: TestE2E_Pagination_IslandMode_NoFullReload
// The site's /components/pagination page renders the plain pager: the
// gallery's ui.PaginationConfig leaves the Island zero, so the page
// anchors are plain navigations and no island RPC exists to keep
// whole. The website's island RPC at /islands/pagination-demo/page has
// no equivalent registered route in site. Dropping this sub-test
// rather than faking it.
//
// SOFTENED: TestE2E_Pagination_PageLinkPointsAtCorrectURL
// The site's pagination demo names its page parameter "page"
// (ui.PaginationConfig{PageParam: "page"}), not "p", so the assertion
// is updated to match what the site actually renders. The
// "aria-current" link test still holds; the href check accepts any
// "?page=" prefix.

func TestE2E_Pagination_FirstPagePrevDisabled(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// The site renders a static demo that includes an atFirst variant
	// (Page 1 in the catalog Demo). The pager is ui.Pagination now: a
	// disabled boundary is the anchor itself saying aria-disabled,
	// not a span inside an .is-disabled item.
	var disabledCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pagination"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.pagination a[aria-disabled="true"]').length`, &disabledCount),
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

	// The demo's typed config: ui.PaginationConfig{PageParam: "page"}.
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
		if !strings.HasPrefix(h, "?page=") {
			t.Errorf("aria-current href should match pattern '?page=N', got %q", h)
		}
	}
}

func TestE2E_Breadcrumbs_AriaCurrentIsExactlyOne(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// The showcase page carries two trails: the page's own docpage
	// crumbs above the article and the live ui.Breadcrumbs demo in the
	// stage. The contract under test is per trail: every breadcrumb
	// landmark marks exactly one step aria-current="page".
	var perTrail []int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/breadcrumbs"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-cui-comp="ui-breadcrumbs"]')).map(trail => trail.querySelectorAll('.fui-breadcrumbs__link[aria-current="page"]').length)`, &perTrail),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(perTrail) < 2 {
		t.Fatalf("expected the page's own crumbs and the demo trail, got %d trails", len(perTrail))
	}
	for i, n := range perTrail {
		if n != 1 {
			t.Errorf("trail %d: expected exactly 1 aria-current=\"page\", got %d", i, n)
		}
	}
}
