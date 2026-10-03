package runtime

import (
	"encoding/json"
	"fmt"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_HiddenCheckboxPairIsScalar pins the HTML checkbox
// idiom: a hidden input followed by a checkbox of the same name
// (hidden "false", checkbox "true") must serialize as ONE value, the
// checkbox's when checked and the hidden's otherwise. The repeated-key
// rule turned a checked pair into ["false","true"], which a bool
// validator refuses: the desktop-notes settings form could save false
// but never true, and the user's notifications stayed off.
//
// A checkbox GROUP (two checkboxes, no hidden) still serializes as an
// array: TestDispatchRPC_MultiValueFormKeys holds that side.
func TestDispatchRPC_HiddenCheckboxPairIsScalar(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any

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
	mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>pair</title></head><body>
<form id="f" data-fui-rpc="/rpc/save" data-fui-rpc-method="POST">
  <input type="hidden" name="on" value="false">
  <input type="checkbox" id="on" name="on" value="true" checked>
  <input type="hidden" name="off" value="false">
  <input type="checkbox" id="off" name="off" value="true">
  <input type="checkbox" name="tag" value="a" checked>
  <input type="checkbox" name="tag" value="b" checked>
  <button type="submit" id="go">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		// Flip both boxes and submit again.
		chromedp.Click(`#on`, chromedp.ByID),
		chromedp.Click(`#off`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("form RPC hit %d time(s), want 2", len(bodies))
	}
	first, second := bodies[0], bodies[1]
	if first["on"] != "true" {
		t.Errorf("checked pair = %v, want the scalar \"true\"", first["on"])
	}
	if first["off"] != "false" {
		t.Errorf("unchecked pair = %v, want the scalar \"false\"", first["off"])
	}
	if second["on"] != "false" || second["off"] != "true" {
		t.Errorf("after flipping: on=%v off=%v, want \"false\" and \"true\"", second["on"], second["off"])
	}
	tag, _ := first["tag"].([]any)
	if len(tag) != 2 {
		t.Errorf("tag=%v, want [a b]: a checkbox group is still an array", first["tag"])
	}
}
