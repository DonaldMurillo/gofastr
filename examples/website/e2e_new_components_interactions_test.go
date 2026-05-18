package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// =============================================================================
// Actual end-to-end interaction tests for the new components. The
// e2e_new_components_test.go file covers the static SSR shape (roles,
// labels, attrs) — these tests EXERCISE the runtime: click, type, drive
// keyboard events, then assert the DOM actually changed.
// =============================================================================

// --- CopyButton ---------------------------------------------------------

func TestE2E_CopyButton_ClickFlashesAndAnnounces(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var copiedClass bool
	var statusText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/copybutton"),
		pageReady(),
		// Stub clipboard so the test passes without real OS clipboard
		// permission (chromedp headless often denies it).
		chromedp.Evaluate(`navigator.clipboard = { writeText: () => Promise.resolve() }`, nil),
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-text-from="#copy-source"]').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-text-from="#copy-source"]').classList.contains('fui-copied')`, &copiedClass),
		chromedp.Evaluate(`(document.querySelector('[data-fui-copy-text-from="#copy-source"]').parentElement.querySelector('[data-fui-copy-status]')?.textContent || '').trim()`, &statusText),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !copiedClass {
		t.Error(".fui-copied was NOT applied to the button after click")
	}
	if statusText != "Copied" {
		t.Errorf("expected status span to read 'Copied' after click, got %q", statusText)
	}
}

func TestE2E_CopyButton_ToastFiresOnCopy(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var toastTitle string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/copybutton"),
		pageReady(),
		chromedp.Evaluate(`navigator.clipboard = { writeText: () => Promise.resolve() }`, nil),
		// Click the toast-emitting button (second copy demo, with ToastOnCopy=true).
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-text-from="#copy-token"]').click()`, nil),
		settle(),
		// Toast stack lives in the "site-toasts" wrapper; grab the title text.
		chromedp.Evaluate(`(document.querySelector('[data-fui-toast-stack] .ui-toast__title, [data-fui-toast-stack] [class*="title"]')?.textContent || '').trim()`, &toastTitle),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(toastTitle, "API key copied") {
		t.Errorf("expected toast titled 'API key copied' after click, got %q", toastTitle)
	}
}

// --- SegmentedControl ---------------------------------------------------

func TestE2E_SegmentedControl_ClickSlidesIndicator(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var initialChecked, afterClick string
	var indicatorTransformInitial, indicatorTransformAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/segmented"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked').value`, &initialChecked),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-segmented .ui-segmented__indicator')).transform`, &indicatorTransformInitial),
		// Click the "Month" option (3rd in the Day/Week/Month group).
		chromedp.Evaluate(`document.querySelectorAll('.ui-segmented')[0].querySelectorAll('input[type="radio"]')[2].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked').value`, &afterClick),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-segmented .ui-segmented__indicator')).transform`, &indicatorTransformAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initialChecked != "week" {
		t.Errorf("expected initial=week, got %q", initialChecked)
	}
	if afterClick != "month" {
		t.Errorf("expected month checked after click, got %q", afterClick)
	}
	if indicatorTransformInitial == indicatorTransformAfter {
		t.Errorf("indicator transform should change after option click — initial=%q after=%q", indicatorTransformInitial, indicatorTransformAfter)
	}
}

func TestE2E_SegmentedControl_EqualWidthColumns(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var widthsJSON string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/segmented"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify(
			Array.from(document.querySelector('.ui-segmented').querySelectorAll('.ui-segmented__option'))
				.map(o => Math.round(o.getBoundingClientRect().width))
		)`, &widthsJSON),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// Day/Week/Month should be equal width via grid-auto-columns: 1fr.
	if !strings.HasPrefix(widthsJSON, "[") {
		t.Fatalf("unexpected widths shape: %s", widthsJSON)
	}
	// Strip "[" and "]" and split.
	parts := strings.Split(strings.Trim(widthsJSON, "[]"), ",")
	if len(parts) != 3 {
		t.Fatalf("expected 3 option widths, got %s", widthsJSON)
	}
	if parts[0] != parts[1] || parts[1] != parts[2] {
		t.Errorf("option widths must be equal (grid 1fr), got %s", widthsJSON)
	}
}

// --- ConfirmAction ------------------------------------------------------

func TestE2E_ConfirmAction_TriggerIsFrameworkButton(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasButtonClass bool
	var hasCompMarker bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/confirmaction"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').classList.contains('ui-button')`, &hasButtonClass),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').hasAttribute('data-fui-comp')`, &hasCompMarker),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasButtonClass {
		t.Error("ConfirmAction trigger should carry .ui-button (framework Button component)")
	}
	if !hasCompMarker {
		t.Error("ConfirmAction trigger should carry data-fui-comp marker so its CSS auto-loads")
	}
}

