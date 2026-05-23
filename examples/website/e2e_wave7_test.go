package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// E2E tests for Wave 7 components: Select, RadioGroup, CheckboxGroup,
// AspectRatio, SkipLink, ThemeToggle, Sticky.
// =============================================================================

// ─── Select ─────────────────────────────────────────────────────────

func TestE2E_Select_BasicRenders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	var hasLabel bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		// Should render at least one <select> inside a form field
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-select"]').length`, &count),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"] label.ui-select__label') !== null`, &hasLabel),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 1 {
		t.Error("expected at least one ui-select component on the page")
	}
	if !hasLabel {
		t.Error("expected a <label> with class ui-select__label inside the component")
	}
}

func TestE2E_Select_HasOptions(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var optCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		// The basic demo has 5 countries
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-select"] select option').length`, &optCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if optCount < 5 {
		t.Errorf("expected ≥5 options across all selects, got %d", optCount)
	}
}

func TestE2E_Select_ErrorState(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasError bool
	var hasAlert bool
	var hasAriaInvalid bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		// The error demo has is-error class
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"].is-error') !== null`, &hasError),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"].is-error .ui-select__error[role="alert"]') !== null`, &hasAlert),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"].is-error select[aria-invalid="true"]') !== null`, &hasAriaInvalid),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasError {
		t.Error("expected an is-error select component")
	}
	if !hasAlert {
		t.Error("expected role=alert error message in error state")
	}
	if !hasAriaInvalid {
		t.Error("expected aria-invalid=true on the <select> in error state")
	}
}

func TestE2E_Select_DisabledState(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasDisabled bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-select"].is-disabled select[disabled]') !== null`, &hasDisabled),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasDisabled {
		t.Error("expected a disabled select component with [disabled] attribute")
	}
}

func TestE2E_Select_CustomArrow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bgImage string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/select"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var sel = document.querySelector('[data-fui-comp="ui-select"] select');
			return getComputedStyle(sel).backgroundImage;
		})()`, &bgImage),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if bgImage == "" || bgImage == "none" {
		t.Error("expected custom chevron background-image on the <select>")
	}
}

// ─── RadioGroup ─────────────────────────────────────────────────────

func TestE2E_RadioGroup_FieldsetRenders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	var legendText string
	var radioCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group[role="radiogroup"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group[role="radiogroup"] legend')?.textContent || ''`, &legendText),
		chromedp.Evaluate(`document.querySelectorAll('.ui-toggle-group[role="radiogroup"] input[type="radio"]').length`, &radioCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "radiogroup" {
		t.Errorf("expected role=radiogroup, got %q", role)
	}
	if !strings.Contains(legendText, "Choose a plan") {
		t.Errorf("expected legend 'Choose a plan', got %q", legendText)
	}
	if radioCount < 3 {
		t.Errorf("expected ≥3 radio inputs in the basic group, got %d", radioCount)
	}
}

func TestE2E_RadioGroup_UniqueIDs(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasDuplicates bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var radios = document.querySelectorAll('.ui-toggle-group[role="radiogroup"] input[type="radio"]');
			var ids = {};
			for (var r of radios) {
				if (r.id && ids[r.id]) return true;
				ids[r.id] = true;
			}
			return false;
		})()`, &hasDuplicates),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hasDuplicates {
		t.Error("radio inputs have duplicate IDs — each radio must have a unique id")
	}
}

func TestE2E_RadioGroup_SharedName(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var names []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		// Get all unique name= values in the first radiogroup only
		chromedp.Evaluate(`(function() {
			var group = document.querySelector('.ui-toggle-group[role="radiogroup"]');
			var radios = group.querySelectorAll('input[type="radio"]');
			var names = new Set();
			for (var r of radios) names.add(r.name);
			return [...names];
		})()`, &names),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("expected all radios to share one name, got %v", names)
	}
}

func TestE2E_RadioGroup_ErrorState(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasError bool
	var hasAlert bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group.is-error') !== null`, &hasError),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group.is-error .ui-toggle-group__error[role="alert"]') !== null`, &hasAlert),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasError {
		t.Error("expected an is-error radiogroup")
	}
	if !hasAlert {
		t.Error("expected role=alert error message in radiogroup error state")
	}
}

