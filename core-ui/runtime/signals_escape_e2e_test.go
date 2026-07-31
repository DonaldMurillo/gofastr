package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestSetSignal_EscapesSpecialCharName pins fix #8: setSignal builds the
// `[data-fui-signal="<name>"]` selector by concatenation, so a signal name
// containing `"]` (or other selector metacharacters) produced a malformed
// selector and querySelectorAll THREW — taking setSignal down with it.
// sse.js already escapes its island name; signals.js was the holdout.
// The fix runs the name through CSS.escape, the same shape sse.js:76 uses.
func TestSetSignal_EscapesSpecialCharName(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// The attribute value is a"]b — single-quoted so the " is literal.
		fmt.Fprint(w, `<!doctype html><html><head><title>esc</title></head><body>
<span id="t" data-fui-signal='a"]b'>old</span>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var threw, text string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// A signal name with selector metacharacters MUST NOT throw.
		chromedp.Evaluate(`(function(){try{window.__gofastr.setSignal('a"]b','hello');return '';}catch(e){return String(e);}})()`, &threw),
		chromedp.Evaluate(`document.getElementById('t').textContent`, &text),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if threw != "" {
		t.Errorf("setSignal threw on a selector-metacharacter name: %s — signals.js builds the selector without CSS.escape (sse.js:76 does escape; this was the holdout)", threw)
	}
	if text != "hello" {
		t.Errorf("signal node text=%q, want \"hello\" — the escaped name did not target its bound node", text)
	}
}
