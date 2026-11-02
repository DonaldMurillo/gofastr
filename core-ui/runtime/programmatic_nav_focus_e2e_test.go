package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A programmatic navigation (__gofastr.navigate: an RPC form's
// data-cui-rpc-navigate, a widget, app code) has no click of its own to
// read a modality from. nav.js said "the browser's heuristic decides"
// but then focused the swap target with focusVisible: true, which
// forces the ring: Meridian's "New Customer" form, submitted with the
// mouse, landed on the list with a focus ring around the whole main
// region. Leaving focusVisible out lets the browser decide: no ring
// after a mouse click, a ring after a keyboard activation. Both
// navigators (plain core and the outlet envelope module) are covered.
func TestProgrammaticNavigationLeavesFocusRingToBrowser(t *testing.T) {
	for _, plain := range []bool{true, false} {
		name := "outlet"
		if plain {
			name = "plain"
		}
		t.Run(name, func(t *testing.T) {
			r := newDoubleClickRig(t, false, plain)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(r.srv.URL+"/"),
				chromedp.WaitVisible(`#goA`, chromedp.ByID),
				chromedp.Evaluate(`(() => {
  const b = document.createElement('button');
  b.id = 'prog'; b.type = 'button'; b.textContent = 'go';
  let n = 0;
  b.addEventListener('click', () => window.__gofastr.navigate(++n === 1 ? '/a' : '/b'));
  document.body.prepend(b);
})()`, nil),
				// A real CDP mouse click.
				chromedp.Click(`#prog`, chromedp.ByID),
			); err != nil {
				t.Fatalf("mouse leg: %v", err)
			}
			if !pollTrue(ctx, `document.querySelector('#main').textContent.indexOf('MAIN-A') >= 0`) {
				t.Fatal("the programmatic navigation's swap never applied")
			}
			id, ring, err := swapFocusState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if id != "main" {
				t.Fatalf("after the navigation the active element is %q, want #main", id)
			}
			if ring {
				t.Error("a mouse-started programmatic navigation painted the focus ring on the swapped region")
			}
			if err := chromedp.Run(ctx,
				chromedp.Focus(`#prog`, chromedp.ByID),
				chromedp.KeyEvent(kb.Enter),
			); err != nil {
				t.Fatalf("keyboard leg: %v", err)
			}
			if !pollTrue(ctx, `document.querySelector('#main').textContent.indexOf('MAIN-B') >= 0`) {
				t.Fatal("the keyboard-started navigation's swap never applied")
			}
			if id, ring, err = swapFocusState(ctx); err != nil {
				t.Fatal(err)
			}
			if id != "main" || !ring {
				t.Errorf("a keyboard-started programmatic navigation: focus %q, ring %v; want #main with the ring", id, ring)
			}
		})
	}
}