func TestE2E_RadioGroup_HelpText(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasHelp bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group .ui-toggle-group__help') !== null`, &hasHelp),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasHelp {
		t.Error("expected help text in a radiogroup")
	}
}

// ─── CheckboxGroup ──────────────────────────────────────────────────

func TestE2E_CheckboxGroup_FieldsetRenders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	var legendText string
	var cbCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		// The CheckboxGroup demo uses role="group"
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group[role="group"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('.ui-toggle-group[role="group"] legend')?.textContent || ''`, &legendText),
		chromedp.Evaluate(`document.querySelectorAll('.ui-toggle-group[role="group"] input[type="checkbox"]').length`, &cbCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "group" {
		t.Errorf("expected role=group, got %q", role)
	}
	if !strings.Contains(legendText, "Preferred contact") {
		t.Errorf("expected legend with 'Preferred contact', got %q", legendText)
	}
	if cbCount < 3 {
		t.Errorf("expected ≥3 checkboxes, got %d", cbCount)
	}
}

func TestE2E_CheckboxGroup_UniqueIDs(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasDuplicates bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var cbs = document.querySelectorAll('.ui-toggle-group[role="group"] input[type="checkbox"]');
			var ids = {};
			for (var c of cbs) {
				if (c.id && ids[c.id]) return true;
				ids[c.id] = true;
			}
			return false;
		})()`, &hasDuplicates),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hasDuplicates {
		t.Error("checkbox inputs have duplicate IDs — each checkbox must have a unique id")
	}
}

func TestE2E_CheckboxGroup_SharedName(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var names []string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var cbs = document.querySelectorAll('.ui-toggle-group[role="group"] input[type="checkbox"]');
			var names = new Set();
			for (var c of cbs) names.add(c.name);
			return [...names];
		})()`, &names),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("expected all checkboxes to share one name, got %v", names)
	}
}

func TestE2E_CheckboxGroup_LabelForAssociation(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var mismatches int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		// Each label[for] must point to an existing input id
		chromedp.Evaluate(`(function() {
			var labels = document.querySelectorAll('.ui-toggle-group[role="group"] label[for]');
			var bad = 0;
			for (var l of labels) {
				var input = document.getElementById(l.getAttribute('for'));
				if (!input || input.type !== 'checkbox') bad++;
			}
			return bad;
		})()`, &mismatches),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if mismatches > 0 {
		t.Errorf("%d label[for] attrs don't point to their checkbox input", mismatches)
	}
}

// ─── AspectRatio ────────────────────────────────────────────────────

func TestE2E_AspectRatio_RendersBoxes(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-aspect-ratio"]').length`, &count),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 4 {
		t.Errorf("expected ≥4 aspect-ratio boxes on the page, got %d", count)
	}
}

func TestE2E_AspectRatio_ClassesApply(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var has1_1, has16_9 bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-aspect-ratio"].ui-ar--1-1') !== null`, &has1_1),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-aspect-ratio"].ui-ar--16-9') !== null`, &has16_9),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !has1_1 {
		t.Error("expected a 1:1 aspect-ratio box")
	}
	if !has16_9 {
		t.Error("expected a 16:9 aspect-ratio box")
	}
}

func TestE2E_AspectRatio_CSSAppliesAspectRatio(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var ar string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		// The 16:9 box should have aspect-ratio: 16 / 9 in computed style
		chromedp.Evaluate(`(function() {
			var el = document.querySelector('[data-fui-comp="ui-aspect-ratio"].ui-ar--16-9');
			return getComputedStyle(el).aspectRatio;
		})()`, &ar),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if ar == "" || ar == "auto" {
		t.Errorf("expected aspect-ratio CSS to apply, got %q", ar)
	}
}

