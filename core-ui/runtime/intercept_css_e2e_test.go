package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// An intercepted pane loads the stylesheet of every component it brings
// that the page under it did not carry: the admin's record drawer drew
// its money field and read-only fields unstyled because the pane's
// innerHTML swap skipped the scan every other swap path runs.
func TestInterceptPaneLoadsComponentCSS(t *testing.T) {
	s := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/list"), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	stackStep(t, ctx, 0)
	if !interceptWait(ctx, `!!document.getElementById('rec-a')`) {
		t.Fatalf("the record pane never opened: %+v", readStack(ctx))
	}
	styled := `(() => { const p = document.querySelector('#rec-a [data-cui-comp="intercept-probe"]');
		return !!document.querySelector('link[data-cui-style="intercept-probe"]') && !!p && p.getBoundingClientRect().width === 7; })()`
	if !interceptWait(ctx, styled) {
		t.Fatal("the pane's component stylesheet never loaded")
	}
}

// An intercepted pane enters the DOM styled: it waits for the sheets it
// brings the way a navigation does, so a slow sheet never shows the pane
// unstyled first.
func TestInterceptPaneWaitsForSheet(t *testing.T) {
	s := startInterceptStackServer(t)
	s.cssDelay.Store(700)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/list"), chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`new MutationObserver(() => { const p = document.querySelector('#rec-a [data-cui-comp="intercept-probe"]');
			if (p && window.__paneW === undefined) window.__paneW = p.getBoundingClientRect().width; })
			.observe(document.body, { childList: true, subtree: true }); true`, nil)); err != nil {
		t.Fatal(err)
	}
	stackStep(t, ctx, 0)
	if !interceptWait(ctx, `!!document.getElementById('rec-a')`) {
		t.Fatalf("the record pane never opened: %+v", readStack(ctx))
	}
	var w float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__paneW`, &w)); err != nil {
		t.Fatal(err)
	}
	if w != 7 {
		t.Fatalf("the pane entered the DOM %vpx wide, want 7: it painted before its component sheet loaded", w)
	}
}
