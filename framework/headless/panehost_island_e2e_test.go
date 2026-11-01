package headless

// A pane host an island swap takes away gives its drawer's scroll lock
// back with no navigation to prompt it.

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// islandDrawerPage serves one open pane host inside an island region
// (#isl) plus a template holding the same host markup, and loads it at
// phone width with the drawer holding the lock.
func islandDrawerPage(t *testing.T) context.Context {
	t.Helper()
	host := string(PaneHost(PaneHostProps{
		ID:            "ph",
		Primary:       render.Text("list"),
		Secondary:     render.Text("detail"),
		SecondaryOpen: true,
	}, nil))
	b := startBehaviorServer(t, `<div id="isl">`+host+`</div><template id="next">`+host+`</template>`)
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(400, 800, 1, false),
		chromedp.Navigate(b.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.documentElement.style.overflow === 'hidden'`) {
		t.Fatal("the open pane never took the scroll lock at phone width")
	}
	return ctx
}

// The island's new markup has no host in it: the page scrolls again
// at once, not at the next navigation (none comes).
func TestE2E_PaneDrawerIslandSwapReleasesScrollLock(t *testing.T) {
	ctx := islandDrawerPage(t)
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__stay = 1; document.getElementById('isl').innerHTML = '<p id="swapped">fresh island</p>'`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__stay === 1 && !!document.getElementById('swapped') && document.documentElement.style.overflow !== 'hidden'`) {
		t.Fatal("after an island swap removed the open drawer the page is still scroll-locked (html overflow:hidden)")
	}
}

// A swap that re-renders the host with its drawer still open keeps the
// page locked: the old host's lock goes, the new host's takes over.
func TestE2E_PaneDrawerIslandSwapWithTheHostKeepsTheLock(t *testing.T) {
	ctx := islandDrawerPage(t)
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__old = document.getElementById('ph'); document.getElementById('isl').innerHTML = document.getElementById('next').innerHTML`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('ph') !== window.__old && document.getElementById('ph').getAttribute('data-hui-pane-mode') === 'overlay'`) {
		t.Fatal("the re-rendered host never became the drawer")
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `document.documentElement.style.overflow`); got != "hidden" {
		t.Fatalf("the re-rendered open drawer left the page unlocked (html overflow = %q)", got)
	}
}
