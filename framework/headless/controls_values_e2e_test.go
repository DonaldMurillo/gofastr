package headless

// The controls never own a value: the counter's belongs to its signal,
// the stepper's to what the input accepts.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A live update to a counter's signal during its arrival animation
// stands: the animation is presentation and never owns the number. A
// rescan of the same markup (an insertion elsewhere) is not a second
// arrival and does not walk the number again.
func TestE2E_CounterAnimationDoesNotOverwriteALiveValue(t *testing.T) {
	from := 0
	c := Counter(CounterProps{Signal: "seats", Label: "Seats", Value: 10, AnimateFrom: &from, DurationMS: 1200}, nil)
	b := startBehaviorServer(t, string(c))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('seats', 42)`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1800 * time.Millisecond)
	read := `document.querySelector('[data-cui-signal="seats"]').textContent + '|' + window.__gofastr.getSignal('seats')`
	if got := evalString(t, ctx, read); got != "42|42" {
		t.Fatalf("after a live update to 42 mid-animation the counter shows|holds %q, want 42|42", got)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._moduleScanners['headless-controls'](document)`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := evalString(t, ctx, read); got != "42|42" {
		t.Fatalf("a rescan of the same counter re-ran its arrival walk: shows|holds %q, want 42|42", got)
	}
}

// The stepper's + lands on a value the input accepts: with min=1 and
// step=5 the valid values are 1, 6, 11, ... and the form's validity
// agrees with what the stepper wrote. At the ceiling it stays put.
func TestE2E_StepperRespectsTheStepBase(t *testing.T) {
	min, max := 1, 11
	n := NumberInput(NumberInputProps{Name: "qty", Label: "Qty", Min: &min, Max: &max, Step: 5, Value: "3"}, nil)
	b := startBehaviorServer(t, `<form>`+string(n)+`</form>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("module never loaded")
	}
	step := `document.querySelector('[data-hui-number-input-increment]').click(); (() => { const i = document.getElementById('qty'); return i.value + '|' + i.validity.stepMismatch; })()`
	for _, want := range []string{"6|false", "11|false", "11|false"} {
		if got := evalString(t, ctx, step); got != want {
			t.Fatalf("+ (min 1, max 11, step 5) gave %q, want %s: the stepper wrote a value the form refuses", got, want)
		}
	}
	dec := `document.querySelector('[data-hui-number-input-decrement]').click(); document.getElementById('qty').value`
	if got := evalString(t, ctx, dec); got != "6" {
		t.Fatalf("- from 11 gave %q, want 6", got)
	}
}
