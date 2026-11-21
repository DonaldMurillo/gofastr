package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// The default CSP requires a Trusted Types value at every HTML and script
// sink, so a runtime write that skips the 'gofastr' policy throws. This
// drives the paths that write markup or load scripts (first load, module
// loads, a poll swap, a signal in html mode, a navigation inside a layout
// with an outlet, one across shells, and Back through the screen cache)
// under the real default chain, and fails on any violation or error.
func TestRuntimeSwapsUnderTrustedTypes(t *testing.T) {
	registry.IsolateForTest(t)
	aside := app.NewOutlet("aside", app.OutletOptions{Default: &rawHTMLComp{html: `<p id="aside">aside</p>`}})
	shell := app.NewLayout("tt-shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(aside))
	})
	other := app.NewLayout("tt-other", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	a := app.NewApp("tt")
	a.RegisterScreen(app.NewScreen("/", &rawHTMLComp{html: `<p id="home">home</p>` +
		`<div id="poll" data-cui-poll="5s" data-cui-poll-src="/frag">waiting</div>` +
		`<div id="sig" data-cui-signal="tt" data-cui-signal-mode="html"></div>` +
		`<a id="goB" href="/b">b</a>`}).WithTitle("Home"), shell)
	a.RegisterScreen(app.NewScreen("/b", &rawHTMLComp{html: `<p id="b">b</p><a id="goC" href="/c">c</a>`}).WithTitle("B"), shell)
	a.RegisterScreen(app.NewScreen("/c", &rawHTMLComp{html: `<p id="c">c</p>`}).WithTitle("C"), other)

	mux := http.NewServeMux()
	mux.HandleFunc("/frag", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<span id="polled">polled</span>`))
	})
	mux.Handle("/", New(a))
	srv := httptest.NewServer(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(mux))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "require-trusted-types-for 'script'") {
		t.Fatalf("the page is not served under Trusted Types enforcement: %q", csp)
	}

	ctx := ownedStyleContext(t)
	wait := func(js string) chromedp.Action {
		return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(15*time.Second))
	}
	var got struct {
		Path     string   `json:"path"`
		Soft     bool     `json:"soft"`
		Problems []string `json:"problems"`
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`window.__tt=[];` +
				`document.addEventListener('securitypolicyviolation',e=>__tt.push(e.violatedDirective+' '+e.blockedURI+' '+e.sample),true);` +
				`addEventListener('error',e=>__tt.push('error '+e.message));` +
				`addEventListener('unhandledrejection',e=>__tt.push('rejection '+(e.reason&&e.reason.message||e.reason)));`).Do(ctx)
			return err
		}),
		chromedp.Navigate(srv.URL+"/"),
		wait(`!!(window.__gofastr && window.__gofastr.navigate) && document.readyState === 'complete'`),
		chromedp.Evaluate(`window.__soft = 1`, nil),
		wait(`!!document.getElementById('polled')`),
		chromedp.Evaluate(`window.__gofastr.setSignal('tt', '<b id="sigb">bold</b>')`, nil),
		wait(`!!document.getElementById('sigb')`),
		chromedp.Click(`#goB`, chromedp.ByQuery),
		wait(`!!document.getElementById('b') && location.pathname === '/b'`),
		chromedp.Click(`#goC`, chromedp.ByQuery),
		wait(`!!document.getElementById('c') && location.pathname === '/c'`),
		chromedp.Evaluate(`history.back()`, nil),
		wait(`!!document.getElementById('b') && location.pathname === '/b'`),
		chromedp.Evaluate(`history.back()`, nil),
		wait(`!!document.getElementById('home') && location.pathname === '/'`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`({path: location.pathname, soft: window.__soft === 1, problems: window.__tt})`, &got),
	); err != nil {
		t.Fatalf("drive the runtime: %v (state %+v)", err, got)
	}
	if !got.Soft {
		t.Fatal("a navigation loaded a new document: every step must run in the runtime")
	}
	if len(got.Problems) > 0 {
		t.Errorf("the runtime hit a Trusted Types violation or error:\n%s", strings.Join(got.Problems, "\n"))
	}

	// Anti-vacuity: the page enforces, so a raw string at a sink throws.
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { try { document.createElement('div').innerHTML = '<i>x</i>'; return 'accepted'; } catch (e) { return 'refused'; } })()`, &raw)); err != nil {
		t.Fatal(err)
	}
	if raw != "refused" {
		t.Fatalf("a raw innerHTML string was %s: the browser is not enforcing Trusted Types, so this test proves nothing", raw)
	}
}
