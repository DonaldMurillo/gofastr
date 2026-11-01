package headless

// A bare Counter hands its own Value to the signal it steps: with no
// page seed the first + reads the number on screen, not zero.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The first + on a Counter{Value: 4} with no page seed shows 5. Before
// the fix the kernel stepped an unset signal from 0 and showed 1.
func TestE2E_CounterStepsFromItsOwnValue(t *testing.T) {
	body := string(Counter(CounterProps{Signal: "qty", Label: "Quantity", Value: 4}, nil)) +
		`<form id="cf">` + string(Counter(CounterProps{Signal: "seats", Label: "Seats", Name: "seats", Value: 7}, nil)) + `</form>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	// The module that hands the runtime the counter's value loads on
	// the counter's own marker; wait for it, then judge by the number.
	pollTrue(ctx, controlsLoadedExpr("headless-controls"))
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-cui-signal-inc="qty"]').click(); document.querySelector('[data-cui-signal-inc="seats"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__gofastr.getSignal('qty') === 5 && document.querySelector('[data-cui-signal="qty"]').textContent === '5'`) {
		t.Fatalf("after one + on a counter showing 4 it reads %q (signal %q), want 5",
			evalString(t, ctx, `document.querySelector('[data-cui-signal="qty"]').textContent`),
			evalString(t, ctx, `String(window.__gofastr.getSignal('qty'))`))
	}
	if !pollTrue(ctx, `new FormData(document.getElementById('cf')).get('seats') === '8'`) {
		t.Fatalf("after one + on a named counter holding 7 the form submits seats=%s, want 8",
			evalString(t, ctx, `String(new FormData(document.getElementById('cf')).get('seats'))`))
	}
}

// An island swap that re-renders the counter with a new server value
// wins over the count the reader stepped to: the fresh markup is the
// number on screen, so it is the number the next + steps from. A
// rescan of the SAME markup changes nothing.
func TestE2E_CounterIslandSwapTakesTheServersValue(t *testing.T) {
	first := string(Counter(CounterProps{Signal: "qty", Label: "Quantity", Value: 4}, nil))
	next := string(Counter(CounterProps{Signal: "qty", Label: "Quantity", Value: 9}, nil))
	b := startBehaviorServer(t, `<div id="isl">`+first+`</div><template id="next">`+next+`</template>`)
	ctx := behaviorPage(t, b)
	pollTrue(ctx, controlsLoadedExpr("headless-controls"))
	inc := `document.querySelector('[data-cui-signal-inc="qty"]').click()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(inc+`; `+inc, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__gofastr.getSignal('qty') === 6`) {
		t.Fatalf("two + from 4 read %s, want 6", evalString(t, ctx, `String(window.__gofastr.getSignal('qty'))`))
	}
	// A rescan of the same markup (the pass a navigation that kept this
	// counter runs over the document) is not an arrival: the stepped
	// count stays.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._moduleScanners['headless-controls'](document)`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := evalString(t, ctx, `document.querySelector('[data-cui-signal="qty"]').textContent + '|' + window.__gofastr.getSignal('qty')`); got != "6|6" {
		t.Fatalf("a rescan of the same counter re-seeded it: shows|holds %q, want 6|6", got)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('isl').innerHTML = document.getElementById('next').innerHTML`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__gofastr.getSignal('qty') === 9`) {
		t.Fatalf("after the swap rendered 9 the signal holds %s", evalString(t, ctx, `String(window.__gofastr.getSignal('qty'))`))
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(inc, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-cui-signal="qty"]').textContent === '10'`) {
		t.Fatalf("one + after the swap showed %q, want 10", evalString(t, ctx, `document.querySelector('[data-cui-signal="qty"]').textContent`))
	}
}
