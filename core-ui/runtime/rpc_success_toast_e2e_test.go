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

// TestRPCSuccessToastShowsTitleAndSurvivesNavigate: a form RPC carrying
// data-cui-rpc-success-toast toasts the attribute's title on the 2xx
// answer, BEFORE the data-cui-rpc-navigate runs, and the toast is still
// on the page after that SPA navigation lands — the toast stack sits
// outside the swapped <main>, which is the whole reason a save that
// bounces the reader to the record page still says "Saved".
func TestRPCSuccessToastShowsTitleAndSurvivesNavigate(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/save", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true}`)
	})
	mux.HandleFunc("/saved", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprint(w, `<h1 id="landed">saved page</h1>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>rpc success</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/saved"}]</script>
</head><body>
<main role="main" tabindex="-1">
<form id="f" data-cui-rpc="/__gofastr/save" data-cui-rpc-method="POST"
  data-cui-rpc-success-toast="Saved." data-cui-rpc-navigate="/saved">
<input name="title"><button type="submit" id="save">Save</button></form>
</main>
`+toastStackHTML()+`
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var path, toastText string
	var toastConnected bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#save`, chromedp.ByID),
		chromedp.Click(`#save`, chromedp.ByID),
		// The navigation target's marker proves the SPA swap ran before
		// the toast is judged: a toast that only lived long enough to be
		// created would pass a check taken too early.
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`(() => {
  const t = document.querySelector('[data-hui-toast-id]');
  return t ? t.textContent : '';
})()`, &toastText),
		chromedp.Evaluate(`!!(document.querySelector('[data-hui-toast-id]') || {}).isConnected`, &toastConnected),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/saved" {
		t.Fatalf("navigation path = %q, want /saved — the toast fired but the navigate did not follow", path)
	}
	if !toastConnected {
		t.Fatal("the success toast did not survive the SPA navigation; the stack must sit outside the swapped region")
	}
	if !strings.Contains(toastText, "Saved.") {
		t.Errorf("toast text %q lacks the attribute's title", toastText)
	}
}
