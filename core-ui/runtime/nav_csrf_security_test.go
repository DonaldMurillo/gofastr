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
						if rotated.Load() {
							second.ServeHTTP(w, r)
						} else {
							first.ServeHTTP(w, r)
						}
					})
				})
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(base), chromedp.WaitVisible(`#go`),
				chromedp.Evaluate(`window.stamp=1;window.naved='';window.addEventListener('gofastr:navigate',event=>{window.naved=location.pathname;window.wasCached=event.detail.cached});document.addEventListener('gofastr:formresult',e=>window.writeOK=e.detail.ok)`, nil)); err != nil {
				t.Fatal(err)
			}
			rotated.Store(true)
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
				if err := chromedp.Run(ctx, navigate, chromedp.Poll(`window.naved===`+fmt.Sprintf("%q", path), nil),
					chromedp.Evaluate(`window.writeOK=null`, nil), chromedp.Click(`#go`),
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
