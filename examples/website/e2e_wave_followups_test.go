package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// E2E tests for the wave-followups components: Icon, PollingIndicator,
// NestedList, and the new Skeleton presets (Card / Row / Avatar).
// =============================================================================

// ─── Icon ───────────────────────────────────────────────────────────

func TestE2EIconGallery(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var svgCount int
	var sample string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		// Every Icon emits one <svg class="ui-icon ...">.
		chromedp.Evaluate(`document.querySelectorAll('svg.ui-icon').length`, &svgCount),
		// One built-in icon ("check") must include the currentColor stroke.
		chromedp.Evaluate(`(function() {
			var s = document.querySelector('svg.ui-icon');
			return s ? s.outerHTML : '';
		})()`, &sample),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if svgCount < 10 {
		t.Errorf("expected at least 10 ui-icon SVGs (built-in gallery), got %d", svgCount)
	}
	if !strings.Contains(sample, `stroke="currentColor"`) {
		t.Errorf("expected currentColor stroke, got: %s", sample)
	}
}

func TestE2EIconAriaHidden(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			// First gallery icon is decorative — no AriaLabel — should be aria-hidden.
			var s = document.querySelector('svg.ui-icon');
			return s && s.getAttribute('aria-hidden') === 'true';
		})()`, &hidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hidden {
		t.Error("expected default Icon to be aria-hidden=\"true\"")
	}
}

// Regression: a previous version registered "custom-square" inside the
// Icon screen's Render(), so the very first request to /components/icon
// rendered the custom-icon demo as empty — the registry only had it
// after a prior page load had mutated global state. Fixed by moving the
// RegisterIcon call to package init.
func TestE2ECustomIconAtInit(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var rectCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		// The "Registering custom icons" demo embeds an SVG with a
		// <rect> body. If the icon failed to render, the rect count is 0.
		chromedp.Evaluate(`document.querySelectorAll('svg.ui-icon rect').length`, &rectCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if rectCount < 1 {
		t.Error("custom Icon was not rendered on first request — the demo's RegisterIcon must run at init time, not inside Render()")
	}
}

func TestE2EIconLabeledImg(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var labeled bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/icon"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var svgs = document.querySelectorAll('svg.ui-icon[role="img"]');
			for (var i = 0; i < svgs.length; i++) {
				if (svgs[i].getAttribute('aria-label')) return true;
			}
			return false;
		})()`, &labeled),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !labeled {
		t.Error("expected at least one Icon with role=\"img\" and aria-label on the demo page")
	}
}

// ─── PollingIndicator ──────────────────────────────────────────────

func TestE2EPollingDotLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var dotCount, labelCount int
	var role string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pollingindicator"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator__dot').length`, &dotCount),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator__label').length`, &labelCount),
		chromedp.Evaluate(`document.querySelector('.ui-polling-indicator')?.getAttribute('role') || ''`, &role),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if dotCount < 3 || labelCount < 3 {
		t.Errorf("expected ≥3 dots and ≥3 labels (default, custom, paused), got dot=%d label=%d", dotCount, labelCount)
	}
	if role != "status" {
		t.Errorf("expected role=\"status\", got %q", role)
	}
}

func TestE2EPollingPaused(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pausedCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pollingindicator"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-polling-indicator--paused').length`, &pausedCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pausedCount < 1 {
		t.Error("expected at least one .ui-polling-indicator--paused on the demo page")
	}
}

// ─── NestedList ────────────────────────────────────────────────────

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

// ─── Skeleton presets ──────────────────────────────────────────────

func TestE2ESkeletonPresetsRender(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var card, row, avatar, footer int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skeleton"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-card').length`, &card),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-row').length`, &row),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-avatar').length`, &avatar),
		chromedp.Evaluate(`document.querySelectorAll('.ui-skeleton-card__footer').length`, &footer),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if card < 1 {
		t.Errorf("expected at least 1 SkeletonCard, got %d", card)
	}
	if row < 2 {
		t.Errorf("expected ≥2 SkeletonRows (demo shows 3), got %d", row)
	}
	if avatar < 1 {
		t.Errorf("expected at least 1 SkeletonAvatar, got %d", avatar)
	}
	if footer < 1 {
		t.Errorf("expected SkeletonCard footer when ShowFooter=true, got %d", footer)
	}
}

