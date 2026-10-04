package runtime

// PROTOTYPE (spike/layout-resolve): the browser half of the
// resolution-driven navigation — the keyed-transition pick through
// A→B, Back and Forward (asserted on the gofastr:transition event's
// types), and parametric group layer keys (the client substitutes its
// matched path segments into the manifest's template keys; one
// project's layer is kept and replays, another project's re-renders).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// resolveSite is a minimal hand-rolled rig: a shell layer whose
// document declares a keyed-transition vocabulary, three routes, and
// partial answers whose X-Gofastr-Transition the test controls.
type resolveSite struct {
	srv    *httptest.Server
	pickOf map[string]string // path → X-Gofastr-Transition value
}

func newResolveSite(t *testing.T) *resolveSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &resolveSite{pickOf: map[string]string{}}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}` +
		`]</script>`
	doc := func(inner, entry string) string {
		return `<!doctype html><html lang="en" data-cui-vt-kinds="fade slide">` +
			`<head><title>resolve</title>` + routes + `</head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a> <a id="goB" href="/b">B</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	page := func(path, inner string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", "Page")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				if pick := s.pickOf[path]; pick != "" {
					w.Header().Set("X-Gofastr-Transition", pick)
				}
				fmt.Fprint(w, inner)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, doc(inner, path))
		}
	}
	mux.HandleFunc("/a", page("/a", `<span id="page-a">A</span>`))
	mux.HandleFunc("/b", page("/b", `<span id="page-b">B</span>`))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "Home")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, `HOME`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`HOME`, "/"))
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// captureTypes installs a gofastr:transition listener that records
// every event's detail.types, and returns a reader for the log.
func captureTypes(ctx context.Context) func() [][]string {
	chromedp.Run(ctx, chromedp.Evaluate(`(() => {
    window.__vtTypes = [];
    document.addEventListener('gofastr:transition', (e) => {
      window.__vtTypes.push(e.detail.types.slice());
    });
  })()`, nil))
	read := func() [][]string {
		var out [][]string
		chromedp.Run(ctx, chromedp.Evaluate(`window.__vtTypes || []`, &out))
		return out
	}
	return read
}

