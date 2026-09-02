package runtime_test

// End-to-end proof for document-lifetime scripts (#372): a script the
// host registers with a page scope installs capabilities INTO the
// document, and removing the tag does not uninstall them — WebMCP's
// navigator.modelContext tools are the case that made this a security
// boundary. The SPA router must therefore refuse to soft-navigate
// across a docScripts change and let the browser load a real document,
// while same-set navigation stays a partial swap.
//
// Two fixtures live here:
//
//   - a hand-rolled HTML site (like nested_layout_chain_e2e_test.go)
//     pinning the generic runtime mechanism: same-scope partial, edge
//     hard-load, back/forward restoration, navigate() hard-load;
//   - a real uihost + webmcp mount driving navigator.modelContext
//     across the boundary in a Chromium launched with
//     --enable-blink-features=WebMCP.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

// one shared browser for the whole file: an allocator per test would
// boot Chrome four times for what is one surface.
var boundaryAlloc struct {
	once sync.Once
	ctx  context.Context
}

func boundaryBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	boundaryAlloc.once.Do(func() {
		execPath, ok := browserpath.Find()
		if !ok {
			return
		}
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(execPath),
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			// WebMCP is a flagged experimental feature; this exposes
			// navigator.modelContext on Chromium 146+ (same flag as
			// webmcp/browser_test.go).
			chromedp.Flag("enable-blink-features", "WebMCP"),
			chromedp.WSURLReadTimeout(90*time.Second),
		)
		allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
		boundaryAlloc.ctx = allocCtx
		// The browser outlives every test; canceling would kill later
		// ones, so the cancel is deliberately dropped (the process dies
		// with the test binary).
		_ = cancel
	})
	if boundaryAlloc.ctx == nil {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	ctx, cancel := chromedp.NewContext(boundaryAlloc.ctx)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)
	return tctx
}

// ---------------------------------------------------------------------------
// Generic mechanism: hand-rolled fixture
// ---------------------------------------------------------------------------

// boundaryRoutes: /a and /b carry one document script; /c carries none.
const boundaryRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/a","docScripts":["/cap.js"]},` +
	`{"path":"/b","docScripts":["/cap.js"]},` +
	`{"path":"/c"}` +
	`]</script>`

// capJS is the fake document capability: a global that only exists in
// documents whose page scope emitted the script.
const capJS = `window.__fuiCap = (window.__fuiCap || 0) + 1;`

type boundarySite struct {
	srv *httptest.Server

	mu       sync.Mutex
	partials map[string]int // path → X-Gofastr-Navigate requests
}

func (s *boundarySite) partialCount(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.partials[path]
}

func (s *boundarySite) record(r *http.Request) {
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		s.mu.Lock()
		s.partials[r.URL.Path]++
		s.mu.Unlock()
	}
}

func (s *boundarySite) page(docScript bool, inner string) string {
	cap := ""
	if docScript {
		cap = `<script src="/cap.js" data-fui-doc></script>`
	}
	return `<!doctype html><html><head><title>t</title>` + boundaryRoutes + `</head><body>` +
		`<main role="main" tabindex="-1">` + inner + `</main>` + cap +
		`<script src="/__gofastr/runtime.js"></script></body></html>`
}

func newBoundarySite(t *testing.T) *boundarySite {
	t.Helper()
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &boundarySite{partials: map[string]int{}}

	screen := func(id, label string, links string) string {
		return `<h1 id="` + id + `">` + label + `</h1>` + links
	}
	const navAB = `<a id="to-b" href="/b">to b</a><a id="to-c" href="/c">to c</a>`
	const navC = `<a id="to-a" href="/a">to a</a>`

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/cap.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(capJS))
	})
	partial := func(w http.ResponseWriter, title, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", title)
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "A", screen("screen-a", "A", navAB))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, s.page(true, screen("screen-a", "A", navAB)))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			partial(w, "B", screen("screen-b", "B", navAB))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, s.page(true, screen("screen-b", "B", navAB)))
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, s.page(false, screen("screen-c", "C", navC)))
	})

	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// sameDocTag marks the live JS context; surviving the navigation means
// the document was reused (partial), undefined means a real load.
const sameDocTag = `window.__sameDoc = true`

func evalBool(ctx context.Context, expr string) (bool, error) {
	var ok bool
	err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok))
	return ok, err
}

