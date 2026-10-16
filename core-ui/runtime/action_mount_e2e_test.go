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

// TestActionMount_FiresOnHydration drives the data-action-mount contract in a
// real browser: a component action registered via actions.js must fire once
// on load (not only on a user event), so a server-rendered island can
// populate itself. This is the keystone the blueprint entity_list / detail /
// relation-select blocks rely on to fetch their data on first paint.
func TestActionMount_FiresOnHydration(t *testing.T) {
	runActionMount(t, "")
}

// The host emits runtime.js and the action scripts with defer. Deferred
// scripts run while readyState is already "interactive", so a runtime
// that ran its mount pass on the spot fired before the action scripts
// after it had registered anything, and the mount was lost. The first
// pass has to wait for DOMContentLoaded, which follows every deferred
// script.
func TestActionMount_FiresWhenDeferred(t *testing.T) {
	runActionMount(t, " defer")
}

func runActionMount(t *testing.T, attr string) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
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
		fmt.Fprintf(w, `<!doctype html><html><head></head><body>
  <div data-component="test-comp">
    <section data-action-mount="fill-on-mount" data-param-label="ok"></section>
    <span id="out">idle</span>
  </div>
  <script%[1]s src="/__gofastr/runtime.js"></script>
  <script%[1]s src="/__gofastr/actions.js"></script>
</body></html>`, attr)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

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
