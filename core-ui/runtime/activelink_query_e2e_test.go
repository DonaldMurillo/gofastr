package runtime

// A query-less href names a screen, and the URL's query is that
// screen's in-page state (sort, page, filter live in the query string),
// so /ask stays current on /ask?page=2. The server marks it that way;
// a client navigation must not clear it. An href that carries its own
// query still needs the exact URL.

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

const activelinkQueryPage = `<!doctype html><html><head><title>activelink query</title>
  <nav aria-label="Sections">
    <a id="ask" href="/ask" data-cui-activelink>ask</a>
    <a id="open" href="/orders?status=open" data-cui-activelink>open</a>
  </nav>
  <main id="main">ready</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`

func TestActiveLinkIgnoresScreenQuery(t *testing.T) {
	srv := activelinkServer(t, activelinkQueryPage)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#main`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._updateActiveLink)`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var got map[string]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const u = window.__gofastr._updateActiveLink;
		const g = (id) => document.getElementById(id).getAttribute('aria-current') || '-';
		const out = {};
		for (const p of ['/ask?page=2', '/orders?status=closed', '/orders?status=open']) {
			u(p, true);
			out[p] = g('ask') + ' ' + g('open');
		}
		return out;
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/ask?page=2":           "page -",
		"/orders?status=closed": "- -",
		"/orders?status=open":   "- page",
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("at %s: ask/open marks = %q, want %q", p, got[p], w)
		}
	}
}
