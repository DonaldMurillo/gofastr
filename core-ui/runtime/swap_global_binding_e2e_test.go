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

// A global keeps the value the reader gave it across a swap, but the
// swapped-in markup carries the server's value. The store said 1 while
// the destination's fresh cart badge read 0, and replaying a cached
// page (Back to the boot entry, Forward to a cached partial) showed the
// count from when that page was left. Every swap paints the stored
// value into the global's new bindings. The first hop goes to a page
// with no seed island, so only the boot seed marks cart global. The
// host leaves #gofastr-signals-page out when every seed key is global.
func TestSwappedGlobalBindingShowsStore(t *testing.T) {
	for _, arm := range []struct{ name, head string }{
		{name: "page list", head: `<script type="application/json" id="gofastr-signals">{"quantity":1,"cart":0}</script>
  <script type="application/json" id="gofastr-signals-page">["quantity"]</script>`},
		{name: "globals only", head: `<script type="application/json" id="gofastr-signals">{"cart":0}</script>`},
	} {
		t.Run(arm.name, func(t *testing.T) { swappedGlobalShowsStore(t, arm.head) })
	}
}

func swappedGlobalShowsStore(t *testing.T, head string) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":10},"g":{"cart":0}}</script>`+
			`<h1 id="pageB">Page B</h1>`+
			`<span id="bcart" data-cui-signal="cart">0</span>`+
			`<button id="badd" data-cui-signal-inc="cart">add</button>`)
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprint(w, `<h1 id="pageC">Page C</h1>`+
			`<button id="cadd" data-cui-signal-inc="cart">add</button>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/b"},{"path":"/c"}]</script>
  `+head+`
</head><body>
  <main role="main" tabindex="-1">
    <h1 id="pageA">Page A</h1>
    <span id="acart" data-cui-signal="cart">0</span>
    <button id="aadd" data-cui-signal-inc="cart">add</button>
    <a id="tob" href="/b">to B</a>
    <a id="toc" href="/c">to C</a>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var unseededBack, onB, backOnA, forwardOnB string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#toc`, chromedp.ByID),
		chromedp.WaitVisible(`#pageC`, chromedp.ByID),
		chromedp.Click(`#cadd`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#pageA`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Text(`#acart`, &unseededBack, chromedp.ByID),
		chromedp.Click(`#tob`, chromedp.ByID),
		chromedp.WaitVisible(`#pageB`, chromedp.ByID),
		chromedp.Text(`#bcart`, &onB, chromedp.ByID),
		chromedp.Click(`#badd`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#pageA`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Text(`#acart`, &backOnA, chromedp.ByID),
		chromedp.Click(`#aadd`, chromedp.ByID),
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.WaitVisible(`#pageB`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Text(`#bcart`, &forwardOnB, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if unseededBack != "1" {
		t.Errorf("badge after Back from a page with no seed = %q, want the stored 1", unseededBack)
	}
	if onB != "1" {
		t.Errorf("fresh badge after the swap = %q, want the stored 1", onB)
	}
	if backOnA != "2" {
		t.Errorf("badge after Back to the boot entry = %q, want the stored 2", backOnA)
	}
	if forwardOnB != "3" {
		t.Errorf("badge after Forward to the cached partial = %q, want the stored 3", forwardOnB)
	}
}
