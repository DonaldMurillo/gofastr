package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Pins #256: the guest header's Sign in CTA rides PersistentActions, so
// at 390px it stays visible in the bar while the regular Actions cluster
// (theme toggle) collapses into the drawer. Measured, not probed: the
// assertions are on rendered bounding boxes inside the viewport.
func TestE2E_SignInStaysInBarAt390(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := e2eBootApp(t)
	ctx := e2eBrowser(t)

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

	var signIn, toggle, drawerToggle, drawerSignIn rect
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(measure(`.ui-site-header__persistent-actions a[href="/login"]`), &signIn),
		chromedp.Evaluate(measure(`.ui-site-header__bar-actions [data-fui-comp="ui-theme-toggle"]`), &toggle),
		// Open the drawer: "collapsed into the drawer" must mean the
		// toggle actually lives there, not that it vanished — and the
		// persistent Sign in must not have a drawer duplicate.
		chromedp.Click(`.ui-site-header__mobile-toggle`, chromedp.ByQuery),
		chromedp.WaitVisible(`.ui-site-header__mobile-links`, chromedp.ByQuery),
		chromedp.Evaluate(measure(`.ui-site-header__mobile-actions [data-fui-comp="ui-theme-toggle"]`), &drawerToggle),
		chromedp.Evaluate(measure(`.ui-site-header__mobile-links a[href="/login"]`), &drawerSignIn),
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
		t.Errorf("regular Actions must collapse into the drawer at 390px: %+v", toggle)
	}
	if !drawerToggle.Present || !drawerToggle.Visible {
		t.Errorf("theme toggle must be reachable in the open drawer: %+v", drawerToggle)
	}
	if drawerSignIn.Present {
		t.Errorf("persistent Sign in must have no drawer duplicate: %+v", drawerSignIn)
	}
}
