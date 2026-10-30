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

// evictSrv serves a record screen whose buttons run an RPC and then
// navigate to the list: a DELETE (the entityui delete action) and a GET.
// Every screen renders "name N", N being that path's fetch count, so a
// repeat of N on Back is a cache hit and a bump is a re-fetch. Under
// prefix "/es" the same screens answer with the locale prefix, which
// the screen cache keeps in its keys.
func evictSrv(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	counts := map[string]int{}
	bump := func(uri string) int {
		mu.Lock()
		defer mu.Unlock()
		counts[uri]++
		return counts[uri]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		src, ok := Module(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p, rest := "", r.URL.Path
		if strings.HasPrefix(rest, "/es/") {
			p, rest = "/es", strings.TrimPrefix(rest, "/es")
		}
		var body string
		switch rest {
		case "/api/rec/7":
			// The runtime sends a page's RPCs under its locale prefix.
			w.WriteHeader(http.StatusNoContent)
			return
		case "/rec/7":
			body = fmt.Sprintf(`<h1 id="v">rec %d</h1>
<button id="del" data-cui-rpc="/api/rec/7" data-cui-rpc-method="DELETE" data-cui-rpc-navigate="%s/list">d</button>
<button id="peek" data-cui-rpc="/api/rec/7" data-cui-rpc-method="GET" data-cui-rpc-navigate="%s/list">p</button>`, bump(r.URL.RequestURI()), p, p)
		case "/list":
			body = fmt.Sprintf(`<h1 id="v">list %d</h1>`, bump(r.URL.RequestURI()))
		case "/":
			body = fmt.Sprintf(`<a id="rec" href="%s/rec/7?tab=1">rec</a>`, p)
		default:
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>evict</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/rec/7"},{"path":"/list"}]</script>
<script type="application/json" id="gofastr-locales">{"prefixes":["es"]}</script></head><body>
<main role="main" tabindex="-1">%s</main><span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script></body></html>`, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A delete that navigates away leaves its own page stale: Back served
// the deleted record from the screen cache. A successful unsafe RPC now
// evicts the page it ran on before it navigates; a GET RPC keeps it.
func TestUnsafeRPCEvictsItsPage(t *testing.T) {
	for _, prefix := range []string{"", "/es"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			srv := evictSrv(t)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			back := chromedp.Evaluate(`history.back()`, nil)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+prefix+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Poll(`window.__gofastr && typeof window.__gofastr.navigate === 'function'`,
					nil, chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
			); err != nil {
				t.Fatal(err)
			}
			steps := []struct {
				name string
				act  chromedp.Action
				want string
			}{
				{"open the record", chromedp.Click("#rec", chromedp.ByID), "rec 1"},
				{"GET RPC, then navigate", chromedp.Click("#peek", chromedp.ByID), "list 1"},
				{"Back after the GET", back, "rec 1"},
				{"DELETE RPC, then navigate", chromedp.Click("#del", chromedp.ByID), "list 2"},
				{"Back after the DELETE", back, "rec 2"},
			}
			for _, s := range steps {
				if err := chromedp.Run(ctx, s.act, waitText("#v", s.want)); err != nil {
					var got, at string
					_ = chromedp.Run(ctx,
						chromedp.Evaluate(`document.querySelector('#v')?.textContent`, &got),
						chromedp.Evaluate(`location.pathname + location.search`, &at))
					t.Fatalf("%s: screen = %q at %s, want %q: %v", s.name, got, at, s.want, err)
				}
			}
		})
	}
}
