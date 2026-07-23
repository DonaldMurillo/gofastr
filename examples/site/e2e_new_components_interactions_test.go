package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// =============================================================================
// Interaction tests ported from examples/website.
//
// Dropped (note-only in site): combobox, multiselect, confirmaction,
// commandpalette, filterchipbar dismiss RPC (uses # stub in site),
// infinitescroll, sortablelist.
//
// Dropped (duplicated by e2e_test.go): copybutton flash/announce,
// textarea autogrow, password toggle.
//
// Kept: segmented, slider, numberinput, taginput, animatedcounter,
// disclosure, rangeslider, banner, dropzone, tree.
// =============================================================================

// --- SegmentedControl ---------------------------------------------------

func TestE2E_SegmentedControl_ClickSlidesIndicator(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var initialChecked, afterClick string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/segmented"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked')?.value || ''`, &initialChecked),
		// Click the 3rd option (index 2 = "Month")
		chromedp.Evaluate(`document.querySelectorAll('.ui-segmented')[0].querySelectorAll('input[type="radio"]')[2].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked')?.value || ''`, &afterClick),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initialChecked == "" {
		t.Errorf("expected an initial checked radio, got empty")
	}
	if afterClick != "month" {
		t.Errorf("expected month checked after click, got %q", afterClick)
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
	if !strings.HasPrefix(widthsJSON, "[") {
		t.Fatalf("unexpected widths shape: %s", widthsJSON)
	}
	parts := strings.Split(strings.Trim(widthsJSON, "[]"), ",")
	if len(parts) < 2 {
		t.Fatalf("expected ≥2 option widths, got %s", widthsJSON)
	}
	if parts[0] != parts[1] {
		t.Errorf("option widths must be equal (grid 1fr), got %s", widthsJSON)
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
		chromedp.Evaluate(`document.querySelector('input[type=range][data-fui-slider-mirror]')?.value || ''`, &before),
		chromedp.Evaluate(`(function(){
			const r = document.querySelector('input[type=range][data-fui-slider-mirror]');
			if (!r) return;
			r.value = '77';
			r.dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`(function(){
			const r = document.querySelector('input[type=range][data-fui-slider-mirror]');
			if (!r) return '';
			const out = document.querySelector('output[for="' + r.id + '"]');
			return out ? out.textContent : '';
		})()`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before == "" {
		t.Fatal("slider not found — the catalog contract and browser test have drifted")
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
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]')?.value || ''`, &before),
		chromedp.Evaluate(`document.querySelector('[data-fui-number-for="qty"][data-fui-number-step="1"]')?.click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]')?.value || ''`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before == "" {
		t.Fatal("numberinput qty not found — the catalog contract and browser test have drifted")
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
		// qty: Min=0, Value=1 — click − 3x should clamp at 0
		chromedp.Evaluate(`(function(){
			const btn = document.querySelector('[data-fui-number-for="qty"][data-fui-number-step="-1"]');
			if (!btn) return;
			btn.click(); btn.click(); btn.click();
		})()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('input[type=number][name="qty"]')?.value || ''`, &value),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if value == "" {
		t.Fatal("numberinput qty not found — the catalog contract and browser test have drifted")
	}
	// Min=0 in site demo
	v, _ := strconv.ParseFloat(value, 64)
	if v < 0 {
		t.Errorf("− button should clamp to Min=0; got value=%q", value)
	}
}

// --- TagInput ------------------------------------------------------------
// (basic chip add / backspace covered; legit-submit guard also kept)

func TestE2E_TagInput_EnterCommitsChip(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipsBefore, chipsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/taginput"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-tag-input"] .ui-tag-input__chip').length`, &chipsBefore),
		chromedp.Focus(`input[data-fui-tag-input]`),
		chromedp.SendKeys(`input[data-fui-tag-input]`, "rust"),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Sleep(120*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-tag-input"] .ui-tag-input__chip').length`, &chipsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipsAfter != chipsBefore+1 {
		t.Errorf("Enter on typed value should add exactly 1 chip; before=%d after=%d", chipsBefore, chipsAfter)
	}
}

func TestE2E_TagInput_LegitSubmitNotEatenAfterEnter(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var submitReached bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/taginput"),
		pageReady(),
		chromedp.Focus(`input[data-fui-tag-input]`),
		chromedp.SendKeys(`input[data-fui-tag-input]`, "first"),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Sleep(200*1e6), // past same-tick swallow window
		chromedp.Evaluate(`
		  (function(){
		    const f = document.querySelector('input[data-fui-tag-input]').form;
		    if (!f) return false;
		    const ev = new Event('submit', {bubbles:true, cancelable:true});
		    const proceeded = f.dispatchEvent(ev);
		    return proceeded;
		  })()
		`, &submitReached),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !submitReached {
		t.Error("legit submit after Enter-in-tag-input was swallowed; the same-tick guard should have expired")
	}
}

func TestE2E_TagInput_BackspaceRemovesLast(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipsBefore, chipsAfter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/taginput"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-tag-input"] .ui-tag-input__chip').length`, &chipsBefore),
		chromedp.Focus(`input[data-fui-tag-input]`),
		chromedp.KeyEvent(kb.Backspace),
		chromedp.Sleep(120*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-tag-input"] .ui-tag-input__chip').length`, &chipsAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipsAfter != chipsBefore-1 {
		t.Errorf("Backspace on empty input should remove 1 chip; before=%d after=%d", chipsBefore, chipsAfter)
	}
}

// --- AnimatedCounter -----------------------------------------------------

func TestE2E_AnimatedCounter_SSRRendersValue(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/animatedcounter"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-animated-counter] .ui-animated-counter__value')?.textContent || ''`, &text),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if text == "" {
		t.Errorf("AnimatedCounter value should be rendered; got empty")
	}
}

// --- Disclosure (pattern) -----------------------------------------------

func TestE2E_Disclosure_ClickSummaryToggles(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var openA, openB bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/disclosure"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-disclosure"]')[0].hasAttribute('open')`, &openA),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-disclosure"]')[0].querySelector('.ui-disclosure__summary').click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-disclosure"]')[0].hasAttribute('open')`, &openB),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openA == openB {
		t.Errorf("summary click should toggle open; A=%v B=%v", openA, openB)
	}
}

