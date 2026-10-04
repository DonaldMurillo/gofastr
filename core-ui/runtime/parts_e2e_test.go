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

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// The parallel-parts e2e rig (spike/layout-parts): a hand-rolled site
// whose shell carries two DEFERRED outlets, with channel-gated page and
// part responses so ordering is deterministic (no sleeps for ordering).
//
//	/      the shell document (OLD-* outlet content)
//	/a     deferred: aside + rail  (preload:hover for the prefetch test)
//	/b     deferred: aside
//
// The page answers under X-Gofastr-Defer with an envelope whose
// deferred fills carry SERVER-* loading content; each part answers one
// fill template. Gates and responders are per-test.

type partsSite struct {
	srv *httptest.Server

	mu       sync.Mutex
	pageReqs map[string]int
	partReqs map[string]int
	docLoads map[string]int

	pageGate map[string]chan struct{}    // gates one page answer, by path
	partGate map[string]chan struct{}    // gates one part ("<path>|<addr>")
	partResp map[string]http.HandlerFunc // overrides one part's answer
	pageResp map[string]http.HandlerFunc // overrides one page answer
}

func (s *partsSite) bump(kind, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case "page":
		s.pageReqs[key]++
	case "part":
		s.partReqs[key]++
	default:
		s.docLoads[key]++
	}
}

func (s *partsSite) count(kind, key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case "page":
		return s.pageReqs[key]
	case "part":
		return s.partReqs[key]
	default:
		return s.docLoads[key]
	}
}

func (s *partsSite) totalParts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.partReqs {
		n += v
	}
	return n
}

