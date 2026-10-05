package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestToastButtonFallsBackWithoutStack: a data-cui-toast trigger on a
// page with no toast stack must still show the toast through the
// kernel's fallback region, the same path a X-Gofastr-Toast header
// takes. The click delegator called NS.toast directly and dropped its
// null, so the button did nothing.
func TestToastButtonFallsBackWithoutStack(t *testing.T) {
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
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>`+
			`</head><body><main role="main" tabindex="-1">`+
			`<button id="tb" type="button" data-cui-toast='{"title":"Saved it"}'>Toast</button>`+
			`</main><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var text string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#tb`, chromedp.ByID),
		chromedp.Click(`#tb`, chromedp.ByID),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`(document.querySelector('[data-cui-toast-fallback]') || {}).textContent || ''`, &text),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(text, "Saved it") {
		t.Fatalf("no fallback toast after the click on a stackless page (fallback region text %q)", text)
	}
}
