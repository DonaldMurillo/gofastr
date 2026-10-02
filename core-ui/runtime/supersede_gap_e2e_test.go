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

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// gapRig is a gated multi-chain rig for the supersede rule's
// individually load-bearing call sites: every navigate answer is held
// until released, so a test decides which navigation applies first
// and which response, part or timer lands last.
//
// Chains: "/" and "/y" are layout-less; "/x", "/x2" and "/d" carry
// l:x. "/d" declares the aside deferred (parts fly for it). The
// loadingTpl, when non-empty, rides the chained pages beside their
// slot. Full-document answers carry the same markers the navigate
// answers do, so a full-load fallback lands the same content.
type gapRig struct {
	srv *httptest.Server

	mu       sync.Mutex
	gates    map[string]chan struct{}
	partGate chan struct{}
}

func (r *gapRig) hold(path string) {
	r.mu.Lock()
	r.gates[path] = make(chan struct{})
	r.mu.Unlock()
}

func (r *gapRig) release(path string) {
	r.mu.Lock()
	g := r.gates[path]
	delete(r.gates, path)
	r.mu.Unlock()
	if g != nil {
		close(g)
	}
}

func (r *gapRig) holdParts() {
	r.mu.Lock()
	r.partGate = make(chan struct{})
	r.mu.Unlock()
}

func (r *gapRig) releaseParts() {
	r.mu.Lock()
	if r.partGate != nil {
		close(r.partGate)
		r.partGate = nil
	}
	r.mu.Unlock()
}

func newGapRig(t *testing.T, loadingTpl, deferredRoute string, failPath string) *gapRig {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	r := &gapRig{gates: map[string]chan struct{}{}}
	deferredField := ""
	if deferredRoute != "" {
		deferredField = `,"deferred":["l:x#aside"]`
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},{"path":"/y"},{"path":"/x","layouts":["l:x"]},` +
		`{"path":"/x2","layouts":["l:x"]},{"path":"/x3","layouts":["l:x"]},` +
		`{"path":"/d","layouts":["l:x"]` + deferredField + `},` +
		`{"path":"/d2","layouts":["l:x"]` + deferredField + `}` +
		`]</script>`
	navLinks := `<nav><a id="goX" href="/x">X</a><a id="goY" href="/y">Y</a><a id="goX2" href="/x2">X2</a><a id="goX3" href="/x3">X3</a><a id="goD" href="/d">D</a><a id="goD2" href="/d2">D2</a></nav>`
	page := func(body string) string {
		return `<!doctype html><html lang="en"><head><title>gap</title>` + routes +
			`</head><body>` + body +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	// chainedFull is the full-document form of a chained page (direct
	// loads and whole-document fallbacks): the shell, the aside outlet
	// and the primary all marked like the navigate answers.
	chainedFull := func(main string) string {
		return `<div data-fui-layout="x" data-fui-layout-key="l:x">` + navLinks +
			`<div data-fui-outlet="l:x#aside" id="aside">BOOT-ASIDE</div>` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:x" id="main">` + main + `</main>` +
			`</div>` + loadingTpl
	}
	plainFull := func(main string) string {
		return navLinks + `<main role="main" tabindex="-1" id="main">` + main + `</main>`
	}
	isChained := func(p string) bool {
		return p == "/x" || p == "/x2" || p == "/x3" || p == "/d" || p == "/d2"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		// Hold EVERY request to a held path: cross-chain full fetches
		// carry no navigate header, and the gate decides ordering, not
		// the wire shape.
		r.mu.Lock()
		g, held := r.gates[req.URL.Path]
		r.mu.Unlock()
		if held {
			<-g
		}
		if failPath == req.URL.Path {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(500)
			return
		}
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", strings.ToUpper(strings.TrimPrefix(req.URL.Path, "/")))
			fill := func(addr, html string) string {
				return fmt.Sprintf(`<template data-fui-fill=%q>%s</template>`, addr, html)
			}
			name := strings.ToUpper(strings.TrimPrefix(req.URL.Path, "/"))
			if isChained(req.URL.Path) {
				w.Header().Set("X-Gofastr-Swap", "l:x")
				if req.Header.Get("X-Gofastr-Fills") == "2" {
					w.Header().Set("X-Gofastr-Envelope", "2")
					fmt.Fprint(w, fill("l:x", "MAIN-"+name)+fill("l:x#aside", "ASIDE-"+name))
				} else {
					fmt.Fprint(w, `<p id="mk-`+strings.ToLower(name)+`">MAIN-`+name+`</p>`)
				}
				return
			}
			fmt.Fprint(w, `<p id="mk-y">MAIN-Y</p>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		switch {
		case req.URL.Path == "/y":
			// The full-document form of the winner carries the same
			// marker its partial answer does, so a cross-chain apply
			// is observable either way.
			fmt.Fprint(w, page(plainFull(`<p id="mk-y">MAIN-Y</p>`)))
		case isChained(req.URL.Path):
			fmt.Fprint(w, page(chainedFull(`<p id="boot-x">BOOT-X</p>`)))
		default:
			fmt.Fprint(w, page(plainFull(`<p id="boot">BOOT</p>`)))
		}
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// warmModule boots a chained page and runs one SAME-CHAIN navigation:
// the handoff loads the envelope module beside that partial fetch, and
// every later navigation runs its navigator. (A cross-chain
// navigation's full fetch never reaches the handoff, and boot loads
// nothing.)
func warmModule(t *testing.T, ctx context.Context, r *gapRig) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/x"),
		chromedp.WaitVisible(`#boot-x`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.navigate('/x2')`, nil),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"] && document.getElementById('main').textContent.includes('MAIN-X2')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("warm-up: %v", err)
	}
}

