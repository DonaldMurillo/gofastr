package runtime

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestStaticActionsSkipRequests(t *testing.T) {
	for _, static := range []bool{true, false} {
		t.Run(fmt.Sprint(static), func(t *testing.T) {
			js, err := RuntimeJS()
			if static {
				js, err = StaticJS()
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("static=%v gzip6=%d gzip1=%d", static, gzipSize(t, js), gzipSizeAt(t, js, gzip.BestSpeed))
			var posts atomic.Int32
			mux := http.NewServeMux()
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/runtime.js", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, js)
			})
			mux.HandleFunc("/__gofastr/action", func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{}`)
			})
			mux.HandleFunc("/__gofastr/widgets.json", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `[]`)
			})
			mux.HandleFunc("/__gofastr/widget/late-comp.js", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, `window.__gofastr.register('late-comp', {mount: () => {window.mounts++; return window.__gofastr._serverActionFor('late-comp', 'load');}}); window.lateLoaded = true;`)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><html><head><script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"}]</script><script>window.__gofastr_actions={'late-comp':'h1'};</script></head><body><main>
<div data-component="server-demo"><i data-action-mount="mount"></i><button id="save" data-action="save">save</button><button id="local" data-action="local">local</button></div>
<a id="next" href="/next">next</a><div id="insert"></div></main><script src="/runtime.js"></script>
<script>window.mounts=0; window.localClicks=0; window.__gofastr.register('server-demo', {
mount: () => {window.mounts++; return window.__gofastr._serverActionFor('server-demo', 'load');},
save: async () => {await window.__gofastr._serverActionFor('server-demo', 'save'); window.saved=true;},
local: () => {window.localClicks++;}});</script></body></html>`)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
			var mounts, localClicks int
			var notice bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.Poll(`document.readyState === 'complete' && !!window.__gofastr.loadedModules.actionloader`, nil),
				chromedp.Click("#save"),
				chromedp.Poll(`window.saved === true`, nil),
				chromedp.Evaluate(`document.body.innerText.includes('Needs the Go server.')`, &notice),
				chromedp.Click("#local"),
				chromedp.Evaluate(`window.localClicks`, &localClicks),
				chromedp.Click("#next"),
				chromedp.Poll(`location.pathname === '/next'`, nil),
				chromedp.Evaluate(`document.getElementById('insert').innerHTML = '<div data-component="late-comp"><i data-action-mount="mount"></i></div>'`, nil),
				chromedp.Poll(`window.lateLoaded === true`, nil),
				chromedp.Sleep(150*time.Millisecond),
				chromedp.Evaluate(`window.mounts`, &mounts),
			); err != nil {
				t.Fatal(err)
			}
			if localClicks != 1 {
				t.Errorf("client-only action fired %d times", localClicks)
			}
			if static {
				if posts.Load() != 0 || mounts != 0 || !notice {
					t.Errorf("static: posts=%d mounts=%d notice=%v; want 0, 0, true", posts.Load(), mounts, notice)
				}
			} else if posts.Load() != 4 || mounts != 3 {
				t.Errorf("live: posts=%d mounts=%d; want 4, 3", posts.Load(), mounts)
			}
		})
	}
}
