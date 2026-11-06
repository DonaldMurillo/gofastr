package runtime

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestNavigationRefreshesCSRFToken(t *testing.T) {
	for _, options := range []struct{ partial, missing, marked, cached, envelope bool }{
		{}, {partial: true}, {missing: true}, {partial: true, missing: true},
		{marked: true}, {partial: true, marked: true},
		{marked: true, envelope: true}, {partial: true, marked: true, envelope: true},
		{cached: true}, {partial: true, cached: true},
		{cached: true, envelope: true}, {partial: true, cached: true, envelope: true},
	} {
		t.Run(fmt.Sprint(options), func(t *testing.T) {
			partial := options.partial
			var rotated atomic.Bool
			body := `<form id="f" action="/submit" method="POST" enctype="application/json"><button id="go">Write</button></form><a id="next" href="/next">Next</a><a id="without" href="/without">Without token</a>`
			head := `<meta name="csrf-token" content="{{tok}}">`
			if options.envelope {
				body += `<aside data-cui-outlet="l:shell#aside"></aside>`
			}
			if options.marked {
				head = `<meta data-cui-page-meta name="csrf-token" content="{{tok}}">`
			}
			if options.missing {
				head = ""
			}
			base := plainFormPage(t, head+`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"},{"path":"/without"}]</script>`, `<main role="main">`+body+`</main>`,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
				func(next http.Handler) http.Handler {
					page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						meta := `<meta name="csrf-token" content="` + middleware.MaskCSRFToken(middleware.TokenFromContext(r.Context())) + `">`
						if options.marked {
							meta = strings.Replace(meta, `<meta `, `<meta data-cui-page-meta `, 1)
						}
						if options.missing && !partial && r.URL.Path == "/next" {
							w.Header().Set("Content-Type", "text/html")
							fmt.Fprint(w, `<html><head>`+meta+`</head><body><main role="main">`+body+`</main></body></html>`)
							return
						}
						if r.URL.Path != "/submit" && ((partial && r.Header.Get("X-Gofastr-Navigate") == "1") || r.URL.Path == "/without") {
							if r.URL.Path == "/without" {
								meta = ""
							}
							head := `<template data-cui-page-head="` + html.EscapeString(meta) + `"></template>`
							w.Header().Set("X-Gofastr-Partial", "true")
							w.Header().Set("Content-Type", "text/html")
							fmt.Fprint(w, head+body)
							return
						}
						next.ServeHTTP(w, r)
					})
					first := middleware.CSRF(middleware.CSRFConfig{SecretKey: []byte("first-test-key")})(page)
					second := middleware.CSRF(middleware.CSRFConfig{SecretKey: []byte("second-test-key")})(page)
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// Only the fixture's own routes pass the rotating CSRF
						// middleware. A favicon or widget-catalog GET carrying the
						// old cookie mints its own replacement under the new key,
						// and whichever response lands last owns the cookie.
						switch r.URL.Path {
						case "/", "/next", "/without", "/submit":
						default:
							w.WriteHeader(http.StatusNotFound)
							return
						}
						if rotated.Load() {
							second.ServeHTTP(w, r)
						} else {
							first.ServeHTTP(w, r)
						}
					})
				})
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(base), chromedp.WaitVisible(`#go`),
				chromedp.Evaluate(`window.stamp=1;window.naved='';
window.addEventListener('gofastr:navigate',event=>{
  window.naved=location.pathname;
  window.wasCached=event.detail.cached;
  document.querySelector('#f').requestSubmit();
});
document.addEventListener('gofastr:formresult',event=>window.writeOK=event.detail.ok)`, nil)); err != nil {
				t.Fatal(err)
			}
			rotated.Store(true)
			var backgroundIsolated bool
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.backgroundCheck=null;
const before=document.cookie;
Promise.all(['/favicon.ico','/__gofastr/widgets'].map(path=>fetch(path).then(response=>response.status)))
  .then(statuses=>{window.backgroundCheck=statuses.every(status=>status===404)&&document.cookie===before})`, nil),
				chromedp.Poll(`window.backgroundCheck!==null`, nil),
				chromedp.Evaluate(`window.backgroundCheck`, &backgroundIsolated)); err != nil || !backgroundIsolated {
				t.Fatalf("background requests changed CSRF state: isolated=%v, err=%v", backgroundIsolated, err)
			}
			steps := []string{"#next", "#without"}
			if options.cached {
				steps = []string{"#next", "back", "forward", "#without"}
			}
			for _, link := range steps {
				var navigate chromedp.Action = chromedp.Click(link)
				path := "/" + strings.TrimPrefix(link, "#")
				if link == "back" || link == "forward" {
					navigate = chromedp.Evaluate(`history.`+link+`()`, nil)
					path = "/"
					if link == "forward" {
						path = "/next"
					}
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.writeOK=null`, nil), navigate,
					chromedp.Poll(`window.naved===`+fmt.Sprintf("%q", path), nil),
					chromedp.Poll(`window.writeOK===true && window.stamp===1`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
					var state string
					chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({nav:window.naved,ok:window.writeOK,stamp:window.stamp})`, &state))
					t.Fatalf("%s: %v (%s)", link, err, state)
				}
				if link == "back" || link == "forward" {
					var cached bool
					if err := chromedp.Run(ctx, chromedp.Evaluate(`window.wasCached`, &cached)); err != nil || !cached {
						t.Fatalf("%s did not replay the page cache: cached=%v, err=%v", link, cached, err)
					}
				}
			}
		})
	}
}

func TestPrefetchReplayKeepsRotatedCSRFToken(t *testing.T) {
	for _, navigator := range []string{"frag", "envelope"} {
		t.Run(navigator, func(t *testing.T) {
			var requests, prefetches atomic.Int32
			body := `<div data-cui-layout="shell" data-cui-layout-key="l:shell"><main role="main" data-cui-layout-slot="l:shell"><a id="next" href="/next">Next</a></main>`
			if navigator == "envelope" {
				body += `<aside data-cui-outlet="l:shell#aside"></aside>`
			}
			body += `</div>`
			head := `<meta data-cui-page-meta name="csrf-token" content="prefetched-token"><script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:shell"]},{"path":"/next","layouts":["l:shell"],"preload":"hover"}]</script>`
			base := plainFormPage(t, head, body, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/next" {
						next.ServeHTTP(w, r)
						return
					}
					requests.Add(1)
					if r.Header.Get("X-Gofastr-Prefetch") == "1" {
						prefetches.Add(1)
					}
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("X-Gofastr-Partial", "true")
					w.Header().Set("X-Gofastr-Swap", "l:shell")
					meta := `<meta data-cui-page-meta name="csrf-token" content="prefetched-token"><meta data-cui-page-meta name="description" content="prefetched page">`
					partial := `<template data-cui-page-head="` + html.EscapeString(meta) + `"></template><h1 id="prefetched-screen">Prefetched page</h1>`
					if navigator == "envelope" {
						w.Header().Set("X-Gofastr-Envelope", "2")
						partial = `<template data-cui-fill="l:shell">` + partial + `</template><template data-cui-fill="l:shell#aside">Prefetched aside</template>`
					}
					fmt.Fprint(w, partial)
				})
			})
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base), chromedp.WaitVisible(`#next`),
				chromedp.Evaluate(`document.querySelector('[data-cui-outlet]') && window.__gofastr.loadModule('envelope')`, nil),
				chromedp.Poll(fmt.Sprintf(`window.__gofastr.loadedModules?.preload && Boolean(window.__gofastr._navHooks?.envelope) === %t`, navigator == "envelope"), nil),
				chromedp.Evaluate(`(() => {
const runtime = window.__gofastr;
const scan = runtime.scanAndLoadCSS;
runtime.scanAndLoadCSS = root => {
  if (root.querySelector('#prefetched-screen')) window.prefetchReady = true;
  return scan(root);
};
window.addEventListener('gofastr:navigate', event => {
  window.prefetchNavigation = {cached:event.detail.cached, token:document.querySelector('meta[name="csrf-token"]').content};
});
document.querySelector('#next').dispatchEvent(new PointerEvent('pointerover', {bubbles:true}));
})()`, nil),
				chromedp.Poll(`window.prefetchReady === true`, nil),
				chromedp.Evaluate(`document.querySelector('meta[name="csrf-token"]').content = 'rotated-token'`, nil),
				chromedp.Click(`#next`), chromedp.WaitVisible(`#prefetched-screen`),
				chromedp.Poll(`window.prefetchNavigation !== undefined`, nil)); err != nil {
				t.Fatal(err)
			}
			var state struct {
				Token       string `json:"token"`
				AtNavigate  string `json:"atNavigate"`
				Description string `json:"description"`
				Cached      bool   `json:"cached"`
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`({token:document.querySelector('meta[name="csrf-token"]').content, atNavigate:window.prefetchNavigation.token, cached:window.prefetchNavigation.cached, description:document.querySelector('meta[name="description"]')?.content})`, &state)); err != nil {
				t.Fatal(err)
			}
			if state.Token != "rotated-token" || state.AtNavigate != "rotated-token" {
				t.Errorf("prefetch replay restored stale CSRF state: meta=%q, navigate=%q", state.Token, state.AtNavigate)
			}
			if state.Cached || state.Description != "prefetched page" {
				t.Errorf("prefetch replay changed navigation metadata: %+v", state)
			}
			if requests.Load() != 1 || prefetches.Load() != 1 {
				t.Errorf("/next requests=%d, prefetches=%d; want one prefetch and no click fetch", requests.Load(), prefetches.Load())
			}
		})
	}
}
