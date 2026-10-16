package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// sheetWaitPage serves one page whose head and body carry the given
// markup, the runtime, and a fullbundle module whose answer the test
// picks. The page's catalog names a component whose sheet 404s; a
// failed link still has a sheet object, so the wait skips it at once.
func sheetWaitPage(t *testing.T, head, body string, module http.HandlerFunc) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/fullbundle.js", module)
	mux.HandleFunc("/css/gone.css", http.NotFound)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><head><title>t</title>` +
			`<script>window.__gofastr_catalog={"gone":{stylePath:"/css/gone.css",version:"1"}};</script>` +
			head + `</head><body>` + body + `<script src="/__gofastr/runtime.js"></script></body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// A fullbundle module that loads but leaves its marker behind.
func stubFullbundle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Write([]byte(`(window.__gofastr.loadedModules ||= {}).fullbundle = true;`))
}

// waitMs runs a navigation's sheet wait for markup that renders the
// named component and reports how long it took, or -1 when a 6 s
// timer won the race (a timer that never runs means a frozen page).
const waitMs = `(async () => {
	const t0 = performance.now();
	const r = await Promise.race([
		window.__gofastr._cssReady('<div data-cui-comp="gone"></div>').then(() => performance.now() - t0),
		new Promise((r) => setTimeout(() => r(-1), 6000)),
	]);
	return r;
})()`

func evalWait(t *testing.T, url, before string) float64 {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	var ms float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._cssReady)`, nil),
		chromedp.Evaluate(before, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
		chromedp.Evaluate(waitMs, &ms, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("chromedp (a frozen page times out here): %v", err)
	}
	return ms
}

// TestBundleMarkerCannotLoop: a bundle marker the fullbundle module
// does not spend (one in page content, or a module from another
// release) sends the sheet wait to the module once, never in a loop
// that starves the page's timers and input.
func TestBundleMarkerCannotLoop(t *testing.T) {
	for _, c := range []struct{ name, head, body string }{
		{"body", "", `<div data-cui-bundle-href="/x.css"></div>`},
		{"head", `<meta data-cui-bundle-href="/x.css">`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			url := sheetWaitPage(t, c.head, c.body, stubFullbundle)
			if ms := evalWait(t, url, `true`); ms < 0 {
				t.Fatal("the sheet wait never settled")
			}
		})
	}
}

// TestStalledBundleModuleIsBounded: a fullbundle module whose fetch
// never answers costs a navigation the sheet bound, not the rest of
// the page's life.
func TestStalledBundleModuleIsBounded(t *testing.T) {
	url := sheetWaitPage(t, `<meta data-cui-bundle-href="/x.css">`, "", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	if ms := evalWait(t, url, `true`); ms < 0 {
		t.Fatal("the sheet wait hung on a module fetch that never answered")
	}
}
