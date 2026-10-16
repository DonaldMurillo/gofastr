package runtime

// A query-less href names a screen, and the URL's query is that
// screen's in-page state (sort, page, filter live in the query string),
// so /ask stays current on /ask?page=2. The server marks it that way;
// a client navigation must not clear it. An href that carries its own
// query still needs the exact URL. A nav that holds query-carrying links
// to the same path is a tab strip whose current tab the server picks,
// so its query-less "All" tab also needs the exact URL: it stays dark
// on every filtered or paged view.

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

const activelinkQueryPage = `<!doctype html><html><head><title>activelink query</title>
  <nav aria-label="Sections">
    <a id="ask" href="/ask" data-cui-activelink>ask</a>
    <a id="all" href="/orders">all</a>
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
		for (const p of ['/ask?page=2', '/orders', '/orders?status=closed', '/orders?status=open', '/orders?status=open&page=2']) {
			u(p, true);
			out[p] = g('ask') + ' ' + g('all') + ' ' + g('open');
		}
		return out;
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/ask?page=2":                "page - -",
		"/orders":                    "- page -",
		"/orders?status=closed":      "- - -",
		"/orders?status=open":        "- - page",
		"/orders?status=open&page=2": "- - -",
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("at %s: ask/all/open marks = %q, want %q", p, got[p], w)
		}
	}
}

// A static export links its pages as /about/ (the form the host serves
// without a redirect), while a link it did not rewrite (a search result,
// a widget's chrome) lands on /about. Both name one page, so either URL
// marks either link; "/" stays the root alone.
const activelinkSlashPage = `<!doctype html><html><head><title>activelink slash</title>
  <nav aria-label="Sections">
    <a id="root" href="/">home</a>
    <a id="about" href="/about/">about</a>
    <a id="docs" href="/docs">docs</a>
  </nav>
  <main id="main">ready</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`

func TestActiveLinkIgnoresTrailingSlash(t *testing.T) {
	srv := activelinkServer(t, activelinkSlashPage)
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
		for (const p of ['/about', '/about/', '/docs/', '/', '/about/team']) {
			u(p, true);
			out[p] = g('root') + ' ' + g('about') + ' ' + g('docs');
		}
		return out;
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/about":      "- page -",
		"/about/":     "- page -",
		"/docs/":      "- - page",
		"/":           "page - -",
		"/about/team": "- - -",
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("at %s: root/about/docs marks = %q, want %q", p, got[p], w)
		}
	}
}
