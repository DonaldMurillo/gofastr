package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A reveal section captured before it scrolled into view comes back
// from the live-DOM cache as a copy of its hidden markup. The copy is a
// new element: the module must observe it again, so it reveals when
// scrolled to. The observed flag used to live in an attribute, which the
// copy carried, so the replayed section stayed invisible.
func TestRevealReplayedSectionReveals(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/__gofastr/runtime/"):]
		name = name[:len(name)-len(".js")]
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>reveal</title></head><body>
  <main role="main" id="main"><span id="ready">ready</span>
    <div style="height:3000px"></div>
    <section id="sec" data-cui-reveal="fade-up">below the fold</section>
  </main>
  <script src="/__gofastr/runtime.js"></script>
  <script>window.__gofastr.loadModule('reveal').then(function () { window.__ready = true; });</script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__ready === true && document.getElementById('sec').classList.contains('cui-hidden')`) {
		t.Fatal("the reveal module never hid the below-the-fold section")
	}
	// The cache round trip: capture the markup while the section is still
	// hidden, put a copy back, and announce the navigation the way a
	// replay does.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const main = document.getElementById('main');
  const html = main.innerHTML;
  main.innerHTML = '';
  main.innerHTML = html;
  window.dispatchEvent(new CustomEvent('gofastr:navigate', { detail: { root: main } }));
  document.getElementById('sec').scrollIntoView();
})()`, nil)); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `document.getElementById('sec').classList.contains('cui-revealed') && !document.getElementById('sec').classList.contains('cui-hidden')`) {
		var cls string
		chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('sec').className`, &cls))
		t.Fatalf("the replayed section never revealed after scrolling to it (class %q)", cls)
	}
}
