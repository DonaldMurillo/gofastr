package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// chromeCtxServer serves the runtime plus one hidden dialog widget whose
// chrome endpoint renders per-request state: the ?ctx= the trigger carried
// (#321) and a server-side principal that the post-sign-out destination
// flips mid-document (#329). Counting chrome fetches per ctx is the point:
// cache behaviour is a question about what left the browser, not about what
// the DOM looks like afterwards.
type chromeCtxServer struct {
	Srv *httptest.Server

	mu         sync.Mutex
	principal  string
	perCtx     map[string]int
	totalFetch int
	// slowfail gate: the FIRST request for ctx "slowfail" blocks on
	// release and then fails 500; later ones serve normally. Lets a test
	// hold a chrome fetch in flight across an SPA navigation and choose
	// when it fails.
	slowSeen bool
	release  chan struct{}
}

// chromeBody is the widget chrome: a mark span showing what the server
// rendered, and a close button so tests can dismiss between opens.
func chromeBody(mark string) string {
	return `<div class="fui-widget fui-pos-center" data-fui-widget="dlg" role="dialog">` +
		`<span id="ctxmark">` + mark + `</span>` +
		`<button type="button" id="closer" data-fui-action="close">Close</button>` +
		`</div>`
}

func (c *chromeCtxServer) snapshot() (principal string, perCtx map[string]int, total int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := map[string]int{}
	for k, v := range c.perCtx {
		cp[k] = v
	}
	return c.principal, cp, c.totalFetch
}

func (c *chromeCtxServer) setPrincipal(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.principal = p
}

// waitSlowArrived blocks until the gated slowfail fetch has reached the
// server — proof it is in flight and its cache entry exists — or fails
// the test at the deadline.
func (c *chromeCtxServer) waitSlowArrived(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		seen := c.slowSeen
		c.mu.Unlock()
		if seen {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("gated slowfail chrome fetch never reached the server")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// releaseSlow lets the gated fetch proceed to its 500. Idempotent.
func (c *chromeCtxServer) releaseSlow() {
	select {
	case <-c.release: // already closed
	default:
		close(c.release)
	}
}

func startChromeCtxServer(t *testing.T, body string) *chromeCtxServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	c := &chromeCtxServer{principal: "alice", perCtx: map[string]int{}, release: make(chan struct{})}
	catalog := `[{"hidden":true,"cfg":{"name":"dlg","position":"center","backdrop":false,` +
		`"closeOnEscape":false,"closeOnClick":false,` +
		`"stylePath":"/core-ui/widget/dlg/style.css","chromePath":"/chrome/dlg"}}]`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
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
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(catalog))
	})
	mux.HandleFunc("/core-ui/widget/dlg/style.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, "")
	})
	mux.HandleFunc("/chrome/dlg", func(w http.ResponseWriter, r *http.Request) {
		ctxVal := r.URL.Query().Get("ctx")
		c.mu.Lock()
		c.perCtx[ctxVal]++
		c.totalFetch++
		first := ctxVal == "slowfail" && !c.slowSeen
		if first {
			c.slowSeen = true
		}
		p := c.principal
		c.mu.Unlock()
		if first {
			// Hold this fetch in flight until the test releases it,
			// then fail it. Everything later for slowfail serves 200.
			<-c.release
			http.Error(w, "chrome exploded", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, chromeBody("ctx="+ctxVal+"|user="+p))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			// The post-sign-out destination's render happens under the new
			// (anonymous) principal — that is what session middleware
			// produces on the very next request after the session dies.
			c.setPrincipal("anon")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "After")
			fmt.Fprint(w, `<h2 id="after-mark">after</h2><button id="open2" data-fui-open="dlg">Open</button><button id="open-ctx" data-fui-open="dlg" data-fui-ctx="slowfail">Open ctx</button>`)
			return
		}
		fmt.Fprintf(w, `<!doctype html><html><head><title>chromectx</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/after"}]</script>
</head><body>
<main id="main-content">
%s
<span id="ready">ready</span>
</main>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, body)
	})
	c.Srv = httptest.NewServer(mux)
	t.Cleanup(c.Srv.Close)
	return c
}

func chromeCtxBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}
	ctx, cancel := context.WithTimeout(browserCtx, 120*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// closeWidget dismisses the open dialog and waits for its removal.
func closeWidget() chromedp.Action {
	return chromedp.Tasks{
		chromedp.Click(`#closer`, chromedp.ByID),
		chromedp.WaitNotPresent(`[data-fui-widget="dlg"]`),
	}
}

