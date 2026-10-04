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

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// dcRig is a gated two-destination outlet site: every navigate answer
// for /a and /b is held until its gate closes, so a test decides which
// navigation's response APPLIES first and which arrives last.
type dcRig struct {
	srv *httptest.Server

	mu    sync.Mutex
	gates map[string]chan struct{}
}

func (r *dcRig) release(path string) {
	r.mu.Lock()
	g := r.gates[path]
	delete(r.gates, path)
	r.mu.Unlock()
	if g != nil {
		close(g)
	}
}

func newDoubleClickRig(t *testing.T, vt bool) *dcRig {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	r := &dcRig{gates: map[string]chan struct{}{}}
	shell := func(main string) string {
		vtKinds := ""
		if vt {
			vtKinds = ` data-cui-vt-kinds="fade"`
		}
		return `<!doctype html><html lang="en"` + vtKinds + `><head><title>dc</title>` +
			`<script type="application/json" id="gofastr-routes">[` +
			`{"path":"/"},{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}` +
			`]</script></head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a><a id="goB" href="/b">B</a></nav>` +
			`<div data-cui-outlet="l:site#aside" id="aside">OLD-ASIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + main + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
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
		if req.URL.Path != "/" && req.Header.Get("X-Gofastr-Navigate") != "1" {
			// Full-document answers for the destinations carry their
			// own content (a whole-document fallback or a cross-chain
			// full fetch lands the real page, not the shell's home).
			name := strings.ToUpper(strings.TrimPrefix(req.URL.Path, "/"))
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, shell(`<p>MAIN-`+name+`</p>`))
			return
		}
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			r.mu.Lock()
			g, held := r.gates[req.URL.Path]
			r.mu.Unlock()
			if held {
				<-g
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", strings.ToUpper(req.URL.Path))
			w.Header().Set("X-Gofastr-Swap", "l:site")
			w.Header().Set("X-Gofastr-Envelope", "2")
			fmt.Fprintf(w, `<template data-cui-fill="l:site">MAIN-%s</template>`+
				`<template data-cui-fill="l:site#aside">ASIDE-%s</template>`,
				strings.ToUpper(strings.TrimPrefix(req.URL.Path, "/")),
				strings.ToUpper(strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, shell(`<p id="home">HOME</p>`))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// hold arms a gate so the next navigate answer for path waits.
func (r *dcRig) hold(path string) {
	r.mu.Lock()
	r.gates[path] = make(chan struct{})
	r.mu.Unlock()
}

// TestDoubleClickAppliesWinnerOutlets: two rapid clicks start two
// navigations; the FIRST click's response is released LAST. The
// supersede rule is one decision — NS._navLive, asked at every point
// that applies DOM, cache or history — so the loser's late response
// must change nothing: the winner's primary and outlet fill stand, the
// URL stays the winner's. Mutation it catches (the rule 11 probe for
// this file): NS._navLive returning true unconditionally lets the
// loser's post-fetch, post-text and commit checks all pass, the stale
// content lands after the winner's, and both assertions below fail.
func TestDoubleClickAppliesWinnerOutlets(t *testing.T) {
	r := newDoubleClickRig(t, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Hold both answers, click A then B (B supersedes A), then apply
	// the WINNER first and let the loser's response arrive last.
	r.hold("/a")
	r.hold("/b")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Click(`#goB`, chromedp.ByID),
	); err != nil {
		t.Fatalf("double click: %v", err)
	}
	r.release("/b")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-B')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("winner never applied: %v", err)
	}
	// The loser's response arrives now — one full second to settle,
	// far past any commit deferral.
	r.release("/a")
	if err := chromedp.Run(ctx, chromedp.Sleep(time.Second)); err != nil {
		t.Fatal(err)
	}

	var main, aside, url, live string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
		chromedp.Evaluate(`document.getElementById('aside').textContent`, &aside),
		chromedp.Evaluate(`location.pathname`, &url),
		chromedp.Evaluate(`String(window.__gofastr.currentPath)`, &live),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(main, "MAIN-B") {
		t.Errorf("main = %q after the superseded navigation's response landed last — the loser applied over the winner", main)
	}
	if aside != "ASIDE-B" {
		t.Errorf("outlet = %q, want the winner's ASIDE-B (the loser's fill must never apply)", aside)
	}
	if url != "/b" || live != "/b" {
		t.Errorf("url = %q currentPath = %q, want /b: a superseded navigation must not touch history", url, live)
	}
}