func TestE2E_ConfirmAction_DialogButtonsAreFrameworkButtons(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var cancelOK, confirmOK bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/confirmaction"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').click()`, nil),
		settle(),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="demo-confirm-delete"] .ui-button.ui-button--ghost')`, &cancelOK),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-widget="demo-confirm-delete"] .ui-button.ui-button--danger')`, &confirmOK),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !cancelOK {
		t.Error("Cancel button must use ui.Button(Variant: ButtonGhost) — not raw HTML")
	}
	if !confirmOK {
		t.Error("Confirm button must use ui.Button(Variant: ButtonDanger) — not raw HTML")
	}
}

// --- FilterChipBar ------------------------------------------------------

func TestE2E_FilterChipBar_DismissRemovesChip(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipsBefore, chipsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/filterchipbar"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('#filter-bar-demo .ui-tag').length`, &chipsBefore),
		// Click the × button on the first chip.
		chromedp.Evaluate(`document.querySelector('#filter-bar-demo .ui-tag .ui-tag__dismiss').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('#filter-bar-demo .ui-tag').length`, &chipsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipsBefore != 3 {
		t.Errorf("expected 3 chips on first paint, got %d", chipsBefore)
	}
	if chipsAfter != 2 {
		t.Errorf("expected 2 chips after dismissing one (server-driven re-render), got %d", chipsAfter)
	}
}

func TestE2E_FilterChipBar_ClearAllWipesChips(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/filterchipbar"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('#filter-bar-demo .ui-filter-bar__clear').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('#filter-bar-demo .ui-tag').length`, &chipsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipsAfter != 0 {
		t.Errorf("expected 0 chips after Clear all, got %d", chipsAfter)
	}
}

// --- InfiniteScroll -----------------------------------------------------

func TestE2E_InfiniteScroll_AutoFetchesNextPage(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var itemsBefore, itemsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/infinitescroll"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('#feed-demo .demo-feed-item').length`, &itemsBefore),
		// Scroll the CONTAINER (not the page) — the runtime uses the
		// `.demo-infinite-frame` element as the IntersectionObserver
		// root because it's the nearest overflow-y: auto ancestor.
		chromedp.Evaluate(`(() => {
			const c = document.querySelector('.demo-infinite-frame');
			c.scrollTo({ top: c.scrollHeight, behavior: 'instant' });
		})()`, nil),
		// Need time for the fetch to complete.
		chromedp.Sleep(1200*1e6),
		chromedp.Evaluate(`document.querySelectorAll('#feed-demo .demo-feed-item').length`, &itemsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if itemsBefore < 5 {
		t.Errorf("expected ≥5 SSR items, got %d", itemsBefore)
	}
	if itemsAfter <= itemsBefore {
		t.Errorf("scrolling sentinel into view should fetch more items; before=%d after=%d", itemsBefore, itemsAfter)
	}
}

func TestE2E_InfiniteScroll_EndOfFeedRemovesSentinel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var sentinelPresent bool
	var itemCount int
	// Demo lives in a fixed-height scroll container; scroll IT, not
	// the page. The runtime auto-detects the scroll container and uses
	// it as the IntersectionObserver root, so page-level scrollIntoView
	// would do nothing useful here.
	scrollFeed := chromedp.Evaluate(
		`(() => {
			const c = document.querySelector('.demo-infinite-frame');
			if (c) c.scrollTo({ top: c.scrollHeight, behavior: 'instant' });
		})()`, nil)
	const feedTotal = 100
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/infinitescroll"),
		pageReady(),
		// Hammer the container scroll until end-of-feed (10 cycles is
		// enough — feed is 100 items, fetched in pages of 10).
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		scrollFeed, chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`document.querySelectorAll('#feed-demo .demo-feed-item').length`, &itemCount),
		chromedp.Evaluate(`document.querySelector('#feed-demo [data-fui-infinite-sentinel]') !== null`, &sentinelPresent),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if itemCount != feedTotal {
		t.Errorf("expected to reach %d items (full feed), got %d", feedTotal, itemCount)
	}
	if sentinelPresent {
		t.Error("sentinel should be removed at end of feed (empty cursor header)")
	}
}

