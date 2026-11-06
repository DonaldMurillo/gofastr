package runtime

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestLoadingArrivesWithNavigation(t *testing.T) {
	page := `<!doctype html><html><head><title>Late loading layout</title>
<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:x"]},{"path":"/warm","layouts":["l:x"]},{"path":"/introduced","layouts":["l:x","l:y"]},{"path":"/slow","layouts":["l:x","l:y"]}]</script>
</head><body><nav><a id="warm" href="/warm">Warm</a><a id="introduce" href="/introduced">Introduce</a><a id="slow" href="/slow">Slow</a></nav>
<div data-cui-layout="x" data-cui-layout-key="l:x"><div role="main" data-cui-layout-slot="l:x">Boot</div><aside data-cui-outlet="l:x#aside">Aside</aside></div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	send := func(w http.ResponseWriter, key, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Swap", key)
		w.Header().Set("X-Gofastr-Envelope", "2")
		fmt.Fprintf(w, `<template data-cui-fill="%s">%s</template><template data-cui-fill="l:x#aside">Aside</template>`, key, body)
	}
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/warm": func(w http.ResponseWriter, _ *http.Request) {
			send(w, "l:x", `<p id="warmed">Warm</p>`)
		},
		"/introduced": func(w http.ResponseWriter, _ *http.Request) {
			send(w, "l:x", `<div data-cui-layout="y" data-cui-layout-key="l:y"><div data-cui-layout-slot="l:y"><p id="introduced">Introduced</p></div><template data-cui-loading="l:y" data-cui-after="10" data-cui-min="0"><span id="late-loading">Loading</span></template></div>`)
		},
		"/slow": func(w http.ResponseWriter, request *http.Request) {
			select {
			case <-gate:
				send(w, "l:y", `<p id="finished">Finished</p>`)
			case <-request.Context().Done():
			}
		},
	})
	t.Cleanup(release)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.Click(`#warm`, chromedp.ByID),
		chromedp.WaitVisible(`#warmed`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr?.loadedModules?.envelope && !window.__gofastr.loadedModules.loading`, nil),
		chromedp.Click(`#introduce`, chromedp.ByID),
		chromedp.WaitVisible(`#introduced`, chromedp.ByID),
		chromedp.Click(`#slow`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('late-loading') !== null`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("a loading layout reached by navigation did not schedule its content: %v", err)
	}
	release()
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`#finished`, chromedp.ByID),
		chromedp.Poll(`!document.getElementById('late-loading') && !document.querySelector('[data-cui-loadstate]')`, nil),
	); err != nil {
		t.Fatal(err)
	}
}
