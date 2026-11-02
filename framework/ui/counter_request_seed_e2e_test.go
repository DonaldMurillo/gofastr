package ui_test

// ui.Counter over a store.Slice that the REQUEST seeds to something
// other than the slice's default: the server paints the seeded number,
// the page's seed block carries the same number, and the first + steps
// from it. Without script the reader sees the seeded number too.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var counterRequestSeedQty = store.New("reqseed").Int("qty", 0)

// counterRequestSeedPage renders the counter the way a screen does: a
// request context carrying the value bag, a producer that Seeds 7, the
// counter rendered on that context, and the page's seed block resolved
// from the same context (store.SeedFor, what the UI host emits).
func counterRequestSeedPage(t *testing.T) string {
	t.Helper()
	ctx := store.WithValues(context.Background())
	counterRequestSeedQty.Seed(ctx, 7)
	c := render.SettleIDs(ctx, string(ui.Counter(ui.CounterConfig{Slice: counterRequestSeedQty, Ctx: ctx})))
	seed, err := json.Marshal(store.SeedFor(ctx, c))
	if err != nil {
		t.Fatal(err)
	}
	return `<script type="application/json" id="gofastr-signals">` + string(seed) + `</script>` + c
}

func TestE2E_CounterSliceRequestSeedIsTheFirstPaint(t *testing.T) {
	body := counterRequestSeedPage(t)
	read := `document.querySelector('[data-cui-signal="reqseed.qty"]').textContent`

	// No script: the server's text is all the reader gets.
	nojs := moduleTestCtx(t, body, emulation.SetScriptExecutionDisabled(true))
	if got := evalString(nojs, read); got != "7" {
		t.Fatalf("without script a request that seeds 7 shows %q, want 7: the server painted the slice default", got)
	}

	ctx := moduleTestCtx(t, body)
	pollJS(ctx, `document.readyState === 'complete'`)
	time.Sleep(300 * time.Millisecond) // the boot pass and any module the markers load
	if got := evalString(ctx, read); got != "7" {
		t.Fatalf("after boot the counter shows %q, want the seeded 7", got)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-cui-signal-inc="reqseed.qty"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, read+` === '8'`) {
		t.Fatalf("one + over the seeded 7 shows %q, want 8", evalString(ctx, read))
	}
}
