package runtime_test

// Version-skew e2e (docs/DESIGN-layout-outlets.md "Mixed versions
// during a deploy"): the envelope version is negotiated in both
// directions and a mismatch always ends in a full load.
//
//   - TestPreviousRuntimeOnNewServer: the v0.85.0 runtime bundle (pinned
//     as a testdata fixture, taken with `git show v0.85.0:…`) navigates
//     against today's server. A same-chain click whose kept shell
//     carries outlets must make the server answer X-Gofastr-Swap:
//     !reload; the old runtime's missing-slot repair full-loads, swaps
//     the whole shell, and the destination's outlets arrive.
//   - TestNewRuntimeOnOldServer: today's runtime navigates against a
//     server that answers plain partials (no X-Gofastr-Envelope, no
//     fill templates — the pre-layout wire). A partial whose document
//     holds an outlet or area outside the target slot full-loads
//     instead of applying; the outlets never go stale.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// loadV085Fixture reads a pinned v0.85.0 runtime artifact.
func loadV085Fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/v0.85.0-" + name + ".js")
	if err != nil {
		t.Fatalf("v0.85.0 fixture: %v", err)
	}
	return b
}

// skewRig is a real uihost app (the NEW server) whose runtime.js is the
// v0.85.0 bundle (the OLD client). Every request is counted by
// (path, partial?) so the tests can prove which wire shape a navigation
// took and how often.
type skewRig struct {
	srv *httptest.Server

	mu      sync.Mutex
	partial map[string]int
	full    map[string]int
}

func (r *skewRig) partials(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partial[path]
}

func (r *skewRig) fulls(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full[path]
}

// newPreviousRuntimeRig builds the old-client/new-server rig:
//
//	/home  → chain ["l:site"] (toolbar + aside outlets, aside default)
//	/other → chain ["l:site"], fills the toolbar outlet
func newPreviousRuntimeRig(t *testing.T) *skewRig {
	t.Helper()
	a := app.NewApp("skew-rig")
	skewToolbar := app.NewOutlet("toolbar")
	skewAside := app.NewOutlet("aside", app.OutletOptions{Default: app.NewStaticComponent(`<span id="aside-default">SITE-ASIDE-DEFAULT</span>`)})
	site := app.NewLayout("site", app.LayoutSpec{
		Outlets: []*app.Outlet{skewToolbar, skewAside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.HTML(`<nav><a id="to-home" href="/home">Home</a><a id="to-other" href="/other">Other</a></nav>`),
			l.Place(skewToolbar),
			l.Place(skewAside),
			l.Primary(),
		)
	})
	a.SetDefaultLayout(site)
	a.RegisterScreen(app.NewScreen("/home", app.NewStaticComponent(`<p id="screen-home">HOME</p>`)), nil)
	a.RegisterScreen(app.NewScreen("/other", app.NewStaticComponent(`<p id="screen-other">OTHER</p>`)).
		Fill(skewToolbar, app.NewStaticComponent(`<span id="other-toolbar">OTHER-TOOLBAR</span>`)), nil)

	ds := uihost.New(a)
	rt := router.New()
	ds.Mount(rt)

	r := &skewRig{partial: map[string]int{}, full: map[string]int{}}
	mux := http.NewServeMux()
	// The OLD client: the pinned v0.85.0 core bundle (and its own
	// activelink module — the idle-loaded cosmetic) shadow the host's.
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(loadV085Fixture(t, "runtime"))
	})
	mux.HandleFunc("/__gofastr/runtime/activelink.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(loadV085Fixture(t, "activelink"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			r.partial[req.URL.Path]++
		} else {
			r.full[req.URL.Path]++
		}
		r.mu.Unlock()
		rt.ServeHTTP(w, req)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// TestPreviousRuntimeOnNewServer: the v0.85.0 runtime clicking a
