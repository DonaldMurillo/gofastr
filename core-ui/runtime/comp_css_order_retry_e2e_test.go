package runtime

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// compCSSServer serves a page whose head links app.css with the cascade
// anchor the host emits (#cui-app-css). The lazy component's sheet answers
// 503 for its first `fails` requests, the way a replica mid-deploy does.
func compCSSServer(t *testing.T, fails int32, extra ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/app.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		// The host's override: same selector as the kit, so only order decides.
		w.Write([]byte(`[data-cui-comp="lazy"] .x{color:rgb(0, 0, 255)}`))
	})
	mux.HandleFunc("/css/lazy.css", func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) <= fails {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(`[data-cui-comp="lazy"] .x{color:rgb(255, 0, 0);font-weight:700}`))
	})
	mux.HandleFunc("/bundle-down.css", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	head, body := "", ""
	if len(extra) == 2 {
		head, body = extra[0], extra[1]
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><head>` + head +
			`<link rel="stylesheet" href="/app.css" id="cui-app-css">` +
			`<script>window.__gofastr_catalog={"lazy":{stylePath:"/css/lazy.css",version:"1"}};</script>` +
			`</head><body><div id="host">` + body + `</div><script src="/__gofastr/runtime.js"></script></body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

const insertLazy = `(() => { const d = document.createElement('div'); d.setAttribute('data-cui-comp', 'lazy');
  d.innerHTML = '<span class="x">x</span>'; document.getElementById('host').appendChild(d); window.__gofastr.scanAndLoadCSS(document.getElementById('host')); return 0; })()`

// A component whose sheet loads after first paint (an island swap, a poll,
// a navigation) must keep the server head's order: kit sheet before
// app.css, so the host's override wins a tie. Appending after app.css
// reversed it for every lazily loaded component.
func TestLazyComponentSheetLandsBeforeAppCSS(t *testing.T) {
	srv, _ := compCSSServer(t, 0)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var color string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(insertLazy, nil),
		chromedp.Poll(`getComputedStyle(document.querySelector('[data-cui-comp=lazy] .x')).fontWeight === '700'`, nil),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-cui-comp=lazy] .x')).color`, &color),
	); err != nil {
		t.Fatal(err)
	}
	if color != "rgb(0, 0, 255)" {
		t.Errorf("lazy component color = %s, want the host's app.css override rgb(0, 0, 255)", color)
	}
}

// A sheet fetch that fails (a 503 mid-deploy) must not pin the component
// unstyled for the rest of the visit: the next time its marker is scanned
// the runtime asks again.
func TestFailedComponentSheetIsRetried(t *testing.T) {
	srv, hits := compCSSServer(t, 1)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(insertLazy, nil),
	); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	// Inserted again and again right after the failure (a poll or push
	// region swapping): the failed sheet is not re-requested on every
	// swap.
	for range 5 {
		if err := chromedp.Run(ctx, chromedp.Evaluate(insertLazy, nil)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("the failed sheet was requested %d times inside its cooldown, want 1", n)
	}
	// After the cooldown, the marker appearing again asks again.
	time.Sleep(3 * time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(insertLazy, nil),
		chromedp.Poll(`getComputedStyle(document.querySelector('[data-cui-comp=lazy] .x')).fontWeight === '700'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("the failed sheet was never fetched again (%d requests): %v", hits.Load(), err)
	}
}

// The head's bundle link names the sheets it covers, and boot seeds those
// names as loaded so the scanner does not fetch them twice. A bundle that
// failed (a 503 mid-deploy) covers nothing: its names must stay with the
// scanner, which loads each component's own sheet.
func TestFailedBundleLeavesItsNamesToTheScanner(t *testing.T) {
	srv, hits := compCSSServer(t, 0,
		`<link rel="stylesheet" href="/bundle-down.css" data-cui-bundle="lazy">`,
		`<div data-cui-comp="lazy"><span class="x">x</span></div>`)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`getComputedStyle(document.querySelector('[data-cui-comp=lazy] .x')).fontWeight === '700'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("the component stayed unstyled behind its failed bundle (%d own-sheet requests): %v", hits.Load(), err)
	}
}

// Markup written by a path that does not scan for sheets itself (a
// compiled action's updateHTML or fetchPage, a module's own innerHTML)
// still loads the sheets of the components it brings: boot's mutation
// observer scans every inserted subtree, the root included.
func TestInsertedMarkupLoadsItsSheets(t *testing.T) {
	srv, _ := compCSSServer(t, 0)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr`, nil),
		chromedp.Evaluate(`window.__gofastr.updateHTML('#host', '<div data-cui-comp="lazy"><span class="x">x</span></div>'); 0`, nil),
		chromedp.Poll(`getComputedStyle(document.querySelector('[data-cui-comp=lazy] .x')).fontWeight === '700'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("markup from updateHTML never loaded its component sheet: %v", err)
	}
}
