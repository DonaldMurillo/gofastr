package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Form module E2E tests — retargeted from /components/forms to per-page routes
// =============================================================================
//
// The original tests targeted the website's combined /components/forms page.
// The site has NO combined forms page — each component lives at its own
// /components/<slug> URL. Tests are retargeted below; several are dropped.
//
// DROPPED sub-tests (no equivalent in site):
//   - TestE2E_ConditionalField_TogglesVisibility
//     /components/conditionalfield is a "Note" page (not live — no radio toggle).
//   - TestE2E_StepWizard_Renders (from combined page)
//     The site's /components/stepwizard page has a static demo (Action="#") — no
//     real POST endpoint, so button/back state can't be relied on for the same
//     interactive assertions. Wizard behavior is fully covered by
//     e2e_wizard_followon_test.go against the real /forms/wizard endpoint.
//   - TestE2E_FormRepeater_* (all repeater island tests)
//     /components/formrepeater is a "Note" page — no live island is wired. All
//     add/remove/value-preservation tests require a running RPC island.
//   - TestE2E_ValidationRoundTrip_ShowsErrors
//     Required a pre-submitted validation state from the combined page.
//   - TestE2E_FormErrorCallout_Renders
//     Required the combined page's danger-callout block.
//   - TestE2E_FormsPage_AllFormsHaveMethod
//   - TestE2E_Select_RendersWithOptions
//   - TestE2E_FormsPage_LabelsMatchInputs
//   - TestE2E_FormsPage_LoadsQuickly
//     All targeted /components/forms directly; no site equivalent.
//   - TestE2E_FormRepeater_RemoveSingleIsDisabled
//   - TestE2E_FormRepeater_RemoveFirstFromThree
//     Both required live repeater island.

// ─── PasswordInput ──────────────────────────────────────────────────
// Retargeted to /components/passwordinput.

func TestE2E_PasswordInput_RendersAndToggles(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/passwordinput"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			var wrapper = document.querySelector('[data-fui-comp="ui-password-input"]');
			if (!wrapper) return {type:'',label:''};
			var input = wrapper.querySelector('input');
			var btn = wrapper.querySelector('.ui-password-input__toggle');
			return {
				type: input ? input.type : '',
				label: btn ? btn.getAttribute('aria-label') : ''
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Type  string `json:"type"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Type != "password" {
		t.Errorf("expected initial type=password, got %q", result.Type)
	}
	if !strings.Contains(strings.ToLower(result.Label), "show") {
		t.Errorf("expected aria-label containing 'show', got %q", result.Label)
	}

	// Click the toggle button.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			var btn = document.querySelector('[data-fui-comp="ui-password-input"] .ui-password-input__toggle');
			if (btn) btn.click();
			var input = document.querySelector('[data-fui-comp="ui-password-input"] input');
			return input ? input.type : '';
		})()`, &result.Type),
	)
	if err != nil {
		t.Fatalf("chromedp toggle click: %v", err)
	}
	if result.Type != "text" {
		t.Errorf("expected type=text after toggle, got %q", result.Type)
	}
}

// ─── SearchInput ────────────────────────────────────────────────────
// Retargeted to /components/searchinput.

func TestE2E_SearchInput_ClearButton(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var value string
	var clearHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/searchinput"),
		pageReady(),
		// Type into the search input.
		chromedp.Evaluate(`(() => {
			var input = document.querySelector('[data-fui-comp="ui-search-input"] input');
			if (!input) return '';
			input.value = 'hello';
			input.dispatchEvent(new Event('input', {bubbles: true}));
			return input.value;
		})()`, &value),
		// Check clear button is visible.
		chromedp.Evaluate(`(() => {
			var btn = document.querySelector('[data-fui-comp="ui-search-input"] .ui-search-input__clear');
			return btn ? btn.hasAttribute('hidden') : true;
		})()`, &clearHidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if value != "hello" {
		t.Errorf("expected search value 'hello', got %q", value)
	}
	if clearHidden {
		t.Error("clear button should be visible when input has value")
	}

	// Click clear.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			var btn = document.querySelector('[data-fui-comp="ui-search-input"] .ui-search-input__clear');
			if (btn) btn.click();
			var input = document.querySelector('[data-fui-comp="ui-search-input"] input');
			return input ? input.value : '';
		})()`, &value),
	)
	if err != nil {
		t.Fatalf("chromedp clear click: %v", err)
	}
	if value != "" {
		t.Errorf("expected empty value after clear, got %q", value)
	}
}

// ─── InputGroup ─────────────────────────────────────────────────────
// Retargeted to /components/inputgroup.
// The site demo: Prepend="$", input, Append="USD".

func TestE2E_InputGroup_Renders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hasPrepend, hasAppend bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/inputgroup"),
		pageReady(),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-comp="ui-input-group"] .ui-input-group__prepend')`, &hasPrepend),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-comp="ui-input-group"] .ui-input-group__append')`, &hasAppend),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasPrepend {
		t.Error("expected prepend element in input group")
	}
	if !hasAppend {
		t.Error("expected append element in input group")
	}
}

