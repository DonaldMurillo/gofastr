package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
)

// chainSite builds a two-chain test app entirely from hand-rolled HTML:
//
//	/docs/a, /docs/b   → chain ["l:site", "g:/docs/:docs"]
//	/account           → chain ["l:site"]
//	/items/:id         → chain ["l:site"]
//	/app               → chain ["l:app"] (disjoint root)
//
// It records every request (path + X-Gofastr-From + full/partial) so tests
// can assert exactly which wire shape each navigation used.
type chainReq struct {
	Path    string
	From    string
	Partial bool
}

type chainSite struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []chainReq
}

func (c *chainSite) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, chainReq{
		Path:    r.URL.Path,
		From:    r.Header.Get("X-Gofastr-From"),
		Partial: r.Header.Get("X-Gofastr-Navigate") == "1",
	})
}

func (c *chainSite) requests() []chainReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]chainReq(nil), c.reqs...)
}

const chainRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/docs/a","layouts":["l:site","g:/docs/:docs"]},` +
	`{"path":"/docs/b","layouts":["l:site","g:/docs/:docs"]},` +
	`{"path":"/account","layouts":["l:site"]},` +
	`{"path":"/items/:id","layouts":["l:site"]},` +
	`{"path":"/app","layouts":["l:app"]}` +
	`]</script>`

// docsLayer renders the docs group layer (sidebar + slot) around content.
func docsLayer(content string) string {
	return `<div class="fui-screen-group" data-fui-screen-group="/docs/">` +
		`<div data-fui-layout="docs" data-fui-layout-key="g:/docs/:docs" class="layout-docs">` +
		`<nav id="docs-sidebar" aria-label="Sidebar"><a id="to-a" href="/docs/a">A</a><a id="to-b" href="/docs/b">B</a></nav>` +
		`<div class="layout-content" tabindex="-1" data-fui-layout-slot="g:/docs/:docs">` + content + `</div>` +
		`</div></div>`
}

// sitePage renders the full document: site shell (layer 0) around inner.
func sitePage(inner string) string {
	return `<!doctype html><html><head><title>t</title>` + chainRoutes + `</head><body>` +
		`<div data-fui-layout="site" data-fui-layout-key="l:site" class="layout-site">` +
		`<header id="site-header"><a id="to-account" href="/account">Account</a>` +
		`<a id="to-item" href="/items/42">Item</a><a id="to-app" href="/app">App</a></header>` +
		`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` + inner + `</main>` +
		`</div><script src="/__gofastr/runtime.js"></script></body></html>`
}

func appPage() string {
	return `<!doctype html><html><head><title>app</title>` + chainRoutes + `</head><body>` +
		`<div data-fui-layout="app" data-fui-layout-key="l:app" class="layout-app">` +
		`<main role="main" tabindex="-1" data-fui-layout-slot="l:app"><h1 id="app-screen">App</h1></main>` +
		`</div><script src="/__gofastr/runtime.js"></script></body></html>`
}

// newChainSite wires the fixture server. Partial responses follow the
// X-Gofastr-From/X-Gofastr-Swap contract exactly as uihost emits it.
func newChainSite(t *testing.T) *chainSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	c := &chainSite{}

	docsScreen := func(id, label string) string {
		return `<h1 id="` + id + `">` + label + `</h1>`
	}
	partial := func(w http.ResponseWriter, swap, title, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", title)
		if swap != "" {
			w.Header().Set("X-Gofastr-Swap", swap)
		}
		fmt.Fprint(w, body)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for _, p := range []struct{ path, id, label string }{
		{"/docs/a", "screen-a", "A"}, {"/docs/b", "screen-b", "B"},
	} {
		p := p
		mux.HandleFunc(p.path, func(w http.ResponseWriter, r *http.Request) {
			c.record(r)
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				from := r.Header.Get("X-Gofastr-From")
				switch {
				case from == "/docs/a" || from == "/docs/b":
					// Whole chain shared → bare content below the docs layer.
					partial(w, "g:/docs/:docs", p.label, docsScreen(p.id, p.label))
				case from != "":
					// Shares the site root only → re-render the docs layer.
					partial(w, "l:site", p.label, docsLayer(docsScreen(p.id, p.label)))
				default:
					partial(w, "", p.label, docsScreen(p.id, p.label))
				}
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, sitePage(docsLayer(docsScreen(p.id, p.label))))
		})
	}
	mux.HandleFunc("/account", func(w http.ResponseWriter, r *http.Request) {
		c.record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			if r.Header.Get("X-Gofastr-From") != "" {
				partial(w, "l:site", "Account", `<h1 id="account-screen">Account</h1>`)
				return
			}
			partial(w, "", "Account", `<h1 id="account-screen">Account</h1>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, sitePage(`<h1 id="account-screen">Account</h1>`))
	})
	mux.HandleFunc("/items/42", func(w http.ResponseWriter, r *http.Request) {
		c.record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "l:site", "Item", `<h1 id="item-screen">Item 42</h1>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, sitePage(`<h1 id="item-screen">Item 42</h1>`))
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		c.record(r)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, appPage())
	})

	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