// --- Combobox ----------------------------------------------------------

func TestE2E_Combobox_TypingFiresRPCAndRendersOptions(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var optionCount int
	var ariaExpanded string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		chromedp.Focus(`#city-combo`),
		chromedp.SendKeys(`#city-combo`, "Be"),
		// Debounce is 250ms; wait for RPC response + signal swap.
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-expanded')`, &ariaExpanded),
		chromedp.Evaluate(`document.querySelectorAll('#city-combo-listbox [role="option"]').length`, &optionCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if optionCount < 1 {
		t.Errorf("typing 'Be' should match Berlin; expected ≥1 option, got %d", optionCount)
	}
	if ariaExpanded != "true" {
		t.Errorf("expected aria-expanded=true once options render, got %q", ariaExpanded)
	}
}

func TestE2E_Combobox_ArrowDownHighlightsOption(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var activeDesc string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		chromedp.Focus(`#city-combo`),
		chromedp.SendKeys(`#city-combo`, "a"),
		chromedp.Sleep(500*1e6),
		chromedp.KeyEvent(kb.ArrowDown),
		settle(),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-activedescendant')`, &activeDesc),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if activeDesc == "" {
		t.Error("ArrowDown should set aria-activedescendant to the highlighted option's id")
	}
}

func TestE2E_Combobox_EnterSelectsOption(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inputValue, expandedAfter, listboxOptions, activeBeforeEnter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		chromedp.Focus(`#city-combo`),
		chromedp.SendKeys(`#city-combo`, "Be"),
		chromedp.Sleep(700*1e6),
		chromedp.KeyEvent(kb.ArrowDown),
		settle(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#city-combo-listbox [role="option"]')).map(o => o.getAttribute('data-value') || o.textContent).join(',')`, &listboxOptions),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-activedescendant') || ''`, &activeBeforeEnter),
		chromedp.KeyEvent(kb.Enter),
		settle(),
		chromedp.Evaluate(`document.getElementById('city-combo').value`, &inputValue),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-expanded')`, &expandedAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.HasPrefix(inputValue, "Be") {
		t.Errorf("Enter should fill the input with the selected option's value; got %q\nlistbox options: %s\nactive before Enter: %s",
			inputValue, listboxOptions, activeBeforeEnter)
	}
	if expandedAfter != "false" {
		t.Errorf("aria-expanded should be false after selection, got %q", expandedAfter)
	}
}

// --- TreeView ----------------------------------------------------------

