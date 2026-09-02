//go:build chromium

package pluginhost

// Browser proof of the trusted-worker profile (#380). The Go tests pin the
// policy STRING (TestWorkerCSPPolicyShape, TestWorkerCSPDefaultsStrict);
// this pins that a real Chrome enforces the geometry Field Assist runs:
// one origin, the REAL security middleware, a strict host document, and a
// worker whose own response carries the narrow profile.
//
//	host page (strict CSP, no 'unsafe-eval') → external probe script
//	          → new Worker("/__wp/on.js")  — WithWorkerCSP profile
//	          → new Worker("/__wp/off.js") — plain host script, strict CSP
//
// The worker WITH the profile must eval a string and compile WebAssembly;
// the byte-identical worker WITHOUT it must be refused by its response's
// CSP; and the host document itself must refuse eval. The host page's
// logic is an EXTERNAL script because the document CSP carries no
// 'unsafe-inline' — same reason a real plugin page loads its broker by
// <script src>.
//
// Where Safari differs it is covered in prose (workerCSP's doc and the
// plugin-platform page): 'self' is correct for a same-origin non-opaque
// worker in both browsers (unlike the opaque plugin frame), and Safari
// releases that predate 'wasm-unsafe-eval' need 'unsafe-eval' for wasm —
// which is why 'unsafe-eval' is in the worker allowlist at all. Chrome is
// the runnable half of that coverage.
//
// Build tag matches the other framework chromium suites; plain
// `go test ./framework/pluginhost/` never launches a browser.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/chromedp/chromedp"
)

// The probe worker evals a string and compiles the minimal valid wasm
// module (magic \0asm + version 1), reporting both outcomes. postMessage
// fires exactly once, after whichever probe finished last.
const workerProfileProbeJS = `(function () {
	const out = { eval: "pending", wasm: "pending" };
	let posted = false;
	const done = function () {
		if (!posted) {
			posted = true;
			postMessage(out);
		}
	};
	try {
		eval("1");
		out.eval = "ok";
	} catch (e) {
		out.eval = "err " + String((e && e.message) || e);
	}
	const bytes = new Uint8Array([0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00]);
	try {
		WebAssembly.compile(bytes).then(
			function () {
				out.wasm = "ok";
				done();
			},
			function (e) {
				out.wasm = "err " + String((e && e.message) || e);
				done();
			}
		);
	} catch (e) {
		out.wasm = "err " + String((e && e.message) || e);
		done();
	}
})();
`

const workerProfileHostPage = `<!doctype html>
<meta charset="utf-8">
<title>worker profile proof</title>
<div id="hosteval">pending</div>
<div id="on">pending</div>
<div id="off">pending</div>
<script src="/__wp/probe.js"></script>
`

// The host probe runs under the document's strict CSP: its own eval must
// fail, and it wires the result sinks for both workers.
const workerProfileHostProbeJS = `(function () {
	const set = function (id, text) {
		document.getElementById(id).textContent = text;
	};
	try {
		eval("1");
		set("hosteval", "ok");
	} catch (e) {
		set("hosteval", "err " + String((e && e.message) || e));
	}
	const report = function (id) {
		return function (e) {
			const d = e.data || {};
			set(id, d.eval + "/" + d.wasm);
		};
	};
	const on = new Worker("/__wp/on.js");
	on.onmessage = report("on");
	const off = new Worker("/__wp/off.js");
	off.onmessage = report("off");
})();
`

func TestWorkerProfileLoadsAndCompiles(t *testing.T) {
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("worker profile proof requires Chrome, Chromium, or Edge")
	}

	srv := NewAssetServer(fstest.MapFS{}, "/__wp", nil)
	srv.AddBytes("/__wp/host.html", "text/html; charset=utf-8", false, []byte(workerProfileHostPage))
	srv.AddBytes("/__wp/probe.js", "text/javascript; charset=utf-8", false, []byte(workerProfileHostProbeJS))
	srv.AddBytes("/__wp/on.js", "text/javascript; charset=utf-8", false, []byte(workerProfileProbeJS),
		WithWorkerCSP(WorkerCSP{
			ScriptKeywords: []string{"'unsafe-eval'"},
			ConnectSources: []string{"'self'"},
			WASM:           true,
		}),
		WithCache(CachePrivateNoStore))
	srv.AddBytes("/__wp/off.js", "text/javascript; charset=utf-8", false, []byte(workerProfileProbeJS))
	rt := router.New()
	srv.Register(rt)
	// The production geometry: the app's security middleware wraps the
	// router, so the host document and the plain worker get the strict
	// default CSP and only /__wp/on.js carries the profile.
	hs := httptest.NewServer(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(rt))
	defer hs.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox,
			chromedp.ExecPath(path),
		)...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	defer cancel()

	var hostEval, on, off string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(hs.URL+"/__wp/host.html"),
		chromedp.Poll(`(function () {
			var a = document.getElementById('hosteval').textContent;
			var b = document.getElementById('on').textContent;
			var c = document.getElementById('off').textContent;
			if (a === 'pending' || b === 'pending' || c === 'pending') return '';
			return a + '|' + b + '|' + c;
		})()`, &hostEval, chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("drive worker profile page: %v", err)
	}
	parts := strings.Split(hostEval, "|")
	if len(parts) != 3 {
		t.Fatalf("probe output malformed: %q", hostEval)
	}
	hostEval, on, off = parts[0], parts[1], parts[2]

	if !strings.HasPrefix(hostEval, "err") {
		t.Errorf("host document must refuse eval (strict CSP), got %q", hostEval)
	}
	low := strings.ToLower(hostEval)
	if !strings.Contains(low, "unsafe-eval") && !strings.Contains(low, "content security policy") && !strings.Contains(low, "csp") {
		t.Errorf("host eval refusal must be the CSP gate (not a harness error), got %q", hostEval)
	}
	if on != "ok/ok" {
		t.Errorf("worker WITH the profile: eval and wasm compile must succeed, got %q", on)
	}
	for _, half := range strings.Split(off, "/") {
		if !strings.HasPrefix(half, "err") {
			t.Errorf("worker WITHOUT the profile: every probe must fail under the strict response CSP, got %q", off)
			break
		}
	}
	low = strings.ToLower(off)
	if !strings.Contains(low, "unsafe-eval") && !strings.Contains(low, "wasm-eval") && !strings.Contains(low, "content security policy") && !strings.Contains(low, "csp") {
		t.Errorf("strict worker refusals must be CSP gates (not harness errors), got %q", off)
	}
	t.Logf("strict worker rejection: %s", off)
}
