package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A click that lands while a view transition runs hit-tests to <html>;
// the transition module holds it and delivers it once the transition
// has finished. A later click that reaches its element in the meantime
// used to run first, and the held click then landed after it: two quick
// clicks on two links ended on the FIRST link's page (the tracker's
// TestTrackerSupersedingPointerClicksKeepRingOff failed about once in
// fifteen runs under load for this reason). Clicks keep their order:
// the page the user clicked last is the page shown.
func TestHeldClickNeverLandsAfterALaterClick(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/","layouts":["l:site"]},{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}` +
		`]</script>`
	doc := func(inner string) string {
		return `<!doctype html><html lang="en">` +
			`<head><title>order</title>` + routes + `</head><body>` +
			`<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a> <a id="goB" href="/b">B</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main" data-cui-vt="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	page := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", body)
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprintf(w, `<span id="page">%s</span>`, body)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, doc(`<span id="page">`+body+`</span>`))
		})
	}
	page("/a", "PAGE-A")
	page("/b", "PAGE-B")
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

	// hold starts a view transition whose update the test releases, and
	// sends a click to <html> at #goA's centre while it runs: the click
	// the module holds for #goA.
	const hold = `(() => {
		if (!document.startViewTransition || !('activeViewTransition' in document)) return false;
		document.startViewTransition({ update: () => new Promise((r) => { window.__release = r; }) });
		const b = document.querySelector('#goA').getBoundingClientRect();
		document.documentElement.dispatchEvent(new MouseEvent('click', {
			bubbles: true, cancelable: true, detail: 1,
			clientX: b.x + b.width / 2, clientY: b.y + b.height / 2,
		}));
		return true;
	})()`

	cases := []struct {
		name, between, wantPath, wantText string
	}{
		// With no later click the held one is delivered: the fix must
		// not simply drop held clicks.
		{"held click alone is delivered", ``, "/a", "PAGE-A"},
		{"later click wins", `document.querySelector('#goB').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, detail: 1}));`, "/b", "PAGE-B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/")); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.transition)`) {
				t.Fatal("the transition module never loaded on a page with a [data-cui-vt] cell")
			}
			var armed bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(hold, &armed)); err != nil {
				t.Fatal(err)
			}
			if !armed {
				t.Skip("this browser has no view transitions with document.activeViewTransition")
			}
			if !pollTrue(ctx, `typeof window.__release === 'function'`) {
				t.Fatal("the held transition's update never ran, so no click was held behind it")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(tc.between+` 1`, nil),
				chromedp.Evaluate(`window.__release(); 1`, nil),
			); err != nil {
				t.Fatal(err)
			}
			waitTextContent(t, ctx, `#main`, tc.wantText)
			// A held click delivered late would navigate again after the
			// page settled; give it the time to.
			var path, text string
			if err := chromedp.Run(ctx,
				chromedp.Sleep(500*time.Millisecond),
				chromedp.Evaluate(`window.__gofastr.currentPath`, &path),
				chromedp.Evaluate(`document.querySelector('#main').textContent`, &text),
			); err != nil {
				t.Fatal(err)
			}
			if path != tc.wantPath || text != tc.wantText {
				t.Fatalf("landed on %s showing %q, want %s showing %q", path, text, tc.wantPath, tc.wantText)
			}
		})
	}
}
