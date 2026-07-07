package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A post-mutation navigation (data-fui-rpc-navigate) calls
// loadPage(path, {bypassCache:true}). When the server answers that
// fetch with X-Gofastr-Location, the recursive loadPage for the
// redirect target must KEEP bypassing the screen cache — otherwise the
// redirect destination is served from a stale cached copy even though
// the RPC just mutated the state it displays.
func TestRedirectNavBypassesStaleCache(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var staleN atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// /stale — content version bumps on every fetch. First visit
	// caches v1; the post-mutation redirect must show v2, not v1.
	mux.HandleFunc("/stale", func(w http.ResponseWriter, r *http.Request) {
		n := staleN.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprintf(w, `<h1 id="v">v%d</h1><a id="home" href="/">home</a>`, n)
	})
	// /redir — server policy redirect: 200 + X-Gofastr-Location.
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Location", "/stale")
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	})
	mux.HandleFunc("/mutate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>redirect-bypass</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/stale"},{"path":"/redir"}]</script>
</head><body>
  <main role="main" tabindex="-1">
    <a id="tostale" href="/stale">stale</a>
    <button id="mut" data-fui-rpc="/mutate" data-fui-rpc-method="POST"
            data-fui-rpc-navigate="/redir">mutate</button>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var v string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Visit /stale (v1) so it lands in the screen cache…
		chromedp.Click(`#tostale`, chromedp.ByID),
		chromedp.WaitVisible(`#v`, chromedp.ByID),
		// …then go home again.
		chromedp.Click(`#home`, chromedp.ByID),
		chromedp.WaitVisible(`#mut`, chromedp.ByID),
		// Post-mutation navigate to /redir, which redirects to /stale.
		chromedp.Click(`#mut`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Text(`#v`, &v, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if v != "v2" {
		t.Fatalf("redirect target content = %q, want v2 — bypassCache was dropped across the X-Gofastr-Location redirect", v)
	}
}
