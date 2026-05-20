package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Form module E2E tests
// =============================================================================
//
// Covers all new form components: PasswordInput, SearchInput, InputGroup,
// ConditionalField, StepWizard, FormRepeater, ValidationSummary,
// and the Checkbox/Radio HTML primitives on the /components/forms page.

// ─── PasswordInput ──────────────────────────────────────────────────

func TestE2E_PasswordInput_RendersAndToggles(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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

func TestE2E_SearchInput_ClearButton(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var value string
	var clearHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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

func TestE2E_InputGroup_Renders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hasPrepend, hasAppend bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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

// ─── ConditionalField ───────────────────────────────────────────────

func TestE2E_ConditionalField_TogglesVisibility(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Initially the conditional field should be hidden.
	var initiallyHidden bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`(() => {
			var cf = document.querySelector('[data-fui-comp="ui-conditional-field"]');
			return cf ? cf.hasAttribute('hidden') : true;
		})()`, &initiallyHidden),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !initiallyHidden {
		t.Error("conditional field should start hidden")
	}

	// Click the "Business" radio to show the conditional field.
	var afterClickHidden bool
	var _discard bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			var radios = document.querySelectorAll('input[name="account-type"]');
			for (var i = 0; i < radios.length; i++) {
				if (radios[i].value === 'business') {
					radios[i].click();
					break;
				}
			}
			return true;
		})()`, &_discard),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(() => {
			var cf = document.querySelector('[data-fui-comp="ui-conditional-field"]');
			return cf ? cf.hasAttribute('hidden') : true;
		})()`, &afterClickHidden),
	)
	if err != nil {
		t.Fatalf("chromedp business click: %v", err)
	}
	if afterClickHidden {
		t.Error("conditional field should be visible after selecting 'business'")
	}

	// Click "Personal" to hide it again.
	var afterPersonalHidden bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			var radios = document.querySelectorAll('input[name="account-type"]');
			for (var i = 0; i < radios.length; i++) {
				if (radios[i].value === 'personal') {
					radios[i].click();
					break;
				}
			}
			return true;
		})()`, &_discard),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(() => {
			var cf = document.querySelector('[data-fui-comp="ui-conditional-field"]');
			return cf ? cf.hasAttribute('hidden') : true;
		})()`, &afterPersonalHidden),
	)
	if err != nil {
		t.Fatalf("chromedp personal click: %v", err)
	}
	if !afterPersonalHidden {
		t.Error("conditional field should be hidden after selecting 'personal'")
	}
}

// ─── StepWizard ─────────────────────────────────────────────────────

func TestE2E_StepWizard_Renders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			var wiz = document.querySelector('[data-fui-comp="ui-step-wizard"]');
			if (!wiz) return {steps:0, cont:false, back:false};
			return {
				steps: wiz.querySelectorAll('.ui-step-wizard__step-dot').length,
				cont: !!wiz.querySelector('button[value="next"]'),
				back: !!wiz.querySelector('button[value="back"]')
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Steps int  `json:"steps"`
		Cont  bool `json:"cont"`
		Back  bool `json:"back"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Steps != 3 {
		t.Errorf("expected 3 step dots, got %d", result.Steps)
	}
	if !result.Cont {
		t.Error("expected Continue/Submit button on wizard")
	}
	if result.Back {
		t.Error("first step should NOT have a Back button")
	}
}

// ─── FormRepeater ───────────────────────────────────────────────────

func TestE2E_FormRepeater_Renders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			if (!rep) return {items:0, add:false};
			return {
				items: rep.querySelectorAll('.ui-form-repeater__item').length,
				add: !!rep.querySelector('[data-fui-rpc*="action=add"]')
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Items int  `json:"items"`
		Add   bool `json:"add"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Items < 1 {
		t.Errorf("expected at least 1 repeater item, got %d", result.Items)
	}
	if !result.Add {
		t.Error("expected Add button in repeater")
	}
}

// ─── FormRepeater: Add increases item count ────────────────────────

func TestE2E_FormRepeater_AddIncreasesCount(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Initial: 1 item
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			return rep ? rep.querySelectorAll('.ui-form-repeater__item').length : 0;
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if raw != "1" {
		t.Fatalf("expected 1 item initially, got %s", raw)
	}

	// Click Add
	err = chromedp.Run(ctx,
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			return rep ? rep.querySelectorAll('.ui-form-repeater__item').length : 0;
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp after add: %v", err)
	}
	if raw != "2" {
		t.Errorf("expected 2 items after add, got %s", raw)
	}
}

// ─── FormRepeater: Remove decreases item count ──────────────────────

