package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	gofastrruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/chromedp/chromedp"
)

const computeWorkerJS = `
var calls = 0;
self.onmessage = async function (event) {
  var msg = event.data;
  if (msg.fn === 'hang') return;
  try {
    var result;
    if (msg.fn === 'count') {
      calls++;
      result = calls;
    } else if (msg.fn === 'sum') {
      var loaded = await WebAssembly.instantiateStreaming(fetch(msg.payload.wasmURL));
      result = loaded.instance.exports.sum(msg.payload.a, msg.payload.b);
    } else if (msg.fn === 'fail') {
      throw new Error('worker rejected task');
    } else {
      throw new Error('unknown function');
    }
    self.postMessage({ id: msg.id, ok: true, result: result });
  } catch (err) {
    self.postMessage({ id: msg.id, ok: false, error: err.message || String(err) });
  }
};`

const crashWorkerJS = `self.onmessage = function () { throw new Error('worker crashed'); };`

var sumWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x07, 0x01, 0x03, 0x73, 0x75, 0x6d, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
}

func TestComputeMarkerPreloads(t *testing.T) {
	got := gofastrruntime.NeededModules(`<div data-fui-compute></div>`)
	if want := []string{"compute"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NeededModules=%v want %v", got, want)
	}
}

func TestComputeMarkerMatchesRuntimeJS(t *testing.T) {
	src, err := os.ReadFile("runtime.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `{ name: 'compute',`) ||
		!strings.Contains(string(src), `selector: '[data-fui-compute]'`) {
		t.Fatal("runtime scanner missing data-fui-compute marker")
	}
}

func TestComputeTaskRoundTrip(t *testing.T) {
	base := startComputeServer(t)
	ctx := newComputeBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`window.__gofastr && window.__gofastr.compute`, nil),
		chromedp.Evaluate(`window.__computeRoundTrip = 'pending'; (async function () {
  var c = window.__gofastr.compute;
  var first = await c.task('e2e-worker', 'count', null);
  var second = await c.task('e2e-worker', 'count', null);
  var wasmURL = c.wasmURL('e2e-wasm');
  var sum = await c.task('e2e-worker', 'sum', { wasmURL: wasmURL, a: 19, b: 23 });
  c.dispose('e2e-worker');
  var afterDispose = await c.task('e2e-worker', 'count', null);
  return JSON.stringify({ first: first, second: second, afterDispose: afterDispose, wasmURL: wasmURL, sum: sum });
})().then(function (result) { window.__computeRoundTrip = result; },
          function (error) { window.__computeRoundTrip = 'ERROR:' + error.message; });`, nil),
		chromedp.Poll(`window.__computeRoundTrip !== 'pending'`, nil),
		chromedp.Evaluate(`window.__computeRoundTrip`, &raw),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var got struct {
		First        int    `json:"first"`
		Second       int    `json:"second"`
		AfterDispose int    `json:"afterDispose"`
		WASMURL      string `json:"wasmURL"`
		Sum          int    `json:"sum"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("result JSON %q: %v", raw, err)
	}
	wasm, _ := compute.LookupWASM("e2e-wasm")
	wantURL := "/__gofastr/compute/e2e-wasm.wasm?v=" + wasm.Hash()
	if got.First != 1 || got.Second != 2 || got.AfterDispose != 1 {
		t.Fatalf("worker reuse/dispose counts=%d,%d,%d", got.First, got.Second, got.AfterDispose)
	}
	if got.WASMURL != wantURL {
		t.Fatalf("wasmURL=%q want %q", got.WASMURL, wantURL)
	}
	if got.Sum != 42 {
		t.Fatalf("sum=%d want 42", got.Sum)
	}
}

func TestComputeTaskRejects(t *testing.T) {
	base := startComputeServer(t)
	ctx := newComputeBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`window.__gofastr && window.__gofastr.compute`, nil),
		chromedp.Evaluate(`window.__computeRejects = 'pending'; (async function () {
  var c = window.__gofastr.compute;
  var out = {};
  try { await c.task('e2e-worker', 'fail', null); } catch (err) { out.protocol = err.message; }
  try { await c.task('e2e-crash-worker', 'count', null); } catch (err) { out.worker = err.message; }
  var realSetTimeout = window.setTimeout;
  window.setTimeout = function (fn, ms) {
    return realSetTimeout(fn, ms === 30000 ? 25 : ms);
  };
  try { await c.task('e2e-worker', 'hang', null); } catch (err) { out.timeout = err.message; }
  finally { window.setTimeout = realSetTimeout; }
  return JSON.stringify(out);
})().then(function (result) { window.__computeRejects = result; },
          function (error) { window.__computeRejects = 'ERROR:' + error.message; });`, nil),
		chromedp.Poll(`window.__computeRejects !== 'pending'`, nil),
		chromedp.Evaluate(`window.__computeRejects`, &raw),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("result JSON: %v", err)
	}
	if got["protocol"] != "worker rejected task" {
		t.Fatalf("protocol error=%q", got["protocol"])
	}
	if !strings.Contains(got["worker"], "worker crashed") {
		t.Fatalf("worker error=%q", got["worker"])
	}
	if !strings.Contains(got["timeout"], "timed out") {
		t.Fatalf("timeout error=%q", got["timeout"])
	}
}

func startComputeServer(t *testing.T) string {
	t.Helper()
	compute.RegisterWorker("e2e-worker", []byte(computeWorkerJS))
	compute.RegisterWorker("e2e-crash-worker", []byte(crashWorkerJS))
	compute.RegisterWASM("e2e-wasm", sumWASM)

	core, err := gofastrruntime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	module, ok := gofastrruntime.Module("compute")
	if !ok {
		t.Fatal("compute module not embedded")
	}
	moduleHash := widget.RuntimeModuleHash("compute")
	worker, _ := compute.LookupWorker("e2e-worker")
	crash, _ := compute.LookupWorker("e2e-crash-worker")
	wasm, _ := compute.LookupWASM("e2e-wasm")
	assetHashes := map[string]string{
		"/__gofastr/compute/e2e-worker.js":       worker.Hash(),
		"/__gofastr/compute/e2e-crash-worker.js": crash.Hash(),
		"/__gofastr/compute/e2e-wasm.wasm":       wasm.Hash(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(core))
	})
	mux.HandleFunc("/__gofastr/runtime/compute.js", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != moduleHash {
			http.Error(w, "missing module version", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(module))
	})
	mux.HandleFunc("/__gofastr/compute/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != assetHashes[r.URL.Path] {
			http.Error(w, "missing asset version", http.StatusBadRequest)
			return
		}
		widget.ServeComputeAsset(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head>%s</head><body>
<div id="compute-trigger" data-fui-compute></div>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, widget.RuntimeModuleManifestScript())
	})
	handler := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(mux)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newComputeBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1024, 768),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}
