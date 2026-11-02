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

// A navigation's view transition skipped before it is ready (a click
// during the animation, or the next navigation committing over it)
// rejects the transition's ready promise with "Transition was skipped".
// The runtime held that promise with no handler, so every skip surfaced
// as an unhandled rejection: a page error in the console and in any
// error tracker, found by rage-clicking the tracker's issue list. The
// skip itself is the runtime's own choice and must stay silent.
func TestSkippedTransitionRaisesNoUnhandledRejection(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := `<script type="application/json" id="gofastr-routes">[` +
		`{"path":"/","layouts":["l:site"]},{"path":"/a","layouts":["l:site"]}` +
		`]</script>`
	doc := func(inner string) string {
		return `<!doctype html><html lang="en">` +
			`<head><title>skip</title>` + routes + `</head><body>` +
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
			fmt.Fprint(w, `<span id="page-a">A</span>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`<span id="page-a">A</span>`))
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
	// Record unhandled rejections, and skip the navigation's transition
	// the moment it starts, through the runtime's own click-during-
	// animation path: a click that lands on <html>.
	var armed bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__rejections = [];
		window.__started = 0;
		window.addEventListener('unhandledrejection', (e) => {
			window.__rejections.push(String(e.reason && (e.reason.message || e.reason)));
		});
		const sVT = document.startViewTransition;
		if (!sVT || !('activeViewTransition' in document)) return false;
		document.startViewTransition = function (opts) {
			const vt = sVT.call(document, opts);
			window.__started++;
			document.documentElement.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, clientX: 1, clientY: 1 }));
			return vt;
		};
		return true;
	})()`, &armed)); err != nil {
		t.Fatal(err)
	}
	if !armed {
		t.Skip("this browser has no view transitions with document.activeViewTransition")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#goA`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click: %v", err)
	}
	waitTextContent(t, ctx, `#main`, "A")
	if !pollTrue(ctx, `window.__started >= 1`) {
		t.Fatal("the navigation never started a view transition, so nothing was skipped")
	}
	// Let the skipped promises settle and any unhandled rejection fire.
	if err := chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var rejections []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__rejections`, &rejections)); err != nil {
		t.Fatal(err)
	}
	if len(rejections) != 0 {
		t.Fatalf("a skipped view transition raised unhandled rejections: %q", rejections)
	}
}
