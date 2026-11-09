package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The navigate re-fetch asks for the destination's page, not its URL: a
// link to /b?x=1 lands on /b, and a widget scoped with .Pages("/b")
// belongs there. nav's gofastr:navigate carries the query in detail.path,
// and the catalog's ?page= used to send it whole, so the server matched
// "/b?x=1" against the widget's routes and left it out.
func TestNavCatalogDropsTheQuery(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var pages []string
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	// The server side of .Pages("/b"): the widget is offered on /b only.
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		pages = append(pages, page)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if page != "/b" {
			fmt.Fprint(w, `[]`)
			return
		}
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"scoped","chromePath":"/chrome/scoped"}}]`)
	})
	serveTwoScreens(mux, "/b?x=1")
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var synced string
	var inCatalog bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.__synced = new Promise((done) => addEventListener('gofastr:widgets-synced', (e) => { if (e.detail.path !== '/a') done(e.detail.path); })); true`, nil),
		chromedp.Evaluate(`document.getElementById('to-b').click(); true`, nil),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`window.__synced`, &synced, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.Evaluate(`!!(window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog.scoped)`, &inCatalog),
	); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if synced != "/b" || !inCatalog {
		t.Fatalf("after a link to /b?x=1: widgets-synced path %q, scoped widget in catalog %v; catalog pages asked %q", synced, inCatalog, pages)
	}
}