// newPartsSite builds the rig. afterMs is the loading templates' After
// (0 parks immediately; a huge value never shows). /a declares
// preload:hover: the preload module only fires on a real pointerover,
// and every other test clicks via evaluate, so it stays dormant.
// withVT, when true, declares a data-cui-vt-kinds vocabulary on the
// document: view transitions are opt-in (the corrected Opt-in table),
// so the one test that asserts transition behavior must opt the rig
// in; the default rig stays plain and covers the module-absent
// configuration.
func newPartsSite(t *testing.T, afterMs int, withVT ...bool) *partsSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &partsSite{
		pageReqs: map[string]int{},
		partReqs: map[string]int{},
		docLoads: map[string]int{},
		pageGate: map[string]chan struct{}{},
		partGate: map[string]chan struct{}{},
		partResp: map[string]http.HandlerFunc{},
		pageResp: map[string]http.HandlerFunc{},
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},` +
		`{"path":"/a","layouts":["l:site"],"deferred":["l:site#aside","l:site#rail"],"preload":"hover"},` +
		`{"path":"/b","layouts":["l:site"],"deferred":["l:site#aside"]},` +
		`{"path":"/c","layouts":["l:site"]}` +
		`]</script>`
	vtAttr := ""
	if len(withVT) > 0 && withVT[0] {
		vtAttr = ` data-cui-vt-kinds="fade"`
	}
	shell := func(inner string) string {
		return `<!doctype html><html` + vtAttr + `><head><title>parts</title>` + routes +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			// The nav links live in the SHELL (outside the swapped slot),
			// so tests can click them after any navigation.
			`<nav><a id="goA" href="/a">A</a> <a id="goB" href="/b">B</a> <a id="goC" href="/c">C</a></nav>` +
			`<div data-cui-outlet="l:site#aside" id="aside">OLD-ASIDE<button id="in-aside">b</button></div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`<div data-cui-outlet="l:site#rail" id="rail">OLD-RAIL</div>` +
			`</div>` +
			`<template data-cui-loading="l:site#aside" data-cui-after="` + fmt.Sprint(afterMs) + `" data-cui-min="0"><span>CLIENT-SKELETON</span></template>` +
			`<template data-cui-loading="l:site#rail" data-cui-after="` + fmt.Sprint(afterMs) + `" data-cui-min="0"><span>CLIENT-RAIL-SKELETON</span></template>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	home := shell(`HOME`)

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(r.URL.Path[len("/__gofastr/runtime/"):], ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})

	writeEnvelope := func(w http.ResponseWriter, primary string, fills ...string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "Page")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		w.Header().Set("X-Gofastr-Envelope", "2")
		var b strings.Builder
		fmt.Fprintf(&b, `<template data-cui-fill="l:site">%s</template>`, primary)
		for _, f := range fills {
			fmt.Fprintf(&b, `<template data-cui-fill="l:site#%s">SERVER-%s-SKELETON</template>`, f, f)
		}
		fmt.Fprint(w, b.String())
	}
	partBody := func(addr, body string) string {
		return fmt.Sprintf(`<template data-cui-fill="l:site#%s">%s</template>`, addr, body)
	}

	pageHandler := func(path, primary string, fills ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if addr := r.Header.Get("X-Gofastr-Part"); addr != "" {
				addr = strings.TrimPrefix(addr, "l:site#")
				s.bump("part", path+"|"+addr)
				s.mu.Lock()
				g := s.partGate[path+"|"+addr]
				resp := s.partResp[path+"|"+addr]
				s.mu.Unlock()
				if g != nil {
					<-g
				}
				if resp != nil {
					resp(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, partBody(addr, "PART-"+strings.ToUpper(strings.TrimPrefix(path, "/"))+"-"+strings.ToUpper(addr)))
				return
			}
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				s.bump("page", path)
				s.mu.Lock()
				g := s.pageGate[path]
				resp := s.pageResp[path]
				s.mu.Unlock()
				if g != nil {
					<-g
				}
				if resp != nil {
					resp(w, r)
					return
				}
				writeEnvelope(w, primary, fills...)
				return
			}
			s.bump("doc", path)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, shell(`<span id="doc-`+strings.TrimPrefix(path, "/")+`">DOC</span>`))
		}
	}
	mux.HandleFunc("/a", pageHandler("/a", `A-PRIMARY`, "aside", "rail"))
	mux.HandleFunc("/c", pageHandler("/c", `C-PRIMARY`))
	mux.HandleFunc("/b", pageHandler("/b", `B-PRIMARY`, "aside"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			s.bump("page", "/")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "Home")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `<template data-cui-fill="l:site">HOME-PARTIAL</template>`)
			return
		}
		s.bump("doc", "/")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, home)
	})

	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// gatePart installs a release gate for one part and returns its
// release func. The gate is also released at test cleanup (a t.Fatal
// between arming and releasing must not wedge the httptest server's
// Close on a blocked handler).
func (s *partsSite) gatePart(t *testing.T, key string) func() {
	t.Helper()
	s.mu.Lock()
	g := make(chan struct{})
	s.partGate[key] = g
	s.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(g) }) }
	t.Cleanup(release)
	return release
}

func (s *partsSite) setPartResp(key string, fn http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partResp[key] = fn
}

func (s *partsSite) setPageResp(key string, fn http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageResp[key] = fn
}

func (s *partsSite) gatePage(t *testing.T, path string) func() {
	t.Helper()
	s.mu.Lock()
	g := make(chan struct{})
	s.pageGate[path] = g
	s.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(g) }) }
	t.Cleanup(release)
	return release
}

func readText(t *testing.T, ctx context.Context, sel string) string {
	t.Helper()
	var out string
	expr := `(() => { const el = document.querySelector('` + sel + `'); ` +
		`return el ? el.textContent.trim() : '!missing'; })()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out)); err != nil {
		t.Fatalf("read %s: %v", sel, err)
	}
	return out
}

func waitContains(t *testing.T, ctx context.Context, sel, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readText(t, ctx, sel), want) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting %s ~ %q (last %q)", sel, want, readText(t, ctx, sel))
}

