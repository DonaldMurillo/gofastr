package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A widget opened on /a whose chrome is still loading when the reader
// navigates to /b must not open on /b. The chrome fetch carries on
// after the navigate swaps the client cache; mounting its answer there
// shows a panel the reader walked away from, its deep link writes
// ?panel=late onto the new page's URL, and its params land as signals.
func TestStaleChromeSkipsNextPage(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)

	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"late","chromePath":"/chrome/late","deepLinkKey":"panel","deepLinkValue":"late","deepLinkParams":["who"]}}]`)
	})
	mux.HandleFunc("/chrome/late", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="cui-widget" data-cui-widget="late" role="dialog" aria-label="late">late</div>`)
	})
	routes := `<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"}]</script>`
	for path, screen := range map[string]string{"/a": "screen-a", "/b": "screen-b"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			body := `<h1 id="` + screen + `">` + screen + `</h1><a id="to-b" href="/b">B</a>`
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
				`<main role="main" tabindex="-1">`+body+`</main>`+
				`<script src="/__gofastr/runtime.js"></script></body></html>`)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var mounted bool
	var search, who string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog.late)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// Not awaited: the chrome request hangs until release.
		chromedp.Evaluate(`window.__gofastr.openWidget('late', {pushUrl: true, params: {who: 'stale'}}); true`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.ActionFunc(func(context.Context) error { free(); return nil }),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="late"]') || !!window.__gofastr._widgets.late`, &mounted),
		chromedp.Evaluate(`location.search`, &search),
		chromedp.Evaluate(`String(window.__gofastr.getSignal('who') ?? '')`, &who),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if mounted {
		t.Errorf("widget opened on /a mounted on /b after its chrome arrived late")
	}
	if who != "" {
		t.Errorf("signal who = %q on /b, want unset (the stale open applied its deep-link params)", who)
	}
	if search != "" {
		t.Errorf("location.search = %q on /b, want empty (the stale open pushed its deep link)", search)
	}
}
