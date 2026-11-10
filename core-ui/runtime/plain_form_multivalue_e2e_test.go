package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A plain form with enctype="application/json" posts every successful
// control, and a name that repeats (a checkbox group, a multi-select)
// posts every value it holds. The bridge built its body with
// `obj[k] = v`: the last value won and the rest never left the page,
// the encoding the data-cui-rpc path dropped (see
// TestDispatchRPC_MultiValueFormKeys). Both paths now share one rule: an
// array when a name repeats, a scalar when it occurs once, and the
// hidden+checkbox pair a bool field renders stays one value.
func TestPlainJSONForm_MultiValueFormKeys(t *testing.T) {
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
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
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
<form id="f" action="/save" method="post" enctype="application/json">
  <input name="single" value="x">
  <input name="constructor" value="admin">
  <input type="checkbox" name="tag" value="a" checked>
  <input type="checkbox" name="tag" value="b" checked>
  <select name="role" multiple>
    <option value="r1" selected>r1</option>
    <option value="r2">r2</option>
    <option value="r3" selected>r3</option>
  </select>
  <input type="hidden" name="on" value="false">
  <input type="checkbox" name="on" value="true" checked>
  <input type="hidden" name="off" value="false">
  <input type="checkbox" name="off" value="true">
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
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("form posted %d time(s), want 1", hits)
	}
	if got["single"] != "x" {
		t.Errorf("single=%v, want \"x\": a name that occurs once stays a scalar", got["single"])
	}
	if got["constructor"] != "admin" {
		t.Errorf("constructor=%v, want \"admin\": a field named after a prototype member stays a scalar", got["constructor"])
	}
	for name, want := range map[string][]string{"tag": {"a", "b"}, "role": {"r1", "r3"}} {
		vals, _ := got[name].([]any)
		if len(vals) != len(want) {
			t.Errorf("%s=%v, want %v: a repeated name posts every value", name, got[name], want)
			continue
		}
		for i, w := range want {
			if fmt.Sprint(vals[i]) != w {
				t.Errorf("%s=%v, want %v", name, got[name], want)
				break
			}
		}
	}
	if got["on"] != "true" {
		t.Errorf("on=%v, want \"true\": a checked hidden+checkbox pair is the checkbox's one value", got["on"])
	}
	if got["off"] != "false" {
		t.Errorf("off=%v, want \"false\": an unchecked hidden+checkbox pair is the hidden's one value", got["off"])
	}
}