func jsClick(t *testing.T, ctx context.Context, sel string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('`+sel+`').click()`, nil)); err != nil {
		t.Fatalf("click %s: %v", sel, err)
	}
}

// netLog collects the CDP network events for part-shaped requests
// (those carrying X-Gofastr-Part).
type netLog struct {
	mu       sync.Mutex
	requests map[string]*netReq // CDP request ID → record
}

type netReq struct {
	url      string
	partAddr string
	priority string
	finished bool
	failed   string
}

func newNetLog(ctx context.Context, t *testing.T) *netLog {
	t.Helper()
	nl := &netLog{requests: map[string]*netReq{}}
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			part, _ := e.Request.Headers["X-Gofastr-Part"].(string)
			if part == "" {
				return
			}
			// The fetch priority hint rides Chrome's scheduler priority,
			// not a request header: fetch(priority:'low') maps to a
			// Low/VeryLow resource priority, the page's 'high' to
			// High/VeryHigh.
			nl.mu.Lock()
			nl.requests[string(e.RequestID)] = &netReq{url: e.Request.URL, partAddr: strings.TrimPrefix(part, "l:site#"), priority: string(e.Request.InitialPriority)}
			nl.mu.Unlock()
		case *network.EventLoadingFinished:
			nl.mu.Lock()
			if r := nl.requests[string(e.RequestID)]; r != nil {
				r.finished = true
			}
			nl.mu.Unlock()
		case *network.EventLoadingFailed:
			nl.mu.Lock()
			if r := nl.requests[string(e.RequestID)]; r != nil {
				r.failed = e.ErrorText
			}
			nl.mu.Unlock()
		}
	})
	return nl
}

func (nl *netLog) snapshot() map[string]*netReq {
	nl.mu.Lock()
	defer nl.mu.Unlock()
	out := make(map[string]*netReq, len(nl.requests))
	for k, v := range nl.requests {
		out[k] = v
	}
	return out
}

// TestPartsAreSeparateRequestsWithBodies: one CDP network request per
// deferred outlet, each finishing with loadingFinished and a body
// readable through Network.getResponseBody — the DevTools contract
// that killed streaming (Chrome keeps no body for a fetch() read
// through a stream reader and reports net::ERR_ABORTED).
func TestPartsAreSeparateRequestsWithBodies(t *testing.T) {
	s := newPartsSite(t, 10000)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	nl := newNetLog(ctx, t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		network.Enable(),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")

	reqs := nl.snapshot()
	if len(reqs) != 2 {
		t.Fatalf("CDP saw %d part requests, want exactly 2 (one per deferred outlet)", len(reqs))
	}
	var bodies []string
	for id, r := range reqs {
		if !r.finished {
			t.Errorf("part %s did not reach loadingFinished (failed=%q)", r.partAddr, r.failed)
		}
		if r.failed != "" {
			t.Errorf("part %s loadingFailed: %s (a text() read must finish cleanly)", r.partAddr, r.failed)
			continue
		}
		if strings.Contains(r.priority, "High") {
			t.Errorf("part %s resource priority = %q; the parts must not contend with the page's High", r.partAddr, r.priority)
		}
		var body string
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			raw, err := network.GetResponseBody(network.RequestID(id)).Do(c)
			if err != nil {
				return err
			}
			body = string(raw)
			return nil
		})); err != nil {
			t.Errorf("part %s: Network.getResponseBody: %v (the DevTools body must survive)", r.partAddr, err)
			continue
		}
		if !strings.Contains(body, `data-cui-fill="l:site#`+r.partAddr+`"`) {
			t.Errorf("part %s body = %q, want its fill template", r.partAddr, body)
		}
		bodies = append(bodies, r.partAddr+":"+body)
	}
	if len(bodies) != 2 {
		t.Fatalf("read %d/2 part bodies through CDP", len(bodies))
	}
}

// TestPartLandsBeforePageHeldUntilCommit: a fast part landing while
// the slow page is still in flight waits in the buffer — NO DOM change
// before the commit — then applies with the commit.
func TestPartLandsBeforePageHeldUntilCommit(t *testing.T) {
	s := newPartsSite(t, 10000)
	pageGate := s.gatePage(t, "/a")

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	// The parts answer instantly; the page is gated. Wait until both
	// parts have been served, then assert the DOM never moved.
	deadline := time.Now().Add(10 * time.Second)
	for s.totalParts() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.totalParts() != 2 {
		t.Fatalf("parts served = %d, want 2", s.totalParts())
	}
	time.Sleep(150 * time.Millisecond) // let any eager (buggy) apply show
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-A") || strings.Contains(got, "SKELETON") {
		t.Errorf("aside before the commit = %q, want the untouched OLD content (a part that lands early must wait in the buffer)", got)
	}
	if got := readText(t, ctx, `#rail`); strings.Contains(got, "PART-A") || strings.Contains(got, "SKELETON") {
		t.Errorf("rail before the commit = %q, want the untouched OLD content", got)
	}
	pageGate()
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
}

