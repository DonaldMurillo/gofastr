package runtime

// The activelink load-time sweep and the data-cui-activelink handover.
// headless.Sidebar marks every leaf data-cui-activelink so the module
// can clear a first-paint aria-current a client navigation left behind.
// The handover covers navigations only: with no navigation the server's
// mark is the truth, even where the href is not exactly the URL
// (Active:true on /orders served at /orders?page=2, or on a detail page).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func activelinkSweepPage(path string) string {
	orders, settings := "", ""
	switch path {
	case "/orders":
		orders = ` aria-current="page"`
	case "/settings":
		settings = ` aria-current="page"`
	}
	return fmt.Sprintf(`<!doctype html><html><head><title>%s</title></head><body>
  <nav aria-label="Sidebar">
    <a id="orders" href="/orders"%s data-cui-activelink>Orders</a>
    <a id="settings" href="/settings"%s data-cui-activelink>Settings</a>
  </nav>
  <main id="main"><a id="go-settings" href="/settings">Go to settings</a> %s</main>
  <script>window.__gofastr_routes = [{path: "/orders"}, {path: "/settings"}];</script>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, path, orders, settings, path)
}

// activelinkSweepServer serves the sidebar page and its partials. When
// hold is non-nil the activelink module's response waits on it, so a
// test can land a client navigation before the module loads.
func activelinkSweepServer(t *testing.T, hold chan struct{}) *httptest.Server {
	t.Helper()
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
	mux.HandleFunc("/__gofastr/runtime/activelink.js", func(w http.ResponseWriter, r *http.Request) {
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		src, _ := Module("activelink")
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", r.URL.Path)
			fmt.Fprint(w, r.URL.Path)
			return
		}
		// The server settles the current entry by section, the way an
		// app sets SidebarItem.Active: /orders?page=2 is still Orders.
		fmt.Fprint(w, activelinkSweepPage(r.URL.Path))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const activelinkLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.activelink)`

func ariaCurrent(id string) string {
	return fmt.Sprintf(`document.getElementById(%q).getAttribute('aria-current') || ''`, id)
}

// TestActiveLeafKeepsMarkOnQueryLoad: a server-marked sidebar leaf at
// /orders?page=2 keeps aria-current once the module loads (no
// navigation happened), and loses it after a client navigation to
// /settings. Before the fix the load-time sweep cleared it at once.
func TestActiveLeafKeepsMarkOnQueryLoad(t *testing.T) {
	srv := activelinkSweepServer(t, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var loaded bool
	var orders string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/orders?page=2"),
		chromedp.WaitVisible(`#main`, chromedp.ByID),
		chromedp.Poll(activelinkLoaded, &loaded, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(ariaCurrent("orders"), &orders),
	); err != nil {
		t.Fatalf("load: %v", err)
	}
	if orders != "page" {
		t.Fatalf("the server's mark on /orders was cleared at load with no navigation (aria-current=%q)", orders)
	}

	var path, settings string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__softNav = 1`, nil),
		chromedp.Click(`#go-settings`, chromedp.ByID),
		chromedp.Poll(`location.pathname === '/settings'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__softNav === 1 ? location.pathname : 'full load'`, &path),
		chromedp.Evaluate(ariaCurrent("orders"), &orders),
		chromedp.Evaluate(ariaCurrent("settings"), &settings),
	); err != nil {
		t.Fatalf("client nav: %v", err)
	}
	if path != "/settings" {
		t.Fatalf("the click did not end in a client navigation to /settings (got %q)", path)
	}
	if orders != "" {
		t.Errorf("the stale mark on /orders survived a client navigation to %s (aria-current=%q)", path, orders)
	}
	if settings != "page" {
		t.Errorf("/settings is not marked after the navigation (aria-current=%q)", settings)
	}
}

// TestLeafMarkClearedIfNavBeforeLoad: the race the handover exists for.
// A client navigation lands before the idle module loads; when it does
// load, its sweep clears the first-paint mark the navigation left on
// the old leaf. Holding the module's response makes the order certain.
func TestLeafMarkClearedIfNavBeforeLoad(t *testing.T) {
	hold := make(chan struct{})
	srv := activelinkSweepServer(t, hold)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var loaded, soft bool
	var orders, settings string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/orders"),
		chromedp.WaitVisible(`#main`, chromedp.ByID),
		chromedp.Poll(`document.readyState === 'complete'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__softNav = 1`, nil),
		chromedp.Click(`#go-settings`, chromedp.ByID),
		chromedp.Poll(`location.pathname === '/settings'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__softNav === 1`, &soft),
		chromedp.Evaluate(activelinkLoaded, &loaded),
		chromedp.Evaluate(ariaCurrent("orders"), &orders),
	); err != nil {
		t.Fatalf("navigate before load: %v", err)
	}
	if !soft {
		t.Fatal("the click was a full document load, not a client navigation")
	}
	if loaded {
		t.Fatal("the activelink module loaded before the held response was released")
	}
	if orders != "page" {
		t.Fatalf("the navigation itself moved the mark (aria-current=%q): the fixture no longer isolates the module's sweep", orders)
	}
	close(hold)
	if err := chromedp.Run(ctx,
		chromedp.Poll(activelinkLoaded, &loaded, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(ariaCurrent("orders"), &orders),
		chromedp.Evaluate(ariaCurrent("settings"), &settings),
	); err != nil {
		t.Fatalf("module load: %v", err)
	}
	if orders != "" {
		t.Errorf("the first-paint mark on /orders survived a navigation that landed before load (aria-current=%q): two lit entries", orders)
	}
	if settings != "page" {
		t.Errorf("/settings is not marked after the module loaded (aria-current=%q)", settings)
	}
}
