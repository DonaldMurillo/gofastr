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

// TestRpcNavigateRefusesJavascriptScheme — security: a successful
// RPC whose data-fui-rpc-navigate carries an attacker-controlled
// javascript: URL must NOT reach history.pushState / replaceState.
// The widget path routes through NS.navigate (which applies the
// _isUnsafeSignalUrl guard); the CORE dispatchRPC path must do the
// same. We spy on history.pushState/replaceState and assert the
// unsafe URL never lands there. (Modern Chrome also blocks a
// javascript: pushState at the browser layer, but the guard is the
// framework's choke point — defense in depth + older engines.)
func TestRpcNavigateRefusesJavascriptScheme(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var mutateN atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/mutate", func(w http.ResponseWriter, r *http.Request) {
		mutateN.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>nav-guard</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
</head><body>
  <main role="main" tabindex="-1">
    <button id="mut" data-fui-rpc="/mutate" data-fui-rpc-method="POST"
            data-fui-rpc-navigate="javascript:alert(1)">mutate</button>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var joined string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Spy on pushState/replaceState BEFORE the click so any URL the
		// navigate path hands to history is recorded.
		chromedp.Evaluate(`(function(){
			window.__navCalls=[];
			var ps=history.pushState.bind(history);
			var rs=history.replaceState.bind(history);
			history.pushState=function(s,t,u){window.__navCalls.push('P'+String(u));try{return ps(s,t,u)}catch(e){}};
			history.replaceState=function(s,t,u){window.__navCalls.push('R'+String(u));try{return rs(s,t,u)}catch(e){}};
		})();`, nil),
		chromedp.Click(`#mut`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(window.__navCalls||[]).join('\n')`, &joined),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if mutateN.Load() < 1 {
		t.Fatal("RPC /mutate was never called — click did not exercise the navigate path; test is vacuous")
	}
	if joined != "" && contains(joined, "javascript:") {
		t.Errorf("SECURITY: data-fui-rpc-navigate=javascript: reached history API — guard bypassed:\n%s", joined)
	}
}