// same-chain link whose kept shell carries outlets. The server cannot
// send fills to a fills-less client, so it answers !reload; the old
// runtime finds no such slot, full-loads the destination, and swaps
// the whole shell — the destination's toolbar fill and the aside's
// default arrive, the URL lands, one partial and at least one
// full-document request were made.
func TestPreviousRuntimeOnNewServer(t *testing.T) {
	rig := newPreviousRuntimeRig(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var path, toolbar, aside string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/home"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),

		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-other`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`document.querySelector('[data-fui-outlet="l:site#toolbar"]')?.innerHTML || ''`, &toolbar),
		chromedp.Evaluate(`document.querySelector('[data-fui-outlet="l:site#aside"]')?.innerHTML || ''`, &aside),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if path != "/other" {
		t.Errorf("URL = %s, want /other", path)
	}
	if rig.partials("/other") != 1 {
		t.Errorf("partial requests for /other = %d, want exactly 1 (the !reload answer)", rig.partials("/other"))
	}
	if rig.fulls("/other") < 1 {
		t.Errorf("full-document requests for /other = %d, want >= 1 (the old runtime's missing-slot repair)", rig.fulls("/other"))
	}
	if !strings.Contains(toolbar, "OTHER-TOOLBAR") {
		t.Errorf("toolbar = %q, want the destination's fill OTHER-TOOLBAR after the whole-shell swap", toolbar)
	}
	if !strings.Contains(aside, "SITE-ASIDE-DEFAULT") {
		t.Errorf("aside = %q, want the shell's default fill after the whole-shell swap", aside)
	}
}

// oldServerRig serves today's runtime against a hand-rolled "old
// server" that answers plain partials: X-Gofastr-Partial + Swap, no
// X-Gofastr-Envelope, no fill templates — the pre-layout wire shape.
type oldServerRig struct {
	srv *httptest.Server

	mu      sync.Mutex
	partial map[string]int
	full    map[string]int
}

func (r *oldServerRig) partials(p string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partial[p]
}

func (r *oldServerRig) fulls(p string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full[p]
}

func newOldServerRig(t *testing.T) *oldServerRig {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	r := &oldServerRig{partial: map[string]int{}, full: map[string]int{}}
	page := func(inner, aside string) string {
		return `<!doctype html><html lang="en"><head><title>old-server</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/a","layouts":["l:site"]}]</script>` +
			`</head><body><div data-fui-layout="site" data-fui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` +
			`<div data-fui-outlet="l:site#aside" id="aside">` + aside + `</div>` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			r.partial[req.URL.Path]++
		} else {
			r.full[req.URL.Path]++
		}
		r.mu.Unlock()
		if req.Header.Get("X-Gofastr-Navigate") == "1" && req.URL.Path == "/a" {
			// The pre-layout answer: a plain partial, swap at the shell.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "A")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `<p id="screen-a">A-CONTENT</p>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if req.URL.Path == "/a" {
			fmt.Fprint(w, page(`<p id="screen-a">A-CONTENT</p>`, `A-ASIDE`))
			return
		}
		fmt.Fprint(w, page(`<p id="screen-home">HOME</p>`, `HOME-ASIDE`))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// TestNewRuntimeOnOldServer: today's runtime meets a plain partial (no
// envelope header, no fills) while the document holds an outlet
// outside the target slot. Applying it would swap the primary and
// leave the outlet stale with no repair; the runtime full-loads
// instead — a second, non-partial request for the destination, and
// the outlet carries the destination document's content.
func TestNewRuntimeOnOldServer(t *testing.T) {
	rig := newOldServerRig(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var aside, mainTxt string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),

		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('aside')?.textContent || ''`, &aside),
		chromedp.Evaluate(`document.getElementById('main')?.textContent || ''`, &mainTxt),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rig.partials("/a") < 1 {
		t.Errorf("partial requests for /a = %d, want >= 1 (the click's fetch)", rig.partials("/a"))
	}
	if rig.fulls("/a") < 1 {
		t.Errorf("full-document requests for /a = %d, want >= 1 (a plain partial with outlets outside the slot must full-load, not apply)", rig.fulls("/a"))
	}
	if !strings.Contains(mainTxt, "A-CONTENT") {
		t.Errorf("main = %q, want the destination's content A-CONTENT", mainTxt)
	}
	if !strings.Contains(aside, "A-ASIDE") {
		t.Errorf("aside = %q, want the destination document's A-ASIDE (never the origin's HOME-ASIDE)", aside)
	}
}
