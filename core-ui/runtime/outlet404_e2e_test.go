package runtime_test

// The 404 outlet in a real browser (docs/DESIGN-layout-outlets.md
// "404 outlet", Decided 5): the runtime accepts a 404 answer on BOTH
// nav branches — the partial/envelope branch for a same-chain
// navigation (applied, never cached) and the full-document branch a
// cross-chain navigation takes (the shell swap that repairs the chain).
// The rig is a real uihost app so the wire bytes are the server's own.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// nfBody is the rig's configured 404 body: a marker the tests wait for.
type nfBody struct{}

func (nfBody) Render() render.HTML {
	return render.HTML(`<h1 id="nf-body">no-fill-404</h1>`)
}

// nfRig is a real uihost app with two disjoint chains:
//
//	/home          → chain ["l:site"]  (aside outlet, default fill)
//	/help/present  → chain ["l:help"]  (toc filled by the screen: 200)
//	/help/gone     → chain ["l:help"]  (toc unfilled: FallbackNotFound)
//
// Every request is counted by (path, partial?) so tests can assert
// which wire shape a navigation used and how often.
type nfRig struct {
	srv *httptest.Server

	mu      sync.Mutex
	partial map[string]int
	full    map[string]int
}

func (r *nfRig) partials(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partial[path]
}

func (r *nfRig) fulls(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full[path]
}

func (r *nfRig) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Header.Get("X-Gofastr-Navigate") == "1" {
		r.partial[req.URL.Path]++
	} else {
		r.full[req.URL.Path]++
	}
}

func newNotFoundOutletRig(t *testing.T) *nfRig {
	t.Helper()
	a := app.NewApp("nf-rig")

	siteAside := app.NewOutlet("aside", app.OutletOptions{Default: nfStaticFill("site-aside")})
	site := app.NewLayout("site", app.LayoutSpec{
		Outlets: []*app.Outlet{siteAside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.HTML(`<nav><a id="to-home" href="/home">Home</a><a id="cross-gone" href="/help/gone">Gone</a></nav>`),
			l.Place(siteAside),
			l.Primary(),
		)
	})
	helpToc := app.NewOutlet("toc", app.OutletOptions{Fallback: app.FallbackNotFound})
	helpAside := app.NewOutlet("aside", app.OutletOptions{Default: nfStaticFill("help-aside")})
	help := app.NewLayout("help", app.LayoutSpec{
		Outlets: []*app.Outlet{helpToc, helpAside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.HTML(`<nav><a id="to-home" href="/home">Home</a><a id="to-present" href="/help/present">Present</a><a id="to-gone" href="/help/gone">Gone</a></nav>`),
			l.Place(helpToc),
			l.Primary(),
			l.Place(helpAside),
		)
	})

	a.RegisterScreen(app.NewScreen("/home", nfStaticScreen(`<p id="screen-home">HOME</p>`)), site)
	a.RegisterScreen(app.NewScreen("/help/present", nfStaticScreen(`<p id="screen-present">PRESENT</p>`)).
		Fill(helpToc, nfStaticFill(`<span id="toc-fill">TOC</span>`)), help)
	a.RegisterScreen(app.NewScreen("/help/gone", nfStaticScreen(`<p id="screen-gone">GONE</p>`)), help)

	ds := uihost.New(a, uihost.WithNotFoundScreen(&nfBody{}))
	rt := router.New()
	ds.Mount(rt)

	r := &nfRig{partial: map[string]int{}, full: map[string]int{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.record(req)
		rt.ServeHTTP(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// TestOutlet404FromOtherChainAndRepair: a navigation from ANOTHER chain
// to a 404-outlet route takes the full-document branch, which must
// accept the 404 answer (an HTML body) and repair the chain — the
// destination's shell replaces the origin's, the URL stays the target,
// and the not-found body shows in <main>. Navigating home again swaps
// the shell back.
func TestOutlet404FromOtherChainAndRepair(t *testing.T) {
	rig := newNotFoundOutletRig(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var layout, url, homeBack string
	var siteShellGone bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/home"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),

		// Cross-chain click: no shared root, the runtime fetches the
		// FULL document; the server answers 404 + the not-found page.
		chromedp.Click(`#cross-gone`, chromedp.ByID),
		chromedp.WaitVisible(`#nf-body`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('[data-cui-layout]').getAttribute('data-cui-layout')`, &layout),
		chromedp.Evaluate(`location.pathname`, &url),
		chromedp.Evaluate(`!document.querySelector('[data-cui-layout-key="l:site"]')`, &siteShellGone),

		// The repair round-trips: home is cross-chain from the help
		// shell too, and its 200 full document swaps the shell back.
		chromedp.Click(`#to-home`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('[data-cui-layout]').getAttribute('data-cui-layout')`, &homeBack),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if layout != "help" {
		t.Errorf("the 404 full document must install the destination chain's shell, got layout %q", layout)
	}
	if !siteShellGone {
		t.Error("the origin chain's shell must be replaced (repair), not kept around the 404 body")
	}
	if url != "/help/gone" {
		t.Errorf("url = %q, want /help/gone", url)
	}
	if rig.fulls("/help/gone") != 1 {
		t.Errorf("the cross-chain nav must have fetched the full document once, got %d", rig.fulls("/help/gone"))
	}
	if homeBack != "site" {
		t.Errorf("navigating home must swap the shell back, got layout %q", homeBack)
	}
}

// TestPartial404NotCached: a SAME-CHAIN navigation to a 404-outlet
// route gets the partial/envelope answer with status 404 — applied
// into the live shell (no reload), never cached: going Back refetches
// it instead of replaying stored bytes.
func TestPartial404NotCached(t *testing.T) {
	rig := newNotFoundOutletRig(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var status float64
	var urlAfterBack string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/help/present"),
		chromedp.WaitVisible(`#screen-present`, chromedp.ByID),

		// The partial branch answers 404-shaped; the status is the
		// server's own decision (writePartialResult).
		chromedp.Evaluate(`fetch('/help/gone', {headers: {'X-Gofastr-Navigate': '1', 'X-Gofastr-Fills': '2', 'X-Gofastr-From': '/help/present'}}).then(r => r.status).then(s => { window.__nfStatus = s; }), 0`, &status),

		// Same-chain click: the envelope partial applies the 404 page
		// into the live shell (no full reload).
		chromedp.Click(`#to-gone`, chromedp.ByID),
		chromedp.WaitVisible(`#nf-body`, chromedp.ByID),

		// Away, then Back: the 404 partial must be refetched, never
		// replayed from the cache.
		chromedp.Click(`#to-present`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-present`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#nf-body`, chromedp.ByID),
		chromedp.Evaluate(`location.pathname`, &urlAfterBack),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__nfStatus`, &status),
	); err != nil {
		t.Fatalf("chromedp (status read): %v", err)
	}
	if int(status) != 404 {
		t.Errorf("partial fetch status = %d, want 404", int(status))
	}
	if urlAfterBack != "/help/gone" {
		t.Errorf("url after Back = %q, want /help/gone", urlAfterBack)
	}
	if got := rig.partials("/help/gone"); got != 3 {
		t.Errorf("the 404 partial must be applied once and never cached — Back refetches a fresh verdict: got %d partial fetches, want 3 (status probe + click + Back)", got)
	}
}

// nfStaticFill and nfStaticScreen are the rig's tiny components.
type nfStaticFill string

func (f nfStaticFill) Render() render.HTML { return render.HTML(string(f)) }

type nfStaticScreen string

func (s nfStaticScreen) Render() render.HTML { return render.HTML(string(s)) }
