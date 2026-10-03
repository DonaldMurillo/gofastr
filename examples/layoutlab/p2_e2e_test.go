package main

// Browser e2e for fill-error containment (DESIGN "A failing fill is
// contained to its outlet"): one e2e per broken case — a Load error,
// a render panic, an ErrorBoundary fill. The navigation applies, only
// the outlet degrades, no toast, no hostile text. This is the browser
// twin of core-ui/app's TestFillErrorContainedToOutlet.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestFillErrorContainedToOutlet(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	srv := labServe(t)

	browser := labBrowserCtx(t)
	tab, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	ctx, tcancel := context.WithTimeout(tab, 120*time.Second)
	t.Cleanup(tcancel)

	for _, tc := range p2Cases {
		t.Run(tc.route, func(t *testing.T) {
			if err := chromedp.Run(ctx,
				page.BringToFront(),
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#lab-SCREEN-HOME`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("navigate home: %v", err)
			}

			if err := chromedp.Run(ctx,
				page.BringToFront(),
				chromedp.Evaluate(`if (document.activeViewTransition) document.activeViewTransition.skipTransition(); true`, nil),
				chromedp.Click(`a[data-nav="`+tc.route+`"]`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("click %s: %v", tc.route, err)
			}

			// The navigation applies; the screen renders and the
			// outlet degrades.
			if err := chromedp.Run(ctx,
				chromedp.WaitVisible(`#lab-`+tc.screen, chromedp.ByQuery),
				chromedp.Sleep(150*time.Millisecond),
			); err != nil {
				t.Fatalf("screen %s must render: %v", tc.screen, err)
			}
			if got := labRead(t, ctx, asideSel); got != tc.aside {
				t.Errorf("aside = %q, want %q", got, tc.aside)
			}
			var toastVisible bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`!!document.querySelector('.fui-nav-toast.is-visible')`, &toastVisible)); err != nil {
				t.Fatal(err)
			}
			if toastVisible {
				t.Error("a contained fill failure must not show the failure toast")
			}
			var pageHTML string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.documentElement.outerHTML`, &pageHTML)); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"<img src=x", "\u009b", "\u202e", "onerror=alert"} {
				if strings.Contains(pageHTML, bad) {
					t.Errorf("hostile error text echoed into the rendered page (%q)", bad)
				}
			}
		})
	}
}
