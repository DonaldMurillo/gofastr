package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The first-navigation focus-modality contract (the eval-2 fix): an
// outlet page's FIRST navigation demand-loads the envelope module
// beside its fetch — after the click — so the module cannot learn the
// click's pointer modality from a listener of its own (it would have
// registered too late and missed exactly that click). Core's
// modality record rides the delegated load instead (nav's `pointer`
// opt), and the swap tail keeps focusVisible:false for a real mouse
// click, focusVisible:true for a keyboard activation, cold or warm.
//
// The mouse leg drives a real CDP mouse click (detail 1, the shape
// core's handler reads), never el.click() — a synthetic click carries
// detail 0, the keyboard shape, and cannot express the distinction.

// swapFocusState reads the active element's id and its :focus-visible
// match in one round trip.
func swapFocusState(ctx context.Context) (id string, ring bool, err error) {
	var m map[string]any
	if err = chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.activeElement;
		return {id: el ? (el.id || el.tagName) : '',
			ring: !!(el && el.matches(':focus-visible'))};
	})()`, &m)); err != nil {
		return "", false, err
	}
	get := func(k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	return get("id"), m["ring"] == true, nil
}

func TestFirstOutletNavigationFocusModality(t *testing.T) {
	r := newDoubleClickRig(t, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Cold start: the outlet page loads nothing at boot (the opt-in
	// handoff), so the first navigation is the one whose click the
	// module's old listener missed.
	var warm bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules['envelope'])`, &warm)); err != nil {
		t.Fatalf("read module state: %v", err)
	}
	if warm {
		t.Fatal("envelope module already loaded at boot — the cold first-navigation path is not under test")
	}

	// Mouse leg: a real click on the first navigation.
	if err := chromedp.Run(ctx, chromedp.Click(`#goA`, chromedp.ByID)); err != nil {
		t.Fatalf("click #goA: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('#main') && document.querySelector('#main').textContent.indexOf('MAIN-A') >= 0`) {
		t.Fatal("the first navigation's swap never applied")
	}
	id, ring, err := swapFocusState(ctx)
	if err != nil {
		t.Fatalf("read focus state: %v", err)
	}
	if id != "main" {
		t.Fatalf("after the mouse navigation the active element is %q, want the swap target #main", id)
	}
	if ring {
		t.Fatal("the FIRST mouse navigation painted the focus ring on the swapped region — the module missed the click's pointer modality")
	}

	// Keyboard leg: focus the second link, press Enter. The ring is
	// keyboard signal and must stay.
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#goB`, chromedp.ByID),
		chromedp.KeyEvent(kb.Enter),
	); err != nil {
		t.Fatalf("keyboard activation: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('#main') && document.querySelector('#main').textContent.indexOf('MAIN-B') >= 0`) {
		t.Fatal("the keyboard navigation's swap never applied")
	}
	id, ring, err = swapFocusState(ctx)
	if err != nil {
		t.Fatalf("read focus state: %v", err)
	}
	if id != "main" {
		t.Fatalf("after the keyboard navigation the active element is %q, want the swap target #main", id)
	}
	if !ring {
		t.Fatal("a keyboard navigation lost the focus ring — focusVisible must stay true for keyboard activations")
	}
}