func TestE2E_AspectRatio_ChildIsAbsolute(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pos string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/aspectratio"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var box = document.querySelector('[data-fui-comp="ui-aspect-ratio"].ui-ar--16-9');
			var child = box ? box.firstElementChild : null;
			return child ? getComputedStyle(child).position : 'none';
		})()`, &pos),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pos != "absolute" {
		t.Errorf("expected child to be position:absolute, got %q", pos)
	}
}

// ─── SkipLink ───────────────────────────────────────────────────────

func TestE2E_SkipLink_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var href string
	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skiplink"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-skip-link"]')?.getAttribute('href') || ''`, &href),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-skip-link"]')?.textContent || ''`, &text),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if href != "#main-content" {
		t.Errorf("expected href='#main-content', got %q", href)
	}
	if !strings.Contains(text, "Skip") {
		t.Errorf("expected link text to contain 'Skip', got %q", text)
	}
}

func TestE2E_SkipLink_HiddenByDefault(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var offScreen bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/skiplink"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var link = document.querySelector('a[data-fui-comp="ui-skip-link"]');
			if (!link) return false;
			var rect = link.getBoundingClientRect();
			// Off-screen = far left or zero width/height
			return rect.left < -100 || (rect.width === 0 && rect.height === 0);
		})()`, &offScreen),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !offScreen {
		t.Error("SkipLink should be off-screen by default (only visible on focus)")
	}
}

// ─── ThemeToggle ────────────────────────────────────────────────────

func TestE2E_ThemeToggle_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var iconPresent, pillPresent bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/themetoggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-theme-toggle][aria-label="Toggle color scheme"]') !== null`, &iconPresent),
		chromedp.Evaluate(`document.querySelector('[data-fui-theme-toggle="pill"]') !== null`, &pillPresent),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !iconPresent {
		t.Error("expected icon variant of ThemeToggle")
	}
	if !pillPresent {
		t.Error("expected pill variant of ThemeToggle")
	}
}

func TestE2E_ThemeToggle_RuntimeModuleLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var loaded bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/themetoggle"),
		chromedp.Sleep(1500*time.Millisecond), // wait for demand-load
		chromedp.Evaluate(`(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.themeswitch) || false`, &loaded),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !loaded {
		t.Error("themeswitch runtime module should have been demand-loaded")
	}
}

func TestE2E_ThemeToggle_ClickCyclesScheme(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var schemeAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/themetoggle"),
		chromedp.Sleep(1500*time.Millisecond), // wait for module load
		// Click the icon toggle
		chromedp.Evaluate(`document.querySelector('button[data-fui-theme-toggle][aria-label="Toggle color scheme"]').click()`, nil),
		settle(),
		// Check that data-color-scheme was set on <html>
		chromedp.Evaluate(`document.documentElement.getAttribute('data-color-scheme') || ''`, &schemeAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if schemeAfter == "" {
		t.Error("expected data-color-scheme to be set on <html> after clicking ThemeToggle")
	}
}

// ─── Sticky ─────────────────────────────────────────────────────────

func TestE2E_Sticky_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sticky"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-sticky"]').length`, &count),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 3 {
		t.Errorf("expected ≥3 sticky elements on the page, got %d", count)
	}
}

func TestE2E_Sticky_TopHasStickyCSS(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pos string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sticky"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var el = document.querySelector('[data-fui-comp="ui-sticky"].ui-sticky--top');
			return el ? getComputedStyle(el).position : 'none';
		})()`, &pos),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// "sticky" or "-webkit-sticky" (Chrome normalizes to "sticky")
	if pos != "sticky" && pos != "-webkit-sticky" {
		t.Errorf("expected position:sticky, got %q", pos)
	}
}

func TestE2E_Sticky_BottomSticky(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bottom string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sticky"),
		pageReady(),
		chromedp.Evaluate(`(function() {
			var el = document.querySelector('[data-fui-comp="ui-sticky"].ui-sticky--bottom');
			return el ? getComputedStyle(el).bottom : 'none';
		})()`, &bottom),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if bottom == "auto" || bottom == "" {
		t.Errorf("expected bottom != auto on bottom-sticky element, got %q", bottom)
	}
}

// ─── BackToTop ──────────────────────────────────────────────────────

func TestE2E_BackToTop_Renders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var exists bool
	var tag string
	var ariaLabel string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		// Check the default BackToTop button exists
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]') !== null`, &exists),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.tagName?.toLowerCase() || ''`, &tag),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.getAttribute('aria-label') || ''`, &ariaLabel),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected [data-fui-back-to-top] element to exist")
	}
	if tag != "button" {
		t.Errorf("expected tag button, got %q", tag)
	}
	if ariaLabel != "Back to top" {
		t.Errorf("expected aria-label 'Back to top', got %q", ariaLabel)
	}
}

