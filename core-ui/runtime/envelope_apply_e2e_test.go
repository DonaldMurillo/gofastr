package runtime_test

// Browser tests for the envelope APPLY transaction's recovery rules
// (docs/DESIGN-layout-outlets.md, "Client apply algorithm"): every
// fill target must resolve exactly once and NO target may contain
// another — either miss recovers with a full-page load instead of a
// half-applied envelope — and a fill's bytes are applied as inert
// markup: scripts parse but never run.
//
// The rig is a hand-rolled server (the module_demand harness shape):
// it serves today's runtime, a boot page with outlet/area markers,
// and a configurable envelope answer for the /a navigation, so a test
// can ship wire shapes the app would never produce (that is the
// point: the client must recover, not trust).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// envelopeApplyRig serves the new runtime against one boot page and
// one navigation target whose partial answer is the test's envelope.
type envelopeApplyRig struct {
	srv *httptest.Server

	// envelope is the partial answer's body for /a (fill templates).
	envelope string

	mu      sync.Mutex
	partial map[string]int
	full    map[string]int
}

func (r *envelopeApplyRig) partials(p string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partial[p]
}

func (r *envelopeApplyRig) fulls(p string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full[p]
}

// newEnvelopeApplyRig builds the rig. markers is the boot page's
// region markup (carried verbatim into the served documents so the
// client's fills can resolve).
func newEnvelopeApplyRig(t *testing.T, markers string) *envelopeApplyRig {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	r := &envelopeApplyRig{partial: map[string]int{}, full: map[string]int{}}
	page := func(inner string) string {
		return `<!doctype html><html lang="en"><head><title>apply</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/a","layouts":["l:site"]}]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` + markers +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
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
		if req.Header.Get("X-Gofastr-Navigate") == "1" && req.URL.Path == "/a" && r.envelope != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "A")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			w.Header().Set("X-Gofastr-Envelope", "2")
			fmt.Fprint(w, r.envelope)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(`<p id="screen-a">A-CONTENT</p>`))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// TestNestedTargetsFullLoads: an envelope whose fills address two
// NESTED regions — the outer area cell contains the inner outlet
// cell. Applying both would write the inner fill into a node the
// outer write is about to orphan (content lost silently, the DOM
// half-applied); the containment scan treats it like any other miss
// and the runtime full-loads the destination.
func TestNestedTargetsFullLoads(t *testing.T) {
	rig := newEnvelopeApplyRig(t, `<div data-cui-area="l:site~wrap" id="wrap"><div data-cui-outlet="l:site#aside" id="aside">HOME-ASIDE</div></div>`)
	rig.envelope = `<template data-cui-fill="l:site">A-CONTENT</template>` +
		`<template data-cui-fill="l:site~wrap"><div data-cui-outlet="l:site#aside" id="aside">WRAP-OUTER</div></template>` +
		`<template data-cui-fill="l:site#aside">INNER-ASIDE</template>`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var mainTxt, asideTxt string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),

		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('main')?.textContent || ''`, &mainTxt),
		chromedp.Evaluate(`document.getElementById('aside')?.textContent || ''`, &asideTxt),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rig.partials("/a") < 1 {
		t.Errorf("partial requests for /a = %d, want >= 1 (the click's fetch)", rig.partials("/a"))
	}
	if rig.fulls("/a") < 1 {
		t.Errorf("full-document requests for /a = %d, want >= 1 (nested fill targets must full-load, not apply)", rig.fulls("/a"))
	}
	if !strings.Contains(mainTxt, "A-CONTENT") {
		t.Errorf("main = %q, want the destination's A-CONTENT", mainTxt)
	}
	if !strings.Contains(asideTxt, "HOME-ASIDE") {
		t.Errorf("aside = %q, want the destination document's own content (never a half-applied fill)", asideTxt)
	}
}

// TestEnvelopeMissingTargetFullLoads: an envelope whose fills name an
// address no DOM holds (l:site#ghost) is a deploy-skew disagreement —
// the primary must never half-apply; the destination arrives through a
// full-document load.
func TestEnvelopeMissingTargetFullLoads(t *testing.T) {
	rig := newEnvelopeApplyRig(t, `<div data-cui-outlet="l:site#aside" id="aside">HOME-ASIDE</div>`)
	rig.envelope = `<template data-cui-fill="l:site">A-CONTENT</template>` +
		`<template data-cui-fill="l:site#ghost">GHOST</template>`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var mainTxt, asideTxt string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),

		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('main')?.textContent || ''`, &mainTxt),
		chromedp.Evaluate(`document.getElementById('aside')?.textContent || ''`, &asideTxt),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rig.fulls("/a") < 1 {
		t.Errorf("full-document requests for /a = %d, want >= 1 (an unresolvable fill address must full-load, not apply)", rig.fulls("/a"))
	}
	if !strings.Contains(mainTxt, "A-CONTENT") {
		t.Errorf("main = %q, want the destination's A-CONTENT", mainTxt)
	}
	if asideTxt != "HOME-ASIDE" {
		t.Errorf("aside = %q, want the destination document's own content (the ghost fill never applied)", asideTxt)
	}
}

// TestEnvelopeScriptsDoNotRun: a fill's bytes are markup, not code —
// parsed into the inert detached template and applied through
// innerHTML, so a <script> in a fill lands as a (present) inert node
// and never executes; an onerror handler with no src never fires.
func TestEnvelopeScriptsDoNotRun(t *testing.T) {
	rig := newEnvelopeApplyRig(t, `<div data-cui-outlet="l:site#aside" id="aside">HOME-ASIDE</div>`)
	rig.envelope = `<template data-cui-fill="l:site">A-CONTENT</template>` +
		`<template data-cui-fill="l:site#aside"><script>window.__pwned=1</script><img onerror="window.__imgPwned=1"><span id="fill-body">FILL</span></template>`
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var pwned, imgPwned, hasScript bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),

		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.WaitVisible(`#fill-body`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__pwned === undefined`, &pwned),
		chromedp.Evaluate(`window.__imgPwned === undefined`, &imgPwned),
		chromedp.Evaluate(`!!document.querySelector('#aside script')`, &hasScript),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !pwned {
		t.Error("a fill's <script> executed (window.__pwned set) — fills must apply as inert markup")
	}
	if !imgPwned {
		t.Error("a fill's <img onerror> fired — fills must apply as inert markup")
	}
	if !hasScript {
		t.Error("the script NODE should be present and inert (parsed, never executed)")
	}
}