// TestDoubleClickCrossChainPlain pins the plain navigator's cross-chain
// resume pair: two layout-crossing navigations, the loser's FULL-page
// answer released last, must not swap the shell back. The two guards
// (post-fetch, post-text) sit adjacent with no interleaving window
// between them that one alone guards, so they are pinned as a pair;
// each alone is backstopped by its neighbour.
func TestDoubleClickCrossChainPlain(t *testing.T) {
	r := newGapRig(t, "", "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goX`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	r.hold("/x")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX`, chromedp.ByID),
		chromedp.Click(`#goY`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("winner (cross-chain to plain) never applied: %v", err)
	}
	r.release("/x")
	if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var main string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('main').textContent`, &main))
	if !strings.Contains(main, "MAIN-Y") {
		t.Errorf("main = %q after the superseded cross-chain response landed last — the stale full-page answer swapped the shell back over the winner", main)
	}
}

// TestDoubleClickCrossChainEnvelope pins the envelope navigator's
// cross-chain resume pair on a marker page (the module live): same
// shape, the loser's full-document answer arrives last.
func TestDoubleClickCrossChainEnvelope(t *testing.T) {
	r := newGapRig(t, "", "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	warmModule(t, ctx, r)
	// Only the LOSER is held (a FRESH page — an already-visited one
	// replays from the cache and never fetches): the winner's
	// cross-chain answer applies first, the loser's arrives last.
	r.hold("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX3`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Click(`#goY`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("winner never applied: %v", err)
	}
	r.release("/x3")
	if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var main, title string
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
		chromedp.Evaluate(`document.title`, &title),
	)
	if !strings.Contains(main, "MAIN-Y") {
		t.Errorf("main = %q after the superseded cross-chain response landed last — the stale full-page answer swapped the shell back over the winner", main)
	}
	if strings.Contains(strings.ToUpper(title), "X3") {
		t.Errorf("title = %q after the superseded response landed last — its pre-commit title write ran over the winner's", title)
	}
}

// TestSupersededNavKeepsBusy pins the finally busy-clear guards of
// BOTH navigators: a navigation that stands down after being superseded
// must leave the busy mark to the in-flight winner. The module-cold
// round exercises the plain navigator's finally (the handoff window),
// the warm round the envelope module's.
func TestSupersededNavKeepsBusy(t *testing.T) {
	r := newGapRig(t, "", "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goX`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Cold round: two PLAIN navigations from the layout-less origin
	// (the module never loads — /x never applies), the loser's
	// cross-chain full answer released while the winner's partial is
	// still held.
	r.hold("/x")
	r.hold("/y")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Click(`#goY`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
	); err != nil {
		t.Fatalf("cold round clicks: %v", err)
	}
	r.release("/x")
	var busy string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`String(document.documentElement.getAttribute('aria-busy'))`, &busy),
	); err != nil {
		t.Fatal(err)
	}
	r.release("/y")
	_ = chromedp.Run(ctx, chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)))
	if busy != "true" {
		t.Errorf("cold round: html aria-busy = %q while the winner was still in flight — the superseded navigation's finally cleared the mark it no longer owned", busy)
	}
	// Warm round: enter the chain (the full document answers BOOT-X,
	// a whole shell swap), then load the module through a same-chain
	// partial, then the same shape — both navigations run the module's
	// navigator and its finally guard.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.navigate('/x')`, nil),
		chromedp.Poll(`document.getElementById('main').textContent.includes('BOOT-X')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.navigate('/x2')`, nil),
		chromedp.Poll(`window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"] && document.getElementById('main').textContent.includes('MAIN-X2')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("warm round entry: %v", err)
	}
	r.hold("/x3")
	r.hold("/y")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX3`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Click(`#goY`, chromedp.ByID),
	); err != nil {
		t.Fatalf("warm round clicks: %v", err)
	}
	r.release("/x3")
	busy = ""
	if err := chromedp.Run(ctx,
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`String(document.documentElement.getAttribute('aria-busy'))`, &busy),
	); err != nil {
		t.Fatal(err)
	}
	r.release("/y")
	_ = chromedp.Run(ctx, chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)))
	if busy != "true" {
		t.Errorf("warm round: html aria-busy = %q while the winner was still in flight — the superseded navigation's finally cleared the mark it no longer owned", busy)
	}
}

