package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestSPANavigationClosesOrdinaryDisclosureAndPreservesOptOut proves that
// ordinary disclosures keep the documented dismiss-on-navigation behavior,
// while a shell disclosure can opt out explicitly when it owns persistent
// navigation state.
func TestSPANavigationPreservesSidebarDisclosure(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		_, _ = fmt.Fprint(w, `<h1 id="next">Next</h1>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><html><head>
<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"}]</script>
</head><body>
<aside>
  <details id="sidebar" data-fui-disclosure data-fui-disclosure-persist open>
    <summary>Documentation</summary>
    <a id="sidebar-link" href="/next">Next</a>
  </details>
  <details id="ordinary" data-fui-disclosure open>
    <summary>Ordinary menu</summary>
  </details>
  <details id="drawer" data-fui-disclosure data-fui-disclosure-trap open>
    <summary>Mobile navigation</summary>
  </details>
</aside>
<main role="main" tabindex="-1"><h1>Home</h1></main>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
  var sidebarOpen, ordinaryOpen, drawerOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#sidebar-link`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && typeof window.__gofastr.navigate === 'function'`, nil,
			chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Click(`#sidebar-link`, chromedp.ByID),
		chromedp.WaitVisible(`#next`, chromedp.ByID),
		chromedp.Evaluate(`String(document.getElementById('sidebar').hasAttribute('open'))`, &sidebarOpen),
		chromedp.Evaluate(`String(document.getElementById('ordinary').hasAttribute('open'))`, &ordinaryOpen),
		chromedp.Evaluate(`String(document.getElementById('drawer').hasAttribute('open'))`, &drawerOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if sidebarOpen != "true" {
		t.Errorf("persistent sidebar disclosure closed during SPA navigation: got %q", sidebarOpen)
	}
	if ordinaryOpen != "false" {
		t.Errorf("ordinary disclosure stayed open during SPA navigation: got %q", ordinaryOpen)
	}
	if drawerOpen != "false" {
		t.Errorf("focus-trapped drawer stayed open during SPA navigation: got %q", drawerOpen)
	}
}
