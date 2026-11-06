package runtime

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestBarePrimaryLoading(t *testing.T) {
	for _, scenario := range []string{"swap", "supersede", "no-loading"} {
		t.Run(scenario, func(t *testing.T) {
			loading := `<template data-cui-loading="l:x" data-cui-after="10" data-cui-min="300"><p id="primary-loading">Loading primary</p></template>`
			if scenario == "no-loading" {
				loading = ""
			}
			page := `<!doctype html><html><head><title>Primary loading</title>
<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:x"]},{"path":"/slow","layouts":["l:x"]},{"path":"/fast","layouts":["l:x"]}]</script>
</head><body><div data-cui-layout="x" data-cui-layout-key="l:x"><nav><a id="slow" href="/slow">Slow</a><a id="fast" href="/fast">Fast</a></nav>
<main data-cui-layout-slot="l:x"><p id="original">Original primary</p></main>` + loading + `</div>
<script src="/__gofastr/runtime.js"></script></body></html>`
			gate := make(chan struct{})
			started := make(chan struct{})
			finished := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			send := func(w http.ResponseWriter, body string) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "l:x")
				fmt.Fprint(w, body)
			}
			base := startPollServer(t, page, map[string]http.HandlerFunc{
				"/slow": func(w http.ResponseWriter, request *http.Request) {
					close(started)
					defer close(finished)
					select {
					case <-gate:
						send(w, `<p id="slow-result">Slow result</p>`)
					case <-request.Context().Done():
					}
				},
				"/fast": func(w http.ResponseWriter, _ *http.Request) {
					send(w, `<p id="fast-result">Fast result</p>`)
				},
			})
			t.Cleanup(release)
			ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base),
				chromedp.Poll(`!!window.__gofastr?.navigate`, nil),
				chromedp.Evaluate(`window.__originalPrimary = document.getElementById('original')`, nil),
			); err != nil {
				t.Fatal(err)
			}
			var bootModules bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__gofastr.loadedModules.envelope || !!window.__gofastr.loadedModules.loading`, &bootModules)); err != nil {
				t.Fatal(err)
			}
			if bootModules {
				t.Fatal("bare-primary layout fetched navigation modules before navigation")
			}
			if err := chromedp.Run(ctx, chromedp.Click(`#slow`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("navigation never reached its held request")
			}
			if scenario != "no-loading" {
				if err := chromedp.Run(ctx,
					chromedp.Poll(`document.getElementById('primary-loading') !== null`, nil, chromedp.WithPollingTimeout(3*time.Second)),
					chromedp.WaitVisible(`#primary-loading`, chromedp.ByID),
				); err != nil {
					t.Fatalf("held bare-primary navigation never showed its loading content: %v", err)
				}
				if directory := os.Getenv("GOFASTR_PRIMARY_VISUAL_DIR"); directory != "" {
					var screenshot []byte
					if err := chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 90)); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(directory, "primary-"+scenario+".png"), screenshot, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			winner := "slow-result"
			if scenario == "supersede" {
				winner = "fast-result"
				if err := chromedp.Run(ctx, chromedp.Click(`#fast`, chromedp.ByID)); err != nil {
					t.Fatal(err)
				}
			} else {
				release()
			}
			if err := chromedp.Run(ctx,
				chromedp.WaitVisible(`#`+winner, chromedp.ByID),
				chromedp.Poll(`!document.getElementById('primary-loading') && !document.querySelector('[data-cui-loadstate],[aria-busy="true"]') && !window.__originalPrimary.isConnected`, nil),
			); err != nil {
				t.Fatalf("navigation did not replace loading content and discard its park: %v", err)
			}
			release()
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("held response did not finish")
			}
			if err := chromedp.Run(ctx,
				chromedp.Poll(`!document.documentElement.hasAttribute('aria-busy')`, nil),
				chromedp.Sleep(100*time.Millisecond),
				chromedp.WaitVisible(`#`+winner, chromedp.ByID),
			); err != nil {
				t.Fatalf("the superseded response changed the winning page: %v", err)
			}
			if scenario == "no-loading" {
				var fetched bool
				if err := chromedp.Run(ctx, chromedp.Evaluate(`performance.getEntriesByType('resource').some(entry => /\/runtime\/(loading|envelope)\.js(?:\?|$)/.test(entry.name)) || !!document.querySelector('script[src*="/runtime/loading.js"],script[src*="/runtime/envelope.js"]')`, &fetched)); err != nil {
					t.Fatal(err)
				}
				if fetched {
					t.Fatal("a layout without Loading fetched a loading or envelope module")
				}
			}
		})
	}
}
