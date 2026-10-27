package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// An SSR-inlined widget is hydrated in place and re-hidden on close, so
// every reopen mounts the same root. Its listeners belong to one mount:
// a reopened dialog's RPC button sends one request per click, and closing
// it again re-hides the root instead of removing it.
func TestHydratedWidgetReopenWiresOnce(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var posts atomic.Int64
	catalog := `[{"hidden":true,"cfg":{"name":"dlg","position":"center","backdrop":false,` +
		`"closeOnEscape":false,"closeOnClick":false,"stylePath":"/style.css","chromePath":"/chrome/dlg"}}]`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		src, ok := Module(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(catalog))
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>reopen</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
</head><body><main><button id="open" data-cui-open="dlg">Open</button></main>
<div class="cui-widget" data-cui-widget="dlg" role="dialog" hidden>
<button type="button" id="act" data-cui-rpc="/act">Act</button>
<button type="button" id="close" data-cui-action="close">Close</button>
</div>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	shown := `!document.querySelector('[data-cui-widget="dlg"]').hidden`
	hidden := `document.querySelector('[data-cui-widget="dlg"]')?.hidden === true`
	ctx := chromedptest.Context(t)
	var rootLeft bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#open`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.loadModule === 'function'`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Poll(shown, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#close`, chromedp.ByID),
		chromedp.Poll(hidden, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Poll(shown, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#act`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click(`#close`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(hidden, &rootLeft),
	); err != nil {
		t.Fatal(err)
	}
	if n := posts.Load(); n != 1 {
		t.Errorf("one click on the reopened dialog sent %d requests, want 1", n)
	}
	if !rootLeft {
		t.Error("second close removed the SSR root instead of re-hiding it")
	}
}
