//go:build chromium

package pluginhost

// Browser proof of the wasm opt-in tier (#255). The Go-level tests pin the
// policy STRING (TestFramedCSPWasmTierScriptSrcOnly,
// TestFramedCSPDefaultByteIdentical); this test pins that a real Chrome
// enforces the gate in the framed geometry the wysiwyg editor uses:
//
//	host page → sandbox="allow-scripts" iframe (opaque origin)
//	          → external probe script (no 'unsafe-inline' in the frame)
//	          → WebAssembly.instantiate(minimal module)
//
// The frame served WITH the tier must instantiate; the byte-identical frame
// WITHOUT it must reject the same call with a CSP error. The frames report
// back over postMessage (the pluginhost bridge pattern) and the host page
// writes the results into its own DOM, which chromedp reads — the opaque
// frames' internals are unreachable by design.
//
// Build tag matches the other framework chromium suites (framework/ui);
// plain `go test ./framework/pluginhost/` never launches a browser.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/chromedp/chromedp"
)

// The frame document loads its logic as an EXTERNAL script: the framed CSP
// carries no 'unsafe-inline', exactly like a real plugin frame.
const wasmTierFrameDoc = `<!doctype html>
<meta charset="utf-8">
<script src="probe.js"></script>
`

// The probe compiles the minimal valid wasm module (magic \0asm + version 1,
// 8 bytes, no imports or exports) and reports the outcome to the host page,
// tagged by query param so one host page can carry both frames.
const wasmTierProbeJS = `(function () {
	var tag = new URLSearchParams(location.search).get("tag") || "?";
	var bytes = new Uint8Array([0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00]);
	WebAssembly.instantiate(bytes).then(
		function () { parent.postMessage({ tag: tag, ok: true }, "*"); },
		function (e) {
			parent.postMessage({ tag: tag, ok: false, err: String((e && e.message) || e) }, "*");
		}
	);
})();
`

const wasmTierHostPage = `<!doctype html>
<meta charset="utf-8">
<title>wasm tier proof</title>
<iframe sandbox="allow-scripts" src="/__wt/on/frame.html?tag=on" width="8" height="8"></iframe>
<iframe sandbox="allow-scripts" src="/__wt/off/frame.html?tag=off" width="8" height="8"></iframe>
<div id="on">pending</div>
<div id="off">pending</div>
<script>
window.addEventListener("message", function (e) {
	var d = e.data;
	if (!d || !d.tag) return;
	var el = document.getElementById(d.tag);
	if (el) el.textContent = d.ok ? "ok" : "err " + d.err;
});
</script>
`

func TestWasmTierInstantiatesInFrame(t *testing.T) {
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("wasm tier proof requires Chrome, Chromium, or Edge")
	}

	specs := []AssetSpec{
		{Name: "frame.html", ContentType: "text/html; charset=utf-8", Framed: true},
		{Name: "probe.js", ContentType: "text/javascript; charset=utf-8", Framed: true},
	}
	fsys := fstest.MapFS{
		"frame.html": &fstest.MapFile{Data: []byte(wasmTierFrameDoc)},
		"probe.js":   &fstest.MapFile{Data: []byte(wasmTierProbeJS)},
	}
	rt := router.New()
	NewAssetServer(fsys, "/__wt/on", specs).
		WithCSP([]string{"'wasm-unsafe-eval'"}).Register(rt)
	NewAssetServer(fsys, "/__wt/off", specs).Register(rt)
	host := NewAssetServer(fstest.MapFS{}, "/__wt", nil)
	host.AddBytes("/__wt/host.html", "text/html; charset=utf-8", false, []byte(wasmTierHostPage))
	host.Register(rt)
	srv := httptest.NewServer(rt)
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			// A cold Chrome start on a shared runner can take longer than
			// chromedp's 20s default wait for the DevTools websocket URL, which
			// surfaces as "websocket url timeout reached" and looks like a test
			// failure rather than a slow launch. The cmd/gofastr chromedp tests
			// already raise it to 90s; these did not.
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox,
			chromedp.ExecPath(path),
		)...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	defer cancel()

	// Poll returns "" (falsy) until BOTH frames have reported, then the
	// combined results land in one string.
	var res string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/__wt/host.html"),
		chromedp.Poll(`(function () {
			var a = document.getElementById('on').textContent;
			var b = document.getElementById('off').textContent;
			if (a === 'pending' || b === 'pending') return '';
			return a + '|' + b;
		})()`, &res, chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("drive wasm tier page: %v", err)
	}
	on, off, _ := strings.Cut(res, "|")
	if on != "ok" {
		t.Errorf("frame WITH the tier: WebAssembly.instantiate must succeed, got %q", on)
	}
	if !strings.HasPrefix(off, "err") {
		t.Errorf("frame WITHOUT the tier: instantiate must fail, got %q", off)
	}
	low := strings.ToLower(off)
	if !strings.Contains(low, "wasm-unsafe-eval") && !strings.Contains(low, "content security policy") && !strings.Contains(low, "csp") {
		t.Errorf("failure must be the CSP gate (not a harness error), got %q", off)
	}
	t.Logf("tier-off rejection: %s", off)
}
