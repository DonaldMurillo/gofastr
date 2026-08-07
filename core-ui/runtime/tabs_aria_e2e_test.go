package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// framework/ui.Tabs drives the visible highlight via CSS keyed on the
// wrapper's data-active, but assistive tech reads aria-selected on the
// role=tab buttons. The runtime must mirror the new index into
// aria-selected whenever the data-active attribute is written through
// the signal path — otherwise SR users hear the SSR-time selection
// forever.
func TestTabClickUpdatesAriaSelected(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// The exact shape framework/ui.Tabs SSRs: signal-bound wrapper
		// mirroring the signal into data-active, role=tab buttons with
		// data-fui-signal-set + data-fui-tab-index.
		fmt.Fprint(w, `<!doctype html><html><head><title>tabs-aria</title></head><body>
  <div id="wrap" data-fui-signal="tabsig" data-fui-signal-mode="attr"
       data-fui-signal-attr="data-active" data-active="0">
    <nav role="tablist">
      <button id="t0" role="tab" aria-selected="true"  data-fui-tab-index="0" data-fui-signal-set="tabsig:0">A</button>
      <button id="t1" role="tab" aria-selected="false" data-fui-tab-index="1" data-fui-signal-set="tabsig:1">B</button>
    </nav>
    <div role="tabpanel" data-fui-tab-index="0">a</div>
    <div role="tabpanel" data-fui-tab-index="1">b</div>
  </div>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// CI runners intermittently take >20s (the chromedp default)
		// to cold-start Chrome; a generous websocket-URL deadline turns
		// that from a flaky suite failure into a few slow seconds.
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run: allocate against the
	// browser context so the browser's lifetime is the browser context's —
	// passing a timeout context here would make the browser die when that
	// deadline passed. The watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)

	var sel0, sel1, active string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#t1`, chromedp.ByID),
		chromedp.Click(`#t1`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('t0').getAttribute('aria-selected')`, &sel0),
		chromedp.Evaluate(`document.getElementById('t1').getAttribute('aria-selected')`, &sel1),
		chromedp.Evaluate(`document.getElementById('wrap').getAttribute('data-active')`, &active),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if active != "1" {
		t.Fatalf("data-active should be 1 after clicking the second tab, got %q", active)
	}
	if sel1 != "true" {
		t.Errorf("clicked tab must get aria-selected=true, got %q", sel1)
	}
	if sel0 != "false" {
		t.Errorf("previous tab must get aria-selected=false, got %q", sel0)
	}
}