// TestSupersededNavAbortsParts: a newer navigation aborts the older
// one's part requests — CDP reports loadingFailed with net::ERR_ABORTED
// (the "(canceled)" DevTools row), which is then TRUE.
func TestSupersededNavAbortsParts(t *testing.T) {
	s := newPartsSite(t, 10000)
	relAside := s.gatePart(t, "/a|aside")
	relRail := s.gatePart(t, "/a|rail")
	pageA := s.gatePage(t, "/a")

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	nl := newNetLog(ctx, t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		network.Enable(),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	time.Sleep(200 * time.Millisecond) // both /a parts + page in flight, gated
	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")

	canceledOf := func() int {
		n := 0
		for _, r := range nl.snapshot() {
			if strings.Contains(r.url, "/a") && strings.Contains(r.failed, "ERR_ABORTED") {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && canceledOf() < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := canceledOf(); n < 2 {
		t.Errorf("superseded /a parts canceled = %d, want 2 (CDP loadingFailed net::ERR_ABORTED)", n)
	}
	// Release the gates so the server handlers return; the client is
	// long gone.
	relAside()
	relRail()
	pageA()
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-A") {
		t.Errorf("aside after supersede = %q; the aborted part leaked", got)
	}
}

// TestPageRedirectDropsParts: a fast part beside a slow page answer
// that REDIRECTS — the buffered part must never land, its request is
// canceled, and the destination page owns the region.
func TestPageRedirectDropsParts(t *testing.T) {
	s := newPartsSite(t, 10000)
	s.setPageResp("/a", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gofastr-Location", "/b")
		w.WriteHeader(http.StatusOK)
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-B-ASIDE")
	time.Sleep(400 * time.Millisecond) // let the /a part land if it wrongly could
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-A") {
		t.Errorf("aside after a redirecting page = %q; the discarded /a part leaked into the destination", got)
	}
	if n := s.count("part", "/a|aside"); n == 0 {
		t.Error("the /a part was never requested; the click must launch it beside the page")
	}
}

// TestPartFailureContained: a part answering 200 with the region's
// fallback applies it to ITS region only; the page stands, no toast.
func TestPartFailureContained(t *testing.T) {
	s := newPartsSite(t, 10000)
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<template data-cui-fill="l:site#aside">CONTAINED-FALLBACK</template>`)
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#aside`, "CONTAINED-FALLBACK")
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
	var toast bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('.cui-nav-toast.is-visible')`, &toast)); err != nil {
		t.Fatal(err)
	}
	if toast {
		t.Error("a contained part failure must not show the failure toast")
	}
}

// TestPartLoadingRestoredOnAbort: each part owns its region's loading
// content; when the navigation is superseded before the commit, the
// parked nodes come back EXACTLY (same nodes — the button inside the
// aside keeps its identity).
func TestPartLoadingRestoredOnAbort(t *testing.T) {
	s := newPartsSite(t, 0) // After 0: the loading parks immediately
	relAside := s.gatePart(t, "/a|aside")
	relRail := s.gatePart(t, "/a|rail")
	s.gatePage(t, "/a") // the /a page never lands; /b flows free

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var btnBefore string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__btn = document.getElementById('in-aside');
		return window.__btn.isConnected ? 'live' : 'dead';
	})()`, &btnBefore)); err != nil {
		t.Fatal(err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#aside`, "CLIENT-SKELETON")
	// Supersede: navigate to /c (no deferred outlets of its own, so
	// nothing else will touch the aside) before /a's page or parts
	// land. The abort must restore the parked nodes.
	jsClick(t, ctx, `#goC`)
	waitContains(t, ctx, `#main`, "C-PRIMARY")
	// The aborted /a parts' regions restore the parked nodes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(readText(t, ctx, `#aside`), "OLD-ASIDE") {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if got := readText(t, ctx, `#aside`); !strings.Contains(got, "OLD-ASIDE") {
		t.Errorf("aside after abort = %q, want the parked OLD-ASIDE nodes back (the /c page has no aside fill; only the restore wrote it)", got)
	}
	var btnAfter string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__btn && window.__btn.isConnected ? 'live' : 'dead'`, &btnAfter)); err != nil {
		t.Fatal(err)
	}
	if btnBefore == "live" && btnAfter != "live" {
		t.Errorf("the parked aside button did not come back as the SAME node (%s → %s)", btnBefore, btnAfter)
	}
	relAside()
	relRail()
}

// TestPartResetReloadsOnce: parts answering the 409 reset make the
// runtime reload the URL as a whole document — at most once per
// navigation, even when both parts reset.
func TestPartResetReloadsOnce(t *testing.T) {
	s := newPartsSite(t, 10000)
	reset := func(w http.ResponseWriter) {
		w.Header().Set("X-Gofastr-Part-Reset", "1")
		w.WriteHeader(http.StatusConflict)
	}
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) { reset(w) })
	s.setPartResp("/a|rail", func(w http.ResponseWriter, _ *http.Request) { reset(w) })

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	// The reload lands the /a DOCUMENT; the reset stops there: no loop.
	// WaitVisible, not an Evaluate poll: #doc-a exists only in a document
	// load, and a poll that lands mid-reload fails with "Inspected target
	// navigated or closed" where the query action retries onto the new
	// document.
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#doc-a`, chromedp.ByID)); err != nil {
		t.Fatalf("wait for the /a document: %v", err)
	}
	time.Sleep(700 * time.Millisecond) // wait out any would-be second reload
	if n := s.count("doc", "/a"); n != 1 {
		t.Errorf("document loads of /a after reset = %d, want exactly 1 (reload once per navigation)", n)
	}
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-A") {
		t.Errorf("aside after reset = %q; the 409 body must never apply", got)
	}
	var path string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &path)); err != nil {
		t.Fatal(err)
	}
	if path != "/a" {
		t.Errorf("URL after reset = %s, want /a", path)
	}
}

