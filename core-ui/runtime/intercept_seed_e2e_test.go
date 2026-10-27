package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// An overlay pane applies the signal seed its answer carries, as an
// in-page navigation does, and drops the inert seed script.
func TestInterceptPaneAppliesSeed(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := `[{"path":"/list"},{"path":"/rec/:id","intercept":{"from":"/list","as":"drawer"}}]`
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Intercept") != "" {
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"open":true},"g":{}}</script><p id="pane">PANE</p>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">`+routes+`</script>`+
			`</head><body><main id="list-main"><a id="to-a" href="/rec/a">rec a</a></main>`+
			`<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got struct {
		Open   any  `json:"open"`
		Script bool `json:"script"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/list"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#to-a`, chromedp.ByID),
		chromedp.WaitVisible(`#pane`, chromedp.ByID),
		chromedp.Evaluate(`({open: window.__gofastr.getSignal('open'), script: !!document.getElementById('gofastr-signals-partial')})`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got.Open != true || got.Script {
		t.Errorf("after the pane opened: open = %v, seed script left = %v; want open = true and no script", got.Open, got.Script)
	}
}
