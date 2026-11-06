package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestFailedPanelDoesNotReopen(t *testing.T) {
	for _, navigator := range []string{"plain", "envelope"} {
		t.Run(navigator, func(t *testing.T) { testFailedPanel(t, navigator == "envelope") })
	}
}

func testFailedPanel(t *testing.T, envelope bool) {
	t.Helper()
	server := startInterceptStackServer(t)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.srv.URL+"/rec/a")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!window.__gofastr?.loadedModules?.intercept`) {
		t.Fatal("intercept did not load")
	}
	if envelope {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('main').insertAdjacentHTML('beforeend', '<aside data-cui-outlet="site#aside"></aside>')`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const original = window.fetch;
		let fail = true;
		window.__panelFetches = 0;
		window.fetch = function(url, options) {
			if (options?.headers?.['X-Gofastr-Intercept']) window.__panelFetches++;
			if (fail && new URL(url, location.href).pathname === '/list') {
				fail = false;
				return Promise.reject(new Error('offline'));
			}
			return original.apply(this, arguments);
		};
	})()`, nil), chromedp.Click(`#to-panel`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('cui-nav-toast') && location.pathname === '/rec/a'`) {
		t.Fatal("failed list navigation did not restore the record page")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/list'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('plain-screen')`) {
		t.Fatal("ordinary list navigation did not land")
	}
	var fetches int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__panelFetches`, &fetches)); err != nil {
		t.Fatal(err)
	}
	if fetches != 0 {
		t.Fatalf("ordinary list navigation fetched %d stale panels", fetches)
	}
}
