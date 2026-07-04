package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A <select> change is delivered as an `input` event in every modern
// browser, so a hand-written island can drive a select-triggered RPC with
// the documented recipe — wrap the control in
// `<form data-fui-rpc data-fui-rpc-trigger="input">` — WITHOUT any dedicated
// data-fui-rpc-trigger="change". This test is the empirical proof behind the
// recipe in framework/docs/content/interactive-patterns.md: it selects an
// option and asserts the runtime posted the control's `name` as the JSON key.
func TestInputTrigger_SelectFiresRPC(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	var last atomic.Pointer[map[string]string]
	var hits atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/filter", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last.Store(&body)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// debounce-ms=1 keeps the test fast; the select still rides the
		// `input` event the same as any keystroke would.
		fmt.Fprint(w, `<!doctype html><html><head><title>select-trigger</title></head><body>
  <form data-fui-rpc="/filter" data-fui-rpc-method="POST"
        data-fui-rpc-trigger="input" data-fui-rpc-debounce-ms="1">
    <select id="cat" name="category">
      <option value="all">All</option>
      <option value="tools">Tools</option>
      <option value="parts">Parts</option>
    </select>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	t.Cleanup(cancel)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#cat`, chromedp.ByID),
		chromedp.SetValue(`#cat`, "parts", chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if hits.Load() == 0 {
		t.Fatal("selecting an option did not fire the input-triggered RPC — the wrap-in-form recipe is broken")
	}
	got := last.Load()
	if got == nil {
		t.Fatal("RPC fired but no body recorded")
	}
	// The JSON key is the control's `name` attribute, not its id.
	if (*got)["category"] != "parts" {
		t.Fatalf("expected {\"category\":\"parts\"}, got %v", *got)
	}
}
