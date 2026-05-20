package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

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