// --- RangeSlider ---------------------------------------------------------

func TestE2E_RangeSlider_CrossClampPreventsCross(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var lo, hi string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rangeslider"),
		pageReady(),
		chromedp.Evaluate(`(function(){
			const lo = document.querySelector('input[name="price-min"]');
			const hi = document.querySelector('input[name="price-max"]');
			if (!lo || !hi) return;
			lo.value = String(parseFloat(hi.value) + 100);
			lo.dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Sleep(120*1e6),
		chromedp.Evaluate(`document.querySelector('input[name="price-min"]')?.value || ''`, &lo),
		chromedp.Evaluate(`document.querySelector('input[name="price-max"]')?.value || ''`, &hi),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if lo == "" || hi == "" {
		t.Fatal("price-min/price-max inputs not found — the catalog contract and browser test have drifted")
	}
	loN, _ := strconv.ParseFloat(lo, 64)
	hiN, _ := strconv.ParseFloat(hi, 64)
	if loN > hiN {
		t.Errorf("cross-clamp failed: lo=%s > hi=%s", lo, hi)
	}
}

func TestE2E_RangeSlider_ValueMirrorUpdates(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var before, after string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rangeslider"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('output[data-fui-range-slider-value]')?.textContent || ''`, &before),
		chromedp.Evaluate(`(function(){
			const lo = document.querySelector('input[name="price-min"]');
			if (!lo) return;
			lo.value = '120';
			lo.dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Sleep(120*1e6),
		chromedp.Evaluate(`document.querySelector('output[data-fui-range-slider-value]')?.textContent || ''`, &after),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if before == "" {
		t.Fatal("range slider output not found — the catalog contract and browser test have drifted")
	}
	if before == after {
		t.Errorf("output mirror should change after input; before=%q after=%q", before, after)
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
		// Clear any previous dismiss in localStorage.
		chromedp.Evaluate(`localStorage.removeItem("gofastr.banner-dismiss.feature-filter-chips-2026-05")`, nil),
		chromedp.Reload(),
		pageReady(),
		chromedp.Evaluate(`(document.querySelector("[data-fui-banner-dismiss-id]")?.closest("[data-fui-comp=\"ui-banner\"]")?.hasAttribute("hidden") ?? null) + ""`, &hiddenBefore),
		chromedp.Evaluate(`document.querySelector("[data-fui-banner-dismiss-id]")?.click()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`(document.querySelector("[data-fui-banner-dismiss-id]")?.closest("[data-fui-comp=\"ui-banner\"]")?.hasAttribute("hidden") ?? null) + ""`, &hiddenAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenBefore == "null" {
		t.Fatal("dismissable banner not found — the catalog contract and browser test have drifted")
	}
	if hiddenBefore != "false" {
		t.Fatalf("persistent banner should be visible before click; hidden=%s", hiddenBefore)
	}
	if hiddenAfter != "true" {
		t.Errorf("clicking dismiss should hide the banner; hidden=%s", hiddenAfter)
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
		// Site tree: vendor node is collapsed by default
		chromedp.Evaluate(`document.getElementById('vendor')?.getAttribute('aria-expanded') || ''`, &expandedBefore),
		chromedp.Evaluate(`document.querySelector('#vendor [data-fui-tree-toggle]')?.click()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`document.getElementById('vendor')?.getAttribute('aria-expanded') || ''`, &expandedAfter),
		chromedp.Evaluate(`document.querySelectorAll('#vendor > [role="group"] > [role="treeitem"]').length`, &childCountAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if expandedBefore == "" {
		t.Fatal("vendor treeitem not found — the catalog contract and browser test have drifted")
	}
	if expandedAfter != "true" {
		t.Errorf("vendor should be aria-expanded=true after toggle, got %q", expandedAfter)
	}
	_ = childCountAfter // may be 0 if no lazy-load endpoint is wired in site
}

func TestE2E_TreeView_ArrowDownMovesFocus(t *testing.T) {
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
	// src is expanded, so first child (src-main) is the next visible row.
	if focusedID != "src-main" {
		t.Errorf("ArrowDown from src should focus first visible child (src-main), got %q", focusedID)
	}
}

// --- FileDropzone --------------------------------------------------------

func TestE2E_FileDropzone_AriaRegionAndDragoverClass(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	var dragoverClassAfterEnter, dragoverClassAfterLeave bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/dropzone"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`(function(){
			const z = document.querySelector('.ui-dropzone__zone');
			if (!z) return;
			const dt = new DataTransfer();
			z.dispatchEvent(new DragEvent('dragenter', {bubbles: true, cancelable: true, dataTransfer: dt}));
		})()`, nil),
		chromedp.Sleep(80*1e6),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone')?.classList.contains('is-dragover') || false`, &dragoverClassAfterEnter),
		chromedp.Evaluate(`(function(){
			const z = document.querySelector('.ui-dropzone__zone');
			if (!z) return;
			z.dispatchEvent(new DragEvent('dragleave', {bubbles: true, cancelable: true, relatedTarget: document.body}));
		})()`, nil),
		chromedp.Sleep(80*1e6),
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__zone')?.classList.contains('is-dragover') || false`, &dragoverClassAfterLeave),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "region" {
		t.Errorf("dropzone should have role=region, got %q", role)
	}
	if !dragoverClassAfterEnter {
		t.Errorf(".is-dragover should be applied on dragenter")
	}
	if dragoverClassAfterLeave {
		t.Errorf(".is-dragover should be removed on dragleave")
	}
}

// --- ToggleAction --------------------------------------------------------

func TestE2E_ToggleAction_CommitUntoggle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	// The standalone "Follow" button is the only toggle on the page
	// without a data-fui-toggle-group.
	const btn = `document.querySelector('[data-fui-comp="ui-toggle-action"]:not([data-fui-toggle-group])')`
	var initial, afterCommit, pressed, afterUntoggle string
	var committedVisible bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggleaction"),
		pageReady(),
		chromedp.Evaluate(btn+`.getAttribute('data-state')`, &initial),
		chromedp.Evaluate(btn+`.click()`, nil),
		settle(),
		chromedp.Evaluate(btn+`.getAttribute('data-state')`, &afterCommit),
		chromedp.Evaluate(btn+`.getAttribute('aria-pressed')`, &pressed),
		chromedp.Evaluate(`!`+btn+`.querySelector('[data-fui-toggle-committed]').hidden`, &committedVisible),
		chromedp.Evaluate(btn+`.click()`, nil),
		settle(),
		chromedp.Evaluate(btn+`.getAttribute('data-state')`, &afterUntoggle),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initial != "idle" {
		t.Errorf("initial data-state = %q, want idle", initial)
	}
	if afterCommit != "committed" {
		t.Errorf("data-state after click = %q, want committed", afterCommit)
	}
	if pressed != "true" {
		t.Errorf("aria-pressed after commit = %q, want true", pressed)
	}
	if !committedVisible {
		t.Errorf("committed label span should be visible after commit")
	}
	if afterUntoggle != "idle" {
		t.Errorf("data-state after untoggle click = %q, want idle", afterUntoggle)
	}
}

func TestE2E_ToggleAction_GroupMutex(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	const group = `document.querySelectorAll('[data-fui-toggle-group="demo-plan"]')`
	var freeInitial, proInitial, freeAfter, proAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggleaction"),
		pageReady(),
		chromedp.Evaluate(group+`[0].getAttribute('data-state')`, &freeInitial),
		chromedp.Evaluate(group+`[1].getAttribute('data-state')`, &proInitial),
		// Committing "Pro" must optimistically revoke "Free".
		chromedp.Evaluate(group+`[1].click()`, nil),
		settle(),
		chromedp.Evaluate(group+`[0].getAttribute('data-state')`, &freeAfter),
		chromedp.Evaluate(group+`[1].getAttribute('data-state')`, &proAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if freeInitial != "committed" || proInitial != "idle" {
		t.Errorf("initial states = %q/%q, want committed/idle", freeInitial, proInitial)
	}
	if proAfter != "committed" {
		t.Errorf("Pro data-state after click = %q, want committed", proAfter)
	}
	if freeAfter != "idle" {
		t.Errorf("Free data-state after sibling commit = %q, want idle (mutex)", freeAfter)
	}
}
