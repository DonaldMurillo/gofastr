//go:build chromium

package sitesearch

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	pageInput  = `document.querySelector('[data-search-page-input]')`
	pageStatus = `document.querySelector('[data-search-page-status]').textContent`
	pageTitles = `[...document.querySelectorAll('[data-search-page-list] .fui-search-results__title')].map(a => a.textContent).join('|')`
)

// typeQuery sets the page's input as a reader would and fires input.
func typeQuery(q string) chromedp.Action {
	return chromedp.Evaluate(pageInput+`.value = `+jsString(q)+`; `+pageInput+`.dispatchEvent(new Event('input', {bubbles: true}))`, nil)
}

// On a live server, typing updates the status, the list and the pager
// in place from the server's render: the page never reloads, the input
// the reader types in stays the same element, and the address follows
// the query without adding history entries. Submitting runs at once,
// and the server's pager links still page.
func TestPageUpdatesInPlace(t *testing.T) {
	app, _ := searchSite(t, 25)
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	ctx := browser(t)

	var search string
	var kept, sameInput, histKept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search?q=walrus"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['sitesearch-page'])`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.kept = 1; `+pageInput+`.kept = 1; window.hist = history.length`, nil),
		typeQuery("valid"),
		chromedp.Poll(pageStatus+` === '1 result for “valid”'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`location.search`, &search),
		chromedp.Evaluate(`window.kept === 1 && !document.querySelector('[data-search-page-pager]')`, &kept),
		chromedp.Evaluate(pageInput+`.kept === 1`, &sameInput),
		chromedp.Evaluate(`history.length === window.hist`, &histKept),
	); err != nil {
		t.Fatalf("typing: %v", err)
	}
	if search != "?q=valid" {
		t.Errorf("address search = %q, want ?q=valid", search)
	}
	if !kept {
		t.Error("the page reloaded, or the pager outlived a one-result query")
	}
	if !sameInput {
		t.Error("the input the reader types in was replaced")
	}
	if !histKept {
		t.Error("typing added history entries")
	}

	var titles string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(pageInput+`.value = 'walrus'`, nil),
		chromedp.SendKeys(`[data-search-page-input]`, "\r", chromedp.ByQuery),
		chromedp.Poll(pageStatus+` === '25 results for “walrus”' && !!document.querySelector('[data-search-page-pager] a[href*="page=2"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`[data-search-page-pager] a[href*="page=2"]`, chromedp.ByQuery),
		chromedp.Poll(`location.search.includes('page=2') && document.querySelectorAll('[data-search-page-list] > li').length === 5`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.kept === 1`, &kept),
		chromedp.Evaluate(pageTitles, &titles),
	); err != nil {
		t.Fatalf("submit and page: %v", err)
	}
	if !kept {
		t.Error("submitting or paging reloaded the page")
	}
	if !strings.Contains(titles, "Walrus") {
		t.Errorf("page 2 titles = %q", titles)
	}
}

// A slow answer to an earlier query never overwrites a later one.
func TestPageDropsStaleAnswer(t *testing.T) {
	app, _ := searchSite(t, 0)
	h := app.Router()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") != "" && r.URL.Query().Get("q") == "val" {
			time.Sleep(800 * time.Millisecond)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	var status string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['sitesearch-page'])`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		typeQuery("val"),
		chromedp.Sleep(300*time.Millisecond),
		typeQuery("valid"),
		chromedp.Poll(pageStatus+` === '1 result for “valid”'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.Evaluate(pageStatus, &status),
	); err != nil {
		t.Fatalf("queries: %v", err)
	}
	if status != "1 result for “valid”" {
		t.Errorf("status = %q: the earlier, slower answer won", status)
	}
}

// staticPage serves an exported search page: the markup Screen renders
// with no query, the client and page modules, and the index files.
func staticPage(t *testing.T, path string, perPage int) *httptest.Server {
	t.Helper()
	files, err := Build(parityCorpus()).Files()
	if err != nil {
		t.Fatal(err)
	}
	body := ui.SearchResults(ui.SearchResultsConfig{Action: "/search", PerPage: perPage})
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-cui-static><meta charset=utf-8>%s
<script>window.__gofastr = {loadModule: function (n) {
  return new Promise(function (ok) {
    const s = document.createElement('script');
    s.src = '/' + n + '.js';
    s.onload = ok;
    document.head.append(s);
  });
}};</script>
<script src="/sitesearch.js"></script><script src="/sitesearch-page.js"></script>`, body)
	})
	for name, src := range map[string]string{"sitesearch": clientJS, "sitesearch-static": staticJS, "sitesearch-page": pageJS} {
		mux.HandleFunc("/"+name+".js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, src)
		})
	}
	mux.HandleFunc(Root+"{file}", func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.PathValue("file")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// An export renders the page with no query; the ?q= the reader arrived
// with runs on load, ranked in the browser, in the server's markup.
func TestPageRanksOnExport(t *testing.T) {
	srv := staticPage(t, "/search", 20)
	ctx := browser(t)
	var value, titles, href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search?q=server+errors"),
		chromedp.Poll(pageTitles+` !== ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(pageInput+`.value`, &value),
		chromedp.Evaluate(pageTitles, &titles),
		chromedp.Evaluate(`document.querySelector('[data-search-page-list] .fui-search-results__section').getAttribute('href')`, &href),
	); err != nil {
		t.Fatalf("load: %v", err)
	}
	if value != "server errors" {
		t.Errorf("input = %q, want the query from the address", value)
	}
	if !strings.HasPrefix(titles, "Forms") {
		t.Errorf("titles = %q, want Forms first", titles)
	}
	if href != "/docs/forms#server-errors" {
		t.Errorf("section href = %q", href)
	}
}

// With more matches than a page holds, an export offers the next ones
// behind a button, there being no server to page.
func TestPageShowsMoreOnExport(t *testing.T) {
	srv := staticPage(t, "/search", 1)
	ctx := browser(t)
	var before, after int
	var status string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search?q=form"),
		chromedp.Poll(`!!document.querySelector('[data-search-page-pager] button')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[data-search-page-list] > li').length`, &before),
		chromedp.Evaluate(pageStatus, &status),
		chromedp.Click(`[data-search-page-pager] button`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('[data-search-page-list] > li').length > 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[data-search-page-list] > li').length`, &after),
	); err != nil {
		t.Fatalf("more: %v", err)
	}
	if before != 1 || after != 2 {
		t.Errorf("results %d then %d, want 1 then 2", before, after)
	}
	if !strings.Contains(status, "results for “form”") {
		t.Errorf("status = %q, want the full count", status)
	}

	// The button the reader pressed is gone: focus moves to the first
	// result it revealed. A translation naming {q} or {n} twice gets
	// both filled.
	var focused bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.activeElement === document.querySelectorAll('[data-search-page-list] > li')[1].querySelector('a')`, &focused),
		chromedp.Evaluate(`document.querySelector('[data-search-page]').setAttribute('data-search-page-count', '{n} ({n}) for {q}, {q}')`, nil),
		chromedp.Click(`[data-search-page-pager] button`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('[data-search-page-list] > li').length > 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(pageStatus, &status),
	); err != nil {
		t.Fatalf("focus: %v", err)
	}
	if !focused {
		t.Error("focus did not move to the first revealed result")
	}
	if strings.Contains(status, "{") || !strings.Contains(status, ", form") {
		t.Errorf("status = %q, want every placeholder filled", status)
	}
}

// Typing, then leaving before the debounce fires: the pending query
// must not write the destination's address or fetch for it. An answer
// still in flight when the reader leaves must not act either, not even
// a redirect.
func TestPageLeavingDropsPendingQuery(t *testing.T) {
	app, _ := searchSite(t, 1)
	h := app.Router()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") != "" && r.URL.Query().Get("q") == "slow" {
			time.Sleep(800 * time.Millisecond)
			w.Header().Set("X-Gofastr-Location", "/search?q=walrus")
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	var addr string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['sitesearch-page'])`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		typeQuery("valid"),
		chromedp.Evaluate(`window.__gofastr.navigate('/walrus/0')`, nil),
		chromedp.Poll(`location.pathname === '/walrus/0' && !document.querySelector('[data-search-page]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`location.pathname + location.search`, &addr),
	); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if addr != "/walrus/0" {
		t.Errorf("address = %q: the pending query wrote it", addr)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['sitesearch-page'])`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		typeQuery("slow"),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.navigate('/walrus/0')`, nil),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`location.pathname + location.search`, &addr),
	); err != nil {
		t.Fatalf("leave in flight: %v", err)
	}
	if addr != "/walrus/0" {
		t.Errorf("address = %q: an answer that arrived after leaving acted", addr)
	}
}

// The live answer is a navigation answer: a session rollover reaches
// the stream meta, and a policy redirect is followed by the router
// rather than read as an empty page.
func TestPageFollowsRouterHeaders(t *testing.T) {
	app, _ := searchSite(t, 3)
	h := app.Router()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") != "" && r.URL.Query().Get("q") == "gone" {
			w.Header().Set("X-Gofastr-Session", "fresh")
			w.Header().Set("X-Gofastr-Location", "/search?q=walrus")
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	var meta string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/search"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['sitesearch-page'])`, nil,
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(document.querySelector('meta[name="gofastr-sse"]') || document.head.appendChild(Object.assign(document.createElement('meta'), {name: 'gofastr-sse'}))).setAttribute('content', '/__gofastr/sse?session=old')`, nil),
		typeQuery("gone"),
		chromedp.Poll(`location.search === '?q=walrus' && `+pageStatus+` === '3 results for “walrus”'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]').getAttribute('content')`, &meta),
	); err != nil {
		t.Fatalf("redirect: %v", err)
	}
	if !strings.Contains(meta, "session=fresh") {
		t.Errorf("stream meta = %q, want the rolled-over session", meta)
	}
}

// Markup naming another page as its search page is left alone: the
// form stays a plain GET there.
func TestPageLeavesOtherPagesAlone(t *testing.T) {
	srv := staticPage(t, "/components/searchresults", 20)
	ctx := browser(t)
	var value, titles string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/components/searchresults?q=form"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._sitesearch)`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(pageInput+`.value`, &value),
		chromedp.Evaluate(pageTitles, &titles),
	); err != nil {
		t.Fatalf("load: %v", err)
	}
	if value != "" || titles != "" {
		t.Errorf("the module ran on another page: input %q, titles %q", value, titles)
	}
}
