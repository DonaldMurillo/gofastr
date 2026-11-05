package runtime

import (
	"fmt"
	"html"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestNavigationRefreshesCSRFToken(t *testing.T) {
	for _, options := range []struct{ partial, missing bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprint(options), func(t *testing.T) {
			partial := options.partial
			var rotated atomic.Bool
			body := `<form id="f" action="/submit" method="POST" enctype="application/json"><button id="go">Write</button></form><a id="next" href="/next">Next</a><a id="without" href="/without">Without token</a>`
			head := `<meta name="csrf-token" content="{{tok}}">`
			if options.missing {
				head = ""
			}
			base := plainFormPage(t, head+`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"},{"path":"/without"}]</script>`, `<main role="main">`+body+`</main>`,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
				func(next http.Handler) http.Handler {
					page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						meta := `<meta name="csrf-token" content="` + middleware.MaskCSRFToken(middleware.TokenFromContext(r.Context())) + `">`
						if options.missing && !partial && r.URL.Path == "/next" {
							w.Header().Set("Content-Type", "text/html")
							fmt.Fprint(w, `<html><head>`+meta+`</head><body><main role="main">`+body+`</main></body></html>`)
							return
						}
						if r.URL.Path != "/submit" && ((partial && r.Header.Get("X-Gofastr-Navigate") == "1") || r.URL.Path == "/without") {
							head := ""
							if r.URL.Path != "/without" {
								head = `<template data-cui-page-head="` + html.EscapeString(meta) + `"></template>`
							}
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
						if rotated.Load() {
							second.ServeHTTP(w, r)
						} else {
							first.ServeHTTP(w, r)
						}
					})
				})
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(base), chromedp.WaitVisible(`#go`),
				chromedp.Evaluate(`window.stamp=1;window.naved='';window.addEventListener('gofastr:navigate',()=>window.naved=location.pathname);document.addEventListener('gofastr:formresult',e=>window.writeOK=e.detail.ok)`, nil)); err != nil {
				t.Fatal(err)
			}
			rotated.Store(true)
			for _, link := range []string{"#next", "#without"} {
				if err := chromedp.Run(ctx, chromedp.Click(link), chromedp.Poll(`window.naved===`+fmt.Sprintf("%q", "/"+link[1:]), nil),
					chromedp.Evaluate(`window.writeOK=null`, nil), chromedp.Click(`#go`),
					chromedp.Poll(`window.writeOK===true && window.stamp===1`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
					var state string
					chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({nav:window.naved,ok:window.writeOK,stamp:window.stamp})`, &state))
					t.Fatalf("%s: %v (%s)", link, err, state)
				}
			}
		})
	}
}
