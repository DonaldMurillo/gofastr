package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A demand module's re-scan after SPA navigation must bind swapped-in
// elements before the user can click them. gofastr:navigate is dispatched
// synchronously after swapMainContent, so a scan deferred to
// requestAnimationFrame leaves a visible-but-unbound window: in an
// occluded headless renderer rAF starves for seconds (the CI failure
// mode), and on any slow device the first click after a navigation is
// silently dropped.
//
// The test makes the starvation deterministic by delaying every rAF
// callback 3s, then navigates so #tog is swapped in fresh and clicks it
// as soon as it is visible. The click must commit: binding may not
// depend on the renderer producing a frame.
func TestNavSwappedElementsBindWithoutAFrame(t *testing.T) {
	srv := invalidationSrv(t)
	ctx := newSeedBrowserCtx(t)

	steps := []chromedp.Action{
		chromedp.Navigate(srv.URL + "/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && typeof window.__gofastr.navigate === 'function'`,
			nil, chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		// Load the toggleaction module and prove the toggle works at all
		// (this also seeds one committed→untoggle is not allowed, so
		// re-click stays sticky; the assert below uses the fresh copy).
		chromedp.WaitVisible("#tog", chromedp.ByID),
		// Starve rAF: every callback lands 3s late, far beyond a click.
		chromedp.Evaluate(`window.requestAnimationFrame = (cb) => setTimeout(() => cb(performance.now()), 3000);`, nil),
	}
	// Navigate away and back: #tog is now a freshly swapped-in element
	// whose binding must not wait for a frame.
	steps = append(steps,
		chromedp.Click("#open", chromedp.ByID),
		waitText("#v", "items-open 1"),
		chromedp.Click("#home", chromedp.ByID),
		chromedp.WaitVisible("#tog", chromedp.ByID),
		chromedp.Click("#tog", chromedp.ByID),
		chromedp.Poll(`document.querySelector('#tog')?.getAttribute('data-state') === 'committed'`,
			nil, chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
	)

	if err := chromedp.Run(ctx, steps...); err != nil {
		t.Fatalf("chromedp (first click after nav was dropped — scan waited for a frame?): %v", err)
	}
}