// waitFor polls an evaluable boolean expression until true.
func waitFor(t *testing.T, ctx context.Context, expr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := evalBool(ctx, expr)
		if err != nil {
			t.Fatalf("evaluate %s: %v", expr, err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", expr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForURL(t *testing.T, ctx context.Context, suffix string) {
	t.Helper()
	waitFor(t, ctx, `location.pathname === "`+suffix+`"`, 10*time.Second)
}

// Same docScripts set on both sides: the click stays a partial swap —
// same document, partial request on the wire, capability preserved.
func TestSameScopeNavigationStaysPartial(t *testing.T) {
	site := newBoundarySite(t)
	ctx := boundaryBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.WaitVisible("#screen-a", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /a: %v", err)
	}
	waitFor(t, ctx, `typeof window.__fuiCap === 'number'`, 10*time.Second)

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(sameDocTag+`; true`, nil),
		chromedp.Click("#to-b", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click to /b: %v", err)
	}
	waitForURL(t, ctx, "/b")
	waitFor(t, ctx, `!!document.getElementById('screen-b')`, 10*time.Second)

	sameDoc, err := evalBool(ctx, `window.__sameDoc === true`)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDoc {
		t.Error("same-scope navigation loaded a new document; the swap must stay partial")
	}
	if n := site.partialCount("/b"); n != 1 {
		t.Errorf("/b served %d partial requests, want 1", n)
	}
	if ok, _ := evalBool(ctx, `!!document.querySelector('script[data-fui-doc]')`); !ok {
		t.Error("document script vanished across a same-scope navigation")
	}
}

// Crossing the scope edge (/a → /c): a real document load. The new
// document has no capability script, and no partial request was made.
func TestScopeEdgeLoadsNewDocument(t *testing.T) {
	site := newBoundarySite(t)
	ctx := boundaryBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.WaitVisible("#screen-a", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /a: %v", err)
	}
	waitFor(t, ctx, `typeof window.__fuiCap === 'number'`, 10*time.Second)
	if err := chromedp.Run(ctx, chromedp.Evaluate(sameDocTag+`; true`, nil)); err != nil {
		t.Fatal(err)
	}

	if err := chromedp.Run(ctx, chromedp.Click("#to-c", chromedp.ByQuery)); err != nil {
		t.Fatalf("click to /c: %v", err)
	}
	waitForURL(t, ctx, "/c")
	waitFor(t, ctx, `!!document.getElementById('screen-c')`, 10*time.Second)

	if sameDoc, _ := evalBool(ctx, `window.__sameDoc === true`); sameDoc {
		t.Error("scope edge reused the document; leaving a docScripts scope must be a real navigation")
	}
	if ok, _ := evalBool(ctx, `!!document.querySelector('script[data-fui-doc]')`); ok {
		t.Error("new document still carries the origin's document script")
	}
	if typeof, _ := evalBool(ctx, `typeof window.__fuiCap !== 'undefined'`); typeof {
		t.Error("capability global survived into the out-of-scope document")
	}
	if n := site.partialCount("/c"); n != 0 {
		t.Errorf("/c was served %d partial requests, want 0 (hard navigation)", n)
	}
}

// Back/forward across the edge restores exactly the destination's
// capabilities: back to the scoped page re-runs its script, forward to
// the unscoped page has none.
func TestBackForwardRestoresDocCapabilities(t *testing.T) {
	site := newBoundarySite(t)
	ctx := boundaryBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.WaitVisible("#screen-a", chromedp.ByQuery),
		chromedp.Click("#to-c", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate + click: %v", err)
	}
	waitForURL(t, ctx, "/c")
	waitFor(t, ctx, `!!document.getElementById('screen-c')`, 10*time.Second)

	// Back: the scoped page's document again, capability re-installed.
	// history.back() via Evaluate, not chromedp.NavigateBack: a bfcache
	// restore fires no load event, which is exactly what NavigateBack
	// waits for (the repo's other back/forward tests do the same).
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatalf("back: %v", err)
	}
	waitForURL(t, ctx, "/a")
	waitFor(t, ctx, `typeof window.__fuiCap === 'number'`, 10*time.Second)
	if ok, _ := evalBool(ctx, `!!document.querySelector('script[data-fui-doc]')`); !ok {
		t.Error("back to the scoped page lost its document script")
	}

	// Forward: out of scope again, no capability.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.forward(); true`, nil)); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if ok, _ := evalBool(ctx, `typeof window.__fuiCap !== 'undefined'`); ok {
		t.Error("forward to the unscoped page still carries the capability")
	}
}