// ─── ValidationSummary ──────────────────────────────────────────────
// Retargeted to /components/validationsummary.
// Site demo has 2 errors: email + password. FieldOrder: ["email","password"].

func TestE2E_ValidationSummary_RendersLinks(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/validationsummary"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			var summaries = document.querySelectorAll('[data-fui-comp="ui-validation-summary"]');
			if (summaries.length === 0) return {links:0, anchor:false};
			var last = summaries[summaries.length - 1];
			var links = last.querySelectorAll('a[href^="#"]');
			return {
				links: links.length,
				anchor: links.length > 0 && links[0].getAttribute('href').startsWith('#')
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Links  int  `json:"links"`
		Anchor bool `json:"anchor"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	// Site demo has 2 errors (email + password).
	if result.Links < 2 {
		t.Errorf("expected at least 2 validation summary links, got %d", result.Links)
	}
	if !result.Anchor {
		t.Error("expected anchor links in validation summary")
	}
}

// TestE2EFieldOrderAndTitle verifies deterministic order + custom-title
// features. The site demo uses FieldOrder=["email","password"] with no
// custom Title (so the default title text is shown). Anchors are "#email"
// and "#password" (not "#val-*" — that was the website's demo).
//
// Softened from website: we only assert field order is deterministic and
// anchors start with "#"; we do NOT assert a custom title (site uses default).
func TestE2EFieldOrderAndTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/validationsummary"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			var sums = document.querySelectorAll('[data-fui-comp="ui-validation-summary"]');
			if (sums.length === 0) return {found: false};
			var last = sums[sums.length - 1];
			var links = Array.from(last.querySelectorAll('a[href^="#"]'));
			return {
				found: true,
				titleText: (last.querySelector('.ui-validation-summary__title') || {}).textContent || '',
				hrefs: links.map(a => a.getAttribute('href')),
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Found     bool     `json:"found"`
		TitleText string   `json:"titleText"`
		Hrefs     []string `json:"hrefs"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !result.Found {
		t.Fatal("no ui-validation-summary on /components/validationsummary")
	}
	if len(result.Hrefs) < 2 {
		t.Fatalf("expected ≥2 anchor links, got %v", result.Hrefs)
	}
	// Field order must be deterministic: email before password.
	// The site renders FieldOrder: ["email","password"].
	emailIdx, pwIdx := -1, -1
	for i, h := range result.Hrefs {
		if strings.Contains(h, "email") {
			emailIdx = i
		}
		if strings.Contains(h, "password") {
			pwIdx = i
		}
	}
	if emailIdx < 0 {
		t.Errorf("no email anchor found in hrefs %v", result.Hrefs)
	}
	if pwIdx < 0 {
		t.Errorf("no password anchor found in hrefs %v", result.Hrefs)
	}
	if emailIdx >= 0 && pwIdx >= 0 && emailIdx > pwIdx {
		t.Errorf("FieldOrder violated: email (%d) should come before password (%d), hrefs=%v",
			emailIdx, pwIdx, result.Hrefs)
	}
}

// ─── Checkbox/Radio Primitives ──────────────────────────────────────
// Retargeted to /components/checkbox and /components/radio individually.

func TestE2E_CheckboxRadio_PrimitivesPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Check /components/checkbox has a checkbox input.
	var checkboxCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/checkbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('input[type="checkbox"]').length`, &checkboxCount),
	); err != nil {
		t.Fatalf("chromedp checkbox: %v", err)
	}
	if checkboxCount < 1 {
		t.Error("expected at least one checkbox on /components/checkbox")
	}

	// Check /components/radio has radio inputs.
	var radioCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/radio"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('input[type="radio"]').length`, &radioCount),
	); err != nil {
		t.Fatalf("chromedp radio: %v", err)
	}
	if radioCount < 1 {
		t.Error("expected at least one radio on /components/radio")
	}
}

// ─── Page Load Timing ──────────────────────────────────────────────
// Retargeted to /components/passwordinput as a representative form component.

func TestE2E_FormsPage_LoadsQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	start := time.Now()
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/passwordinput"),
		pageReady(),
	)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("page took %v to load (expected <5s)", elapsed)
	}
}
