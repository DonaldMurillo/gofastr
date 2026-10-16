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

// #503: a link that opts in to highlighting (data-cui-match-prefix or
// data-cui-activelink) but sits outside a <nav> landmark was never
// marked, with no error. Opt-in links light up wherever they are; plain
// links outside nav stay untouched, as before.
func TestActiveLinkOptInOutsideNav(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/activelink.js", func(w http.ResponseWriter, _ *http.Request) {
		src, _ := Module("activelink")
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>t</title></head><body>
  <header>
    <a id="issues" href="/issues" data-cui-match-prefix="">Issues</a>
    <a id="board" href="/board" data-cui-activelink>Board</a>
    <a id="plain" href="/issues/42">Plain</a>
  </header>
  <main>%s</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, r.URL.Path)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t)

	var issues, plain string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/issues/42"),
		chromedp.WaitVisible(`#issues`, chromedp.ByID),
		// The module loads idle: wait for its sweep to mark the link, then
		// read the plain link, which that same sweep would have touched.
		chromedp.Poll(`document.getElementById('issues').getAttribute('aria-current')`, &issues, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.getElementById('plain').getAttribute('aria-current')`, &plain),
	); err != nil {
		t.Fatalf("chromedp /issues/42: %v", err)
	}
	if issues != "page" {
		t.Errorf(`a header link with data-cui-match-prefix must be active on /issues/42, got aria-current=%q`, issues)
	}
	if plain == "page" {
		t.Errorf(`a plain link outside nav must stay untouched, got aria-current=%q`, plain)
	}

	var board string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/board"),
		chromedp.WaitVisible(`#board`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('board').getAttribute('aria-current')`, &board, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("chromedp /board: %v", err)
	}
	if board != "page" {
		t.Errorf(`a header link with data-cui-activelink must be active on its own page, got aria-current=%q`, board)
	}
}
