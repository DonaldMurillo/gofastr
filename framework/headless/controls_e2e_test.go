package headless

// Browser coverage for headless-controls: the kernel loads it on a
// marker, the stepper steps inside the declared bounds, the slider's
// output mirrors the thumb, the range pair cross-clamps and re-formats
// its sentence through the server's words, and the counter animates
// toward the SSR value. Same harness as behavior_e2e_test.go.

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func controlsLoadedExpr(name string) string {
	return fmt.Sprintf(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules[%q])`, name)
}

// The stepper steps by the input's own step attribute, clamps at the
// declared bounds, and reports the change as a real input event.
func TestE2E_ControlsStepperStepsAndClamps(t *testing.T) {
	min, max := 1, 3
	body := NumberInput(NumberInputProps{Name: "replicas", Label: "Replicas",
		Value: "2", Min: &min, Max: &max}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("the stepper marker never loaded headless-controls")
	}
	click := `document.querySelector('[data-hui-number-input-increment]').click()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(click, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('replicas').value === '3'`) {
		t.Fatal("one increment did not step 2 to 3")
	}
	// At the ceiling the step clamps: the input said max=3.
	if err := chromedp.Run(ctx, chromedp.Evaluate(click, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('replicas').value === '3'`) {
		t.Fatal("a step past the declared max escaped the clamp")
	}
	// The change reached the page as a real input event, the thing
	// every form pipeline listens for.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__huiSaw = 0; document.getElementById('replicas').addEventListener('input', () => window.__huiSaw++)`, nil)); err != nil {
		t.Fatal(err)
	}
	dec := `document.querySelector('[data-hui-number-input-decrement]').click()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(dec, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__huiSaw === 1 && document.getElementById('replicas').value === '2'`) {
		t.Fatal("a step did not dispatch a real input event with the stepped value")
	}
}

// The slider's output mirrors the thumb from the SSR value onward; a
// bare slider (no output) never loads the module for its mirror.
func TestE2E_ControlsSliderMirrorsItsOutput(t *testing.T) {
	body := Slider(SliderProps{Name: "cpu", Label: "CPU share", Value: 40, ShowValue: true}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("the output marker never loaded headless-controls")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`const i = document.getElementById('cpu'); i.value = 70; i.dispatchEvent(new Event('input', {bubbles:true}))`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-slider-output]').textContent === '70'`) {
		t.Fatal("the output did not mirror the thumb's live value")
	}

	bare := startBehaviorServer(t, string(Slider(SliderProps{Name: "mem", Label: "Memory"}, nil)))
	bctx := behaviorPage(t, bare)
	if err := chromedp.Run(bctx, chromedp.Sleep(0)); err != nil {
		t.Fatal(err)
	}
	var loaded bool
	if err := chromedp.Run(bctx, chromedp.Evaluate(controlsLoadedExpr(ControlsBehaviorName), &loaded)); err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Error("a bare slider loaded headless-controls: no output means no mirror, and the fetch is waste")
	}
}

// The range pair cross-clamps the moved thumb and re-formats the
// output through the sentence the component rendered into its hook —
// the {name}-free "%s to %s" the server's Strings resolved.
func TestE2E_ControlsRangePairClampsAndSpeaksInServerWords(t *testing.T) {
	body := RangeSlider(RangeSliderProps{Name: "price", Label: "Price",
		Min: 0, Max: 100, ValueLow: 20, ValueHigh: 80, ShowValue: true}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("the pair marker never loaded headless-controls")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`const lo = document.getElementById('price-min'); lo.value = 90; lo.dispatchEvent(new Event('input', {bubbles:true}))`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('price-min').value === '80'`) {
		t.Fatal("a low thumb dragged past the high thumb was not clamped")
	}
	if !pollTrue(ctx, `(function () { const o = document.querySelector('[data-hui-range-slider-output]'); return o && o.textContent === '80 to 80'; })()`) {
		t.Fatal("the output was not re-formatted through the server's sentence")
	}
}

// The animated counter walks from its recorded start to the SSR value
// and lands on it exactly; an unanimated counter never needs the
// module at all.
func TestE2E_ControlsCounterAnimatesToTheSSRValue(t *testing.T) {
	from := 0
	body := Counter(CounterProps{Signal: "deployed", Label: "Deployed", Value: 4820,
		AnimateFrom: &from, DurationMS: 300}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(ControlsBehaviorName)) {
		t.Fatal("the animate marker never loaded headless-controls")
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-counter-animate] [data-fui-signal]').textContent === '4820'`) {
		t.Fatal("the animation did not land on the SSR value")
	}
}
