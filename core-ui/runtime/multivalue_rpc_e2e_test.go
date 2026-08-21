package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_MultiValueFormKeys pins that a data-fui-rpc form with
// repeated field names (checkbox group, multi-select) preserves EVERY
// value in the JSON body. The old encoding `obj[k] = v` was last-wins,
// silently dropping all but the last value. The fix emits an array when
// a key repeats and a scalar when it occurs once, matching the
// repeated-key shape the GET + urlencoded paths already produce.
func TestDispatchRPC_MultiValueFormKeys(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	hits := 0

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
		hits++
		got = m
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>mv</title></head><body>
<form id="f" data-fui-rpc="/rpc/save" data-fui-rpc-method="POST">
  <input name="single" value="x">
  <input name="constructor" value="admin">
  <input type="checkbox" name="tag" value="a" checked>
  <input type="checkbox" name="tag" value="b" checked>
  <button type="submit" id="go">Go</button>
</form>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("form RPC hit %d time(s), want 1", hits)
	}
	if got["single"] != "x" {
		t.Errorf("single=%v, want \"x\" (a one-shot field must stay a scalar)", got["single"])
	}
	// A field named after an Object.prototype member must stay a scalar.
	// On a plain {} accumulator, obj["constructor"] resolves up the
	// prototype chain and is never undefined, so this posted [null,"admin"].
	if got["constructor"] != "admin" {
		t.Errorf("constructor=%v, want \"admin\" (a field named after a prototype member must stay a scalar)", got["constructor"])
	}
	tag, _ := got["tag"].([]any)
	if len(tag) != 2 || fmt.Sprint(tag[0]) != "a" || fmt.Sprint(tag[1]) != "b" {
		t.Errorf("tag=%v, want [a b] — a repeated field must serialize as an array preserving every value (last-wins dropped all but the last)", got["tag"])
	}
}