// TestPartLandingDoesNotSkipPageTransition: the page's commit runs
// through one view transition; a part landing during or after it never
// starts a second. The rig declares a vocabulary (view transitions are
// opt-in since the corrected Opt-in table: a plain page starts none at
// all, and this assertion is about the transition-declaring one).
func TestPartLandingDoesNotSkipPageTransition(t *testing.T) {
	s := newPartsSite(t, 10000, true)
	// Gate the parts so they land AFTER the page's commit — the apply
	// path that could wrongly start a second view transition.
	relAside := s.gatePart(t, "/a|aside")
	relRail := s.gatePart(t, "/a|rail")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			window.__vtEvents = 0;
			document.addEventListener('gofastr:transition', () => { window.__vtEvents++; });
			return true;
		})()`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	relAside()
	relRail()
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
	time.Sleep(300 * time.Millisecond)
	var events int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__vtEvents`, &events)); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("gofastr:transition events = %d, want exactly 1 (a part never starts a view transition)", events)
	}
}

// TestBackToEntryWithUnlandedParts: Back to an entry whose parts never
// landed replays what the entry holds and REQUESTS the missing parts;
// the regions show their loading content meanwhile.
func TestBackToEntryWithUnlandedParts(t *testing.T) {
	s := newPartsSite(t, 10000)
	relAside := s.gatePart(t, "/a|aside")
	relRail := s.gatePart(t, "/a|rail")

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	// The deferred regions hold the server's loading content while
	// their parts stay in flight.
	if got := readText(t, ctx, `#aside`); !strings.Contains(got, "SERVER-aside-SKELETON") {
		t.Errorf("aside with unlanded part = %q, want the loading content meanwhile", got)
	}
	// Supersede (aborts /a's parts), then go Back: the entry replays
	// and re-requests its missing parts.
	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	relAside()
	relRail()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
	if n := s.count("part", "/a|aside"); n != 2 {
		t.Errorf("aside part requests = %d, want 2 (the unlanded part is re-requested on Back)", n)
	}
}

// TestFailedPartNotCached: a failed part is not stored on the entry;
// returning to the page re-requests it.
func TestFailedPartNotCached(t *testing.T) {
	s := newPartsSite(t, 10000)
	var failMu sync.Mutex
	failNext := true
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) {
		failMu.Lock()
		fail := failNext
		failNext = false
		failMu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "boom")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<template data-cui-fill="l:site#aside">PART-A-ASIDE</template>`)
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	// The failed part's region keeps its loading content; the page and
	// the other part stand.
	time.Sleep(250 * time.Millisecond)
	if got := readText(t, ctx, `#aside`); strings.Contains(got, "PART-A-ASIDE") {
		t.Fatalf("aside after a failed part = %q; a failure must not apply", got)
	}
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")

	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	if n := s.count("part", "/a|aside"); n != 2 {
		t.Errorf("aside part requests = %d, want 2 (a failed part is not cached)", n)
	}
}