func TestE2E_BackToTop_HiddenByDefault(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inert bool
	var visibleAttr bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		// The button should be inert (not focusable, AT-hidden) and have no data-fui-btt-visible.
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('inert')`, &inert),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('data-fui-btt-visible')`, &visibleAttr),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !inert {
		t.Error("expected inert attribute initially (button must not be focusable when hidden)")
	}
	if visibleAttr {
		t.Error("expected no data-fui-btt-visible attribute initially")
	}
}

func TestE2E_BackToTop_CustomIcon(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	// The page has multiple BackToTop buttons. Find the one with a filled arrow (custom icon).
	var hasCustomIcon bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		// The custom icon uses fill="currentColor" — check that one exists
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-fui-back-to-top] svg')).some(s => s.getAttribute('fill') === 'currentColor')`, &hasCustomIcon),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCustomIcon {
		t.Error("expected at least one BackToTop with custom icon (fill=currentColor)")
	}
}

func TestE2E_BackToTop_SizeClasses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasSM bool
	var hasLG bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--sm') !== null`, &hasSM),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--lg') !== null`, &hasLG),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSM {
		t.Error("expected a BackToTop with --sm class")
	}
	if !hasLG {
		t.Error("expected a BackToTop with --lg class")
	}
}

func TestE2E_BackToTop_VariantClasses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasSecondary bool
	var hasGhost bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--secondary') !== null`, &hasSecondary),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--ghost') !== null`, &hasGhost),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSecondary {
		t.Error("expected a BackToTop with --secondary class")
	}
	if !hasGhost {
		t.Error("expected a BackToTop with --ghost class")
	}
}

func TestE2E_BackToTop_PositionClasses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hasBL bool
	var hasTR bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--bl') !== null`, &hasBL),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top].ui-back-to-top--tr') !== null`, &hasTR),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBL {
		t.Error("expected a BackToTop with --bl class")
	}
	if !hasTR {
		t.Error("expected a BackToTop with --tr class")
	}
}

func TestE2E_BackToTop_RuntimeModuleLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var loaded bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		// Wait for module to load
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.backtotop) || false`, &loaded),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Error("expected backtotop runtime module to be loaded")
	}
}

func TestE2E_BackToTop_ScrollShowsButton(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var visible bool
	var inert bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		// Scroll down to trigger the sentinel IntersectionObserver
		chromedp.Evaluate(`window.scrollTo(0, 600)`, nil),
		// Give IntersectionObserver time to fire
		chromedp.Sleep(500*time.Millisecond),
		// Check the first (default) BackToTop button
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('data-fui-btt-visible') || false`, &visible),
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top]')?.hasAttribute('inert')`, &inert),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !visible {
		t.Error("expected data-fui-btt-visible after scrolling past threshold")
	}
	if inert {
		t.Error("expected inert to be removed after scrolling, button should be focusable")
	}
}

func TestE2E_BackToTop_ClickScrollsToTop(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var scrollY int64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/backtotop"),
		pageReady(),
		// Scroll down first
		chromedp.Evaluate(`window.scrollTo(0, 800)`, nil),
		chromedp.Sleep(500*time.Millisecond),
		// Click the first BackToTop button
		chromedp.Evaluate(`document.querySelector('[data-fui-back-to-top][data-fui-btt-visible]')?.click()`, nil),
		// Wait for smooth scroll
		chromedp.Sleep(800*time.Millisecond),
		// Check scroll position
		chromedp.Evaluate(`window.scrollY`, &scrollY),
	)
	if err != nil {
		t.Fatal(err)
	}
	if scrollY > 50 {
		t.Errorf("expected scrollY near 0 after clicking BackToTop, got %d", scrollY)
	}
}
