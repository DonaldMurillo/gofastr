package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// ARCHITECTURE.md "Screen cache + invalidation": an RPC that changes what
// the current screen shows returns island HTML, and the runtime evicts
// the page the write ran on from the screen cache. The entry holds the
// page as fetched, before the write, and the plain navigator replays
// entries as fetched (only the envelope module re-captures the page it
// leaves). Without the eviction a reader who added a row, followed a
// link and pressed Back saw the list without the row until a reload.
// rpc_navigate_evict_e2e_test.go holds the same rule for a write that
// navigates away.

// islandSrv serves /a, a list an island RPC adds to, and /b. Every
// render of /a is counted, so a Back that re-fetches it shows.
type islandSrv struct {
	srv *httptest.Server
	mu  sync.Mutex
	// notes is the server's state; rendersA counts the times /a rendered.
	notes, rendersA int
}

func startIslandSrv(t *testing.T, method string) *islandSrv {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &islandSrv{}
	rows := func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return strings.Repeat(`<li class="note">note</li>`, s.notes)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	// The island's answer: a 200 with its fresh HTML, no navigate and no
	// X-Gofastr-Invalidate (the documented shape for the current screen).
	mux.HandleFunc("/rpc/add", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.notes++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, rows())
	})
	// The toggle an action posts to: it answers 204 and the page paints
	// the new state itself.
	mux.HandleFunc("/rpc/pin", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.notes++
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	routes := `<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"}]</script>`
	page := func(body func() string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				fmt.Fprint(w, body())
				return
			}
			fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
				`<main role="main" tabindex="-1">`+body()+`</main>`+
				`<script src="/__gofastr/runtime.js"></script></body></html>`)
		}
	}
	mux.HandleFunc("/a", page(func() string {
		s.mu.Lock()
		s.rendersA++
		s.mu.Unlock()
		return `<h1 id="screen-a">A</h1>` +
			`<ul id="list" data-cui-signal="notes" data-cui-signal-mode="html">` + rows() + `</ul>` +
			`<button id="add" type="button" data-cui-rpc="/rpc/add" data-cui-rpc-method="` + method + `" data-cui-rpc-signal="notes">Add</button>` +
			`<button id="pin" type="button" data-state="idle">Pin</button>` +
			`<a id="to-b" href="/b">B</a>`
	}))
	mux.HandleFunc("/b", page(func() string { return `<h1 id="screen-b">B</h1>` }))
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// leaveAndReturn follows the link to /b and presses Back, then reports
// how many notes /a shows and whether the document survived.
func leaveAndReturn(shown *int, sameDoc *bool) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(500 * time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('#list .note').length`, shown),
		chromedp.Evaluate(`window.__stamp === 1`, sameDoc),
	}
}

// TestBackAfterIslandMutationShowsServerState: a POST island RPC adds a
// row; a link away and Back must show the row. The write evicted /a, so
// Back fetches it (a second render), in the same document. A GET RPC is
// a read: /a stays cached and Back replays it with no request.
func TestBackAfterIslandMutationShowsServerState(t *testing.T) {
	for _, tc := range []struct {
		method  string
		renders int // of /a, by the end
	}{{"POST", 2}, {"GET", 1}} {
		t.Run(tc.method, func(t *testing.T) {
			s := startIslandSrv(t, tc.method)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var shown int
			var sameDoc bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(s.srv.URL+"/a"),
				chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
				chromedp.Evaluate(`window.__stamp = 1; true`, nil),
				chromedp.Click(`#add`, chromedp.ByID),
				chromedp.Poll(`document.querySelectorAll('#list .note').length === 1`, nil),
				leaveAndReturn(&shown, &sameDoc),
			); err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if !sameDoc {
				t.Fatal("Back loaded a new document: the eviction must cost a partial fetch, never a reload")
			}
			if s.rendersA != tc.renders {
				t.Errorf("/a rendered %d time(s) after a %s island RPC, a link away and Back; want %d", s.rendersA, tc.method, tc.renders)
			}
			// The GET arm's handler counts too, so its replayed page is one
			// behind by construction; only the write's page must match.
			if tc.method == "POST" && shown != s.notes {
				t.Fatalf("Back to /a after an island mutation on it: the list shows %d note(s), the server holds %d", shown, s.notes)
			}
		})
	}
}

// TestBackAfterActionShowsServerState: the action primitive (the toggle
// and optimistic adapters' request) is a write too. Its page paints the
// new state itself, so the cached copy is stale the same way.
func TestBackAfterActionShowsServerState(t *testing.T) {
	s := startIslandSrv(t, "POST")
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var shown int
	var sameDoc bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.__stamp = 1; window.__gofastr.loadModule('action').then(() => { window.__gofastr.action.bind(document.getElementById('pin'), { endpoint: '/rpc/pin' }); window.__armed = true; }); true`, nil),
		chromedp.Poll(`window.__armed === true`, nil),
		chromedp.Click(`#pin`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('pin').getAttribute('data-state') === 'committed'`, nil),
		leaveAndReturn(&shown, &sameDoc),
	); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameDoc {
		t.Fatal("Back loaded a new document")
	}
	if shown != s.notes || s.rendersA != 2 {
		t.Fatalf("Back to /a after an action on it: the list shows %d note(s), the server holds %d; /a rendered %d time(s), want 2", shown, s.notes, s.rendersA)
	}
}
