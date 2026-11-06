package headless

// The controls never own a value: the counter's belongs to its signal,
// the stepper's to what the input accepts.

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A live update before the counter's first frame or between frames
// stands, even when it matches the last painted number: the animation
// is presentation and never owns the number. Holding the browser's
// animation callbacks makes each ordering deterministic. A rescan of
// the same markup is not a second arrival.
func TestE2E_CounterAnimationDoesNotOverwriteALiveValue(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		firstFrame bool
		liveValue  string
	}{
		{name: "before-first-frame", liveValue: "42"},
		{name: "between-frames", firstFrame: true, liveValue: "42"},
		{name: "matching-painted-value", firstFrame: true, liveValue: "0"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			from := 0
			counter := Counter(CounterProps{Signal: "seats", Label: "Seats", Value: 10, AnimateFrom: &from, DurationMS: 1200}, nil)
			server := startBehaviorServer(t, `<script>
window.__counterFrames = [];
window.requestAnimationFrame = callback => window.__counterFrames.push({callback, requested: performance.now()});
</script>`+string(counter))
			ctx := behaviorPage(t, server)
			if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
				t.Fatal("module never loaded")
			}
			read := `document.querySelector('[data-cui-signal="seats"]').textContent + '|' + window.__gofastr.getSignal('seats')`
			queued := `String(window.__counterFrames.length)`
			if got := evalString(t, ctx, read); got != "10|10" {
				t.Fatalf("before the first frame the counter shows|holds %q, want 10|10", got)
			}
			if got := evalString(t, ctx, queued); got != "1" {
				t.Fatalf("module load queued %s animation frames, want 1", got)
			}
			t.Log("module loaded: shows|holds 10|10; first animation frame queued, not run")
			if testCase.firstFrame {
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__counterFrames.splice(0).forEach(frame => frame.callback(frame.requested))`, nil)); err != nil {
					t.Fatal(err)
				}
				if got := evalString(t, ctx, read); got != "0|10" {
					t.Fatalf("the first frame shows|holds %q, want 0|10", got)
				}
				t.Log("first animation frame: shows|holds 0|10")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('seats', `+testCase.liveValue+`)`, nil)); err != nil {
				t.Fatal(err)
			}
			want := testCase.liveValue + "|" + testCase.liveValue
			if got := evalString(t, ctx, read); got != want {
				t.Fatalf("the live write shows|holds %q, want %s", got, want)
			}
			t.Logf("live write before queued frame: shows|holds %s", want)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__counterFrames.splice(0).forEach(frame => frame.callback(frame.requested + 1200))`, nil)); err != nil {
				t.Fatal(err)
			}
			if got := evalString(t, ctx, read); got != want {
				t.Fatalf("after the queued frame the counter shows|holds %q, want %s", got, want)
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._moduleScanners['headless-controls'](document)`, nil)); err != nil {
				t.Fatal(err)
			}
			if got := evalString(t, ctx, queued); got != "0" {
				t.Fatalf("a rescan queued %s animation frames, want 0", got)
			}
			if got := evalString(t, ctx, read); got != want {
				t.Fatalf("a rescan of the same counter shows|holds %q, want %s", got, want)
			}
		})
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
