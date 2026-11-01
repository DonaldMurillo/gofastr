//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The rating's choices are in reading order (1..Max in the DOM) and the
// fill moved from a reverse-order ~ cascade to :has(). Pixels, read
// through the rendered page: the stars lay out 1..Max from the
// inline-start edge (left in LTR, right in RTL), the checked star and
// every one before it are lit and none after, hovering a star lights it
// and every one before it over a lower checked value, and ArrowRight
// moves to the star drawn to the right.
func TestRatingInputFillAndOrderFollowReadingOrder(t *testing.T) {
	th := theme.Default()
	css := th.CSSCustomProperties() + ratingStyle.Entry().CSSFor(th)
	body := string(RatingInput(RatingConfig{Name: "ltr", Label: "LTR", Value: 2})) +
		`<div dir="rtl">` + string(RatingInput(RatingConfig{Name: "rtl", Label: "RTL", Value: 2})) + `</div>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><meta charset=utf-8>
<style>%s</style><style>*{transition:none!important}</style><body>%s</body>`, css, body)
	}))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	// state reports, per choice in value order, its centre x and whether
	// it is lit (painted in a colour other than the unlit border token).
	state := func(name string) (xs []float64, lit []bool) {
		t.Helper()
		var out struct {
			X   []float64
			Lit []bool
		}
		js := `(() => {
			const ls = [...document.querySelectorAll('input[name=` + name + `] + .fui-rating__choice')];
			const unlit = getComputedStyle(document.body).getPropertyValue('--color-border').trim();
			const probe = document.createElement('span'); probe.style.color = unlit; document.body.append(probe);
			const off = getComputedStyle(probe).color; probe.remove();
			return {X: ls.map(l => { const r = l.getBoundingClientRect(); return r.x + r.width / 2; }),
				Lit: ls.map(l => getComputedStyle(l).color !== off)};
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
			t.Fatal(err)
		}
		return out.X, out.Lit
	}
	wantLit := func(label string, lit []bool, n int) {
		t.Helper()
		for i, l := range lit {
			if l != (i < n) {
				t.Errorf("%s: star %d lit=%v, want stars 1..%d lit and none after (got %v)", label, i+1, l, n, lit)
				return
			}
		}
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatal(err)
	}

	xs, lit := state("ltr")
	if len(xs) != 5 {
		t.Fatalf("found %d LTR choices, want 5", len(xs))
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] <= xs[i-1] {
			t.Fatalf("LTR star %d is drawn at x=%.0f, not right of star %d at x=%.0f", i+1, xs[i], i, xs[i-1])
		}
	}
	wantLit("LTR checked 2", lit, 2)

	rxs, rlit := state("rtl")
	for i := 1; i < len(rxs); i++ {
		if rxs[i] >= rxs[i-1] {
			t.Fatalf("RTL star %d is drawn at x=%.0f, not left of star %d at x=%.0f", i+1, rxs[i], i, rxs[i-1])
		}
	}
	wantLit("RTL checked 2", rlit, 2)

	// Hover star 4 over a checked 2: stars 1..4 light.
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, xs[3], ltrRowY(ctx, t)).Do(ctx)
	})); err != nil {
		t.Fatal(err)
	}
	_, lit = state("ltr")
	wantLit("LTR hover 4 over checked 2", lit, 4)
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, 1, 1).Do(ctx)
	})); err != nil {
		t.Fatal(err)
	}

	// ArrowRight from the checked star moves to the star drawn to its
	// right, and the fill follows.
	if err := chromedp.Run(ctx, chromedp.Focus(`#ltr-2`, chromedp.ByQuery), chromedp.KeyEvent(kb.ArrowRight)); err != nil {
		t.Fatal(err)
	}
	var now string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('input[name=ltr]:checked').value`, &now)); err != nil {
		t.Fatal(err)
	}
	if now != "3" {
		t.Fatalf("ArrowRight from star 2 checked %s, want 3, the star drawn to its right", now)
	}
	_, lit = state("ltr")
	wantLit("LTR after ArrowRight", lit, 3)
}

// ltrRowY is the vertical centre of the LTR rating's stars.
func ltrRowY(ctx context.Context, t *testing.T) float64 {
	t.Helper()
	var y float64
	if err := chromedp.Evaluate(`(() => { const r = document.querySelector('label[for=ltr-1]').getBoundingClientRect(); return r.y + r.height / 2; })()`, &y).Do(ctx); err != nil {
		t.Fatal(err)
	}
	return y
}
