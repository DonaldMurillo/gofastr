package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestFanout_ProducerUpdatesConsumersWithoutPerConsumerRequests is the
// headline proof of the store primitive: one producer RPC updates every
// presentational consumer client-side, with NO server round-trip per
// consumer. We assert (a) both consumers reflect the new value, and
// (b) the only dynamic request the click triggered was the single
// producer RPC — every other path counter stays zero.
func TestFanout_ProducerUpdatesConsumersWithoutPerConsumerRequests(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var renameHits, unexpectedHits int64
	var unexpectedMu sync.Mutex
	var unexpectedPaths []string

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/rename", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&renameHits, 1)
		// The response body becomes the new signal value (data-fui-rpc-signal).
		fmt.Fprint(w, "NewCo")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Ignore browser-default noise (favicon, devtools probes) — we
			// only care about app requests a consumer might trigger.
			p := r.URL.Path
			// Ignore browser-default noise: favicon, devtools probes,
			// sourcemap fetches, any /__gofastr asset path.
			noise := p == "/favicon.ico" ||
				strings.HasPrefix(p, "/.well-known") ||
				strings.HasPrefix(p, "/__gofastr") ||
				strings.HasSuffix(p, ".map")
			if !noise {
				atomic.AddInt64(&unexpectedHits, 1)
				unexpectedMu.Lock()
				unexpectedPaths = append(unexpectedPaths, p)
				unexpectedMu.Unlock()
			}
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-signals">{"org.company":"Acme"}</script>
</head><body>
  <span id="c1" data-fui-signal="org.company">Acme</span>
  <strong id="c2" data-fui-signal="org.company">Acme</strong>
  <button id="producer" data-fui-rpc="/rename" data-fui-rpc-method="POST" data-fui-rpc-signal="org.company">Rename</button>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var c1Before, c2Before, c1After, c2After string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Seeded value is present before interaction.
		chromedp.Text(`#c1`, &c1Before, chromedp.ByID),
		chromedp.Text(`#c2`, &c2Before, chromedp.ByID),
		// One producer click.
		chromedp.Click(`#producer`, chromedp.ByID),
		// Wait until the fan-out lands.
		chromedp.Poll(`document.getElementById('c1').textContent === 'NewCo'`, nil),
		chromedp.Text(`#c1`, &c1After, chromedp.ByID),
		chromedp.Text(`#c2`, &c2After, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if c1Before != "Acme" || c2Before != "Acme" {
		t.Errorf("seed not stamped before interaction: c1=%q c2=%q", c1Before, c2Before)
	}
	if c1After != "NewCo" || c2After != "NewCo" {
		t.Errorf("fan-out failed: c1=%q c2=%q, want both NewCo", c1After, c2After)
	}
	if n := atomic.LoadInt64(&renameHits); n != 1 {
		t.Errorf("producer RPC hit %d times, want exactly 1", n)
	}
	if n := atomic.LoadInt64(&unexpectedHits); n != 0 {
		unexpectedMu.Lock()
		paths := unexpectedPaths
		unexpectedMu.Unlock()
		t.Errorf("a consumer triggered %d unexpected server request(s) %v — fan-out should be pure client-side", n, paths)
	}
}
