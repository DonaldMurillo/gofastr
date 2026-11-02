package ui_test

// ui.Counter over a store.Slice: the page's seed owns the signal, and
// the headless counter's own seed never races it.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var counterSeedQty = store.New("seedtest").Int("qty", 0)

// The page seeds the slice at 7 while the counter's markup carries the
// slice's default, 0. The first + steps the page's 7 to 8, and an
// island swap whose partial seed says 12 steps 12 to 13: the server's
// value wins across the swap, as it did before the headless counter
// learned to seed itself.
func TestE2E_CounterSliceSeedWinsOverTheMarkup(t *testing.T) {
	c := string(ui.Counter(ui.CounterConfig{Slice: counterSeedQty}))
	body := `<script type="application/json" id="gofastr-signals">{"seedtest.qty":7}</script>` +
		`<div id="isl">` + c + `</div>` +
		`<template id="next"><script type="application/json" id="gofastr-signals-partial">{"p":{"seedtest.qty":12}}</script>` + c + `</template>`
	ctx := moduleTestCtx(t, body)
	read := `document.querySelector('[data-cui-signal="seedtest.qty"]').textContent`
	inc := `document.querySelector('[data-cui-signal-inc="seedtest.qty"]').click()`
	// Settle the boot pass (any module the counter's markers load) before
	// stepping, so a late seed would have had its chance to land.
	pollJS(ctx, `document.readyState === 'complete'`)
	time.Sleep(300 * time.Millisecond)
	if err := chromedp.Run(ctx, chromedp.Evaluate(inc, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, read+` === '8'`) {
		t.Fatalf("one + over the page's seed of 7 shows %q, want 8: the counter's markup default raced the page's seed", evalString(ctx, read))
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const isl = document.getElementById('isl');
		isl.innerHTML = document.getElementById('next').innerHTML;
		window.__gofastr._mergeSeed(isl);
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `window.__gofastr.getSignal('seedtest.qty') === 12`) {
		t.Fatalf("after the swap's partial seed of 12 the signal holds %s", evalString(ctx, `String(window.__gofastr.getSignal('seedtest.qty'))`))
	}
	time.Sleep(300 * time.Millisecond) // the arrival scan of the swapped markup
	if err := chromedp.Run(ctx, chromedp.Evaluate(inc, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, read+` === '13'`) {
		t.Fatalf("one + after the swap's seed of 12 shows %q, want 13", evalString(ctx, read))
	}
}
