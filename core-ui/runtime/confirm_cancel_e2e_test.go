package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_ConfirmCancelDoesNotAbortInFlight pins fix #7: the
// pre-flight confirm MUST run before the per-signal AbortController
// setup. Previously the in-flight abort happened BEFORE the confirm, so
// a cancel still aborted the previous in-flight request for that signal
// and left a stale AbortController in _rpcInFlight (the early return
// preceded the try/finally that owns the slot).
//
// Observable shape: a slow signal-bound RPC (click 1, confirm=true) is
// in flight; a second click with confirm overridden to false cancels.
// With the bug, the cancel aborts click 1's fetch (AbortError → the
// signal never receives the value). With the fix, click 1 completes and
// the signal lands.
func TestDispatchRPC_ConfirmCancelDoesNotAbortInFlight(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	var completed int32
	mux.HandleFunc("/rpc/slow", func(w http.ResponseWriter, r *http.Request) {
		// Slow enough that click 2's cancel lands while click 1 is in flight.
		time.Sleep(600 * time.Millisecond)
		atomic.AddInt32(&completed, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"v":1}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>cc</title></head><body>
<button id="b" data-fui-rpc="/rpc/slow" data-fui-rpc-signal="sig" data-fui-confirm="Sure?">Go</button>
<span id="out" data-fui-signal="sig"></span>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var sigVal any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		// Click 1: confirm=true → slow RPC enters flight.
		chromedp.Evaluate(`window.confirm=function(){return true;};`, nil),
		chromedp.Click(`#b`, chromedp.ByID),
		// Click 2 while click 1 is still in flight: confirm=false → cancel.
		chromedp.Evaluate(`window.confirm=function(){return false;};`, nil),
		chromedp.Click(`#b`, chromedp.ByID),
		// Let the slow /rpc/slow finish.
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr?._signals?.["sig"]?.value`, &sigVal),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	m, _ := sigVal.(map[string]any)
	if m == nil || m["v"] != float64(1) {
		t.Errorf("confirm-cancel aborted the in-flight RPC: signal sig = %#v, want {v:1} — the cancel must not touch _rpcInFlight (confirm must run before the abort setup)", sigVal)
	}
}