// TestWidgetChromeCtx_DistinctPerCtxAndCachedPerCtx pins #321: two open
// triggers with different data-fui-ctx must produce two distinct chromes,
// and re-opening the SAME ctx must be served from the (name, ctx) client
// cache — the server sees exactly one fetch per ctx. A test where both
// triggers share a ctx proves nothing; this one varies it.
func TestWidgetChromeCtx_DistinctPerCtxAndCachedPerCtx(t *testing.T) {
	body := `
<button id="open-a" data-fui-open="dlg" data-fui-ctx="inv-42">A</button>
<button id="open-b" data-fui-open="dlg" data-fui-ctx="inv-99">B</button>`
	c := startChromeCtxServer(t, body)
	ctx := chromeCtxBrowser(t)

	var a1, b, a2 string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(c.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`#open-a`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		chromedp.Text(`#ctxmark`, &a1, chromedp.ByQuery),
		closeWidget(),
		chromedp.Click(`#open-b`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		chromedp.Text(`#ctxmark`, &b, chromedp.ByQuery),
		closeWidget(),
		chromedp.Click(`#open-a`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		chromedp.Text(`#ctxmark`, &a2, chromedp.ByQuery),
		closeWidget(),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if a1 != "ctx=inv-42|user=alice" {
		t.Errorf("first open (inv-42): mark = %q, want ctx=inv-42", a1)
	}
	if b != "ctx=inv-99|user=alice" {
		t.Errorf("open with different ctx (inv-99): mark = %q, want ctx=inv-99 — distinct ctx must not reuse inv-42's cached chrome", b)
	}
	if a2 != "ctx=inv-42|user=alice" {
		t.Errorf("reopen (inv-42): mark = %q, want ctx=inv-42", a2)
	}
	_, perCtx, total := c.snapshot()
	if got := perCtx["inv-42"]; got != 1 {
		t.Errorf("chrome fetches for inv-42 = %d, want 1 (second open must hit the client cache)", got)
	}
	if got := perCtx["inv-99"]; got != 1 {
		t.Errorf("chrome fetches for inv-99 = %d, want 1", got)
	}
	if total != 2 {
		t.Errorf("total chrome fetches = %d, want 2", total)
	}
}

// TestWidgetChromeCacheClearedOnPrincipalChange pins #329: the chrome cache
// lives on window and SPA navigation keeps the document, so a principal
// change that happens without a full page load (sign-in/sign-out via an
// intercepted form or RPC+navigate — the sign-out control's redirect is an
// SPA nav) must not leave the previous principal's chrome cached for the
// next open. The "sign out" is modelled by the destination page's partial
// render happening under a different principal, exactly what session
// middleware produces on the first request after the session dies.
func TestWidgetChromeCacheClearedOnPrincipalChange(t *testing.T) {
	body := `
<button id="open1" data-fui-open="dlg">Open</button>
<a id="nav-out" href="/after">Sign out</a>`
	c := startChromeCtxServer(t, body)
	ctx := chromeCtxBrowser(t)

	var before, after, path string
	step := func(name string, acts ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, acts...); err != nil {
			t.Fatalf("step %s: %v", name, err)
		}
		t.Logf("step %s ok", name)
	}
	step("nav0", chromedp.Navigate(c.Srv.URL+"/"), chromedp.WaitVisible(`#ready`, chromedp.ByID), chromedp.Sleep(200*time.Millisecond))
	step("open1", chromedp.Click(`#open1`, chromedp.ByID), chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery), chromedp.Text(`#ctxmark`, &before, chromedp.ByQuery))
	step("close1", closeWidget())
	step("signout-nav", chromedp.Click(`#nav-out`, chromedp.ByID))
	step("after-visible", chromedp.WaitVisible(`#after-mark`, chromedp.ByID), chromedp.Sleep(300*time.Millisecond), chromedp.Evaluate(`location.pathname`, &path))
	step("open2", chromedp.Click(`#open2`, chromedp.ByID), chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery), chromedp.Text(`#ctxmark`, &after, chromedp.ByQuery))
	step("close2", closeWidget())

	if path != "/after" {
		t.Errorf("SPA nav did not reach /after, at %q", path)
	}
	if before != "ctx=|user=alice" {
		t.Errorf("pre-signout chrome = %q, want user=alice", before)
	}
	if after != "ctx=|user=anon" {
		t.Errorf("post-signout chrome = %q, want user=anon — the cache must not serve the previous principal's chrome across a principal change", after)
	}
	_, _, total := c.snapshot()
	if total != 2 {
		t.Errorf("total chrome fetches = %d, want 2 (the cache must refetch after the principal change)", total)
	}
}