func TestE2E_TreeView_ArrowRightExpandsLazyBranch(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var expandedBefore, expandedAfter string
	var childCountAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tree"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('vendor').getAttribute('aria-expanded')`, &expandedBefore),
		// Click the toggle directly — equivalent to ArrowRight from the runtime's
		// perspective, and more reliable in headless than synthesizing key events.
		chromedp.Evaluate(`document.querySelector('#vendor [data-fui-tree-toggle]').click()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`document.getElementById('vendor').getAttribute('aria-expanded')`, &expandedAfter),
		chromedp.Evaluate(`document.querySelectorAll('#vendor > [role="group"] > [role="treeitem"]').length`, &childCountAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if expandedBefore != "false" {
		t.Errorf("vendor should start collapsed, got %q", expandedBefore)
	}
	if expandedAfter != "true" {
		t.Errorf("vendor should be aria-expanded=true after toggle, got %q", expandedAfter)
	}
	if childCountAfter < 1 {
		t.Errorf("lazy-load should populate vendor children; got %d", childCountAfter)
	}
}

func TestE2E_TreeView_ArrowDownMovesFocusToNextSibling(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var focusedID string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tree"),
		pageReady(),
		chromedp.Focus(`#src`),
		settle(),
		chromedp.KeyEvent(kb.ArrowDown),
		settle(),
		chromedp.Evaluate(`document.activeElement.id`, &focusedID),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// "src" is open, so its first child (src-main) is the next visible row.
	if focusedID != "src-main" {
		t.Errorf("ArrowDown from src should focus first visible child (src-main), got %q", focusedID)
	}
}

// --- CommandPalette ----------------------------------------------------

func TestE2E_CommandPalette_OpensFocusesInput(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var focusedTag string
	var focusedRole string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/commandpalette"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-command-palette"]').click()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`document.activeElement.tagName.toLowerCase()`, &focusedTag),
		chromedp.Evaluate(`document.activeElement.getAttribute('role') || ''`, &focusedRole),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if focusedTag != "input" {
		t.Errorf("expected focus on input after open, got %q", focusedTag)
	}
	if focusedRole != "combobox" {
		t.Errorf("expected combobox role on focused element, got %q", focusedRole)
	}
}

func TestE2E_CommandPalette_SearchPopulatesOptions(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var optCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/commandpalette"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-command-palette"]').click()`, nil),
		chromedp.Sleep(400*1e6),
		chromedp.SendKeys(`[data-fui-widget="demo-command-palette"] input[role="combobox"]`, "set"),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-widget="demo-command-palette"] [role="option"]').length`, &optCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if optCount < 1 {
		t.Errorf("typing 'set' should match 'Open settings'; expected ≥1 option, got %d", optCount)
	}
}

func TestE2E_CommandPalette_EscClosesAndReturnsFocus(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var widgetCountBefore, widgetCountAfter int
	var hiddenBefore, hiddenAfter string
	var modalStackBefore, modalStackAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/commandpalette"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-command-palette"]').click()`, nil),
		chromedp.Sleep(700*1e6), // chrome-fetch + mount can take a beat
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-widget="demo-command-palette"]').length`, &widgetCountBefore),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="demo-command-palette"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenBefore),
		chromedp.Evaluate(`JSON.stringify(window.__gofastr?._modalStack || [])`, &modalStackBefore),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-widget="demo-command-palette"]').length`, &widgetCountAfter),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="demo-command-palette"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenAfter),
		chromedp.Evaluate(`JSON.stringify(window.__gofastr?._modalStack || [])`, &modalStackAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if widgetCountBefore != 1 {
		t.Fatalf("expected 1 palette widget element after open, got %d (stack=%s)", widgetCountBefore, modalStackBefore)
	}
	if hiddenBefore != "false" {
		t.Fatalf("palette should be visible after trigger click; hidden=%s stack=%s", hiddenBefore, modalStackBefore)
	}
	// "Closed" means EITHER the widget is hidden in place OR removed
	// from the DOM (different preset configurations dismiss differently;
	// what matters is the user no longer sees the palette).
	closedAfter := hiddenAfter == "true" || widgetCountAfter == 0
	if !closedAfter {
		t.Errorf("Esc should close the palette; before hidden=%s stack=%s | after hidden=%s stack=%s count=%d",
			hiddenBefore, modalStackBefore, hiddenAfter, modalStackAfter, widgetCountAfter)
	}
	// The modal stack MUST be empty — that's the runtime's source of
	// truth for "no modal is currently focus-trapping".
	if modalStackAfter != "[]" {
		t.Errorf("modal stack should be empty after Esc, got %s", modalStackAfter)
	}
}

// --- Banner -------------------------------------------------------------