func TestE2E_FormRepeater_RemoveDecreasesCount(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Add to get 2 items
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// Now remove first item
		chromedp.Click(`[data-fui-rpc*="action=remove-0"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			return rep ? rep.querySelectorAll('.ui-form-repeater__item').length : 0;
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if raw != "1" {
		t.Errorf("expected 1 item after remove, got %s", raw)
	}
}

// ─── FormRepeater: Remove targets specific item ─────────────────────

func TestE2E_FormRepeater_RemoveSpecificItem(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Fill row 0
		chromedp.SetValue(`#f-m0-name`, "Alice", chromedp.ByQuery),
		// Add row 1
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.SetValue(`#f-m1-name`, "Bob", chromedp.ByQuery),
		// Add row 2
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.SetValue(`#f-m2-name`, "Charlie", chromedp.ByQuery),
		// Remove row 1 (Bob — the middle one)
		chromedp.Click(`[data-fui-rpc*="action=remove-1"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// After swap: should have 2 rows, Alice=0, Charlie=1
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			if (!rep) return {items:0, r0:"", r1:"", r2:""};
			var val = (id) => { var el = document.getElementById(id); return el ? el.value : "__MISSING__"; };
			return {
				items: rep.querySelectorAll('.ui-form-repeater__item').length,
				r0: val('f-m0-name'),
				r1: val('f-m1-name'),
				r2: val('f-m2-name')
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Items int    `json:"items"`
		R0    string `json:"r0"`
		R1    string `json:"r1"`
		R2    string `json:"r2"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Items != 2 {
		t.Errorf("expected 2 items, got %d", result.Items)
	}
	if result.R0 != "Alice" {
		t.Errorf("row 0: expected Alice, got %q", result.R0)
	}
	if result.R1 != "Charlie" {
		t.Errorf("row 1: expected Charlie (re-indexed from row 2), got %q", result.R1)
	}
	if result.R2 != "__MISSING__" {
		t.Errorf("row 2 should not exist, got %q", result.R2)
	}
}

// ─── FormRepeater: Values preserved on add ──────────────────────────

func TestE2E_FormRepeater_ValuesPreservedOnAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Fill row 0
		chromedp.SetValue(`#f-m0-name`, "Alice", chromedp.ByQuery),
		chromedp.SetValue(`#f-m0-email`, "alice@test.com", chromedp.ByQuery),
		// Add row 1
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// Check row 0 values survived
		chromedp.Evaluate(`JSON.stringify((() => {
			var val = (id) => { var el = document.getElementById(id); return el ? el.value : ""; };
			return { name: val('f-m0-name'), email: val('f-m0-email') };
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Name != "Alice" {
		t.Errorf("name: expected Alice, got %q", result.Name)
	}
	if result.Email != "alice@test.com" {
		t.Errorf("email: expected alice@test.com, got %q", result.Email)
	}
}

// ─── ValidationSummary ──────────────────────────────────────────────

func TestE2E_ValidationSummary_RendersLinks(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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
	if result.Links < 3 {
		t.Errorf("expected at least 3 validation summary links, got %d", result.Links)
	}
	if !result.Anchor {
		t.Error("expected anchor links in validation summary")
	}
}

// TestE2E_ValidationSummary_FieldOrderAndTitle verifies the new
// deterministic-order + custom-title features: the demo wires
// FieldOrder=[name, email, password] and Title="Please fix the
// highlighted fields", so on the live page the rows MUST appear in
// that order and the heading must carry the custom title.
func TestE2EFieldOrderAndTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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
		t.Fatal("no ui-validation-summary on /components/forms")
	}
	if !strings.Contains(result.TitleText, "Please fix the highlighted fields") {
		t.Errorf("expected custom Title, got %q", result.TitleText)
	}
	want := []string{"#val-name", "#val-email", "#val-password"}
	if len(result.Hrefs) < 3 {
		t.Fatalf("expected ≥3 anchor links, got %v", result.Hrefs)
	}
	for i, w := range want {
		if result.Hrefs[i] != w {
			t.Errorf("FieldOrder violated at index %d: want %q, got %q (full=%v)",
				i, w, result.Hrefs[i], result.Hrefs)
		}
	}
}

// ─── Validation Round-Trip ──────────────────────────────────────────

func TestE2E_ValidationRoundTrip_ShowsErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var errorCount int
	var hasAriaInvalid bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-form-field__error').length`, &errorCount),
		chromedp.Evaluate(`document.querySelectorAll('[aria-invalid="true"]').length > 0`, &hasAriaInvalid),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if errorCount < 2 {
		t.Errorf("expected at least 2 error messages in validation demo, got %d", errorCount)
	}
	if !hasAriaInvalid {
		t.Error("expected aria-invalid=true on at least one field in validation demo")
	}
}

// ─── Checkbox/Radio Primitives ──────────────────────────────────────

func TestE2E_CheckboxRadio_PrimitivesPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var checkboxCount, radioCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('input[type="checkbox"]').length`, &checkboxCount),
		chromedp.Evaluate(`document.querySelectorAll('input[type="radio"]').length`, &radioCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if checkboxCount < 1 {
		t.Error("expected at least one checkbox on the forms page")
	}
	if radioCount < 1 {
		t.Error("expected at least one radio on the forms page")
	}
}

// ─── Form Error Callout ─────────────────────────────────────────────

func TestE2E_FormErrorCallout_Renders(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var hasCallout bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.ui-callout--danger').length > 0`, &hasCallout),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !hasCallout {
		t.Error("expected danger callout in validation demo form")
	}
}

