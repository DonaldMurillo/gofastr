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

// ctxSwitchServer serves one hidden popover-style widget ("row", closes
// on an outside click) whose chrome names the ?ctx= it was asked for,
// and a page with one trigger per row, each carrying its own
// data-cui-ctx. head runs before the runtime loads.
func ctxSwitchServer(t *testing.T, head string) string {
	t.Helper()
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
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"row","chromePath":"/chrome/row","closeOnClick":true,"closeOnEscape":true,"stylePath":"/row.css"}}]`)
	})
	mux.HandleFunc("/row.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
	})
	mux.HandleFunc("/chrome/row", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("ctx") {
		case "slow":
			time.Sleep(300 * time.Millisecond)
		case "slower":
			time.Sleep(900 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="cui-widget" data-cui-widget="row" role="dialog" aria-label="row"><span id="rowmark">%s</span></div>`, r.URL.Query().Get("ctx"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>rows</title>%s</head><body><main>
<button id="a" data-cui-open="row" data-cui-ctx="1">Row 1</button>
<button id="b" data-cui-open="row" data-cui-ctx="2">Row 2</button>
<button id="s" data-cui-open="row" data-cui-ctx="slow">Slow row</button>
<button id="t" data-cui-open="row" data-cui-ctx="slower">Slower row</button>
<p id="away">elsewhere</p>
</main><span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`, head)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

// A per-entity widget follows the trigger that opened it. With row 1's
// chrome open, a click on row 2's trigger showed row 1's content again:
// _mountByName returned early on any live mount whatever its ctx, and
// the outside-click handler skips every trigger of the widget, so
// nothing dismissed row 1. The second trigger now replaces the chrome
// with its own context's.
func TestWidgetOpenWithOtherCtxRemounts(t *testing.T) {
	url := ctxSwitchServer(t, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var mark string
	var roots int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#a`, chromedp.ByID),
		chromedp.Poll(`(document.getElementById('rowmark') || {}).textContent === '1'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`(document.getElementById('rowmark') || {}).textContent || ''`, &mark),
		chromedp.Evaluate(`document.querySelectorAll('[data-cui-widget="row"]').length`, &roots),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if mark != "2" || roots != 1 {
		t.Fatalf("after opening row 2 over row 1: chrome shows ctx %q with %d roots, want ctx 2 in one root", mark, roots)
	}
	// The same trigger again keeps the open chrome (no remount churn),
	// and an outside click still closes it.
	var again string
	var left int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__mountedRoot = document.querySelector('[data-cui-widget="row"]'); true`, nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-cui-widget="row"]') === window.__mountedRoot ? 'same' : 'remounted'`, &again),
		chromedp.Click(`#away`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-cui-widget="row"]').length`, &left),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if again != "same" {
		t.Fatalf("re-clicking the open row's own trigger %s the chrome, want it kept", again)
	}
	if left != 0 {
		t.Fatalf("an outside click left %d roots open", left)
	}
}

// A popover dismissed in the same task that mounted it (a
// fui:widget-open listener closing it at once) must leave no
// outside-click listener behind. The listener was added from a
// setTimeout(0) scheduled at mount, which ran AFTER dismiss had already
// removed the (not yet added) handler, so the handler stayed on the
// document for the page's life. head counts every run of a document
// click listener whose source names the widget trigger selector.
func TestWidgetDismissedAtMountLeavesNoOutsideHandler(t *testing.T) {
	url := ctxSwitchServer(t, `<script>
(() => {
  const add = EventTarget.prototype.addEventListener, rem = EventTarget.prototype.removeEventListener;
  const wrapped = new WeakMap();
  window.__outsideRuns = 0;
  document.addEventListener = function (t, f, o) {
    if (t === 'click' && typeof f === 'function' && String(f).indexOf('[data-cui-open="') >= 0) {
      const w = function (e) { window.__outsideRuns++; return f.call(this, e); };
      wrapped.set(f, w);
      return add.call(this, t, w, o);
    }
    return add.call(this, t, f, o);
  };
  document.removeEventListener = function (t, f, o) { return rem.call(this, t, wrapped.get(f) || f, o); };
  add.call(document, 'fui:widget-open', (e) => { if (window.__closeAtOnce) window.__gofastr.closeWidget(e.detail.name); });
})();
</script>`)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var runs int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__closeAtOnce = true`, nil),
		chromedp.Click(`#a`, chromedp.ByID),
		chromedp.Poll(`!!window.__gofastr._widgetCatalog && !window.__gofastr._widgets.row && !!window.__gofastr._chromeCache['row\u00001']`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__outsideRuns = 0; true`, nil),
		chromedp.Click(`#away`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__outsideRuns`, &runs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if runs != 0 {
		t.Fatalf("a click after the popover closed ran %d outside-click handlers, want 0 (a stale listener outlived its mount)", runs)
	}
}

// The newest trigger wins whatever order the chromes land in: a click
// on the slow row and then on the slower one shows the slower row.
// The slow row's chrome lands first and used to mount; the slower
// row's then met a mounted widget and was dropped, leaving the row the
// reader clicked away from.
func TestWidgetLateChromeOfEarlierOpenMountsNothing(t *testing.T) {
	url := ctxSwitchServer(t, "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var mark string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!window.__gofastr._widgetCatalog && !!window.__gofastr._mountByName`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// One turn, so both opens start before either chrome lands.
		chromedp.Evaluate(`document.getElementById('s').click(); document.getElementById('t').click(); true`, nil),
		chromedp.Sleep(1600*time.Millisecond),
		chromedp.Evaluate(`[...document.querySelectorAll('#rowmark')].map((n) => n.textContent).join(',')`, &mark),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if mark != "slower" {
		t.Fatalf("after clicking the slow row then the slower row: chrome shows %q, want slower", mark)
	}
}
