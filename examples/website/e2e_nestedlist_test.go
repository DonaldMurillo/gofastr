package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestE2ENestedListFlat(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var ulCount, linkCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('ul.nested-list').length`, &ulCount),
		chromedp.Evaluate(`document.querySelectorAll('.nested-list__link').length`, &linkCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ulCount < 1 {
		t.Error("expected at least one ul.nested-list")
	}
	if linkCount < 3 {
		t.Errorf("expected ≥3 nested-list links (flat demo has 3 leaves), got %d", linkCount)
	}
}

func TestE2ENestedListBranchDetails(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var detailsCount, openCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.nested-list details.nested-list__branch').length`, &detailsCount),
		// "Account" branch is initially expanded.
		chromedp.Evaluate(`document.querySelectorAll('.nested-list details[open]').length`, &openCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if detailsCount < 2 {
		t.Errorf("expected ≥2 <details> branches on the nested demo, got %d", detailsCount)
	}
	if openCount < 1 {
		t.Errorf("expected at least one initially-expanded branch, got %d", openCount)
	}
}

// Regression: the previous version registered the NestedList component
// but never wired core-ui/patterns/nestedlist.BaseCSS() into the website
// theme bundle, so links rendered as default-browser red underlined text
// and <details> branches showed the raw ▶ marker. This test catches
// the next "shipped without its stylesheet" instance: any rule from
// BaseCSS() guarantees the bundle picked up the pattern.
func TestE2ENestedListStyled(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var liStyle, summaryStyle string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		// Outer <ul> reset: padding-inline-start must be 0 (not the
		// browser default of ~40px). Any non-zero value means the
		// pattern's BaseCSS isn't being bundled.
		chromedp.Evaluate(`(function(){
			var ul = document.querySelector('ul.nested-list');
			return ul ? getComputedStyle(ul).paddingInlineStart : '';
		})()`, &liStyle),
		// Branch summary should be flex (our custom layout) — not the
		// browser default "list-item" display.
		chromedp.Evaluate(`(function(){
			var s = document.querySelector('.nested-list summary');
			return s ? getComputedStyle(s).display : '';
		})()`, &summaryStyle),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if liStyle != "0px" {
		t.Errorf("expected ul.nested-list padding-inline-start: 0 (BaseCSS reset), got %q", liStyle)
	}
	if !strings.Contains(summaryStyle, "flex") {
		t.Errorf("expected summary display: inline-flex (our BaseCSS), got %q", summaryStyle)
	}
}

func TestE2ENestedListOL(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var olCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/nestedlist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('ol.nested-list').length`, &olCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if olCount < 1 {
		t.Error("expected at least one ol.nested-list (Ordered demo)")
	}
}
