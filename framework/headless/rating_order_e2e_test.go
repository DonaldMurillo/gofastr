package headless

// The rating's choices are native radios in reading order, so the
// browser's own radio-group arrows move the way the stars read (WCAG
// 2.4.3). In reverse DOM order ArrowRight went to the lower star.

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestE2E_RatingArrowsFollowReadingOrder(t *testing.T) {
	ltr := Rating(RatingProps{Name: "ltr", Label: "LTR", Value: 3}, nil)
	rtl := Rating(RatingProps{Name: "rtl", Label: "RTL", Value: 3}, nil)
	b := startBehaviorServer(t, string(ltr)+`<div dir="rtl">`+string(rtl)+`</div>`)
	ctx := behaviorPage(t, b)
	checked := func(name string) string {
		return evalString(t, ctx, `(document.querySelector('input[name=`+name+`]:checked') || {}).value || ''`)
	}
	press := func(sel string, key string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Focus(sel, chromedp.ByQuery), chromedp.KeyEvent(key)); err != nil {
			t.Fatal(err)
		}
	}
	// Left to right: the next star (the one drawn to the right) is the
	// higher one, the previous the lower one.
	press(`#ltr-3`, kb.ArrowRight)
	if got := checked("ltr"); got != "4" {
		t.Fatalf("ArrowRight from 3 checked %q, want 4 (the next star in reading order)", got)
	}
	press(`#ltr-4`, kb.ArrowLeft)
	if got := checked("ltr"); got != "3" {
		t.Fatalf("ArrowLeft from 4 checked %q, want 3", got)
	}
	press(`#ltr-3`, kb.ArrowDown)
	if got := checked("ltr"); got != "4" {
		t.Fatalf("ArrowDown from 3 checked %q, want 4 (document order)", got)
	}
	// Right to left the stars read from the right, so the browser's
	// radio group flips the horizontal arrows: ArrowLeft moves to the
	// next (higher) star, drawn to the left.
	press(`#rtl-3`, kb.ArrowLeft)
	if got := checked("rtl"); got != "4" {
		t.Fatalf("RTL ArrowLeft from 3 checked %q, want 4 (the next star, drawn to the left)", got)
	}
	press(`#rtl-4`, kb.ArrowRight)
	if got := checked("rtl"); got != "3" {
		t.Fatalf("RTL ArrowRight from 4 checked %q, want 3", got)
	}
}