// TestDoubleClickDuringLoadingHold pins the envelope navigator's
// post-loading-wait resume: a navigation superseded while its Min hold
// or exit animation runs must not apply after the wait.
func TestDoubleClickDuringLoadingHold(t *testing.T) {
	tpl := `<template data-fui-loading="l:x" data-fui-after="0" data-fui-min="600"><span id="skeleton">SKELETON</span></template>`
	r := newGapRig(t, tpl, "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	warmModule(t, ctx, r)
	// The loser's answer arrives and its Min hold runs; the winner
	// supersedes DURING the hold.
	r.hold("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX3`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('skeleton') !== null`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#goX2`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	r.release("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-X2')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(900*time.Millisecond),
	); err != nil {
		t.Fatalf("winner never applied: %v", err)
	}
	var main, aside string
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
		chromedp.Evaluate(`document.getElementById('aside').textContent`, &aside),
	)
	if strings.Contains(main, "MAIN-X3") || !strings.Contains(main, "MAIN-X2") || aside == "ASIDE-X3" {
		t.Errorf("main=%q aside=%q after the loser's loading hold elapsed — the navigation superseded during its own Min hold applied over the winner", main, aside)
	}
}

// TestSupersededLoadingTimerSleeps pins the loading module's
// after-timer guard: a superseded navigation's pending timer must not
// park its skeleton over the newer navigation's content.
func TestSupersededLoadingTimerSleeps(t *testing.T) {
	tpl := `<template data-fui-loading="l:x" data-fui-after="500" data-fui-min="0"><span id="skeleton">SKELETON</span></template>`
	r := newGapRig(t, tpl, "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	warmModule(t, ctx, r)
	r.hold("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX3`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond), // inside the 500ms After window
		chromedp.Click(`#goY`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	// The loser's timer fires at ~500ms, well after the supersede;
	// probe BEFORE releasing its response — once the loser stands down,
	// its own settle would clean up whatever its timer showed.
	var skeleton string
	_ = chromedp.Run(ctx,
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('skeleton') !== null)`, &skeleton),
	)
	r.release("/x3")
	if skeleton == "true" {
		t.Error("the superseded navigation's After timer parked its skeleton over the winner — the timer's epoch guard let a dead navigation show loading content")
	}
}

// TestSupersededPartDoesNotShowLoading pins the parts module's
// claimed-entry show guard: a part whose navigation was superseded
// between its claim and its landing must not show the claimed
// region's loading content over the winner.
func TestSupersededPartDoesNotShowLoading(t *testing.T) {
	r := newGapRig(t, `<template data-fui-loading="l:x#aside" data-fui-after="0" data-fui-min="0"><span id="sk-part">PART-SKELETON</span></template>`, "/d", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	// Boot on the deferred page: its address is live in the document,
	// so parts AND envelope boot-load and every navigation launches
	// parts through the module navigator.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/d"),
		chromedp.WaitVisible(`#boot-x`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"] && !!window.__gofastr.loadedModules["parts"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Warm the chain, then a deferred navigation whose PART is held.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.navigate('/x')`, nil),
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-X')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("warm navigation: %v", err)
	}
	r.holdParts()
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.navigate('/d2')`, nil),
		// The page lands (ungated); the claimed region waits on the
		// held part.
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-D2')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		// Supersede while the part still flies: the winner applies
		// (ungated) and the part stays held.
		chromedp.Click(`#goY`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('mk-y') !== null`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	r.releaseParts()
	var skeleton string
	_ = chromedp.Run(ctx,
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('sk-part') !== null)`, &skeleton),
	)
	if skeleton == "true" {
		t.Error("the superseded navigation's part showed its claimed loading content over the winner — the claimed-entry show's epoch guard let a dead part park a skeleton")
	}
}
