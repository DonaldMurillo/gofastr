package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// navGapSite serves the plain navigator (no envelope module) over pages
// the test describes: each page is a partial body for a navigation and a
// whole document for a load, optionally inside the l:site shell.
type navGapSite struct {
	srv   *httptest.Server
	mu    sync.Mutex
	hits  map[string]int
	pages map[string]navGapPage
}

type navGapPage struct {
	shell  bool
	inner  func(hit int) (status int, html string)
	header http.Header
}

func newNavGapSite(t *testing.T, routes string, pages map[string]navGapPage) *navGapSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &navGapSite{hits: map[string]int{}, pages: pages}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		s.hits[r.URL.Path]++
		hit := s.hits[r.URL.Path]
		s.mu.Unlock()
		for k, v := range p.header {
			w.Header()[k] = v
		}
		status, inner := p.inner(hit)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "t")
			if p.shell {
				w.Header().Set("X-Gofastr-Swap", "l:site")
			}
			w.WriteHeader(status)
			fmt.Fprint(w, inner)
			return
		}
		main := `<main role="main" tabindex="-1">` + inner + `</main>`
		if p.shell {
			main = `<div data-cui-layout="site" data-cui-layout-key="l:site">` +
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">` + inner + `</main></div>`
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">`+routes+`</script>`+
			`</head><body>`+main+`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *navGapSite) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func okPage(html string) func(int) (int, string) {
	return func(int) (int, string) { return http.StatusOK, html }
}

// A prefetched layout-less page is not swapped into a layout shell: the
// click leaves the shell the way an unprefetched one does, in the plain
// navigator and in the envelope one (an outlet marker hands it over).
func TestPrefetchedBarePageLeavesShell(t *testing.T) {
	for name, marker := range map[string]string{"plain": "", "envelope": `<div data-cui-outlet="side"></div>`} {
		t.Run(name, func(t *testing.T) {
			routes := `[{"path":"/hub","layouts":["l:site"]},{"path":"/bare","preload":"eager"}]`
			s := newNavGapSite(t, routes, map[string]navGapPage{
				"/hub":  {shell: true, inner: okPage(`<h1 id="hub">Hub</h1><a id="to-bare" href="/bare">Bare</a>` + marker)},
				"/bare": {inner: okPage(`<p id="bare">BARE</p>`)},
			})
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(s.srv.URL+"/hub"),
				chromedp.WaitVisible(`#hub`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for s.count("/bare") == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the eager route was never prefetched")
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(200 * time.Millisecond)
			var shells int
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.getElementById('to-bare').click()`, nil),
				chromedp.WaitVisible(`#bare`, chromedp.ByID),
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(`document.querySelectorAll('[data-cui-layout-key]').length`, &shells),
			); err != nil {
				t.Fatal(err)
			}
			if shells != 0 {
				t.Errorf("the layout-less page kept %d layout shell(s) around it", shells)
			}
		})
	}
}

// An error page a cross-chain navigation fetched is not cached: going
// back to it fetches it again.
func TestCrossChainErrorPageNotCached(t *testing.T) {
	routes := `[{"path":"/hub","layouts":["l:site"]},{"path":"/oops"},{"path":"/other"}]`
	s := newNavGapSite(t, routes, map[string]navGapPage{
		"/hub": {shell: true, inner: okPage(`<h1 id="hub">Hub</h1><a id="to-oops" href="/oops">Oops</a>`)},
		"/oops": {inner: func(hit int) (int, string) {
			if hit == 1 {
				return http.StatusInternalServerError, `<p id="m">OOPS</p><a id="to-other" href="/other">Other</a>`
			}
			return http.StatusOK, `<p id="m">FIXED</p><a id="to-other" href="/other">Other</a>`
		}},
		"/other": {inner: okPage(`<p id="m">OTHER</p>`)},
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var text string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('to-oops').click()`, nil),
		chromedp.WaitVisible(`#to-other`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('to-other').click()`, nil),
		chromedp.WaitNotPresent(`#to-other`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#to-other`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Text(`#m`, &text, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if n := s.count("/oops"); n != 2 || text != "FIXED" {
		t.Errorf("back to /oops showed %q after %d fetch(es), want FIXED after 2 (the 500 page replayed from the cache)", text, n)
	}
}

// X-Gofastr-Session on a navigation answer is an identity change: the
// plain navigator drops its cache, as the envelope navigator does.
func TestNavSessionChangeClearsCache(t *testing.T) {
	routes := `[{"path":"/"},{"path":"/a"},{"path":"/b"}]`
	s := newNavGapSite(t, routes, map[string]navGapPage{
		"/":  {inner: okPage(`<p id="m">HOME</p><a id="to-a" href="/a">A</a>`)},
		"/a": {inner: okPage(`<p id="m">PAGE-A</p><a id="to-b" href="/b">B</a>`)},
		"/b": {inner: okPage(`<p id="m">PAGE-B</p>`), header: http.Header{"X-Gofastr-Session": {"fresh-id"}}},
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#to-a`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('to-a').click()`, nil),
		chromedp.WaitVisible(`#to-b`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('to-b').click()`, nil),
		chromedp.WaitNotPresent(`#to-b`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#to-b`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if n := s.count("/a"); n != 2 {
		t.Errorf("fetches of /a after Back = %d, want 2 (the session change must clear the cache)", n)
	}
}
