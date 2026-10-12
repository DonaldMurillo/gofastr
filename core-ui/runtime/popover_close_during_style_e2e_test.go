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

// TestPopoverClosedDuringStyleLoadLeavesTriggerIdle: anchoring waits
// for the widget's stylesheet before it measures. A close that lands
// during that wait used to be ignored: the anchoring resumed, marked
// the trigger active and attached window resize/scroll listeners to a
// widget nothing referenced any more, so the trigger stayed in its
// open state and the listeners ran for the page's lifetime.
func TestPopoverClosedDuringStyleLoadLeavesTriggerIdle(t *testing.T) {
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
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"cfg":{"name":"notes","hidden":true,"closeOnEscape":true,
			"closeOnClickOutside":true,"chromePath":"/chrome/notes","stylePath":"/style/notes.css"},
			"hidden":true}]`)
	})
	mux.HandleFunc("/chrome/notes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="cui-widget cui-pos-top-right" data-cui-widget="notes" role="dialog" aria-label="Notifications"><p id="note-body">a note</p></div>`)
	})
	mux.HandleFunc("/style/notes.css", func(w http.ResponseWriter, _ *http.Request) {
		// A slow sheet: the anchoring's await spans the close below.
		time.Sleep(1500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.cui-widget { position: fixed; }`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, openAnchorPage())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#bell`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.closeWidget('notes'); true`, nil),
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const b = document.getElementById('bell');
			return JSON.stringify({
				active: b.classList.contains('is-popover-trigger-active'),
				attr: b.getAttribute('data-cui-popover-trigger'),
				widgets: Object.keys(window.__gofastr._widgets || {}),
			});
		})()`, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	want := `{"active":false,"attr":null,"widgets":[]}`
	if state != want {
		t.Fatalf("after a close during the style wait: %s, want %s", state, want)
	}
}