// ─── Forms Semantic ────────────────────────────────────────────────

func TestE2E_FormsPage_AllFormsHaveMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var formsWithoutMethod int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`(() => {
			var forms = document.querySelectorAll('form');
			var bad = 0;
			for (var i = 0; i < forms.length; i++) {
				if (!forms[i].getAttribute('method')) bad++;
			}
			return bad;
		})()`, &formsWithoutMethod),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if formsWithoutMethod > 0 {
		t.Errorf("found %d forms without method attribute", formsWithoutMethod)
	}
}

// ─── Select Dropdown ────────────────────────────────────────────────

func TestE2E_Select_RendersWithOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var optionCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`(() => {
			var sel = document.querySelector('select[name="country"]');
			return sel ? sel.options.length : 0;
		})()`, &optionCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if optionCount < 5 {
		t.Errorf("expected at least 5 country options, got %d", optionCount)
	}
}

// ─── Labels Associated ──────────────────────────────────────────────

func TestE2E_FormsPage_LabelsMatchInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var orphans int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		chromedp.Evaluate(`(() => {
			var labels = document.querySelectorAll('label[for]');
			var bad = 0;
			for (var i = 0; i < labels.length; i++) {
				var forId = labels[i].getAttribute('for');
				if (!document.getElementById(forId)) bad++;
			}
			return bad;
		})()`, &orphans),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if orphans > 0 {
		t.Errorf("found %d labels with 'for' pointing to non-existent IDs", orphans)
	}
}

// ─── Page Load Timing ──────────────────────────────────────────────

func TestE2E_FormsPage_LoadsQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	start := time.Now()
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
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

// ─── C-7: Remove single item → empty ────────────────────────────────

func TestE2E_FormRepeater_RemoveSingleToEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var itemCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Fill the initial row
		chromedp.SetValue(`#f-m0-name`, "Solo", chromedp.ByQuery),
		// Remove the only row
		chromedp.Click(`[data-fui-rpc*="action=remove-0"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// After swap: should have 0 items
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-form-repeater"] .ui-form-repeater__item').length`, &itemCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if itemCount != 0 {
		t.Errorf("expected 0 items after removing the only one, got %d", itemCount)
	}
}

// ─── C-8: Remove first from 3 items ─────────────────────────────────

func TestE2E_FormRepeater_RemoveFirstFromThree(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Fill rows 0, 1, 2
		chromedp.SetValue(`#f-m0-name`, "First", chromedp.ByQuery),
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.SetValue(`#f-m1-name`, "Second", chromedp.ByQuery),
		chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.SetValue(`#f-m2-name`, "Third", chromedp.ByQuery),
		// Remove the first row
		chromedp.Click(`[data-fui-rpc*="action=remove-0"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// After swap: 2 items, Second→0, Third→1
		chromedp.Evaluate(`JSON.stringify((() => {
			var val = (id) => { var el = document.getElementById(id); return el ? el.value : "__MISSING__"; };
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			return {
				items: rep ? rep.querySelectorAll('.ui-form-repeater__item').length : 0,
				r0: val('f-m0-name'),
				r1: val('f-m1-name'),
				r2: val('f-m2-name')
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Items int    `json:"items"`
		R0    string `json:"r0"`
		R1    string `json:"r1"`
		R2    string `json:"r2"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Items != 2 {
		t.Errorf("expected 2 items, got %d", result.Items)
	}
	if result.R0 != "Second" {
		t.Errorf("row 0: expected Second (re-indexed), got %q", result.R0)
	}
	if result.R1 != "Third" {
		t.Errorf("row 1: expected Third (re-indexed), got %q", result.R1)
	}
	if result.R2 != "__MISSING__" {
		t.Errorf("row 2 should not exist, got %q", result.R2)
	}
}
