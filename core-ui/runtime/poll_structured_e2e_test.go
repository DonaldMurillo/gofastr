package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// A widget poll skips a key whose value did not change, so an unchanged
// count does not re-render or flash its badge. A structured value
// decodes into a fresh object on every response, so the identity test
// never matched it: each tick rewrote the bound node and replayed its
// flash. The same object stays untouched; a changed one still renders.
func TestPollSkipsUnchangedObject(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var changed atomic.Bool
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if changed.Load() {
			fmt.Fprint(w, `{"order":{"id":7,"lines":[1,2,3]},"count":2}`)
			return
		}
		fmt.Fprint(w, `{"order":{"id":7,"lines":[1,2]},"count":2}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-signals">{"order":{"id":7,"lines":[1,2]},"count":2}</script>
</head><body>
  <main role="main" tabindex="-1">
    <span id="order" data-cui-signal="order" data-cui-flash-on-update>{"id":7,"lines":[1,2]}</span>
    <span id="count" data-cui-signal="count" data-cui-flash-on-update>2</span>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
  <script>
    window.__writes = { order: 0, count: 0 };
    for (const key of ['order', 'count']) {
      new MutationObserver((records) => { window.__writes[key] += records.length; })
        .observe(document.getElementById(key), { childList: true, characterData: true, subtree: true, attributes: true });
    }
    __gofastr.loadModule('poll').then(() => {
      window.__writes = { order: 0, count: 0 };
      __gofastr._widgetPoll({ name: 'orders', statePath: '/state', pollMs: 100 }, {});
      window.__ready = true;
    });
  </script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var unchanged, after string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__ready === true && (__gofastr.pollStatus || {}).ticks >= 4`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`JSON.stringify(window.__writes)`, &unchanged),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if unchanged != `{"order":0,"count":0}` {
		t.Fatalf("bound nodes written on unchanged polls = %s, want none", unchanged)
	}
	changed.Store(true)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('order').textContent.includes('[1,2,3]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => { const ticks = __gofastr.pollStatus.ticks; return new Promise((done) => { const wait = () => (__gofastr.pollStatus.ticks >= ticks + 3 ? done(JSON.stringify(window.__writes)) : setTimeout(wait, 50)); wait(); }); })()`, &after, func(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}),
	); err != nil {
		t.Fatalf("a changed object did not render: %v", err)
	}
	var writes struct{ Order, Count int }
	if err := json.Unmarshal([]byte(after), &writes); err != nil {
		t.Fatal(err)
	}
	if writes.Order == 0 || writes.Count != 0 {
		t.Fatalf("writes after one change = %s, want order written and count untouched", after)
	}
}