// The programmatic choke point hard-loads too: navigate() across the
// edge must not pushState + swap.
func TestNavigateAPIHardLoadsAcrossScope(t *testing.T) {
	site := newBoundarySite(t)
	ctx := boundaryBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/a"),
		chromedp.WaitVisible("#screen-a", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /a: %v", err)
	}
	waitFor(t, ctx, `!!window.__gofastr && typeof window.__gofastr.navigate === 'function'`, 10*time.Second)
	if err := chromedp.Run(ctx, chromedp.Evaluate(sameDocTag+`; true`, nil)); err != nil {
		t.Fatal(err)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/c'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitForURL(t, ctx, "/c")
	waitFor(t, ctx, `!!document.getElementById('screen-c')`, 10*time.Second)
	if sameDoc, _ := evalBool(ctx, `window.__sameDoc === true`); sameDoc {
		t.Error("navigate() across the scope edge reused the document")
	}
	if n := site.partialCount("/c"); n != 0 {
		t.Errorf("/c was served %d partial requests via navigate(), want 0", n)
	}
}

// ---------------------------------------------------------------------------
// WebMCP: getTools() across the boundary on a real uihost mount
// ---------------------------------------------------------------------------

// boundaryScreen is a uihost screen whose content is one link.
type boundaryScreen struct {
	linkID, href, text string
}

func (s boundaryScreen) Render() render.HTML {
	return html.Link(html.LinkConfig{ID: s.linkID, Href: s.href, Text: s.text})
}

func (boundaryScreen) SetParams(map[string]string) {}

// newWebmcpBoundarySite mounts a real uihost app with a support scope:
// /support and /support/deep carry the WebMCP bridge document-scoped,
// /public carries nothing. One tool ("support_ping") is declared.
func newWebmcpBoundarySite(t *testing.T) *httptest.Server {
	t.Helper()
	application := app.NewApp("webmcp boundary")
	application.RegisterScreen(app.NewScreen("/support", boundaryScreen{"to-public", "/public", "public"}), nil)
	application.RegisterScreen(app.NewScreen("/support/deep", boundaryScreen{"to-public", "/public", "public"}), nil)
	application.RegisterScreen(app.NewScreen("/public", boundaryScreen{"to-support", "/support", "support"}), nil)

	ds := uihost.New(application)
	rt := router.New()
	ds.Mount(rt)

	h := webmcp.New()
	if err := h.Register(webmcp.Tool{
		Name:        "support_ping",
		Description: "Support-scoped probe.",
		Method:      http.MethodGet,
		Path:        "/api/ping",
	}); err != nil {
		t.Fatal(err)
	}
	rt.Get("/api/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	if _, err := h.Mount(rt, ds, webmcp.WithDocumentScope(func(path string) bool {
		return strings.HasPrefix(path, "/support")
	})); err != nil {
		t.Fatalf("webmcp mount: %v", err)
	}

	srv := httptest.NewServer(rt)
	t.Cleanup(srv.Close)
	return srv
}

func toolNamesExpr() string {
	return `(document.modelContext || navigator.modelContext).getTools().then(ts => ts.map(t => t.name).sort().join(","))`
}

func waitForToolNames(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	var names string
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(toolNamesExpr(), &names,
			func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
				return p.WithAwaitPromise(true)
			})); err != nil {
			t.Fatalf("getTools: %v", err)
		}
		if names == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("getTools() = %q, want %q", names, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The #372 acceptance flow on the real mount: tools registered on the
// support page survive NOTHING but their own scope. A same-scope
// partial navigation keeps them (same document), leaving the scope is
// a real navigation that leaves them behind, and Back restores them
// for the destination document only.
func TestWebMCPToolsAcrossDocumentBoundary(t *testing.T) {
	srv := newWebmcpBoundarySite(t)
	ctx := boundaryBrowserCtx(t)

	// Page A: tools registered.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/support"),
		chromedp.WaitVisible("#to-public", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /support: %v", err)
	}
	waitForToolNames(t, ctx, "support_ping")
	if err := chromedp.Run(ctx, chromedp.Evaluate(sameDocTag+`; true`, nil)); err != nil {
		t.Fatal(err)
	}

	// Same-scope partial navigation (/support → /support/deep by URL
	// bar push, as an island link would): same document, tools intact.
	// Reached through the runtime's own navigate() so no full load is
	// involved; the scope sets are equal, so this must be a soft nav.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/support/deep'); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitForURL(t, ctx, "/support/deep")
	waitFor(t, ctx, `window.__sameDoc === true`, 10*time.Second)
	waitForToolNames(t, ctx, "support_ping")

	// Leaving the scope through an ordinary link: real navigation, the
	// public document starts with zero tools.
	if err := chromedp.Run(ctx, chromedp.Click("#to-public", chromedp.ByQuery)); err != nil {
		t.Fatalf("click to /public: %v", err)
	}
	waitForURL(t, ctx, "/public")
	waitFor(t, ctx, `!!document.getElementById('to-support')`, 10*time.Second)
	waitForToolNames(t, ctx, "")

	// Back: the support document again — its tools, and only its tools.
	// (Evaluate, not NavigateBack: a bfcache restore fires no load
	// event for NavigateBack to wait on.)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatalf("back: %v", err)
	}
	waitForURL(t, ctx, "/support/deep")
	waitFor(t, ctx, `!!document.getElementById('to-public')`, 10*time.Second)
	waitForToolNames(t, ctx, "support_ping")
}
