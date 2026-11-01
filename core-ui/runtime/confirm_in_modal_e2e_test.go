package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A data-cui-confirm control inside a modal widget asks in the kit's
// <dialog>, opened with showModal() on top of the widget. The widget's
// Tab trap (capture phase) and Escape handler knew only the widget
// modal stack: Tab from the dialog's Cancel was pulled back toward the
// widget (whose controls the open dialog makes inert), so Accept was
// unreachable by keyboard, and Escape closed the widget as well as the
// dialog. Both now stand down while a modal dialog outside the top
// widget is showing.
func TestConfirmInsideModalWidgetOwnsKeyboard(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"dlg","chromePath":"/chrome/dlg","backdrop":true,"closeOnEscape":true}}]`)
	})
	mux.HandleFunc("/chrome/dlg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="cui-widget" data-cui-widget="dlg" role="dialog" aria-label="dlg">
<button id="first">First</button>
<button id="del" data-cui-rpc="/act" data-cui-confirm="It cannot be undone.">Delete</button>
</div>`)
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>confirm in modal</title></head><body><main>home</main>
`+confirmTemplate+`
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	focused := `(() => { const a = document.activeElement; return a ? (a.getAttribute('data-cui-confirm-part') || a.id || a.tagName) : ''; })()`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var onOpen, afterTab string
	var dialogGone, widgetOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.openWidget('dlg')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`window.__gofastr._modalStack.length === 1 && !!window.__gofastr.loadedModules.widgetfocus`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`#del`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(focused, &onOpen),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(focused, &afterTab),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('body > dialog')`, &dialogGone),
		chromedp.Evaluate(`!!window.__gofastr._widgets.dlg`, &widgetOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if onOpen != "cancel" {
		t.Fatalf("the confirm opened with focus on %q, want cancel", onOpen)
	}
	if afterTab != "accept" {
		t.Fatalf("Tab from the confirm's Cancel focused %q, want accept", afterTab)
	}
	if !dialogGone || !widgetOpen {
		t.Fatalf("Escape on the confirm: dialog gone=%v, widget still open=%v; want the confirm closed and the widget kept", dialogGone, widgetOpen)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("a dismissed confirm sent %d requests", n)
	}
	// With the confirm gone, the widget owns the keys again: Escape
	// closes it.
	var closed bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('first').focus(); true`, nil),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!window.__gofastr._widgets.dlg`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!window.__gofastr._widgets.dlg`, &closed),
	); err != nil {
		t.Fatalf("Escape after the confirm closed: %v", err)
	}
}
