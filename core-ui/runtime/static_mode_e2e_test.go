package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Static-export mode: when <html> carries data-fui-static (injected only
// by framework/static.Builder), the runtime resolves server-backed
// affordances against the static tree instead of the live server.
// data-fui-open overlays still open, the static composition ships
// widgets-boot-static, which fetches the dumped /__gofastr/widgets.json
// catalog the exporter writes, and the per-widget chrome HTML the
// exporter dumps at /core-ui/widget/<name>/chrome. data-fui-rpc clicks
// are the one thing that genuinely need the server, so rpc-stub
// surfaces a "Needs the Go server" notice for them instead of firing
// a dead request. Client-only features (theme toggle, copy, signals)
// are unaffected.

// startStaticModeServer serves the appropriate runtime composition plus a
// page that optionally carries the static marker. When static=true the page
// is served the `static` composition (kernel+rpc-stub+signals+nav+
// widgets-boot-static), the composition IS the static-mode switch now,
// replacing the old runtime branch on <html data-fui-static>. When
// static=false the page gets the `full` composition (the regression
// guard). Counters record live widget, RPC-module, and dead RPC requests so
// static mode proves it neither loads nor dispatches RPC.
func startStaticModeServer(t *testing.T, static bool) (base string, widgetHits, moduleHits, rpcHits *int32) {
	t.Helper()
	var js string
	var err error
	if static {
		js, err = StaticJS()
	} else {
		js, err = RuntimeJS()
	}
	if err != nil {
		t.Fatal(err)
	}
	var wh, mh, rh int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		if name == "rpc" {
			atomic.AddInt32(&mh, 1)
		}
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&wh, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	// Static mode fetches the dumped catalog file (not the live session-gated
	// endpoint). Serve an empty catalog so the runtime resolves cleanly.
	mux.HandleFunc("/__gofastr/widgets.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	mux.HandleFunc("/dead-rpc", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&rh, 1)
		w.WriteHeader(200)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		htmlAttr := ""
		if static {
			htmlAttr = " data-fui-static"
		}
		fmt.Fprintf(w, `<!doctype html><html%s><head><title>static</title></head><body>
  <button id="rpc" data-fui-rpc="/dead-rpc">rpc</button>
  <button id="opener" data-fui-open="palette">open</button>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, htmlAttr)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, &wh, &mh, &rh
}

// TestStaticMode_SkipsServerBackedRequests: the `static` composition
// fetches the dumped catalog at /__gofastr/widgets.json (written by
// framework/static.Builder.dumpWidgetAssets), NOT the live session-gated
// endpoint /__gofastr/widgets?page=…, which would 404 on a serverless
// host. rpc-stub still intercepts data-fui-rpc clicks and surfaces a
// notice. The live widget endpoint stays at zero hits; the RPC endpoint
// stays at zero hits.
func TestStaticMode_SkipsServerBackedRequests(t *testing.T) {
	base, widgetHits, moduleHits, rpcHits := startStaticModeServer(t, true)
	ctx := newSeedBrowserCtx(t)

	var ready string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('ready').textContent`, &ready),
		chromedp.Click(`#rpc`, chromedp.ByID),
		chromedp.Click(`#opener`, chromedp.ByID),
		// Let any in-flight fetches land.
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got := atomic.LoadInt32(widgetHits); got != 0 {
		t.Errorf("static composition must not hit the live /__gofastr/widgets?page endpoint (it fetches the dumped /__gofastr/widgets.json instead), got %d hits", got)
	}
	if got := atomic.LoadInt32(moduleHits); got != 0 {
		t.Errorf("static composition must not demand-load the RPC module, got %d module request(s)", got)
	}
	if got := atomic.LoadInt32(rpcHits); got != 0 {
		t.Errorf("static mode must skip RPC dispatch, got %d hits", got)
	}
}

// TestStaticMode_LiveStillFiresRequests is the regression guard: the
// guard must be a no-op on a live page (no marker), the catalog fetch
// fires on boot and an RPC click still reaches the server.
func TestStaticMode_LiveStillFiresRequests(t *testing.T) {
	base, widgetHits, moduleHits, rpcHits := startStaticModeServer(t, false)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Catalog fetch fires on boot; give it a beat.
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Click(`#rpc`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got := atomic.LoadInt32(widgetHits); got == 0 {
		t.Error("live mode should fetch the widget catalog (guard must be a no-op without the marker)")
	}
	if got := atomic.LoadInt32(moduleHits); got == 0 {
		t.Error("live mode should prefetch the RPC module when an RPC marker is present")
	}
	if got := atomic.LoadInt32(rpcHits); got == 0 {
		t.Error("live mode should dispatch the RPC on click (guard must be a no-op without the marker)")
	}
}

// TestStaticMode_RPCShowsNotice: on a static page, clicking a data-fui-rpc
// control must NOT fail silently, it surfaces a "Needs the Go server" notice
// so the user understands why the demo is dead and how to run it live. The
// notice renders synchronously into #fui-nav-toast (the CSP-clean mini toast).
func TestStaticMode_RPCShowsNotice(t *testing.T) {
	base, _, _, rpcHits := startStaticModeServer(t, true)
	ctx := newSeedBrowserCtx(t)

	var toastText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#rpc`, chromedp.ByID),
		// _showNavToast renders synchronously into #fui-nav-toast (no
		// async module fetch) so it's visible immediately after click.
		chromedp.WaitVisible(`#fui-nav-toast`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('fui-nav-toast').textContent`, &toastText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(toastText, "Needs the Go server") {
		t.Errorf("static RPC click should show a 'Needs the Go server' notice; got toast text %q", toastText)
	}
	if got := atomic.LoadInt32(rpcHits); got != 0 {
		t.Errorf("static mode must not hit the RPC endpoint, got %d hits", got)
	}
}

// TestStaticMode_WidgetOpensFromStaticCatalog is the regression guard for
// restoring widget mounting to the `static` composition. A data-fui-open
// click on a static page must still OPEN the overlay, resolving the widget
// from the dumped /__gofastr/widgets.json catalog and fetching its chrome
// HTML from the per-widget file the exporter dumps. This is the capability
// that was lost when widgets-boot was dropped from the static composition
// and replaced with a rpc-stub interceptor that surfaced a "Needs the Go
// server" notice for every data-fui-open click.
//
// Against the regressed code (rpc-stub intercepts data-fui-open) the
// chrome endpoint is never hit, so chromeHits stays at 0 and the test
// fails on the chromedp.WaitVisible (no widget ever mounts), exactly the
// silent-failure mode the composition safety rule exists to prevent.
func TestStaticMode_WidgetOpensFromStaticCatalog(t *testing.T) {
	js, err := StaticJS()
	if err != nil {
		t.Fatal(err)
	}
	widgetsModule, ok := Module("widgets")
	if !ok {
		t.Fatal("embedded widgets module missing — required to exercise openWidget end-to-end")
	}

	// One hidden modal widget in the dumped catalog. Hidden => not
	// auto-mounted at boot, so opening it goes through the full
	// openWidget -> _mountByName -> chrome fetch path (the path the
	// rpc-stub interceptor used to swallow).
	widgetCatalog := `[{"hidden":true,"cfg":{` +
		`"name":"palette",` +
		`"position":"center",` +
		`"backdrop":true,` +
		`"closeOnEscape":true,` +
		`"closeOnClick":true,` +
		`"chromePath":"/core-ui/widget/palette/chrome",` +
		`"stylePath":"/core-ui/widget/palette/style.css"` +
		`}}]`

	// Chrome HTML carries a unique marker the test can read out of the
	// DOM, proving the runtime mounted the bytes it fetched (not just
	// that the fetch landed server-side). The data-fui-widget attribute
	// matches what mountWidget expects to find as the root.
	const chromeMarker = "palette-chrome-mounted-xyz789"

	var jsonHits, chromeHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/widgets.json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&jsonHits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(widgetCatalog))
	})
	mux.HandleFunc("/__gofastr/runtime/widgets.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(widgetsModule))
	})
	mux.HandleFunc("/core-ui/widget/palette/chrome", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chromeHits, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div data-fui-widget="palette"><p id="palette-marker">%s</p></div>`, chromeMarker)
	})
	mux.HandleFunc("/core-ui/widget/palette/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		// Empty body, the test does not exercise CSS application.
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html data-fui-static><head><title>static</title></head><body>
  <button id="opener" data-fui-open="palette">open</button>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var mountedText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Let widgets-boot-static's catalog fetch + loadModule('widgets')
		// settle so the eager click delegator's _wready has resolved.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Click(`#opener`, chromedp.ByID),
		// Click chains through loadModule('widgets') → _wready →
		// openWidget → _mountByName → chrome fetch → mountWidget DOM
		// insertion. Wait for the marker that proves the chrome mounted.
		chromedp.WaitVisible(`#palette-marker`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('palette-marker').textContent`, &mountedText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got := atomic.LoadInt32(&jsonHits); got == 0 {
		t.Error("static composition should fetch /__gofastr/widgets.json at boot (widgets-boot-static)")
	}
	if got := atomic.LoadInt32(&chromeHits); got == 0 {
		t.Error("static data-fui-open click must fetch widget chrome — regressed to a 'Needs the Go server' notice when widgets-boot-static was absent")
	}
	if !strings.Contains(mountedText, chromeMarker) {
		t.Errorf("static data-fui-open should mount the widget chrome into the DOM; got %q", mountedText)
	}
}
