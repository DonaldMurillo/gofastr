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

// A signal control inside a widget root did nothing: the document
// delegate skipped widget roots and the widget's own listener handles only
// data-cui-rpc and close, so Tabs in a drawer never switched (2026-10-27
// bug bash R1). Inside a widget a node carrying both a signal mutation
// and data-cui-rpc mutates the signal and sends nothing, as outside one.
func TestSignalControlsWorkInWidget(t *testing.T) {
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
		fmt.Fprint(w, `<!doctype html><html><head><title>signals</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
</head><body><main><button id="open" data-cui-open="dlg">Open</button></main>
<div class="cui-widget" data-cui-widget="dlg" role="dialog" hidden>
<span id="count" data-cui-signal="n">0</span>
<button type="button" id="inc" data-cui-signal-inc="n">Inc</button>
<button type="button" id="set" data-cui-signal-set="tab:2">Tab 2</button>
<button type="button" id="toggle" data-cui-signal-toggle="on">Toggle</button>
<button type="button" id="both" data-cui-signal-set="tab:3" data-cui-rpc="/act">Both</button>
<button type="button" id="act" data-cui-rpc="/act">Act</button>
</div>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	shown := `!document.querySelector('[data-cui-widget="dlg"]').hidden`
	ctx := chromedptest.Context(t)
	var count, tab string
	var on bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#open`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.loadModule === 'function'`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Poll(shown, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#inc`, chromedp.ByID),
		chromedp.Click(`#inc`, chromedp.ByID),
		chromedp.Click(`#set`, chromedp.ByID),
		chromedp.Click(`#toggle`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Text(`#count`, &count, chromedp.ByID),
		chromedp.Evaluate(`String(__gofastr.getSignal('tab'))`, &tab),
		chromedp.Evaluate(`__gofastr.getSignal('on') === true`, &on),
	); err != nil {
		t.Fatal(err)
	}
	if count != "2" {
		t.Errorf("two inc clicks in a widget left the bound count at %q, want 2", count)
	}
	if tab != "2" {
		t.Errorf("a signal-set click in a widget left tab = %q, want 2", tab)
	}
	if !on {
		t.Error("a signal-toggle click in a widget did not flip the signal")
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`#both`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('tab'))`, &tab),
	); err != nil {
		t.Fatal(err)
	}
	if tab != "3" {
		t.Errorf("a signal-and-rpc node in a widget left tab = %q, want 3", tab)
	}
	if n := posts.Load(); n != 0 {
		t.Errorf("a signal-and-rpc node in a widget sent %d requests, want 0 (the signal wins)", n)
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`#act`, chromedp.ByID),
		chromedp.Poll(`true`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if n := posts.Load(); n != 1 {
		t.Errorf("an rpc button in a widget sent %d requests, want 1", n)
	}
}
