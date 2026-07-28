package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Screen-cache invalidation contract: X-Gofastr-Invalidate on an RPC
// response evicts the named screens from the per-tab cache so the next
// visit re-fetches; __gofastr.invalidate/refresh are the JS mirrors.
//
// Every screen renders "name N" where N is that URI's server fetch
// count — a repeat of the same N proves a cache hit, a bump proves a
// re-fetch.

// invalidationSrv serves a home page (links + mutation buttons), three
// item screens (partial renders with per-URI fetch counters), and an
// /entry page that renders full-document or partial depending on the
// nav header (the initial-page-cache case).
func invalidationSrv(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	counts := map[string]int{}
	bump := func(uri string) int {
		mu.Lock()
		defer mu.Unlock()
		counts[uri]++
		return counts[uri]
	}

	const routesJSON = `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/items"},{"path":"/items/42"},{"path":"/entry"},{"path":"/redir"}]</script>`

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// Demand modules (the toggle step loads toggleaction).
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	partial := func(w http.ResponseWriter, name string, n int) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprintf(w, `<h1 id="v">%s %d</h1><a id="home" href="/">home</a>`, name, n)
	}
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		partial(w, "items-"+r.URL.Query().Get("view"), bump(r.URL.RequestURI()))
	})
	mux.HandleFunc("/items/42", func(w http.ResponseWriter, r *http.Request) {
		partial(w, "detail", bump(r.URL.RequestURI()))
	})
	mux.HandleFunc("/entry", func(w http.ResponseWriter, r *http.Request) {
		n := bump(r.URL.RequestURI())
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "entry", n)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>entry</title>%s</head><body>
  <main role="main" tabindex="-1"><h1 id="v">entry %d</h1><a id="home" href="/">home</a></main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, routesJSON, n)
	})
	// Mutation endpoints, one per header shape under test.
	mut := func(header string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set("X-Gofastr-Invalidate", header)
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("ok"))
		}
	}
	mux.HandleFunc("/mut-items", mut(`["/items"]`))
	mux.HandleFunc("/mut-exact", mut(`["/items?view=open"]`))
	mux.HandleFunc("/mut-all", mut(`["*"]`))
	mux.HandleFunc("/mut-bad", mut(`not-json`))
	// Failed mutation: the header on a non-2xx must be ignored.
	mux.HandleFunc("/mut-fail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Invalidate", `["*"]`)
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	// Policy redirect that also invalidates: eviction must be applied
	// BEFORE the X-Gofastr-Location chase so the redirect target is
	// fetched fresh even though a plain nav does not bypass the cache.
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Invalidate", `["/items"]`)
		w.Header().Set("X-Gofastr-Location", "/items?view=open")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>inval</title>%s</head><body>
  <main role="main" tabindex="-1">
    <a id="open" href="/items?view=open">open</a>
    <a id="closed" href="/items?view=closed">closed</a>
    <a id="detail" href="/items/42">detail</a>
    <a id="toentry" href="/entry?x=1">entry</a>
    <a id="redir" href="/redir">redir</a>
    <button id="mut-items" data-fui-rpc="/mut-items" data-fui-rpc-method="POST">a</button>
    <button id="mut-fail" data-fui-rpc="/mut-fail" data-fui-rpc-method="POST">e</button>
    <button id="tog" data-fui-comp="ui-toggle-action" data-state="idle"
            data-fui-toggle-endpoint="/mut-items">
      <span data-fui-toggle-idle>t</span><span data-fui-toggle-committed hidden>c</span>
    </button>
    <button id="mut-exact" data-fui-rpc="/mut-exact" data-fui-rpc-method="POST">b</button>
    <button id="mut-all" data-fui-rpc="/mut-all" data-fui-rpc-method="POST">c</button>
    <button id="mut-bad" data-fui-rpc="/mut-bad" data-fui-rpc-method="POST">d</button>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, routesJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// waitText polls until sel's textContent equals want — robust against
// swap latency without fixed sleeps.
func waitText(sel, want string) chromedp.Action {
	return chromedp.Poll(
		fmt.Sprintf(`document.querySelector(%q)?.textContent === %q`, sel, want),
		nil, chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(100*time.Millisecond))
}