func waitTextContent(t *testing.T, ctx context.Context, sel, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var txt string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(() => { const el = document.querySelector('`+sel+`'); return el ? el.textContent.trim() : '!missing'; })()`, &txt)); err == nil && strings.Contains(txt, want) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting %s ~ %q", sel, want)
}

// waitTextExact is waitTextContent with an equality predicate. A
// Contains wait on a path prefix ("/projects/billing" is contained in
// "/projects/billing/issues/42") returns while the OLD text is still
// in the document — before a navigation's commit has run — and any
// state read after it races the commit.
func waitTextExact(t *testing.T, ctx context.Context, sel, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var txt string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`(() => { const el = document.querySelector('`+sel+`'); return el ? el.textContent.trim() : '!missing'; })()`, &txt)); err == nil && txt == want {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting %s == %q", sel, want)
}

// TestTransitionPickedByDestination: the pick follows the DESTINATION
// on a click, the entry being LEFT on Back (the mirrored edge), and
// the destination entry's record on Forward; a name outside the
// document's vocabulary is ignored. All asserted through the
// gofastr:transition event's types.
func TestTransitionPickedByDestination(t *testing.T) {
	s := newResolveSite(t)
	s.pickOf["/a"] = "slide"
	s.pickOf["/b"] = "fade"

	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/")); err != nil {
		t.Fatal(err)
	}
	read := captureTypes(ctx)

	click := func(sel string) {
		if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
			t.Fatalf("click %s: %v", sel, err)
		}
	}
	// A → B: the click to /a carries /a's pick.
	click(`#goA`)
	waitTextContent(t, ctx, `#main`, "A")
	// B: another forward click, /b's pick.
	click(`#goB`)
	waitTextContent(t, ctx, `#main`, "B")
	// Back to /a: the EDGE mirrors — the pick recorded on the entry
	// being LEFT (/b, fade) with type back.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#main`, "A")
	// Forward to /b: the destination entry's pick (fade).
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.forward(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#main`, "B")

	// Home (Back, Back): the HOME entry was reached as a whole
	// document — the default, no pick beside the direction.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#main`, "A")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#main`, "HOME")

	types := read()
	want := [][]string{
		{"forward", "slide"}, // / → /a
		{"forward", "fade"},  // /a → /b
		{"back", "fade"},     // /b → /a (the edge being left)
		{"forward", "fade"},  // /a → /b (the destination's record)
		{"back", "fade"},     // /b → /a
		{"back", "fade"},     // /a → / (the entry being left carries the mirrored edge's pick)
	}
	if len(types) != len(want) {
		t.Fatalf("captured %d transitions, want %d: %v", len(types), len(want), types)
	}
	for i, w := range want {
		if strings.Join(types[i], ",") != strings.Join(w, ",") {
			t.Errorf("transition %d types = %v, want %v", i, types[i], w)
		}
	}

	// An unknown pick is ignored: the vocabulary gate drops it, the
	// navigation keeps the direction only.
	s.pickOf["/a"] = "spin"
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/")); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#main`, "HOME")
	read2 := captureTypes(ctx)
	click(`#goA`)
	waitTextContent(t, ctx, `#main`, "A")
	got := read2()
	if len(got) == 0 || strings.Join(got[len(got)-1], ",") != "forward" {
		t.Fatalf("a pick outside data-cui-vt-kinds must be ignored, got %v", got)
	}
}

// paramSite is the param-key rig: ONE group pattern
// /projects/:project whose manifest layouts carry the TEMPLATE key,
// and a server that answers partials under the RESOLVED keys.
type paramSite struct {
	srv *httptest.Server
}

func newParamSite(t *testing.T) *paramSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &paramSite{}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},` +
		`{"path":"/projects/:project","layouts":["l:site","g:/projects/{project}/:site"]},` +
		`{"path":"/projects/:project/issues/:n","layouts":["l:site","g:/projects/{project}/:site"]}` +
		`]</script>`
	// shell renders the project layer with the RESOLVED key and a
	// typed filter input whose value proves the layer is KEPT.
	page := func(svc, inner string) string {
		key := "g:/projects/" + svc + "/:site"
		return `<!doctype html><html lang="en"><head><title>param</title>` + routes + `</head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="toBilling" href="/projects/billing">Billing</a> <a id="toSearch" href="/projects/search">Search</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` +
			`<div data-cui-screen-group="/projects/` + svc + `/">` +
			`<div data-cui-layout="site" data-cui-layout-key="` + key + `">` +
			`<input id="proj-filter" value="` + svc + `-typed">` +
			`<div data-cui-layout-slot="` + key + `" id="slot">` + inner + `</div>` +
			`</div></div></main></div>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		svc := ""
		if segs := strings.Split(strings.Trim(path, "/"), "/"); len(segs) >= 2 {
			svc = segs[1]
		}
		if r.Header.Get("X-Gofastr-Navigate") == "1" && svc != "" {
			key := "g:/projects/" + svc + "/:site"
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", svc)
			w.Header().Set("X-Gofastr-Swap", key)
			fmt.Fprintf(w, `<div data-cui-screen-group="/projects/%s/"><div data-cui-layout="site" data-cui-layout-key="%s"><div data-cui-layout-slot="%s" id="slot"><span id="page">%s</span></div></div></div>`, svc, key, key, path)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		switch path {
		case "/":
			fmt.Fprint(w, `<<!DOCTYPE html><html lang="en"><head><title>param</title>`+routes+`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site"><nav><a id="toBilling" href="/projects/billing">Billing</a> <a id="toSearch" href="/projects/search">Search</a></nav><main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">HOME</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
		default:
			fmt.Fprint(w, page(svc, `<span id="page">`+path+`</span>`))
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// TestParamGroupLayerKeyedByResolvedValue (the browser half): the
// client substitutes its matched path segments into the manifest's
// template keys — another project re-renders the layer (a partial
// under the shell), and the same project's deeper pages keep it (the
// typed filter survives, and Back replays the layer).
func TestParamGroupLayerKeyedByResolvedValue(t *testing.T) {
	s := newParamSite(t)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(s.srv.URL+"/projects/billing")); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#page`, "/projects/billing")

	// Type into the project layer's filter: the layer's DOM must
	// survive the navigations below.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('#proj-filter').value = 'BIL-keep'`, nil)); err != nil {
		t.Fatal(err)
	}

	// A deeper page of the SAME project: the layer is kept — no
	// request under X-Gofastr-From was needed to know it, and the
	// filter's typed value survives.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('nav').insertAdjacentHTML('beforeend', ' <a id="deep" href="/projects/billing/issues/42">i42</a>')`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#deep`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#page`, "/projects/billing/issues/42")
	var filterVal string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#proj-filter').value`, &filterVal)); err != nil {
		t.Fatal(err)
	}
	if filterVal != "BIL-keep" {
		t.Fatalf("the same project's layer must be KEPT (filter value lost: %q)", filterVal)
	}

	// Back WITHIN the project. The plain navigator (this page declares
	// no outlet or area marker, so the envelope module never loads)
	// captured the boot entry at boot keyed at layer 0 — the boot html
	// spans everything below <main> — so the Back REPLAYS the capture:
	// the whole stack below <main> comes back from the cached bytes.
	// Keeping the live layer nodes across Back is the envelope
	// navigator's leave-capture contract, pinned on the outlet sites
	// (p13); a plain page replays wholesale, exactly as the pre-layout
	// runtime did. The exact wait matters: a Contains wait on this
	// prefix returns while the deep page's #page still reads
	// "/projects/billing/issues/42", before the replay committed, and
	// the filter read below would race it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextExact(t, ctx, `#page`, "/projects/billing")
	var afterBack string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#proj-filter').value`, &afterBack)); err != nil {
		t.Fatal(err)
	}
	if afterBack != "billing-typed" {
		t.Fatalf("the plain navigator's Back must replay the boot capture at layer 0 (filter value %q, want the captured %q)", afterBack, "billing-typed")
	}
	// Forward again, then leave for ANOTHER project.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.forward(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#page`, "/projects/billing/issues/42")

	// ANOTHER project: the layer re-renders — the new key replaces the
	// old, the filter is the new project's.
	if err := chromedp.Run(ctx, chromedp.Click(`#toSearch`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#page`, "/projects/search")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
      const el = document.querySelector('[data-cui-layout-key^="g:/projects/"]');
      return el ? el.getAttribute('data-cui-layout-key') : '!missing';
    })()`, &filterVal)); err != nil {
		t.Fatal(err)
	}
	if filterVal != "g:/projects/search/:site" {
		t.Fatalf("another project re-renders the layer under its own key, got %q", filterVal)
	}

	// Back across the project edge: the Billing layer is RE-RENDERED
	// (the Search navigation replaced it), the fresh filter carries the
	// served attribute value — the typed value cannot outlive its node.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
		t.Fatal(err)
	}
	waitTextContent(t, ctx, `#page`, "/projects/billing/issues/42")
	var reKey string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
      const el = document.querySelector('[data-cui-layout-key^="g:/projects/"]');
      return el ? el.getAttribute('data-cui-layout-key') : '!missing';
    })()`, &reKey)); err != nil {
		t.Fatal(err)
	}
	if reKey != "g:/projects/billing/:site" {
		t.Fatalf("Back re-renders the Billing layer under its key, got %q", reKey)
	}
}

// TestTransitionVocabularyCopyIsModuleOwned pins the vocabulary copy's
// home (the transition module's gofastr:navigate listener + direct
// setAttribute, not core's applyDocShell seam): a swapped payload
// whose root carries data-cui-vt-kinds updates the live
// documentElement, and no kernel allowlist word is involved (the
// write is a plain attribute set).
// Mutation it catches: removing the listener's setAttribute leaves the
// origin chain's vocabulary gating every pick.
func TestTransitionVocabularyCopyIsModuleOwned(t *testing.T) {
	s := newResolveSite(t)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.Poll(`window.__gofastr && window.__gofastr.loadedModules && !!window.__gofastr.loadedModules["transition"]`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => {
			document.documentElement.setAttribute('data-cui-vt-kinds', 'fade');
			const root = document.createElement('div');
			root.setAttribute('data-cui-vt-kinds', 'fade slide');
			window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { path: '/x', prevPath: '/', cached: false, root } }));
			return true;
		})()`, nil),
		chromedp.Evaluate(`document.documentElement.getAttribute('data-cui-vt-kinds')`, new(string)),
	); err != nil {
		t.Fatal(err)
	}
	var kinds string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.getAttribute('data-cui-vt-kinds')`, &kinds))
	if !strings.Contains(kinds, "slide") {
		t.Errorf("documentElement vocabulary = %q after a swap whose payload declared the full set; the module-side copy (gofastr:navigate listener, direct setAttribute) did not run", kinds)
	}
}
