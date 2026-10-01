package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Pins #256: the guest header's Sign in CTA rides the header's
// persistent slot, so at 390px it stays visible in the bar while the
// regular Actions cluster (theme toggle) folds into the phone menu.
// Measured, not probed: the assertions are on rendered bounding boxes
// inside the viewport.
func TestE2E_SignInStaysInBarAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := e2eBootApp(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))

	// The header's owned style: everything below selects inside it.
	const h = `[data-fui-scope="meridian-siteheader"]`

	type rect struct {
		Present bool    `json:"present"`
		Visible bool    `json:"visible"`
		Width   float64 `json:"width"`
		Right   float64 `json:"right"`
	}
	measure := func(sel string) string {
		return `(() => {
			const el = document.querySelector(` + "`" + sel + "`" + `);
			if (!el) return {present: false};
			const r = el.getBoundingClientRect();
			const visible = r.width > 0 && r.height > 0 &&
				getComputedStyle(el).visibility !== "hidden";
			return {present: true, visible, width: r.width, right: r.right};
		})()`
	}

	var signIn, toggle, menuToggle, menuSignIn rect
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(measure(h+` .end a[href="/login"]`), &signIn),
		chromedp.Evaluate(measure(h+` .bar-actions [data-fui-comp="ui-theme-toggle"]`), &toggle),
		// Open the phone menu: "folded into the menu" must mean the
		// toggle actually lives there, not that it vanished — and the
		// persistent Sign in must not have a menu duplicate.
		chromedp.Click(h+` summary`, chromedp.ByQuery),
		chromedp.WaitVisible(h+` details[open] .panel-links`, chromedp.ByQuery),
		chromedp.Evaluate(measure(h+` details[open] .panel-actions [data-fui-comp="ui-theme-toggle"]`), &menuToggle),
		chromedp.Evaluate(measure(h+` details[open] a[href="/login"]`), &menuSignIn),
	); err != nil {
		t.Fatal(err)
	}
	if !signIn.Present || !signIn.Visible {
		t.Fatalf("Sign in must be visible in the bar at 390px: %+v", signIn)
	}
	if signIn.Right > 390 {
		t.Errorf("Sign in overflows the 390px viewport: %+v", signIn)
	}
	if toggle.Present && toggle.Visible {
		t.Errorf("regular Actions must fold into the phone menu at 390px: %+v", toggle)
	}
	if !menuToggle.Present || !menuToggle.Visible {
		t.Errorf("theme toggle must be reachable in the open menu: %+v", menuToggle)
	}
	if menuSignIn.Present {
		t.Errorf("persistent Sign in must have no menu duplicate: %+v", menuSignIn)
	}
}