// TestDoubleClickCacheHoldsWinner: the same double click, asserted
// through the CACHE: after the loser's late response, the screen
// cache's entry for the loser's path must hold the LOSER's own bytes
// (a cache write is not an apply — it may happen), and a Back to it
// replays the loser's content while the live page keeps the winner's.
// Together with TestDoubleClickAppliesWinnerOutlets this pins the
// cache side of the supersede rule at the commit's cache write.
func TestDoubleClickCacheHoldsWinner(t *testing.T) {
	r := newDoubleClickRig(t, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		// Warm the module first: with it cold, the stand-down ROLLS THE
		// LOSER'S URL BACK (its navigation never ran, so Back cannot
		// reach it — the winner contract holds, the history shape
		// differs). Warmed, both clicks are module navigations and the
		// loser's entry exists in history.
		chromedp.Click(`#goB`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["envelope"] && document.getElementById('main').textContent.includes('MAIN-B')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__gofastr.navigate('/', {force: true})`, nil),
		chromedp.Poll(`location.pathname === '/' && document.getElementById('main').textContent.includes('HOME')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("navigate/warm: %v", err)
	}
	r.hold("/a")
	r.hold("/b")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Click(`#goB`, chromedp.ByID),
	); err != nil {
		t.Fatalf("double click: %v", err)
	}
	r.release("/b")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-B')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("winner never applied: %v", err)
	}
	r.release("/a")
	// Back to the loser's entry: whatever the cache holds for /a, the
	// DOM after Back is the loser's page (replayed or refetched — both
	// legitimate), never a mix with the winner's bytes.
	if err := chromedp.Run(ctx,
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-A')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("Back to the loser: %v", err)
	}
	var aside string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('aside').textContent`, &aside))
	if aside != "ASIDE-A" {
		t.Errorf("outlet after Back = %q, want ASIDE-A (a replayed entry carries its own fills, never the winner's)", aside)
	}
}

// TestDoubleClickWithTransitionCommit: the same double click on a page
// that declares a transition vocabulary, so the transition module is
// live and the commit runs INSIDE document.startViewTransition — the
// module's own epoch check (the run() guard) is what must stop the
// loser's swap there. Mutation it catches: that guard alone bypassed
// lets the loser's update callback swap the DOM a frame after the
// winner's transition.
func TestDoubleClickWithTransitionCommit(t *testing.T) {
	r := newDoubleClickRig(t, true)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["transition"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("navigate (transition module): %v", err)
	}
	r.hold("/a")
	r.hold("/b")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Click(`#goB`, chromedp.ByID),
	); err != nil {
		t.Fatalf("double click: %v", err)
	}
	r.release("/b")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-B')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("winner never applied: %v", err)
	}
	r.release("/a")
	if err := chromedp.Run(ctx, chromedp.Sleep(time.Second)); err != nil {
		t.Fatal(err)
	}
	var main string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('main').textContent`, &main))
	if !strings.Contains(main, "MAIN-B") {
		t.Errorf("main = %q after the loser's commit inside a view transition — the transition module's guard let a superseded swap run", main)
	}
}

// TestHandoffFirstNavNotPoisoned pins the served-page keying of the
// module's evaluation-time capture: the hand-off's click has already
// pushed the destination, so currentPath names the DESTINATION while
// the DOM still shows the served page — keying the capture (or the
// scroll record) under currentPath replays the served page's bytes as
// the destination on the very first navigation. Single click, held
// answer: the first navigation must FETCH (not replay the capture).
func TestHandoffFirstNavNotPoisoned(t *testing.T) {
	r := newDoubleClickRig(t, false)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(r.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	r.hold("/a")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	r.release("/a")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-A')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("the first hand-off navigation never fetched: %v", err)
	}
	var main string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('main').textContent`, &main))
	if !strings.Contains(main, "MAIN-A") {
		t.Errorf("main = %q after the first hand-off navigation, want MAIN-A (never the served page's bytes)", main)
	}
}
