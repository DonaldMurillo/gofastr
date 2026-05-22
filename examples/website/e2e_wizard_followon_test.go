package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Wizard, SearchInput-clear-on-Esc, Repeater MaxItems hardening tests
// =============================================================================
//
// These exercise interactive behaviour that pure SSR tests can't observe:
//   - StepWizard full flow (Next → step 2 → step 3 → Submit → server payload)
//   - Back preserves previously entered values (server-side accumulation)
//   - Final-step Submit can't push past N (no overflow)
//   - SearchInput clears its value on Escape
//   - FormRepeater Add button is disabled at MaxItems
//
// The wizard demo lives at /components/forms/wizard-demo. Each step
// posts the full form; the handler accumulates values via hidden
// fields and re-renders. On final Submit it stores the payload to
// a package var exposed at /components/forms/wizard-demo/last.

// ─── Wizard: happy-path full flow ──────────────────────────────────

func TestE2E_Wizard_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	wizardDemoReset()

	var raw string
	err := chromedp.Run(ctx,
		// Reset state between runs.
		chromedp.Navigate(base+"/components/forms/wizard-demo"),
		pageReady(),
		// Step 1: fill name + email then Continue.
		chromedp.SetValue(`#wd-name`, "Ada Lovelace", chromedp.ByQuery),
		chromedp.SetValue(`#wd-email`, "ada@example.com", chromedp.ByQuery),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		// Step 2 visible: assert the step heading, indicator state.
		chromedp.Evaluate(`JSON.stringify((() => {
			var heading = document.querySelector('.ui-step-wizard__heading');
			var dots = document.querySelectorAll('.ui-step-wizard__step-dot');
			return {
				heading: heading ? heading.textContent.trim() : '',
				step0: dots[0] ? dots[0].className : '',
				step1: dots[1] ? dots[1].className : '',
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp step 1→2: %v", err)
	}
	var s2 struct {
		Heading string
		Step0   string
		Step1   string
	}
	if err := json.Unmarshal([]byte(raw), &s2); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(s2.Heading, "Preferences") {
		t.Errorf("expected step 2 heading 'Preferences', got %q", s2.Heading)
	}
	if !strings.Contains(s2.Step0, "is-completed") {
		t.Errorf("step 0 should be completed, got class %q", s2.Step0)
	}
	if !strings.Contains(s2.Step1, "is-current") {
		t.Errorf("step 1 should be current, got class %q", s2.Step1)
	}

	// Step 2: fill theme then Continue.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			var r = document.querySelector('input[name="wd-theme"][value="dark"]');
			if (r) r.checked = true;
			return true;
		})()`, new(bool)),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		// Step 3: assert heading + Submit visible (no Continue).
		chromedp.Evaluate(`JSON.stringify((() => {
			var heading = document.querySelector('.ui-step-wizard__heading');
			var btns = document.querySelectorAll('button[name="wizard_action"]');
			var labels = Array.from(btns).map(b => b.textContent.trim());
			return {
				heading: heading ? heading.textContent.trim() : '',
				labels: labels,
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp step 2→3: %v", err)
	}
	var s3 struct {
		Heading string
		Labels  []string
	}
	if err := json.Unmarshal([]byte(raw), &s3); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(s3.Heading, "Review") {
		t.Errorf("expected step 3 heading 'Review', got %q", s3.Heading)
	}
	if !containsAny(s3.Labels, "Submit") {
		t.Errorf("expected Submit button on final step, got labels %v", s3.Labels)
	}

	// Final: fill comments, Submit.
	err = chromedp.Run(ctx,
		chromedp.SetValue(`#wd-comments`, "Looks good!", chromedp.ByQuery),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
	)
	if err != nil {
		t.Fatalf("chromedp submit: %v", err)
	}

	got := wizardDemoLast()
	if got == nil {
		t.Fatal("expected wizard submission payload to be recorded; got nil")
	}
	if got.Get("wd-name") != "Ada Lovelace" {
		t.Errorf("payload wd-name: want %q, got %q", "Ada Lovelace", got.Get("wd-name"))
	}
	if got.Get("wd-email") != "ada@example.com" {
		t.Errorf("payload wd-email: want %q, got %q", "ada@example.com", got.Get("wd-email"))
	}
	if got.Get("wd-theme") != "dark" {
		t.Errorf("payload wd-theme: want %q, got %q", "dark", got.Get("wd-theme"))
	}
	if got.Get("wd-comments") != "Looks good!" {
		t.Errorf("payload wd-comments: want %q, got %q", "Looks good!", got.Get("wd-comments"))
	}
}

