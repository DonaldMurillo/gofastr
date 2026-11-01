package headless

// Form-scoped behaviours follow the form's own state: a reset moves
// the watched controls with no input event, and a control joins its
// form through form= wherever it sits.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A form reset puts the watched select back; the conditional region
// follows it, hiding again and disabling the field inside.
func TestE2E_WhenRegionFollowsAFormReset(t *testing.T) {
	region := ConditionalField(ConditionalFieldProps{When: "plan", Value: "pro", ID: "pro-only"}, nil, render.HTML(`<input id="seats" name="seats">`))
	body := `<form id="f"><select id="plan" name="plan"><option value="free" selected>Free</option><option value="pro">Pro</option></select>` + string(region) + `</form>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-when")) || !pollTrue(ctx, `document.getElementById('pro-only').hidden`) {
		t.Fatal("the region never hid for the free plan")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const s = document.getElementById('plan'); s.value = 'pro'; s.dispatchEvent(new Event('change', {bubbles: true})); })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('pro-only').hidden`) {
		t.Fatal("the region never showed for pro")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('f').reset()`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	got := evalString(t, ctx, `document.getElementById('plan').value + '|' + document.getElementById('pro-only').hidden + '|' + document.getElementById('seats').disabled`)
	if got != "free|true|true" {
		t.Fatalf("after reset [plan|region hidden|seats disabled] = %q, want free|true|true", got)
	}
}

// A control outside the form element that joins it through form= is an
// edit to that form: the leave guard counts it, and the form's reset
// cleans it again.
func TestE2E_LeaveGuardCountsAFormAssociatedControl(t *testing.T) {
	body := `<form id="lf" data-hui-leave-guard><input name="a"></form><input id="outside" name="b" form="lf">`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-leaveguard")) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.SendKeys(`#outside`, "x", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if evalString(t, ctx, `String(document.getElementById('lf').hasAttribute('data-hui-dirty'))`) != "true" {
		t.Fatal("typing into a form=lf control outside the form element left the guarded form clean: leaving discards it unasked")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('lf').reset()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.getElementById('lf').hasAttribute('data-hui-dirty')`) {
		t.Fatal("the guarded form's reset left it dirty")
	}
}
