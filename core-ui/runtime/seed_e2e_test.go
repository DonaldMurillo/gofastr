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

// startSeedE2EServer serves runtime.js plus a page carrying an inline
// gofastr-signals JSON island in <head>. The runtime must read it on
// boot and seed __gofastr._signals BEFORE any interaction, so a pure
// presentational consumer (or any getSignal reader) sees the
// server-provided value on first paint — not undefined.
func startSeedE2EServer(t *testing.T, seedJSON string) string {
	t.Helper()
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
		fmt.Fprintf(w, `<!doctype html>
<html>
<head>
  <title>seed e2e</title>
  <script type="application/json" id="gofastr-signals">%s</script>
</head>
<body>
  <span id="consumer" data-fui-signal="greeting">PLACEHOLDER</span>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body>
</html>`, seedJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newSeedBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// CI runners intermittently take >20s (the chromedp default)
		// to cold-start Chrome; a generous websocket-URL deadline turns
		// that from a flaky suite failure into a few slow seconds.
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1024, 768),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestSeed_GetSignalReturnsSeededValueBeforeInteraction is the core
// gap-#1 proof: with a seed island present, getSignal returns the
// server value on boot, no interaction required.
func TestSeed_GetSignalReturnsSeededValueBeforeInteraction(t *testing.T) {
	base := startSeedE2EServer(t, `{"greeting":"hello","count":5,"open":true}`)
	ctx := newSeedBrowserCtx(t)

	var greeting string
	var count int
	var open bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.getSignal('greeting')`, &greeting),
		chromedp.Evaluate(`window.__gofastr.getSignal('count')`, &count),
		chromedp.Evaluate(`window.__gofastr.getSignal('open')`, &open),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if greeting != "hello" {
		t.Errorf("getSignal('greeting') = %q, want \"hello\" (seed not applied)", greeting)
	}
	if count != 5 {
		t.Errorf("getSignal('count') = %d, want 5", count)
	}
	if !open {
		t.Errorf("getSignal('open') = %v, want true", open)
	}
}

// TestSeed_NoBlockLeavesSignalsEmpty ensures the seeding is additive and
// safe: a page with no seed island behaves exactly as before (undefined).
func TestSeed_NoBlockLeavesSignalsEmpty(t *testing.T) {
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
		fmt.Fprint(w, `<!doctype html><html><head><title>no seed</title></head><body><span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var typ string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`typeof window.__gofastr.getSignal('nope')`, &typ),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if typ != "undefined" {
		t.Errorf("unset signal should be undefined, got %q", typ)
	}
}

// TestSeed_MalformedBlockIsIgnored ensures a corrupt seed island never
// breaks boot — the runtime swallows the parse error and continues.
func TestSeed_MalformedBlockIsIgnored(t *testing.T) {
	base := startSeedE2EServer(t, `{not valid json`)
	ctx := newSeedBrowserCtx(t)

	var ready string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('ready').textContent`, &ready),
	); err != nil {
		t.Fatalf("chromedp (boot broke on malformed seed?): %v", err)
	}
	if ready != "ready" {
		t.Errorf("page did not boot with malformed seed island")
	}
}
