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

// A modal that refuses Escape owns the key: Escape fell through to the
// popover stack and closed a popover under the scrim. A popover opened
// above the modal still closes.
func TestEscapeSparesPopoverUnderModal(t *testing.T) {
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
		fmt.Fprint(w, `[
{"hidden":true,"cfg":{"name":"under","chromePath":"/chrome/under","closeOnEscape":true}},
{"hidden":true,"cfg":{"name":"lock","chromePath":"/chrome/lock","backdrop":true}},
{"hidden":true,"cfg":{"name":"over","chromePath":"/chrome/over","closeOnEscape":true}}]`)
	})
	for _, name := range []string{"under", "lock", "over"} {
		mux.HandleFunc("/chrome/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<div class="cui-widget" data-cui-widget="%s" role="dialog" aria-label="%s"><button>%s</button></div>`, name, name, name)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>esc</title></head><body><main>home</main>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	open := `(() => !!window.__gofastr._widgets && Object.keys(window.__gofastr._widgets).sort().join(','))()`
	esc := `document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true})); true`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var afterUnder, afterOver string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.openWidget('under')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`!!document.querySelector('[data-cui-widget="under"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.openWidget('lock')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`window.__gofastr._modalStack.length === 1`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(esc, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(open, &afterUnder),
		chromedp.Evaluate(`window.__gofastr.openWidget('over')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`!!document.querySelector('[data-cui-widget="over"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(esc, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(open, &afterOver),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if afterUnder != "lock,under" {
		t.Fatalf("after Escape under the modal: open %q, want lock,under", afterUnder)
	}
	if afterOver != "lock,under" {
		t.Fatalf("after Escape with a popover above the modal: open %q, want lock,under", afterOver)
	}
}

// Escape belongs to the newest layer. A closable modal returned before
// the popover stack was read, so a popover opened inside it stayed open
// while the modal under it closed.
func TestEscapeClosesPopoverAboveModal(t *testing.T) {
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
		fmt.Fprint(w, `[
{"hidden":true,"cfg":{"name":"dlg","chromePath":"/chrome/dlg","backdrop":true,"closeOnEscape":true}},
{"hidden":true,"cfg":{"name":"pop","chromePath":"/chrome/pop","closeOnEscape":true}}]`)
	})
	for _, name := range []string{"dlg", "pop"} {
		mux.HandleFunc("/chrome/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<div class="cui-widget" data-cui-widget="%s" role="dialog" aria-label="%s"><button>%s</button></div>`, name, name, name)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>esc</title></head><body><main>home</main>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	open := `(() => Object.keys(window.__gofastr._widgets).filter((n) => window.__gofastr._widgets[n]).sort().join(','))()`
	esc := `document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true})); true`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var first, second string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.openWidget('dlg')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`window.__gofastr._modalStack.length === 1`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.openWidget('pop')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`!!document.querySelector('[data-cui-widget="pop"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(esc, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(open, &first),
		chromedp.Evaluate(esc, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(open, &second),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if first != "dlg" {
		t.Fatalf("after the first Escape: open %q, want dlg (the popover above it closes first)", first)
	}
	if second != "" {
		t.Fatalf("after the second Escape: open %q, want none", second)
	}
}