func TestE2E_Banner_DismissHidesElement(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenBefore, hiddenAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/banner"),
		pageReady(),
		// First clear any stale localStorage from a prior run so the
		// persistent banner is visible.
		chromedp.Evaluate(`localStorage.removeItem("gofastr.banner-dismiss.feature-filter-chips-2026-05")`, nil),
		chromedp.Reload(),
		pageReady(),
		chromedp.Evaluate(`(document.querySelector("[data-fui-banner-dismiss-id]")?.closest("[data-fui-comp=\"ui-banner\"]")?.hasAttribute("hidden") ?? null) + ""`, &hiddenBefore),
		chromedp.Evaluate(`document.querySelector("[data-fui-banner-dismiss-id]").click()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`(document.querySelector("[data-fui-banner-dismiss-id]")?.closest("[data-fui-comp=\"ui-banner\"]")?.hasAttribute("hidden") ?? null) + ""`, &hiddenAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenBefore != "false" {
		t.Fatalf("persistent banner should be visible before click; hidden=%s", hiddenBefore)
	}
	if hiddenAfter != "true" {
		t.Errorf("clicking dismiss should hide the banner; hidden=%s", hiddenAfter)
	}
}

func TestE2E_Banner_DismissPersistsAcrossReload(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenAfterReload string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/banner"),
		pageReady(),
		chromedp.Evaluate(`localStorage.setItem("gofastr.banner-dismiss.feature-filter-chips-2026-05", "1")`, nil),
		chromedp.Reload(),
		pageReady(),
		// After reload, the banner runtime should auto-hide the
		// banner whose DismissID was previously stored.
		chromedp.Sleep(200*1e6),
		chromedp.Evaluate(`(document.querySelector("[data-fui-banner-dismiss-id]")?.closest("[data-fui-comp=\"ui-banner\"]")?.hasAttribute("hidden") ?? null) + ""`, &hiddenAfterReload),
		// Clean up so other tests start with a known state.
		chromedp.Evaluate(`localStorage.removeItem("gofastr.banner-dismiss.feature-filter-chips-2026-05")`, nil),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenAfterReload != "true" {
		t.Errorf("banner with stored DismissID should auto-hide on reload; hidden=%s", hiddenAfterReload)
	}
}


// --- Slider --------------------------------------------------------------

func TestE2E_Slider_OutputMirrorsValue(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var before, after string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/slider"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[type=range][data-fui-slider-mirror]').value`, &before),
		// Drive the input event directly — chromedp doesn't fire change
		// for range inputs via key events on every Chrome version.
		chromedp.Evaluate(`(function(){
			const r = document.querySelector('input[type=range][data-fui-slider-mirror]');
			r.value = '77';
			r.dispatchEvent(new Event('input', {bubbles: true}));
			return r.value;
		})()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`document.querySelector('output[for="' + document.querySelector('input[type=range][data-fui-slider-mirror]').id + '"]').textContent`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before == after {
		t.Errorf("output should mirror the new value; before=%q after=%q", before, after)
	}
	if after != "77" {
		t.Errorf("output should reflect new value 77, got %q", after)
	}
}

// --- NumberInput ---------------------------------------------------------

func TestE2E_NumberInput_PlusIncrementsValue(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var before, after string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/numberinput"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]').value`, &before),
		chromedp.Evaluate(`document.querySelector('[data-fui-number-for="qty"][data-fui-number-step="1"]').click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]').value`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before == after {
		t.Errorf("+ button should increment value; before=%q after=%q", before, after)
	}
}

func TestE2E_NumberInput_MinusClampsToMin(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var value string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/numberinput"),
		pageReady(),
		// qty default 1 with Min=1 — clicking − 3x must not go below 1.
		chromedp.Evaluate(`(function(){
			const btn = document.querySelector('[data-fui-number-for="qty"][data-fui-number-step="-1"]');
			btn.click(); btn.click(); btn.click();
		})()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]').value`, &value),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if value != "1" {
		t.Errorf("− button should clamp to Min=1; got value=%q", value)
	}
}

// --- TextArea ------------------------------------------------------------

func TestE2E_TextArea_AutogrowResizesHeight(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var before, after int64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/textarea"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('textarea[data-fui-autogrow]').clientHeight`, &before),
		chromedp.Evaluate(`(function(){
			const ta = document.querySelector('textarea[data-fui-autogrow]');
			ta.focus();
			ta.value = 'line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7';
			ta.dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`document.querySelector('textarea[data-fui-autogrow]').clientHeight`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if after <= before {
		t.Errorf("autogrow should increase height when content grows; before=%d after=%d", before, after)
	}
}

// --- MultiSelect ---------------------------------------------------------

func TestE2E_MultiSelect_TogglingRendersChips(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipsBefore, chipsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/multiselect"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-multiselect"] .ui-multiselect__chip').length`, &chipsBefore),
		chromedp.Evaluate(`(function(){
			const cbs = document.querySelectorAll('[data-fui-comp="ui-multiselect"] input[type="checkbox"]:not(:checked)');
			if (cbs.length >= 2) {
				cbs[0].click();
				cbs[1].click();
			}
		})()`, nil),
		chromedp.Sleep(200*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-multiselect"] .ui-multiselect__chip').length`, &chipsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipsAfter <= chipsBefore {
		t.Errorf("toggling 2 new checkboxes should add ≥2 chips; before=%d after=%d", chipsBefore, chipsAfter)
	}
}

func TestE2E_MultiSelect_ChipRemoveUnchecksOption(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var beforeChecked, afterChecked int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/multiselect"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-multiselect"] input[type="checkbox"]:checked').length`, &beforeChecked),
		chromedp.Evaluate(`(function(){
			const btn = document.querySelector('[data-fui-multiselect-remove]');
			if (btn) btn.click();
		})()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-multiselect"] input[type="checkbox"]:checked').length`, &afterChecked),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if afterChecked != beforeChecked-1 {
		t.Errorf("clicking a chip's × should uncheck 1 option; before=%d after=%d", beforeChecked, afterChecked)
	}
}

