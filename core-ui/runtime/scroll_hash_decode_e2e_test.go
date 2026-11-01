package runtime

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// hashDecodeSite serves /a linking to /b#caf%C3%A9 and /b holding
// id="café" far down the page; location.hash keeps the percent-encoded
// form, the id holds the decoded one. /b#100% is a fragment
// decodeURIComponent throws on, with the raw text as its id, and
// /b#bad%E0%A4 names nothing and does not decode either. regions is
// extra layout markup: a data-cui-area region hands the navigation to
// the envelope module instead of the core navigator.
func hashDecodeSite(t *testing.T, regions string) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	screens := map[string]string{
		"/a": `<h1 id="screen-a">a</h1><a id="to-cafe" href="/b#caf%C3%A9">cafe</a> <a id="to-pct" href="/b#100%">pct</a> <a id="to-bad" href="/b#bad%E0%A4">bad</a>`,
		"/b": `<h1 id="screen-b">b</h1><div style="height:1500px"></div><h2 id="café">café</h2>` +
			`<div style="height:1500px"></div><h2 id="100%">100%</h2><div style="height:3000px"></div>`,
	}
	for path, body := range screens {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
				`<script>window.__errs=[];addEventListener('error',(e)=>__errs.push(String(e.message)));addEventListener('unhandledrejection',(e)=>__errs.push(String(e.reason)));</script>`+
				`<script type="application/json" id="gofastr-routes">[{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script>`+
				`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">`+regions+
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">`+body+`</main>`+
				`</div><script src="/__gofastr/runtime.js"></script></body></html>`)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// runHashNav clicks link on /a and reports the final scrollY and the
// target's document offset.
func runHashNav(t *testing.T, srv *httptest.Server, link, target string) (y, want float64) {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Click(link, chromedp.ByQuery),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`scrollY`, &y),
		chromedp.Evaluate(fmt.Sprintf(`document.getElementById(%q).getBoundingClientRect().top + scrollY`, target), &want),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return y, want
}

// A percent-encoded fragment lands on its decoded id, through the core
// navigator and through the envelope module; a fragment that does not
// decode falls back to the raw id instead of throwing.
func TestEncodedHashScrollsToTarget(t *testing.T) {
	for _, tc := range []struct{ name, regions string }{
		{"core", ""},
		{"envelope", `<aside data-cui-area="side"></aside>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := hashDecodeSite(t, tc.regions)
			for _, nav := range []struct{ link, target string }{
				{"#to-cafe", "café"},
				{"#to-pct", "100%"},
			} {
				y, want := runHashNav(t, srv, nav.link, nav.target)
				if want < 1000 || math.Abs(y-want) > 2 {
					t.Errorf("%s: scrollY = %v, want ≈%v (the #%s target)", nav.link, y, want, nav.target)
				}
			}
			// No target and no decoded form: the top of the page, and
			// no URIError thrown out of the scroll callback.
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var y float64
			var errs []string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/a"),
				chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
				chromedp.Click(`#to-bad`, chromedp.ByID),
				chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
				chromedp.Sleep(800*time.Millisecond),
				chromedp.Evaluate(`scrollY`, &y),
				chromedp.Evaluate(`window.__errs`, &errs),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if y != 0 || len(errs) != 0 {
				t.Errorf("#to-bad: scrollY = %v, errors %q, want 0 and none", y, errs)
			}
		})
	}
}
