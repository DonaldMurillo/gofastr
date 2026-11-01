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

// A component inserted after load (an intercepted route's overlay, an
// island swap, a widget's chrome) carries the same data-action-mount a
// navigated page does. The mount must fire for it, once, or the inserted
// form never fetches its options.
func TestActionMountFiresForInsertedComponent(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/widget/test-comp.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `(() => { window.__fires = 0; window.__gofastr.register("test-comp", {
  "fill-on-mount": (params) => { window.__fires++; for (const o of document.querySelectorAll('[data-component="test-comp"] .out')) o.textContent = 'mounted:' + (params.label || ''); }
}); })();`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// A live page: the actions manifest names the component's
		// content-addressed script, which SSR already linked.
		fmt.Fprint(w, `<!doctype html><html><head><script>window.__gofastr_actions = {"test-comp": "h"};</script></head><body><div id="host"></div>
  <script src="/__gofastr/runtime.js"></script><script src="/__gofastr/widget/test-comp.js?v=h"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var fires int
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`window.__fires === 0 && !!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.actionloader)`, nil),
		chromedp.Evaluate(`document.getElementById('host').innerHTML = '<div data-component="test-comp"><section data-action-mount="fill-on-mount" data-param-label="ok"></section><span class="out">idle</span></div>'; 0`, nil),
		chromedp.Poll(`document.querySelector('.out').textContent === 'mounted:ok'`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__fires`, &fires),
	)
	if err != nil {
		t.Fatalf("an inserted component's data-action-mount never fired: %v", err)
	}
	if fires != 1 {
		t.Errorf("mount fired %d times, want once", fires)
	}
	// A navigation swaps the DOM and dispatches gofastr:navigate in the
	// same task; the observer then sees the same nodes. One fire, still.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`window.__fires = 0;
document.getElementById('host').innerHTML = '<div data-component="test-comp"><section data-action-mount="fill-on-mount" data-param-label="nav"></section><span class="out">idle</span></div>';
window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: {} })); 0`, nil),
		chromedp.Poll(`document.querySelector('.out').textContent === 'mounted:nav'`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__fires`, &fires),
	)
	if err != nil {
		t.Fatal(err)
	}
	if fires != 1 {
		t.Errorf("a navigated swap fired its mount %d times, want once", fires)
	}
}
