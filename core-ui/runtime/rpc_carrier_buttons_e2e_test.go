package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A data-cui-rpc carrier that is not a form serializes its named,
// enabled controls "exactly like a form's". A form never submits a
// button input it was not submitted with, but the carrier's own walk
// took every input[name]: type=button, submit, reset and image inputs
// rode each request with their label as the value.
func TestNonFormCarrierLeavesButtonInputsOut(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	js := MustRuntimeJS()
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = io.WriteString(w, js)
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><html><body>
<div id="carrier" data-cui-rpc="/rpc" data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="50">
  <input id="q" name="q">
  <input type="hidden" name="kept" value="h">
  <input type="button" name="btn" value="B">
  <input type="submit" name="sub" value="S">
  <input type="reset" name="rst" value="R">
  <input type="image" name="img" alt="I">
</div>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitReady(`#q`, chromedp.ByQuery),
		chromedp.SendKeys(`#q`, "abc", chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the carrier sent nothing")
	}
	last := bodies[len(bodies)-1]
	if last["q"] != "abc" || last["kept"] != "h" {
		t.Fatalf("the carrier's own fields are missing: %v", last)
	}
	for _, name := range []string{"btn", "sub", "rst", "img"} {
		if _, ok := last[name]; ok {
			t.Errorf("button input %q rode the body (%v): a form never submits one it was not submitted with", name, last)
		}
	}
}
