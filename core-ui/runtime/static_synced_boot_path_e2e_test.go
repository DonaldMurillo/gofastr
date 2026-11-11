package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The static composition's boot pass announces gofastr:widgets-synced
// for the page it ran for. It read location.pathname after the catalog
// fetch settled, so a navigation that landed while the fetch was out
// made the boot pass name the destination: two events for /next and
// none for /, where the live twin (widgets-boot) names its captured
// boot path.
func TestStaticBootSyncedNamesBootPage(t *testing.T) {
	js, err := StaticJS()
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/widgets.json", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		close(release)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html data-cui-static><head><title>static</title>
  <script>window.__synced = window.__synced || []; addEventListener('gofastr:widgets-synced', (e) => window.__synced.push(e.detail.path));</script>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"}]</script></head><body><main>
  <a id="nav" href="/next">next</a>
  <span id="ready">%s</span>
  </main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, r.URL.Path)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var synced []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#nav`, chromedp.ByID),
		chromedp.Poll(`location.pathname === '/next' && document.getElementById('ready').textContent === '/next'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`fetch('/release').then(() => true)`, nil),
		chromedp.Poll(`window.__synced.length >= 2`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__synced`, &synced),
	); err != nil {
		t.Fatalf("chromedp: %v (synced %q)", err, synced)
	}
	// Each pass names its own page; the navigation's pass may settle
	// first.
	sort.Strings(synced)
	if !slices.Equal(synced, []string{"/", "/next"}) {
		t.Errorf("widgets-synced paths %q, want / once (the boot pass) and /next once", synced)
	}
}