// ─── Wizard: Back preserves values ──────────────────────────────────

func TestE2E_Wizard_BackPreservesState(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	wizardDemoReset()

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms/wizard-demo"),
		pageReady(),
		// Step 1 → 2
		chromedp.SetValue(`#wd-name`, "Grace", chromedp.ByQuery),
		chromedp.SetValue(`#wd-email`, "grace@example.com", chromedp.ByQuery),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		// Step 2 → 3 (pick light)
		chromedp.Evaluate(`(() => {
			var r = document.querySelector('input[name="wd-theme"][value="light"]');
			if (r) r.checked = true;
			return true;
		})()`, new(bool)),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		// Step 3 → Back to step 2
		chromedp.Click(`button[name="wizard_action"][value="back"]`, chromedp.ByQuery),
		pageReady(),
		// Step 2 must show the previously-checked radio.
		chromedp.Evaluate(`JSON.stringify((() => {
			var checked = document.querySelector('input[name="wd-theme"]:checked');
			var heading = document.querySelector('.ui-step-wizard__heading');
			return {
				heading: heading ? heading.textContent.trim() : '',
				themeValue: checked ? checked.value : '',
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var res struct {
		Heading    string
		ThemeValue string
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(res.Heading, "Preferences") {
		t.Errorf("expected to be back on step 2 (Preferences), got %q", res.Heading)
	}
	if res.ThemeValue != "light" {
		t.Errorf("expected wd-theme=light preserved on Back, got %q", res.ThemeValue)
	}
}

// ─── Wizard: final-step Submit doesn't overflow ────────────────────

func TestE2E_Wizard_FinalStepNoOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	wizardDemoReset()

	// Drive to step 3, then simulate a stale POST with step=3 and wizard_action=next.
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms/wizard-demo"),
		pageReady(),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
	)
	if err != nil {
		t.Fatalf("chromedp drive to step 3: %v", err)
	}

	// On step 3, query for buttons — confirm Submit is the active action.
	var raw string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`JSON.stringify((() => {
			var dots = document.querySelectorAll('.ui-step-wizard__step-dot');
			var current = -1;
			dots.forEach((d, i) => { if (d.classList.contains('is-current')) current = i; });
			var hiddenStep = document.querySelector('input[type="hidden"][name="_step"]');
			return {
				dots: dots.length,
				current: current,
				hiddenStep: hiddenStep ? hiddenStep.value : '',
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp probe: %v", err)
	}
	var probe struct {
		Dots       int
		Current    int
		HiddenStep string
	}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		t.Fatalf("json: %v", err)
	}
	if probe.Current != 2 || probe.Dots != 3 {
		t.Fatalf("expected on final step 2/3, got current=%d dots=%d", probe.Current, probe.Dots)
	}

	// Now click Submit and confirm the wizard either submits OR re-renders
	// at the final step — never advances to a non-existent step 4.
	err = chromedp.Run(ctx,
		chromedp.Click(`button[name="wizard_action"][value="next"]`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`JSON.stringify((() => {
			// After submit, either confirmation page or still wizard step 2.
			var confirm = document.querySelector('[data-wizard-confirm]');
			var dots = document.querySelectorAll('.ui-step-wizard__step-dot');
			var current = -1;
			dots.forEach((d, i) => { if (d.classList.contains('is-current')) current = i; });
			return {
				confirm: !!confirm,
				dots: dots.length,
				current: current,
			};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp submit click: %v", err)
	}
	var after struct {
		Confirm bool
		Dots    int
		Current int
	}
	if err := json.Unmarshal([]byte(raw), &after); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !after.Confirm && after.Current > 2 {
		t.Errorf("Submit on final step pushed current past 2 (overflow): got current=%d, dots=%d, confirm=%v", after.Current, after.Dots, after.Confirm)
	}
	if !after.Confirm {
		t.Errorf("expected confirmation page after final Submit, got current=%d dots=%d", after.Current, after.Dots)
	}
}

// ─── PasswordInput toggle ──────────────────────────────────────────

func TestE2E_PasswordInputToggle(t *testing.T) {
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
			var btn = document.querySelector('[data-fui-comp="ui-password-input"] .ui-password-input__toggle');
			var input = document.querySelector('[data-fui-comp="ui-password-input"] input');
			if (!btn || !input) return {ok: false};
			var initial = {type: input.type, pressed: btn.getAttribute('aria-pressed')};
			btn.click();
			var afterShow = {type: input.type, pressed: btn.getAttribute('aria-pressed')};
			btn.click();
			var afterHide = {type: input.type, pressed: btn.getAttribute('aria-pressed')};
			return {ok: true, initial: initial, afterShow: afterShow, afterHide: afterHide};
		})())`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var r struct {
		OK        bool
		Initial   struct{ Type, Pressed string }
		AfterShow struct{ Type, Pressed string }
		AfterHide struct{ Type, Pressed string }
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !r.OK {
		t.Fatal("password input or toggle missing")
	}
	if r.Initial.Type != "password" || r.Initial.Pressed != "false" {
		t.Errorf("initial: want type=password pressed=false, got %+v", r.Initial)
	}
	if r.AfterShow.Type != "text" || r.AfterShow.Pressed != "true" {
		t.Errorf("after show: want type=text pressed=true, got %+v", r.AfterShow)
	}
	if r.AfterHide.Type != "password" || r.AfterHide.Pressed != "false" {
		t.Errorf("after hide: want type=password pressed=false, got %+v", r.AfterHide)
	}
}

// ─── SearchInput: Escape clears value ──────────────────────────────

func TestE2E_SearchInputClearOnEsc(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/forms"),
		pageReady(),
		// Type a value into the SearchInput.
		chromedp.Evaluate(`(() => {
			var input = document.querySelector('[data-fui-comp="ui-search-input"] input');
			if (!input) return false;
			input.focus();
			input.value = 'pizza';
			input.dispatchEvent(new Event('input', {bubbles: true}));
			return true;
		})()`, new(bool)),
		// Now press Escape on the input via keyboard event.
		chromedp.Evaluate(`(() => {
			var input = document.querySelector('[data-fui-comp="ui-search-input"] input');
			if (!input) return '';
			input.focus();
			var ev = new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true});
			input.dispatchEvent(ev);
			return input.value;
		})()`, &raw),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if raw != "" {
		t.Errorf("expected SearchInput cleared on Escape, got %q", raw)
	}
}

// ─── Repeater MaxItems: Add button is disabled at 5 ────────────────

func TestE2E_FormRepeater_MaxItemsDisablesAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Demo enforces MaxItems=5. Click Add 4 times (1→5).
	actions := []chromedp.Action{
		chromedp.Navigate(base + "/components/forms"),
		pageReady(),
	}
	for i := 0; i < 4; i++ {
		actions = append(actions,
			chromedp.Click(`[data-fui-rpc*="action=add"]`, chromedp.ByQuery),
			chromedp.Sleep(1500*time.Millisecond),
		)
	}
	var raw string
	actions = append(actions,
		chromedp.Evaluate(`JSON.stringify((() => {
			var rep = document.querySelector('[data-fui-comp="ui-form-repeater"]');
			if (!rep) return {items: 0, addDisabled: false, addHasRpc: false};
			var items = rep.querySelectorAll('.ui-form-repeater__item').length;
			// Look for the add button — it's the one with text "Add team member".
			var btn = null;
			rep.querySelectorAll('button').forEach(function (b) {
				if (b.textContent.trim() === 'Add team member') btn = b;
			});
			return {
				items: items,
				addDisabled: btn ? btn.hasAttribute('disabled') : false,
				addHasRpc: btn ? btn.hasAttribute('data-fui-rpc') : false,
			};
		})())`, &raw),
	)
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var result struct {
		Items       int  `json:"items"`
		AddDisabled bool `json:"addDisabled"`
		AddHasRpc   bool `json:"addHasRpc"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Items != 5 {
		t.Errorf("expected 5 items at max, got %d (raw=%s)", result.Items, raw)
	}
	if !result.AddDisabled {
		t.Errorf("expected Add button disabled at MaxItems=5, got %s", raw)
	}
	if result.AddHasRpc {
		t.Errorf("disabled Add button must not carry data-fui-rpc, got %s", raw)
	}
}

// containsAny reports whether any element of haystack equals needle.
func containsAny(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// Compile-time hint: these helpers are wired by screen_forms_wizard_demo.go.
var _ = url.Values(nil)
