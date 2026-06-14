package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestActionMount_FiresOnHydration drives the data-action-mount contract in a
// real browser: a component action registered via actions.js must fire once
// on load (not only on a user event), so a server-rendered island can
// populate itself. This is the keystone the blueprint entity_list / detail /
// relation-select blocks rely on to fetch their data on first paint.
func TestActionMount_FiresOnHydration(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// actions.js as emitted by uihost.actionsToJS: register a handler keyed
	// by the action name. The handler fills #out so we can observe the fire.
	mux.HandleFunc("/__gofastr/actions.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `(() => {
  const G = window.__gofastr;
  G.register("test-comp", {
    "fill-on-mount": (params) => { document.getElementById('out').textContent = 'mounted:' + (params.label || ''); }
  });
})();`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head><body>
  <div data-component="test-comp">
    <section data-action-mount="fill-on-mount" data-param-label="ok"></section>
    <span id="out">idle</span>
  </div>
  <script src="/__gofastr/runtime.js"></script>
  <script src="/__gofastr/actions.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var out string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		// The mount action must fire on load with no user interaction, and
		// data-param-* values must reach the handler's params object.
		chromedp.Poll(`document.getElementById('out').textContent === 'mounted:ok'`, nil),
		chromedp.Text(`#out`, &out, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if out != "mounted:ok" {
		t.Errorf("data-action-mount did not fire on hydration: out=%q", out)
	}
}