// TestWidgetChromeCtx_CacheCapped pins the bound on the (name, ctx) cache:
// a page with one delete dialog per row produces one cache entry per
// distinct ctx, so the cache is capped (32) with least-recently-used
// eviction. Opening 34 distinct contexts then re-opening the FIRST must
// refetch (it was evicted), not serve a 33-entry-stale cache.
func TestWidgetChromeCtx_CacheCapped(t *testing.T) {
	var body strings.Builder
	for i := range 34 {
		fmt.Fprintf(&body, `<button id="open-%d" data-fui-open="dlg" data-fui-ctx="c%d">%d</button>`+"\n", i, i, i)
	}
	c := startChromeCtxServer(t, body.String())
	ctx := chromeCtxBrowser(t)

	acts := []chromedp.Action{
		chromedp.Navigate(c.Srv.URL + "/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(200 * time.Millisecond),
	}
	for i := range 34 {
		acts = append(acts,
			chromedp.Click(fmt.Sprintf(`#open-%d`, i), chromedp.ByID),
			chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
			closeWidget(),
		)
	}
	// Re-open the first: evicted twice over (c0 dropped at insert 33, c2
	// dropped at the re-open insert), so it must refetch.
	acts = append(acts,
		chromedp.Click(`#open-0`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		closeWidget(),
	)
	if err := chromedp.Run(ctx, acts...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	_, perCtx, total := c.snapshot()
	if got := perCtx["c0"]; got != 2 {
		t.Errorf("chrome fetches for c0 = %d, want 2 (LRU must evict it past the cap and refetch on re-open)", got)
	}
	if got := perCtx["c33"]; got != 1 {
		t.Errorf("chrome fetches for c33 = %d, want 1 (recent entries must stay cached)", got)
	}
	if total != 35 {
		t.Errorf("total chrome fetches = %d, want 35 (34 first opens + 1 evicted re-open)", total)
	}
}

// TestWidgetChromeCtx_FailedFetchAfterNavKeepsNewCache pins the error
// path of the chrome cache: a fetch that fails AFTER a gofastr:navigate
// swapped in a fresh cache object must delete its entry from the cache
// it started against, not from the current one. Deleting from the new
// cache evicts the new page's entry, so the next open refetches — one
// extra request per failure (measured as 3 fetches where 2 suffice).
func TestWidgetChromeCtx_FailedFetchAfterNavKeepsNewCache(t *testing.T) {
	body := `
<button id="open-slow" data-fui-open="dlg" data-fui-ctx="slowfail">Slow</button>
<a id="nav-out" href="/after">Go</a>`
	c := startChromeCtxServer(t, body)
	ctx := chromeCtxBrowser(t)

	var mark string
	step := func(name string, acts ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, acts...); err != nil {
			t.Fatalf("step %s: %v", name, err)
		}
		t.Logf("step %s ok", name)
	}

	step("nav0", chromedp.Navigate(c.Srv.URL+"/"), chromedp.WaitVisible(`#ready`, chromedp.ByID), chromedp.Sleep(200*time.Millisecond))
	// Open with the gated ctx: the chrome fetch blocks server-side, so
	// it is provably in flight (and cached) before the navigation runs.
	step("open-slow", chromedp.Click(`#open-slow`, chromedp.ByID))
	c.waitSlowArrived(t)
	// SPA-navigate: gofastr:navigate replaces the cache object.
	step("nav-after", chromedp.Click(`#nav-out`, chromedp.ByID),
		chromedp.WaitVisible(`#after-mark`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond))
	// Open the same ctx on the new page: fresh cache, new fetch, mounts.
	step("open-after", chromedp.Click(`#open-ctx`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		chromedp.Text(`#ctxmark`, &mark, chromedp.ByQuery))
	// Now let the PRE-navigation fetch fail. Its error path must evict
	// only the entry in the cache it started against.
	c.releaseSlow()
	step("settle", chromedp.Sleep(400*time.Millisecond))
	step("close-after", closeWidget())
	// Re-open on the new page: served from the new cache (2 fetches
	// total), not refetched because the failed old fetch deleted the
	// new cache's entry (3).
	step("reopen-after", chromedp.Click(`#open-ctx`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery))

	if mark != "ctx=slowfail|user=anon" {
		t.Errorf("post-nav chrome mark = %q, want ctx=slowfail|user=anon", mark)
	}
	_, perCtx, total := c.snapshot()
	if got := perCtx["slowfail"]; got != 2 {
		t.Errorf("chrome fetches for slowfail = %d, want 2 (pre-nav failure + post-nav success; the re-open must hit the new cache, not refetch because the failed pre-nav fetch deleted from it)", got)
	}
	if total != 2 {
		t.Errorf("total chrome fetches = %d, want 2", total)
	}
}

// TestWidgetChromeCtx_LRURecency pins the RECENCY half of the LRU, which
// the cap test above does not: a cache HIT must move its entry to
// most-recently-used, so a later overflow evicts a colder entry instead.
// Without the hit-refresh the cache is plain FIFO and this test fails.
func TestWidgetChromeCtx_LRURecency(t *testing.T) {
	var body strings.Builder
	// c0..c32: 33 distinct ctx buttons. The script fills the cache with
	// c0..c31 (32 entries, no eviction), refreshes c0 by re-opening it,
	// then inserts c32 (33 > 32 → evicts the least-recently-used key).
	// With the refresh, the LRU is c1; without it, c0.
	for i := range 33 {
		fmt.Fprintf(&body, `<button id="open-%d" data-fui-open="dlg" data-fui-ctx="c%d">%d</button>`+"\n", i, i, i)
	}
	c := startChromeCtxServer(t, body.String())
	ctx := chromeCtxBrowser(t)

	click := func(id string) chromedp.Action {
		return chromedp.Tasks{
			chromedp.Click(id, chromedp.ByID),
			chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
			closeWidget(),
		}
	}
	acts := []chromedp.Action{
		chromedp.Navigate(c.Srv.URL + "/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(200 * time.Millisecond),
	}
	for i := range 32 { // fill: c0..c31, exactly the cap, no eviction
		acts = append(acts, click(fmt.Sprintf(`#open-%d`, i)))
	}
	acts = append(acts,
		click(`#open-0`),  // HIT: refreshes c0 to most-recent
		click(`#open-32`), // overflow insert: must evict c1 (LRU), not c0
		click(`#open-0`),  // c0 survived the eviction → served from cache
		click(`#open-1`),  // c1 was the eviction victim → refetches
	)
	if err := chromedp.Run(ctx, acts...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	_, perCtx, total := c.snapshot()
	if got := perCtx["c0"]; got != 1 {
		t.Errorf("chrome fetches for c0 = %d, want 1 (a cache hit must refresh recency: c0 re-opened BEFORE the overflow insert must survive it)", got)
	}
	if got := perCtx["c1"]; got != 2 {
		t.Errorf("chrome fetches for c1 = %d, want 2 (c1, not the refreshed c0, is the least-recently-used entry the overflow must evict)", got)
	}
	if got := perCtx["c32"]; got != 1 {
		t.Errorf("chrome fetches for c32 = %d, want 1 (the newest entry stays cached)", got)
	}
	if total != 34 {
		t.Errorf("total chrome fetches = %d, want 34 (32 fills + c32 insert + c1 evicted re-open; both c0 re-opens hit cache)", total)
	}
}

// TestWidgetChromeCtx_FailedFetchSurfacesToast pins #339: when a
// ctx-carrying trigger drops SSR-inlined chrome (its render is ctx-less,
// #321) and the replacement fetch then fails, the click must not end in
// silence. Before the fix the dropped node was already gone, the error was
// swallowed, and the click did nothing at all — no dialog, no message. The
// failure must be observable the way a dead form RPC's is: a toast appears
// (rendered by the toasts module, which _toastOrFallback loads on demand).
// Two neighbours are pinned in the same run: the dropped SSR node is NOT
// re-inserted (that would re-show ctx-less chrome, the #321 bug), and a
// SUCCESSFUL ctx open must not toast.
func TestWidgetChromeCtx_FailedFetchSurfacesToast(t *testing.T) {
	body := `
<div data-fui-widget="dlg" hidden><span>ssr chrome</span></div>
<button id="open-ok" data-fui-open="dlg" data-fui-ctx="okctx">OK</button>
<button id="open-slow" data-fui-open="dlg" data-fui-ctx="slowfail">Slow</button>`
	c := startChromeCtxServer(t, body)
	ctx := chromeCtxBrowser(t)

	var okMark, toastTitle string
	var happyToasts, dlgNodes int
	step := func(name string, acts ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, acts...); err != nil {
			t.Fatalf("step %s: %v", name, err)
		}
		t.Logf("step %s ok", name)
	}

	step("nav", chromedp.Navigate(c.Srv.URL+"/"), chromedp.WaitVisible(`#ready`, chromedp.ByID), chromedp.Sleep(200*time.Millisecond))
	// Success first, while no toast could exist yet: a healthy ctx open
	// drops the SSR node, fetches fresh chrome, mounts — and stays quiet.
	step("open-ok", chromedp.Click(`#open-ok`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByQuery),
		chromedp.Text(`#ctxmark`, &okMark, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-toast-id]').length`, &happyToasts))
	step("close-ok", closeWidget())
	// Failure: the SSR node is gone (open-ok dropped it), so this open is
	// a pure chrome fetch — the gated one, released into a 500.
	step("open-slow", chromedp.Click(`#open-slow`, chromedp.ByID))
	c.waitSlowArrived(t)
	c.releaseSlow()
	step("toast-visible", chromedp.WaitVisible(`[data-fui-toast-id]`, chromedp.ByQuery),
		chromedp.Text(`.ui-notification__title`, &toastTitle, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-widget="dlg"]').length`, &dlgNodes))

	if okMark != "ctx=okctx|user=alice" {
		t.Errorf("healthy open mark = %q, want ctx=okctx|user=alice", okMark)
	}
	if happyToasts != 0 {
		t.Errorf("successful ctx open produced %d toast(s) — the happy path must stay quiet", happyToasts)
	}
	if dlgNodes != 0 {
		t.Errorf("%d [data-fui-widget] node(s) after the failed open — the dropped SSR chrome must not be re-inserted (#321)", dlgNodes)
	}
	if toastTitle != "Could not open that panel." {
		t.Errorf("toast title = %q, want the failed-open message", toastTitle)
	}
	_, perCtx, total := c.snapshot()
	if got := perCtx["slowfail"]; got != 1 {
		t.Errorf("chrome fetches for slowfail = %d, want 1", got)
	}
	if got := perCtx["okctx"]; got != 1 {
		t.Errorf("chrome fetches for okctx = %d, want 1", got)
	}
	if total != 2 {
		t.Errorf("total chrome fetches = %d, want 2", total)
	}
}