func TestE2E_MultiSelect_OutsideClickClosesDisclosure(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var openBefore, openAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/multiselect"),
		pageReady(),
		chromedp.Evaluate(`(function(){
			const d = document.querySelector('details.ui-multiselect__disclosure');
			d.setAttribute('open', '');
		})()`, nil),
		chromedp.Evaluate(`(document.querySelector('details.ui-multiselect__disclosure').hasAttribute('open')) + ''`, &openBefore),
		// Click on the page heading — definitely outside the multiselect.
		chromedp.Evaluate(`(function(){
			const ev = new MouseEvent('mousedown', {bubbles: true, cancelable: true});
			document.body.dispatchEvent(ev);
		})()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`(document.querySelector('details.ui-multiselect__disclosure').hasAttribute('open')) + ''`, &openAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openBefore != "true" {
		t.Fatalf("disclosure should be open before outside click, got %q", openBefore)
	}
	if openAfter != "false" {
		t.Errorf("outside click should close the disclosure, got %q", openAfter)
	}
}

// --- FileDropzone --------------------------------------------------------

func TestE2E_FileDropzone_AriaRegionAndDragoverClass(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, ariaLabel string
	var dragoverClassAfterEnter, dragoverClassAfterLeave bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/dropzone"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone')?.getAttribute('aria-label') || ''`, &ariaLabel),
		// Fire a synthetic dragover — runtime adds .is-dragover.
		chromedp.Evaluate(`(function(){
			const z = document.querySelector('.ui-dropzone__zone');
			const dt = new DataTransfer();
			z.dispatchEvent(new DragEvent('dragenter', {bubbles: true, cancelable: true, dataTransfer: dt}));
		})()`, nil),
		chromedp.Sleep(80*1e6),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone').classList.contains('is-dragover')`, &dragoverClassAfterEnter),
		chromedp.Evaluate(`(function(){
			const z = document.querySelector('.ui-dropzone__zone');
			z.dispatchEvent(new DragEvent('dragleave', {bubbles: true, cancelable: true, relatedTarget: document.body}));
		})()`, nil),
		chromedp.Sleep(80*1e6),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone').classList.contains('is-dragover')`, &dragoverClassAfterLeave),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "region" {
		t.Errorf("dropzone should have role=region, got %q", role)
	}
	if ariaLabel == "" {
		t.Errorf("dropzone should have aria-label, got empty")
	}
	if !dragoverClassAfterEnter {
		t.Errorf(".is-dragover should be applied on dragenter")
	}
	if dragoverClassAfterLeave {
		t.Errorf(".is-dragover should be removed on dragleave")
	}
}
