package headless

// A pane host's phone-width drawer takes the document scroll lock; a
// client navigation that takes the host away must give it back.

import (
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A link inside the open drawer navigates client-side to a page
// without the host: the next page must scroll. The host is detached
// before the navigate event fires, so a document query finds no host
// to release.
func TestE2E_PaneDrawerNavigationReleasesScrollLock(t *testing.T) {
	host := PaneHost(PaneHostProps{
		ID:            "ph",
		Primary:       render.Text("list"),
		Secondary:     render.HTML(`<a id="away" href="/away">open full page</a>`),
		SecondaryOpen: true,
	}, nil)
	b := startBehaviorServer(t, awayRoutes+string(host), func(mux *http.ServeMux) {
		mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
			shellPage(w, awayRoutes+`<p id="arrived">another page</p>`)
		})
	})
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(400, 800, 1, false),
		chromedp.Navigate(b.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.documentElement.style.overflow === 'hidden' && document.getElementById('ph').getAttribute('data-hui-pane-mode') === 'overlay'`) {
		t.Fatal("the open pane never became the scroll-locking drawer at phone width")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__stay = 1`, nil), chromedp.Click(`#away`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `location.pathname === "/away" && !!document.getElementById("arrived") && window.__stay === 1`) {
		t.Fatal("the client navigation never landed")
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `document.documentElement.style.overflow`); got == "hidden" {
		t.Fatal("after navigating away from the open drawer the new page is still scroll-locked (html overflow:hidden)")
	}
}

// A drawer in chrome the navigation kept is still on screen: it keeps
// its overlay and its lock.
func TestE2E_PaneDrawerInKeptChromeKeepsItsLock(t *testing.T) {
	host := string(PaneHost(PaneHostProps{
		ID:            "ph",
		Primary:       render.Text("list"),
		Secondary:     render.Text("detail"),
		SecondaryOpen: true,
	}, nil))
	b := startBehaviorServer(t, "", func(mux *http.ServeMux) {
		mux.HandleFunc("/p1", func(w http.ResponseWriter, r *http.Request) {
			chromeShellPage(w, host, keptRoutes+`<a id="go" href="/p2">next</a>`)
		})
		mux.HandleFunc("/p2", func(w http.ResponseWriter, r *http.Request) {
			chromeShellPage(w, host, keptRoutes+`<p id="arrived">p2</p>`)
		})
	})
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(400, 800, 1, false),
		chromedp.Navigate(b.srv.URL+"/p1"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.documentElement.style.overflow === 'hidden'`) {
		t.Fatal("the open pane never took the scroll lock at phone width")
	}
	// The drawer's scrim covers the page link: the navigation is the
	// router's, driven the way a link inside the drawer would drive it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__stay = 1; window.__ph = document.getElementById('ph'); window.__gofastr.navigate('/p2')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.getElementById('arrived') && window.__stay === 1 && document.getElementById('ph') === window.__ph`) {
		t.Fatal("the client navigation never landed with the chrome host kept")
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `document.documentElement.style.overflow + '|' + document.getElementById('ph').getAttribute('data-hui-pane-mode')`); got != "hidden|overlay" {
		t.Fatalf("the kept drawer after navigation reads [overflow|mode] = %q, want hidden|overlay", got)
	}
}

// keptRoutes is the route manifest for the two-page kept-chrome tests.
const keptRoutes = `<script type="application/json" id="gofastr-routes">[{"path":"/p1"},{"path":"/p2"}]</script>`
