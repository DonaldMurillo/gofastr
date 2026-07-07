package main

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// =============================================================================
// Behavioral e2e coverage for the multiselect + combobox patterns
// (previously note-only on the gallery; the demos are static-option /
// checkbox-group variants that need no backend wiring).
//
// The multiselect demo deliberately uses Value != Label ("cpp" vs
// "C++") so a chip that renders the raw form Value instead of the
// visible Label fails here.
// =============================================================================

// waitModule polls until the named demand-loaded runtime module is up.
func waitModule(expr string) chromedp.Action {
	return chromedp.Poll(expr, nil, chromedp.WithPollingInterval(50*1e6))
}

// --- Multiselect ----------------------------------------------------------

func TestE2E_MultiselectChipShowsLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bootChip, cppChip string
	var chipCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/multiselect"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.multiselect)`),
		settle(),
		// Go ships Selected:true — the boot scan must render its chip
		// with the visible Label.
		chromedp.Evaluate(`document.querySelector('.ui-multiselect__chip span')?.textContent || ''`, &bootChip),
		// Open the disclosure and pick C++ (Value "cpp").
		chromedp.Click(`.ui-multiselect__summary`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('demo-multiselect-opt-1').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-multiselect__chip').length`, &chipCount),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-multiselect__chip span')).map(s => s.textContent).join('|')`, &cppChip),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if bootChip != "Go" {
		t.Errorf("boot chip must show the Label %q, got %q", "Go", bootChip)
	}
	if chipCount != 2 {
		t.Errorf("expected 2 chips after picking C++, got %d", chipCount)
	}
	if cppChip != "Go|C++" {
		t.Errorf("chips must show Labels, not Values — want Go|C++, got %q", cppChip)
	}
}

func TestE2E_MultiselectChipRemove(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipCount int
	var checked bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/multiselect"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.multiselect)`),
		settle(),
		// Remove the pre-selected Go chip via its × button.
		chromedp.Click(`[data-fui-multiselect-remove="demo-multiselect-opt-0"]`, chromedp.ByQuery),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-multiselect__chip').length`, &chipCount),
		chromedp.Evaluate(`document.getElementById('demo-multiselect-opt-0').checked`, &checked),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipCount != 0 {
		t.Errorf("expected 0 chips after removal, got %d", chipCount)
	}
	if checked {
		t.Error("removing the chip must uncheck the linked checkbox")
	}
}

// --- Combobox --------------------------------------------------------------

func TestE2E_ComboboxFilterAndSelect(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var ssrExpanded, pickedValue, expandedAfterPick string
	var visibleOpts int
	var listboxHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.combobox)`),
		// SSR state: static options render visible, so the input must
		// claim expanded from first paint.
		chromedp.Evaluate(`document.getElementById('demo-combobox').getAttribute('aria-expanded')`, &ssrExpanded),
		// Filter down to Accordion.
		chromedp.SendKeys(`#demo-combobox`, "acc", chromedp.ByID),
		settle(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#demo-combobox-listbox [role="option"]')).filter(o => !o.hidden).length`, &visibleOpts),
		// Pick the surviving option.
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#demo-combobox-listbox [role="option"]')).find(o => !o.hidden).click()`, nil),
		settle(),
		chromedp.Evaluate(`document.getElementById('demo-combobox').value`, &pickedValue),
		chromedp.Evaluate(`document.getElementById('demo-combobox').getAttribute('aria-expanded')`, &expandedAfterPick),
		chromedp.Evaluate(`document.getElementById('demo-combobox-listbox').hasAttribute('hidden')`, &listboxHidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ssrExpanded != "true" {
		t.Errorf("static combobox must SSR aria-expanded=true, got %q", ssrExpanded)
	}
	if visibleOpts != 1 {
		t.Errorf("typing 'acc' should leave 1 visible option, got %d", visibleOpts)
	}
	if pickedValue != "accordion" {
		t.Errorf("picking should fill the input with data-value, got %q", pickedValue)
	}
	if expandedAfterPick != "false" || !listboxHidden {
		t.Errorf("picking must close the listbox (expanded=%q hidden=%v)", expandedAfterPick, listboxHidden)
	}
}

func TestE2E_ComboboxEscapeDismisses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var expanded string
	var hidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		waitModule(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.combobox)`),
		// The listbox is visibly open at SSR; Escape must dismiss it
		// WITHOUT requiring a prior keystroke to sync state.
		chromedp.Click(`#demo-combobox`, chromedp.ByID),
		chromedp.SendKeys(`#demo-combobox`, kb.Escape, chromedp.ByID),
		settle(),
		chromedp.Evaluate(`document.getElementById('demo-combobox').getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`document.getElementById('demo-combobox-listbox').hasAttribute('hidden')`, &hidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if expanded != "false" || !hidden {
		t.Errorf("Escape must dismiss the open listbox (expanded=%q hidden=%v)", expanded, hidden)
	}
}