// Sibling nav inside the docs group must swap only the innermost slot:
// site header AND docs sidebar keep DOM identity, and the request carries
// the origin path.
func TestSiblingNavSwapsInnermostLayer(t *testing.T) {
	site := newChainSite(t)
	ctx := newSeedBrowserCtx(t)

	var headerKept, sidebarKept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/docs/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp = 'kept';
			document.getElementById('docs-sidebar').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp === 'kept'`, &headerKept),
		chromedp.Evaluate(`document.getElementById('docs-sidebar').dataset.stamp === 'kept'`, &sidebarKept),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !headerKept || !sidebarKept {
		t.Errorf("shared layers rebuilt on sibling nav (header kept=%v sidebar kept=%v)", headerKept, sidebarKept)
	}
	for _, rq := range site.requests() {
		if rq.Path == "/docs/b" && rq.Partial && rq.From != "/docs/a" {
			t.Errorf("partial to /docs/b carried From=%q, want /docs/a", rq.From)
		}
	}
}

// Navigating from a docs page to /account (shares only the site root)
// must keep the site header, drop the docs layer, and swap at l:site.
func TestMidChainNavRendersDelta(t *testing.T) {
	site := newChainSite(t)
	ctx := newSeedBrowserCtx(t)

	var headerKept bool
	var sidebarCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/docs/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#to-account`, chromedp.ByID),
		chromedp.WaitVisible(`#account-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp === 'kept'`, &headerKept),
		chromedp.Evaluate(`document.querySelectorAll('#docs-sidebar').length`, &sidebarCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !headerKept {
		t.Error("site header rebuilt although the root layer is shared")
	}
	if sidebarCount != 0 {
		t.Errorf("docs layer must be gone after leaving the group, found %d sidebars", sidebarCount)
	}
}

// Chains with no shared root swap the whole shell via a full fetch.
func TestCrossChainNavSwapsShell(t *testing.T) {
	site := newChainSite(t)
	ctx := newSeedBrowserCtx(t)

	var appVisible bool
	var siteHeaderCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/docs/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Click(`#to-app`, chromedp.ByID),
		chromedp.WaitVisible(`#app-screen`, chromedp.ByID),
		chromedp.Evaluate(`!!document.getElementById('app-screen')`, &appVisible),
		chromedp.Evaluate(`document.querySelectorAll('#site-header').length`, &siteHeaderCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !appVisible {
		t.Fatal("app screen did not render")
	}
	if siteHeaderCount != 0 {
		t.Errorf("old shell chrome survived a cross-chain swap (%d site headers)", siteHeaderCount)
	}
	// The nav to /app must have been a FULL fetch (no navigate header).
	for _, rq := range site.requests() {
		if rq.Path == "/app" && rq.Partial {
			t.Error("cross-chain nav used a partial fetch; must full-fetch the new shell")
		}
	}
}

// A concrete URL of a dynamic route must resolve to the route's chain —
// previously the literal-path manifest lookup returned nothing, so the
// nav was treated as cross-layout and rebuilt the shell.
func TestDynamicRouteResolvesChain(t *testing.T) {
	site := newChainSite(t)
	ctx := newSeedBrowserCtx(t)

	var headerKept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/docs/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#to-item`, chromedp.ByID),
		chromedp.WaitVisible(`#item-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp === 'kept'`, &headerKept),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !headerKept {
		t.Error("dynamic-route nav rebuilt the shared shell — chain lookup missed the pattern")
	}
	for _, rq := range site.requests() {
		if rq.Path == "/items/42" && !rq.Partial {
			t.Error("dynamic-route nav full-fetched; shared root should partial-swap")
		}
	}
}

// Back-nav replay: a cached sibling entry swaps at its recorded layer
// with no refetch and no duplicated chrome.
func TestCachedRevisitReplaysAtLayer(t *testing.T) {
	site := newChainSite(t)
	ctx := newSeedBrowserCtx(t)

	var sidebarCount, mainCount int
	var aVisible bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/docs/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Click(`#to-a`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`!!document.getElementById('screen-a')`, &aVisible),
		chromedp.Evaluate(`document.querySelectorAll('#docs-sidebar').length`, &sidebarCount),
		chromedp.Evaluate(`document.querySelectorAll('main').length`, &mainCount),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !aVisible {
		t.Fatal("cached revisit did not render")
	}
	if sidebarCount != 1 || mainCount != 1 {
		t.Errorf("duplicated chrome after cached replay: %d sidebars, %d mains", sidebarCount, mainCount)
	}
	// /docs/a was boot-loaded once; the cached revisit must not refetch it.
	count := 0
	for _, rq := range site.requests() {
		if rq.Path == "/docs/a" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("cached revisit refetched /docs/a (%d requests, want 1)", count)
	}
}

// A server echoing a swap boundary the DOM does not have (deploy skew)
// must trigger a full-page recovery, never a wrong-cell swap.
func TestSwapEchoMismatchRecovers(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	page := func(inner string) string {
		return `<!doctype html><html><head><title>t</title>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/x","layouts":["l:site"]},{"path":"/y","layouts":["l:site"]}]</script>` +
			`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux.HandleFunc("/x", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="x-screen">X</h1><a id="to-y" href="/y">Y</a>`))
	})
	fullYs := 0
	mux.HandleFunc("/y", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			// Echo a boundary that does not exist client-side.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:renamed-in-new-deploy")
			fmt.Fprint(w, `<h1 id="y-screen">Y</h1>`)
			return
		}
		fullYs++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<h1 id="y-screen">Y</h1>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/x"),
		chromedp.WaitVisible(`#x-screen`, chromedp.ByID),
		chromedp.Click(`#to-y`, chromedp.ByID),
		chromedp.WaitVisible(`#y-screen`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if fullYs == 0 {
		t.Error("mismatched swap echo did not trigger the full-page recovery")
	}
}
