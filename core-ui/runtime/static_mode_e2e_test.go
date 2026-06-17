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
// by framework/static.Builder), the runtime must skip every server-backed
// dispatch — the widget catalog fetch, data-fui-rpc clicks, and
// data-fui-open triggers — so a click on a dead demo does not fire a
// request that 404s against the serverless host. Client-only features
// (theme toggle, copy, signal mutations) are unaffected.

// startStaticModeServer serves runtime.js plus a page that optionally
// carries the static marker. Server-side counters record hits to the
// two server-backed endpoints so the test can assert they were skipped.
func startStaticModeServer(t *testing.T, static bool) (base string, widgetHits, rpcHits *int32) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var wh, rh int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
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
	return srv.URL, &wh, &rh
}

// TestStaticMode_SkipsServerBackedRequests: on a page marked static, the
// runtime fetches the dumped catalog FILE (widgets.json) — NOT the live
// session-gated /__gofastr/widgets endpoint — and an RPC click is skipped
// (dispatchRPC is gated). data-fui-open is no longer gated: overlays resolve
// against dumped files on a real export (the empty catalog here makes the
// open a clean no-op).
func TestStaticMode_SkipsServerBackedRequests(t *testing.T) {
	base, widgetHits, rpcHits := startStaticModeServer(t, true)
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
		t.Errorf("static mode must not hit the live /__gofastr/widgets endpoint (it fetches widgets.json instead), got %d hits", got)
	}
	if got := atomic.LoadInt32(rpcHits); got != 0 {
		t.Errorf("static mode must skip RPC dispatch, got %d hits", got)
	}
}

// TestStaticMode_LiveStillFiresRequests is the regression guard: the
// guard must be a no-op on a live page (no marker) — the catalog fetch
// fires on boot and an RPC click still reaches the server.
func TestStaticMode_LiveStillFiresRequests(t *testing.T) {
	base, widgetHits, rpcHits := startStaticModeServer(t, false)
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
	if got := atomic.LoadInt32(rpcHits); got == 0 {
		t.Error("live mode should dispatch the RPC on click (guard must be a no-op without the marker)")
	}
}

// TestStaticMode_RPCShowsNotice: on a static page, clicking a data-fui-rpc
// control must NOT fail silently — it surfaces a "Needs the Go server" notice
// so the user understands why the demo is dead and how to run it live. The
// notice renders synchronously into #fui-nav-toast (the CSP-clean mini toast).
func TestStaticMode_RPCShowsNotice(t *testing.T) {
	base, _, rpcHits := startStaticModeServer(t, true)
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

// TestStaticMode_MissingWidgetShowsNotice: a data-fui-open trigger whose
// target widget isn't registered (e.g. a note-only showcase demo whose modal
// was never mounted) must not fail silently on a static page — openWidget
// surfaces a "Needs the Go server" notice via the synchronous _fallbackToast.
// Serves the REAL widgets split module so the bail path runs end-to-end.
func TestStaticMode_MissingWidgetShowsNotice(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	widgetsMod, ok := Module("widgets")
	if !ok {
		t.Fatal("widgets module not embedded")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/widgets.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(widgetsMod))
	})
	// Empty catalog — the target widget is intentionally absent.
	mux.HandleFunc("/__gofastr/widgets.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html data-fui-static><head><title>static</title></head><body>
  <button id="opener" data-fui-open="never-mounted">open</button>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var toastText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#opener`, chromedp.ByID),
		// openWidget loads the widgets module, finds no catalog entry, and
		// fires _fallbackToast synchronously into [data-fui-toast-fallback].
		chromedp.WaitVisible(`[data-fui-toast-fallback]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-fui-toast-fallback]').textContent`, &toastText),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(toastText, "Needs the Go server") {
		t.Errorf("missing-widget open on static should show 'Needs the Go server'; got %q", toastText)
	}
}