// visit clicks the home-page link `id` and asserts the screen shows
// `want`, then returns home.
func visit(id, want string) []chromedp.Action {
	return []chromedp.Action{
		chromedp.Click("#"+id, chromedp.ByID),
		waitText("#v", want),
		chromedp.Click("#home", chromedp.ByID),
		chromedp.WaitVisible("#mut-items", chromedp.ByID),
	}
}

func TestInvalidateHeaderEviction(t *testing.T) {
	srv := invalidationSrv(t)
	ctx := newSeedBrowserCtx(t)

	steps := []chromedp.Action{
		chromedp.Navigate(srv.URL + "/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	}
	// Seed the cache with three screens.
	steps = append(steps, visit("open", "items-open 1")...)
	steps = append(steps, visit("closed", "items-closed 1")...)
	steps = append(steps, visit("detail", "detail 1")...)
	// Malformed header: mutation succeeds, cache untouched.
	steps = append(steps, chromedp.Click("#mut-bad", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("open", "items-open 1")...)
	// Exact-query selector: only that variant re-fetches.
	steps = append(steps, chromedp.Click("#mut-exact", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("open", "items-open 2")...)
	steps = append(steps, visit("closed", "items-closed 1")...)
	// Queryless selector: every query variant re-fetches, the detail
	// page (different pathname) survives.
	steps = append(steps, chromedp.Click("#mut-items", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("open", "items-open 3")...)
	steps = append(steps, visit("closed", "items-closed 2")...)
	steps = append(steps, visit("detail", "detail 1")...)
	// Wildcard clears everything.
	steps = append(steps, chromedp.Click("#mut-all", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("detail", "detail 2")...)
	// Non-2xx: the header on a failed mutation is ignored — detail
	// still serves from cache.
	steps = append(steps, chromedp.Click("#mut-fail", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("detail", "detail 2")...)
	// Redirect + invalidate on one response: eviction happens before
	// the X-Gofastr-Location chase, so the redirect target re-fetches
	// (a plain nav would otherwise serve the cached copy).
	steps = append(steps,
		chromedp.Click("#redir", chromedp.ByID),
		waitText("#v", "items-open 4"),
		chromedp.Click("#home", chromedp.ByID),
		chromedp.WaitVisible("#tog", chromedp.ByID),
	)
	// ToggleAction is a mutation surface too — its commit consumes the
	// header (same one-line consumer as optimistic/sortable).
	steps = append(steps, chromedp.Click("#tog", chromedp.ByID), chromedp.Sleep(300*time.Millisecond))
	steps = append(steps, visit("open", "items-open 5")...)

	if err := chromedp.Run(ctx, steps...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
}

func TestInvalidateJSAndRefresh(t *testing.T) {
	srv := invalidationSrv(t)
	ctx := newSeedBrowserCtx(t)

	var n int
	var search string
	if err := chromedp.Run(ctx,
		// Land directly on a query-bearing URL — the initial screen must
		// be cached under pathname+search.
		chromedp.Navigate(srv.URL+"/entry?x=1"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		waitText("#v", "entry 1"),
		// Away and back: cache hit proves the initial-page keying.
		chromedp.Click("#home", chromedp.ByID),
		chromedp.WaitVisible("#toentry", chromedp.ByID),
		chromedp.Click("#toentry", chromedp.ByID),
		waitText("#v", "entry 1"),
		// Exact-selector JS eviction, then back and forth: re-fetch.
		chromedp.Click("#home", chromedp.ByID),
		chromedp.WaitVisible("#toentry", chromedp.ByID),
		chromedp.Evaluate(`(window.__gofastr.invalidate('/entry?x=1'), 1)`, &n),
		chromedp.Click("#toentry", chromedp.ByID),
		waitText("#v", "entry 2"),
		// refresh(): re-render the current screen in place — the URL,
		// including a #fragment, must survive untouched.
		chromedp.Evaluate(`(location.hash = '#keep', window.__gofastr.refresh(), 1)`, &n),
		waitText("#v", "entry 3"),
		chromedp.Evaluate(`location.pathname + location.search + location.hash`, &search),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if search != "/entry?x=1#keep" {
		t.Fatalf("URL after refresh = %q, want /entry?x=1#keep", search)
	}
}
