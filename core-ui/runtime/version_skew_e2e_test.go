package runtime_test

// Version-skew e2e: the envelope version is negotiated in both
// directions and a mismatch always ends in a full load.
//
// TestNewRuntimeOnOldServer: today's runtime navigates against a
// server that answers plain partials (no X-Gofastr-Envelope, no fill
// templates). A partial whose document holds an outlet or area outside
// the target slot full-loads instead of applying; the outlets never go
// stale. The other direction, a stale runtime on today's server, holds
// no pinned bundle of an earlier release: a client that speaks none of
// the page's attributes intercepts nothing, and the browser's own
// navigation is the full load.

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
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` +
			`<div data-cui-outlet="l:site#aside" id="aside">` + aside + `</div>` +
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
