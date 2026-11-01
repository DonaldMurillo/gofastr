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

// A signal named like an Object.prototype member ("toString",
// "valueOf") is an ordinary signal. The boot seed tested store[k],
// which reads the inherited built-in for such a name: the seed wrote
// its value onto Object.prototype.toString and never created the
// signal, so getSignal answered undefined.
func TestBootSeedCreatesPrototypeNamedSignals(t *testing.T) {
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
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>seed</title>
<script type="application/json" id="gofastr-signals">{"toString":"seeded","valueOf":"v"}</script></head><body>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const G = window.__gofastr;
			return {
				ts: G.getSignal('toString') ?? null,
				vo: G.getSignal('valueOf') ?? null,
				own: Object.prototype.hasOwnProperty.call(G._signals, 'toString'),
				leaked: Object.prototype.toString.value !== undefined || Object.prototype.valueOf.value !== undefined,
			};
		})()`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got["ts"] != "seeded" || got["vo"] != "v" || got["own"] != true || got["leaked"] != false {
		t.Fatalf("prototype-named seed keys: %v, want toString=seeded valueOf=v as own signals and nothing written onto Object.prototype", got)
	}
}
