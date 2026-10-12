package runtime_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// leaveCaptureServer: "/" and "/a" share the l:site layer, so a
// navigation between them swaps the l:site slot and the envelope
// navigator captures the LEAVING slot's markup for Back. "/" holds an
// rpc button whose request takes two seconds.
func leaveCaptureServer(t *testing.T) *httptest.Server {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	page := func(inner string) string {
		return `<!doctype html><html lang="en"><head><title>leave</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:site"]},{"path":"/a","layouts":["l:site"]}]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a></nav>` +
			`<div data-cui-outlet="l:site#side" id="side">SIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusNoContent)
	})
	home := `<button id="rb" type="button" data-cui-rpc="/slow">Save</button>`
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			w.Header().Set("X-Gofastr-Envelope", "2")
			if req.URL.Path == "/a" {
				fmt.Fprint(w, `<template data-cui-fill="l:site"><p id="screen-a">A-CONTENT</p></template>`)
			} else {
				fmt.Fprint(w, `<template data-cui-fill="l:site">`+home+`</template>`)
			}
			return
		}
		if req.URL.Path == "/a" {
			fmt.Fprint(w, page(`<p id="screen-a">A-CONTENT</p>`))
			return
		}
		fmt.Fprint(w, page(home))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestLeaveCaptureDropsInFlightState: leaving a page while an rpc is
// in flight captured the live markup with the control's in-flight
// markers (disabled, cui-loading, aria-busy). Back replayed that
// snapshot and nothing ever re-enabled the restored button: the rpc's
// finally only touches the node it disabled, detached by then. The
// cached entry carries the control at rest.
func TestLeaveCaptureDropsInFlightState(t *testing.T) {
	srv := leaveCaptureServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#rb`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond), // the envelope module's boot capture
		chromedp.Click(`#rb`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond), // in flight: disabled + cui-loading
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#rb`, chromedp.ByID),
		chromedp.Sleep(3*time.Second), // past the rpc's two seconds
		chromedp.Evaluate(`(() => {
			const b = document.getElementById('rb');
			return JSON.stringify({ disabled: b.disabled, busy: b.getAttribute('aria-busy'), loading: b.classList.contains('cui-loading') });
		})()`, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	want := `{"disabled":false,"busy":null,"loading":false}`
	if state != want {
		t.Fatalf("restored button after Back: %s, want %s", state, want)
	}
}