// ─── DataTable responsive (container queries) ─────────────────────

func TestE2EDataTableCardsModifier(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var modifierCount, labelCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/datatable"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-data-table--responsive-cards').length`, &modifierCount),
		chromedp.Evaluate(`document.querySelectorAll('.ui-data-table--responsive-cards td[data-label]').length`, &labelCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if modifierCount < 1 {
		t.Error("expected at least one .ui-data-table--responsive-cards on the demo page")
	}
	if labelCount < 6 {
		// 2 rows × 3 columns = 6 cells with data-label in the responsive demo.
		t.Errorf("expected ≥6 td[data-label] cells in responsive demo, got %d", labelCount)
	}
}

func TestE2EDataTableCollapsesNarrow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	// The responsive demo wrapper is hard-clamped to 360px max-inline-size,
	// well below the 640px container-query breakpoint. So the table inside
	// it MUST be in cards mode: <thead> visually hidden (block-size ~1px),
	// and <td> elements rendered as block flex containers (display !== "table-cell").
	var theadHidden, tdNotTableCell bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/datatable"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var wrapper = document.querySelector('[data-fui-datatable-responsive-demo]');
			if (!wrapper) return false;
			var thead = wrapper.querySelector('thead');
			if (!thead) return false;
			var rect = thead.getBoundingClientRect();
			return rect.height < 5; // clipped via position: absolute + 1px box
		})()`, &theadHidden),
		chromedp.Evaluate(`(function() {
			var wrapper = document.querySelector('[data-fui-datatable-responsive-demo]');
			if (!wrapper) return false;
			var td = wrapper.querySelector('td');
			if (!td) return false;
			var disp = getComputedStyle(td).display;
			return disp !== 'table-cell'; // becomes flex/block in cards mode
		})()`, &tdNotTableCell),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !theadHidden {
		t.Error("expected <thead> to be visually clipped under 640px container width")
	}
	if !tdNotTableCell {
		t.Error("expected <td> to NOT be display:table-cell (cards mode should switch it to flex/block)")
	}
}

func TestE2EDataTableDefaultStaysTable(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	// The main "Live (island mode)" DataTable above the responsive demo
	// does NOT set Responsive: ResponsiveCards, so it must remain a real
	// table (no modifier class, no data-label, td stays display:table-cell).
	var hasModifier bool
	var firstTDDisplay string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/datatable"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			// Walk up from the main island, NOT the responsive demo.
			var tables = document.querySelectorAll('.ui-data-table');
			for (var i = 0; i < tables.length; i++) {
				var t = tables[i];
				if (t.closest('[data-fui-datatable-responsive-demo]')) continue;
				return t.classList.contains('ui-data-table--responsive-cards');
			}
			return false;
		})()`, &hasModifier),
		chromedp.Evaluate(`(function() {
			var tables = document.querySelectorAll('.ui-data-table');
			for (var i = 0; i < tables.length; i++) {
				var t = tables[i];
				if (t.closest('[data-fui-datatable-responsive-demo]')) continue;
				var td = t.querySelector('td');
				if (td) return getComputedStyle(td).display;
			}
			return '';
		})()`, &firstTDDisplay),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hasModifier {
		t.Error("the default (non-responsive) DataTable must not carry the responsive-cards modifier")
	}
	if firstTDDisplay != "table-cell" {
		t.Errorf("expected default DataTable cells to be display:table-cell, got %q", firstTDDisplay)
	}
}

func TestE2ESkeletonPresetsAriaHidden(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var allHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skeleton"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var nodes = document.querySelectorAll('.ui-skeleton-card, .ui-skeleton-row, .ui-skeleton-avatar');
			if (nodes.length === 0) return false;
			for (var i = 0; i < nodes.length; i++) {
				if (nodes[i].getAttribute('aria-hidden') !== 'true') return false;
			}
			return true;
		})()`, &allHidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !allHidden {
		t.Error("every skeleton preset wrapper must be aria-hidden=\"true\"")
	}
}