// TestPrefetchedEntryRequestsParts: hover prefetch fetches the PAGE
// ONLY (deferred, so its regions carry loading content); the click
// applies it and requests the missing parts beside nothing else.
func TestPrefetchedEntryRequestsParts(t *testing.T) {
	s := newPartsSite(t, 10000)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// A real pointerover on the link triggers the hover prefetch.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const a = document.querySelector('#goA');
		a.dispatchEvent(new PointerEvent('pointerover', {bubbles: true}));
		return true;
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	// The prefetch fetches the page only: exactly one request to /a
	// with the Defer header and NO part requests.
	deadline := time.Now().Add(10 * time.Second)
	for s.count("page", "/a") < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond)
	if n := s.count("page", "/a"); n != 1 {
		t.Fatalf("prefetch page requests = %d, want exactly 1", n)
	}
	if n := s.totalParts(); n != 0 {
		t.Fatalf("prefetch made %d PART requests; hover prefetch fetches the page only", n)
	}
	// The click applies the prefetched entry and requests the parts.
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	waitContains(t, ctx, `#rail`, "PART-A-RAIL")
	if n := s.count("page", "/a"); n != 1 {
		t.Errorf("page requests after the click = %d, want 1 (the prefetched entry served it)", n)
	}
}

// TestCacheClearedOnSessionChange: X-Gofastr-Session arriving on a
// navigation answer is an identity change — the whole navigation cache
// drops, so one identity's pages never replay for another.
func TestCacheClearedOnSessionChange(t *testing.T) {
	s := newPartsSite(t, 10000)
	// The /b page answer carries a fresh session id (the rollover).
	s.setPageResp("/b", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Session", "fresh-session-id")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "Page")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		w.Header().Set("X-Gofastr-Envelope", "2")
		fmt.Fprint(w, `<template data-cui-fill="l:site">B-PRIMARY</template>`+
			`<template data-cui-fill="l:site#aside">SERVER-aside-SKELETON</template>`)
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE")
	if n := s.count("page", "/a"); n != 1 {
		t.Fatalf("page requests for /a = %d, want 1", n)
	}
	// The /b navigation carries the session change: the cache drops.
	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	// Back to /a must REFETCH (a replayed entry would be one identity's
	// page under another).
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	if n := s.count("page", "/a"); n != 2 {
		t.Errorf("page requests for /a after Back = %d, want 2 (the session change cleared the cache; the entry must not replay)", n)
	}
}

// TestPartResetWaitsForThePage: a 409 part reset reloads only a page
// that committed OK. A reset landing before the page answer waits for
// it; a page answering an error page (the resolver 500 in the tracker)
// discards its parts, so no reload replaces the error page.
func TestPartResetWaitsForThePage(t *testing.T) {
	errorPage := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", "Error")
		w.Header().Set("X-Gofastr-Swap", "l:site")
		w.Header().Set("X-Gofastr-Envelope", "2")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<template data-cui-fill="l:site">ERROR-PAGE</template>`)
	}
	reset := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gofastr-Part-Reset", "1")
		w.WriteHeader(http.StatusConflict)
	}
	run := func(t *testing.T, ok bool) *partsSite {
		s := newPartsSite(t, 10000)
		if !ok {
			s.setPageResp("/a", errorPage)
		}
		release := s.gatePage(t, "/a")
		s.setPartResp("/a|aside", reset)
		s.setPartResp("/a|rail", reset)
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		if err := chromedp.Run(ctx,
			chromedp.Navigate(s.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
		); err != nil {
			t.Fatalf("navigate: %v", err)
		}
		jsClick(t, ctx, `#goA`)
		// Both resets land while the page is held.
		deadline := time.Now().Add(10 * time.Second)
		for s.count("part", "/a|aside") == 0 || s.count("part", "/a|rail") == 0 {
			if time.Now().After(deadline) {
				t.Fatal("the parts were never requested")
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(300 * time.Millisecond)
		if n := s.count("doc", "/a"); n != 0 {
			t.Fatalf("document loads of /a before the page answered = %d, want 0 (a reset waits for the page)", n)
		}
		release()
		if !ok {
			waitContains(t, ctx, `#main`, "ERROR-PAGE")
		}
		time.Sleep(700 * time.Millisecond)
		return s
	}
	t.Run("error page keeps its page", func(t *testing.T) {
		s := run(t, false)
		if n := s.count("doc", "/a"); n != 0 {
			t.Errorf("document loads of /a after an error page = %d, want 0 (its parts die with it)", n)
		}
	})
	t.Run("ok page reloads once", func(t *testing.T) {
		s := run(t, true)
		if n := s.count("doc", "/a"); n != 1 {
			t.Errorf("document loads of /a after an OK page = %d, want 1", n)
		}
	})
}
