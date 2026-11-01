package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A navigation into a chain whose vocabulary the current page does not
// declare plays the pick its answer names: the gate reads the INCOMING
// answer's data-cui-vt-kinds (its payload root carries it), not the
// outgoing page's copy on <html>, which the commit replaces only inside
// the transition's own update callback.
func TestTransitionPickUsesIncomingVocab(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/"},{"path":"/a","layouts":["l:site"]}` +
		`]</script>`
	// Home declares a transition cell (so the module loads) but no keyed
	// vocabulary; /a's chain declares slide and picks it.
	doc := func(inner string) string {
		return `<!doctype html><html lang="en">` +
			`<head><title>vocab</title>` + routes + `</head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main" data-cui-vt="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "A")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			w.Header().Set("X-Gofastr-Transition", "slide")
			fmt.Fprint(w, `<div data-cui-vt-kinds="slide"><span id="page-a">A</span></div>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`<div data-cui-vt-kinds="slide"><span id="page-a">A</span></div>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`HOME`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.transition)`) {
		t.Fatal("the transition module never loaded on a page with a [data-cui-vt] cell")
	}
	read := captureTypes(ctx)
	if err := chromedp.Run(ctx, chromedp.Click(`#goA`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click: %v", err)
	}
	waitTextContent(t, ctx, `#main`, "A")
	got := read()
	if len(got) != 1 || strings.Join(got[0], ",") != "forward,slide" {
		t.Fatalf("transition types = %v, want [[forward slide]]: the pick was gated by the outgoing page's vocabulary", got)
	}
	var vt string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(history.state && history.state.vt) || ''`, &vt)); err != nil {
		t.Fatal(err)
	}
	if vt != "slide" {
		t.Fatalf("the entry recorded vt %q, want slide (the Back edge mirrors it)", vt)
	}
}
