package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

const lateEnvelopeRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/q/0","layouts":["l:q"]},{"path":"/q/1","layouts":["l:q"]},{"path":"/q/2","layouts":["l:q"]}` +
	`]</script>`

// lateEnvelopeContent: /q/0 holds no outlet, so the plain navigator
// runs its first navigation; /q/1 and /q/2 each bring one in, so the
// navigation after /q/1 hands off to the envelope module.
func lateEnvelopeContent(id string) string {
	outlet := ""
	if id != "0" {
		outlet = `<div data-cui-outlet="l:q#aside" id="aside-` + id + `">aside</div>`
	}
	return `<h1 id="q-` + id + `">` + id + `</h1>` + outlet
}

// The envelope module keyed its boot capture on the served URL, read off
// the performance navigation entry, assuming no navigation had applied
// before it loaded. A plain navigation to a page that brings an outlet
// in loads it one navigation late, so it cached /q/1's DOM under /q/0,
// and Back to /q/0 replayed /q/1 (2026-10-27 R29).
func TestLateEnvelopeKeepsTheBootPage(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for _, id := range []string{"0", "1", "2"} {
		mux.HandleFunc("/q/"+id, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", id)
				w.Header().Set("X-Gofastr-Swap", "l:q")
				fmt.Fprint(w, lateEnvelopeContent(id))
				return
			}
			fmt.Fprint(w, `<!doctype html><html lang="en"><head><title>`+id+`</title>`+lateEnvelopeRoutes+`</head><body>`+
				`<div data-cui-layout="q" data-cui-layout-key="l:q">`+
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:q">`+lateEnvelopeContent(id)+
				`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var plainFirst, envelopeLoaded bool
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/q/0"),
		chromedp.WaitVisible(`#q-0`, chromedp.ByID),
		chromedp.Evaluate(`__gofastr.navigate('/q/1')`, nil),
		chromedp.WaitVisible(`#q-1`, chromedp.ByID),
		chromedp.Evaluate(`!__gofastr._navHooks.envelope`, &plainFirst),
		chromedp.Evaluate(`__gofastr.navigate('/q/2')`, nil),
		chromedp.WaitVisible(`#q-2`, chromedp.ByID),
		chromedp.Evaluate(`!!__gofastr._navHooks.envelope`, &envelopeLoaded),
		chromedp.NavigateBack(),
		chromedp.WaitVisible(`#q-1`, chromedp.ByID),
		chromedp.Evaluate(`window.__navs = 0; addEventListener('gofastr:navigate', () => window.__navs++)`, nil),
		chromedp.NavigateBack(),
		chromedp.Poll(`window.__navs > 0 && !document.documentElement.hasAttribute('aria-busy')`, nil),
		chromedp.Evaluate(`location.pathname + ' ' + !!document.getElementById('q-0')`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !plainFirst || !envelopeLoaded {
		t.Fatalf("plain navigator ran /q/1 = %t, envelope module loaded for /q/2 = %t; the test needs both", plainFirst, envelopeLoaded)
	}
	if got != "/q/0 true" {
		t.Errorf("Back to the boot page: location and /q/0 shown = %q, want %q", got, "/q/0 true")
	}
}
