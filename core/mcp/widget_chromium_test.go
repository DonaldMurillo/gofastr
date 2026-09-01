//go:build chromium

package mcp

// Browser e2e for the MCP Apps widget client (widgetclient.js), the piece
// issue #291's testing section asks for: read the `ui://` widget over
// resources/read, then drive a tool-call round trip through the embed
// runtime. widgetclient_test.go pins the client by string-matching its
// source; before this file, nothing had ever executed the handshake in a
// browser.
//
// This test is a HOST SHIM: it plays the chat host's half of the MCP Apps
// protocol (ext-apps spec 2026-01-26) in a parent page, embeds the widget
// document in a sandboxed iframe exactly as a host would (allow-scripts,
// opaque origin, srcdoc), and proves the client's postMessage protocol
// works end to end against a real core/mcp Server: the ui/initialize
// handshake, the host-theme application on the document root, a tools/call
// forwarded by the host over HTTP JSON-RPC, and the ui/message the widget
// sends back. It does NOT prove interop with Claude or ChatGPT — only that
// a spec-faithful host and this client agree on the wire.
//
// Why the negatives exist: WidgetClientHandler sets
// Cross-Origin-Resource-Policy: cross-origin because the opaque ("null")
// origin frame's <script src> is a cross-origin no-cors fetch — the repo's
// global security middleware defaults every response to CORP same-origin,
// which blocks that fetch (verified in this very browser by the probe run
// during development: same-origin blocked, absent header allowed). And
// agent-host.md's #1 authoring mistake is a one-character typo in the
// client script URL: a widget that renders and silently never receives
// anything, with no error anywhere. Both failures are invisible to every
// existing test; here they are asserted by watching initialize never come.
//
// Harness rules: build tag chromium (plain `go test ./core/mcp/` never
// launches a browser), no t.Parallel, one Chrome per subtest closed in
// t.Cleanup, every wait bounded. Console errors from the parent AND the
// frame are captured via chromedp runtime/log events — plus a guaranteed
// channel: the widget script forwards its own error events back over
// postMessage, because a silent failure in the client is the worst
// outcome here.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// ── Widget + host-shim fixtures ─────────────────────────────────────────────

// widgetE2EScript is the widget author's script (WidgetDocument.Script).
// It drives the full round trip and reports through the protocol itself:
// connect → callTool(echo) → ui/message carrying the serialized tools/call
// result plus the document root's data-theme, proving the host-theme
// application. The error-forwarding preamble is test instrumentation: an
// opaque-origin frame's console is not reliably reachable from CDP, so the
// frame reports its own failure (client missing, thrown error) to the host
// shim over postMessage.
const widgetE2EScript = `window.addEventListener("error", function (e) {
  try { parent.postMessage({ harnessError: "frame-error:" + ((e && e.message) || String(e)) }, "*"); } catch (_) {}
});
var app = window.__gofastrMcpApp;
if (!app) {
  parent.postMessage({ harnessError: "widget client missing: window.__gofastrMcpApp is undefined" }, "*");
} else {
  app.connect({ availableDisplayModes: ["inline"] }).then(function (host) {
    return app.callTool({ name: "echo", arguments: { value: "ping" } });
  }).then(function (res) {
    var theme = document.documentElement.getAttribute("data-theme") || "no-theme";
    return app.message({ role: "user", content: [{ type: "text", text: "echo-result:" + JSON.stringify(res) + " theme=" + theme }] });
  }).catch(function (e) {
    parent.postMessage({ harnessError: "widget flow failed:" + JSON.stringify(e) }, "*");
    app.message({ role: "user", content: [{ type: "text", text: "error:" + JSON.stringify(e) }] });
  });
}`

// widgetE2EHostPage is the host shim: a plain parent page that plays the
// chat host. It fetches the widget HTML through the REAL protocol (a
// JSON-RPC resources/read POST to /mcp, the transport's content-type and
// Accept gates included), embeds it in a sandbox="allow-scripts" srcdoc
// iframe — opaque origin, exactly what a host does and exactly why the
// client script needs the CORP relaxation — and answers the client per
// widgetclient.js's expectations: ui/initialize gets a host result with a
// dark hostContext, tools/call is forwarded to the MCP endpoint over
// fetch and the JSON-RPC response posted back verbatim, ui/message is
// recorded and acked. Everything the frame sends lands in window.__log.
const widgetE2EHostPage = `<!doctype html>
<meta charset="utf-8">
<title>mcp apps host shim</title>
<body>
<script>
(function () {
  "use strict";
  var nextHostRPCID = 1;
  window.__log = [];
  window.__harnessErrors = [];
  window.__widgetHTML = "";

  function rpc(method, params) {
    return fetch("/mcp", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream"
      },
      body: JSON.stringify({ jsonrpc: "2.0", id: nextHostRPCID++, method: method, params: params })
    }).then(function (r) { return r.json(); });
  }

  window.addEventListener("message", function (ev) {
    var d = ev.data;
    if (!d || typeof d !== "object") return;
    if (typeof d.harnessError === "string") { window.__harnessErrors.push(d.harnessError); return; }
    if (d.jsonrpc !== "2.0") return;
    window.__log.push(d);
    var src = ev.source;
    function reply(msg) { src.postMessage(msg, "*"); }
    if (d.method === "ui/initialize" && d.id !== undefined) {
      reply({ jsonrpc: "2.0", id: d.id, result: {
        protocolVersion: "2026-01-26",
        hostCapabilities: {},
        hostInfo: { name: "shim", version: "0" },
        hostContext: { theme: "dark" }
      } });
      return;
    }
    if (d.method === "ui/notifications/initialized") {
      // Host half of the spec: once the app is ready, feed it the tool
      // input it is rendering for (spec 2026-01-26 notification).
      reply({ jsonrpc: "2.0", method: "ui/notifications/tool-input", params: { arguments: { value: "ping" } } });
      return;
    }
    if (d.method === "tools/call" && d.id !== undefined) {
      // THE round trip: the widget's tools/call goes over HTTP JSON-RPC
      // to the MCP endpoint and the endpoint's response comes back to the
      // frame verbatim, same id.
      rpc("tools/call", d.params).then(function (resp) {
        reply(resp);
      }, function (e) {
        reply({ jsonrpc: "2.0", id: d.id, error: { code: -32603, message: String(e) } });
      });
      return;
    }
    if (d.id !== undefined) { // ui/message and any other request: ack {}
      reply({ jsonrpc: "2.0", id: d.id, result: {} });
    }
  });

  // Boot through the real protocol: resources/read, then embed exactly as
  // a host does. srcdoc resolves the widget's relative <script src>
  // against this page's base URL, so the client loads from this origin.
  rpc("resources/read", { uri: "ui://demo/widget.html" }).then(function (resp) {
    if (resp.error) throw new Error("resources/read: " + JSON.stringify(resp.error));
    window.__widgetHTML = resp.result.contents[0].text;
    var f = document.createElement("iframe");
    f.setAttribute("sandbox", "allow-scripts");
    f.srcdoc = window.__widgetHTML;
    document.body.appendChild(f);
  }).catch(function (e) {
    window.__harnessErrors.push("host boot: " + String(e));
  });
})();
</script>
</body>`

// ── Harness ─────────────────────────────────────────────────────────────────

// widgetE2EClient modes for serving the client script. The default is the
// shipped behavior (WidgetClientHandler, CORP cross-origin). The negative
// modes each break exactly one property:
//
//   - "corp-same-origin": same client bytes, but the CORP header is forced
//     to same-origin — the state the widget would be in if the handler
//     stopped overriding the repo-wide security middleware default. That
//     default BLOCKS the opaque-origin frame's script fetch (probed in a
//     real Chrome: an absent header allows the load, same-origin refuses
//     it), so "delete the header" is NOT a failing mutation and the
//     negative must pin same-origin instead.
//   - "typo-src": the widget HTML's client <script src> gets a
//     one-character typo — agent-host.md's #1 authoring mistake.
const (
	widgetE2EClientIntact      = "intact"
	widgetE2EClientCORPBlocked = "corp-same-origin"
	widgetE2EClientTypo        = "typo-src"
)

// widgetE2EEnv is one subtest's world: the MCP server (with the echo-tool
// witness), the host shim, and the Chrome tab driving it.
type widgetE2EEnv struct {
	ctx     context.Context
	console *widgetE2EConsole

	mu   sync.Mutex
	echo []map[string]any // arguments of every echo tools/call the SERVER saw
}

// echoCalls returns a copy of the recorded server-side echo arguments.
func (e *widgetE2EEnv) echoCalls() []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]map[string]any, len(e.echo))
	copy(out, e.echo)
	return out
}

// startWidgetE2E boots the whole world for one subtest. clientMode picks
// the client-serving variant (see the widgetE2EClient* constants).
func startWidgetE2E(t *testing.T, clientMode string) *widgetE2EEnv {
	t.Helper()

	// The widget document, assembled by the real builder so the client
	// <script src> comes from WidgetClientScriptURL by construction.
	html, err := WidgetDocument{
		Title:  "Demo widget",
		Body:   `<p id="status">idle</p>`,
		Script: widgetE2EScript,
	}.HTML()
	if err != nil {
		t.Fatalf("build widget document: %v", err)
	}
	if n := strings.Count(html, WidgetClientScriptURL); n != 1 {
		t.Fatalf("widget document must reference the client script exactly once, got %d", n)
	}
	switch clientMode {
	case widgetE2EClientTypo:
		// The one-character typo agent-host.md warns about. The count
		// check above guarantees this replacement bites exactly once.
		typo := strings.TrimSuffix(WidgetClientScriptURL, "js") + "jz"
		html = strings.Replace(html, WidgetClientScriptURL, typo, 1)
		if strings.Contains(html, WidgetClientScriptURL) {
			t.Fatalf("typo mutation failed: intact URL still present")
		}
	}

	env := &widgetE2EEnv{}
	s := NewServer()

	// The domain tool the widget drives. Registered separately from the
	// app: RegisterApp wires the LAUNCHING tool (named "demo", the model
	// calls it to open the widget); "echo" is the tool the widget itself
	// round-trips through the host. The witness records what the SERVER
	// actually executed, so the test asserts both wire halves.
	if err := s.RegisterTool("echo", "Echo the value back.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
			"required": []string{"value"},
		},
		func(ctx context.Context, params map[string]any) (any, error) {
			env.mu.Lock()
			env.echo = append(env.echo, params)
			env.mu.Unlock()
			return map[string]any{"echoed": params["value"]}, nil
		},
	); err != nil {
		t.Fatalf("register echo tool: %v", err)
	}

	if err := s.RegisterApp(AppConfig{
		Name:        "demo",
		Description: "Open the demo widget (e2e).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
			"required": []string{"value"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			// The launcher: the model's entry into the widget. The
			// widget's traffic is the echo tool above.
			return map[string]any{"opened": true, "resource": "ui://demo/widget.html"}, nil
		},
		ResourceURI: "ui://demo/widget.html",
		HTML:        html,
	}); err != nil {
		t.Fatalf("register app: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", s)
	switch clientMode {
	case widgetE2EClientCORPBlocked:
		// Same bytes as WidgetClientHandler, one header mutated to the
		// value the global security middleware would leave on the
		// response if the handler stopped overriding it.
		mux.Handle(WidgetClientScriptURL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Type", "text/javascript; charset=utf-8")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Cache-Control", "no-store, max-age=0")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(widgetClientJSBytes)
		}))
	default:
		mux.Handle(WidgetClientScriptURL, WidgetClientHandler())
	}
	mux.HandleFunc("/host", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(widgetE2EHostPage))
	})
	// Chrome probes /favicon.ico on every navigation; a 404 here would
	// pollute the console capture the failure paths print.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// Pin the transport's Host check to the real listener authority, the
	// way an embedder would; the shim's fetches are same-origin, so no
	// Origin allowlist is needed.
	s.SetAllowedHosts([]string{strings.TrimPrefix(srv.URL, "http://")})

	env.ctx, env.console = widgetE2EChrome(t)
	if err := chromedp.Run(env.ctx, chromedp.Navigate(srv.URL+"/host")); err != nil {
		t.Fatalf("navigate host shim: %v", err)
	}

	// The harness itself must be healthy before any widget assertion: the
	// resources/read completed and the iframe exists. Without this, a
	// negative subtest could pass vacuously (nothing ever embedded).
	// Frame-reported harnessErrors are NOT a gate failure — in the
	// negative modes "widget client missing" is exactly the observation
	// under test; only the parent's own boot failure ("host boot: …")
	// keeps __widgetHTML unset and trips this gate by timeout.
	var armed bool
	if err := chromedp.Run(env.ctx,
		chromedp.Poll(`!!(window.__widgetHTML && document.querySelector("iframe[sandbox]"))`,
			&armed, chromedp.WithPollingInterval(50*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil || !armed {
		env.dump(t)
		t.Fatalf("host shim never armed (resource read / iframe embed failed): %v", err)
	}
	return env
}

// widgetE2EChrome boots one headless Chrome and returns a bounded tab
// context plus a console sink covering the parent page and every frame.
// Mirrors framework/pluginhost/wasm_tier_chromium_test.go: WSURLReadTimeout
// raised for cold starts on shared runners, NoSandbox for CI, no
// t.Parallel, everything cleaned up via t.Cleanup.
func widgetE2EChrome(t *testing.T) (context.Context, *widgetE2EConsole) {
	t.Helper()
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("widget e2e requires Chrome, Chromium, or Edge")
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox,
			chromedp.ExecPath(path),
		)...)
	t.Cleanup(cancelAlloc)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	t.Cleanup(cancelBrowser)

	con := &widgetE2EConsole{}
	chromedp.ListenTarget(browserCtx, con.listen)

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx, con
}

// widgetE2EConsole collects browser console output — runtime console
// calls and log entries, error/warning level, from any frame — so a
// failure can print what the browser actually said instead of "timed out".
type widgetE2EConsole struct {
	mu    sync.Mutex
	lines []string
}

func (c *widgetE2EConsole) listen(ev any) {
	switch e := ev.(type) {
	case *runtime.EventConsoleAPICalled:
		if e.Type != "error" && e.Type != "warning" {
			return
		}
		parts := make([]string, 0, len(e.Args))
		for _, a := range e.Args {
			parts = append(parts, string(a.Value))
		}
		c.add("console." + string(e.Type) + ": " + strings.Join(parts, " "))
	case *log.EventEntryAdded:
		if e.Entry != nil && (e.Entry.Level == "error" || e.Entry.Level == "warning") {
			c.add(string(e.Entry.Source) + " " + string(e.Entry.Level) + ": " + e.Entry.Text)
		}
	}
}

func (c *widgetE2EConsole) add(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
}

// dump prints everything captured; call on every failure path.
func (c *widgetE2EConsole) dump(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.lines) == 0 {
		t.Logf("browser console: (nothing captured)")
		return
	}
	for _, l := range c.lines {
		t.Logf("browser console: %s", l)
	}
}

func (e *widgetE2EEnv) dump(t *testing.T) {
	t.Helper()
	t.Logf("host shim log: %s", e.eval(t, `JSON.stringify(window.__log)`))
	t.Logf("harness errors: %s", e.eval(t, `JSON.stringify(window.__harnessErrors)`))
	e.console.dump(t)
}

// eval runs a JS expression whose result is a string; a failure to
// evaluate is fatal, because every caller is on a failure path already
// and a silent "" would hide the evidence.
func (e *widgetE2EEnv) eval(t *testing.T, expr string) string {
	t.Helper()
	var out string
	if err := chromedp.Run(e.ctx, chromedp.Evaluate(expr, &out)); err != nil {
		t.Fatalf("evaluate %q: %v", expr, err)
	}
	return out
}

// ── Wire shapes read back out of the shim log ──────────────────────────────

// widgetE2ELogEntry is one JSON-RPC message the host shim received from
// the widget client. Params stays raw: each method asserts its own shape.
type widgetE2ELogEntry struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params json.RawMessage `json:"params"`
}

func (e *widgetE2EEnv) log(t *testing.T) []widgetE2ELogEntry {
	t.Helper()
	var entries []widgetE2ELogEntry
	if err := json.Unmarshal([]byte(e.eval(t, `JSON.stringify(window.__log)`)), &entries); err != nil {
		t.Fatalf("decode shim log: %v", err)
	}
	return entries
}

// logIndex returns the position of the first entry with method whose
// message is a REQUEST (id present), or -1. Notifications from the client
// (no id) are excluded so a stray notification can never satisfy a
// request-shaped expectation.
func logIndex(entries []widgetE2ELogEntry, method string) int {
	for i, e := range entries {
		if e.Method == method && len(e.ID) > 0 && string(e.ID) != "null" {
			return i
		}
	}
	return -1
}

// notificationIndex is logIndex for notifications (no id).
func notificationIndex(entries []widgetE2ELogEntry, method string) int {
	for i, e := range entries {
		if e.Method == method && (len(e.ID) == 0 || string(e.ID) == "null") {
			return i
		}
	}
	return -1
}

type widgetE2EToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type widgetE2EMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type widgetE2EMessageParams struct {
	Role    string                    `json:"role"`
	Content []widgetE2EMessageContent `json:"content"`
}

// ── The test ────────────────────────────────────────────────────────────────

func TestWidgetClientE2E(t *testing.T) {
	t.Run("handshake and tool round trip", func(t *testing.T) {
		env := startWidgetE2E(t, widgetE2EClientIntact)

		// Bounded wait (≤20s) for the flow to complete: the widget's
		// final ui/message carrying the echo result.
		var done string
		if err := chromedp.Run(env.ctx,
			chromedp.Poll(`(function () {
				var msgs = window.__log.filter(function (m) { return m.method === "ui/message"; });
				for (var i = 0; i < msgs.length; i++) {
					var t = msgs[i].params && msgs[i].params.content && msgs[i].params.content[0] && msgs[i].params.content[0].text;
					if (t && t.indexOf("echo-result:") === 0) return "done";
					if (t && t.indexOf("error:") === 0) return "widget-error";
				}
				return "";
			})()`, &done, chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(20*time.Second)),
		); err != nil {
			env.dump(t)
			t.Fatalf("widget flow never completed: %v", err)
		}
		if done != "done" {
			env.dump(t)
			t.Fatalf("widget reported failure instead of the echo result")
		}

		entries := env.log(t)
		iInit := logIndex(entries, "ui/initialize")
		iReady := notificationIndex(entries, "ui/notifications/initialized")
		iCall := logIndex(entries, "tools/call")
		iMsg := logIndex(entries, "ui/message")
		if iInit < 0 || iReady < 0 || iCall < 0 || iMsg < 0 {
			env.dump(t)
			t.Fatalf("missing protocol steps: initialize=%d initialized=%d tools/call=%d ui/message=%d",
				iInit, iReady, iCall, iMsg)
		}
		// The order the protocol requires: handshake completes (request
		// + ready notification) before the app acts, and the report
		// lands after the call it describes.
		if !(iInit < iReady && iReady < iCall && iCall < iMsg) {
			env.dump(t)
			t.Fatalf("protocol steps out of order: initialize=%d initialized=%d tools/call=%d ui/message=%d",
				iInit, iReady, iCall, iMsg)
		}

		// The client's ui/initialize params, per widgetclient.js.
		var initParams struct {
			AppCapabilities map[string]any `json:"appCapabilities"`
		}
		if err := json.Unmarshal(entries[iInit].Params, &initParams); err != nil {
			t.Fatalf("decode ui/initialize params %s: %v", entries[iInit].Params, err)
		}
		if _, ok := initParams.AppCapabilities["availableDisplayModes"]; !ok {
			t.Errorf("ui/initialize params must carry the appCapabilities the widget passed, got %s", entries[iInit].Params)
		}

		// The tools/call the HOST saw on the wire, exactly as the client
		// sent it: name + arguments verbatim.
		var call widgetE2EToolCallParams
		if err := json.Unmarshal(entries[iCall].Params, &call); err != nil {
			t.Fatalf("decode tools/call params %s: %v", entries[iCall].Params, err)
		}
		if call.Name != "echo" {
			t.Errorf("tools/call name = %q, want echo", call.Name)
		}
		if call.Arguments["value"] != "ping" {
			t.Errorf("tools/call arguments = %v, want value:ping", call.Arguments)
		}

		// The ui/message text: the serialized tools/call RESULT the host
		// posted back (the real toolsCallResult wire shape: content[]
		// with the plain-map handler return JSON in text) plus the
		// document root's data-theme, proving the host-theme application.
		var msg widgetE2EMessageParams
		if err := json.Unmarshal(entries[iMsg].Params, &msg); err != nil {
			t.Fatalf("decode ui/message params %s: %v", entries[iMsg].Params, err)
		}
		if msg.Role != "user" {
			t.Errorf("ui/message role = %q, want user", msg.Role)
		}
		if len(msg.Content) != 1 || msg.Content[0].Type != "text" {
			t.Fatalf("ui/message content must be one text block, got %+v", msg.Content)
		}
		text := msg.Content[0].Text
		t.Logf("ui/message text: %s", text)
		if !strings.HasPrefix(text, "echo-result:") {
			t.Errorf("ui/message text must start with echo-result:, got %q", text)
		}
		if !strings.Contains(text, "theme=dark") {
			t.Errorf("ui/message text must carry the applied data-theme (theme=dark), got %q", text)
		}
		// JSON.stringify escapes the inner JSON's quotes; unescape before
		// asserting the echoed pair came through the whole round trip.
		flat := strings.ReplaceAll(text, `\"`, `"`)
		if !strings.Contains(flat, `"echoed":"ping"`) {
			t.Errorf("ui/message text must carry the echo tool result with echoed:ping, got %q", text)
		}

		// Server side: the MCP endpoint actually executed the tool the
		// frame asked for (not just a host-side echo).
		calls := env.echoCalls()
		if len(calls) != 1 {
			t.Errorf("server saw %d echo calls, want exactly 1: %+v", len(calls), calls)
		} else if calls[0]["value"] != "ping" {
			t.Errorf("server saw echo arguments %v, want value:ping", calls[0])
		}

		// The frame's own instrumentation must have stayed quiet.
		if errs := env.eval(t, `JSON.stringify(window.__harnessErrors)`); errs != "[]" {
			env.dump(t)
			t.Errorf("harness errors must be empty on the happy path, got %s", errs)
		}
	})

	// Negative (a), hard rule 11: the CORP relaxation on the client asset
	// is load-bearing. Served with Cross-Origin-Resource-Policy:
	// same-origin (what the repo-wide security middleware defaults every
	// response to), the opaque-origin frame's <script src> is refused, so
	// the handshake can never start. An ABSENT header does not block
	// (probed in Chrome during development) — same-origin is the failing
	// mutation that pins this property.
	t.Run("client without CORP relaxation never initializes", func(t *testing.T) {
		env := startWidgetE2E(t, widgetE2EClientCORPBlocked)

		var arrived string
		err := chromedp.Run(env.ctx,
			chromedp.Poll(`window.__log.some(function (m) { return m.method === "ui/initialize"; }) ? "arrived" : ""`,
				&arrived, chromedp.WithPollingInterval(50*time.Millisecond), chromedp.WithPollingTimeout(3*time.Second)),
		)
		if err == nil || arrived == "arrived" {
			env.dump(t)
			t.Fatalf("ui/initialize arrived although the client script was CORP-blocked")
		}
		// Liveness proof: the page still answers (the poll timed out
		// because the condition never held, not because the tab died).
		if got := env.eval(t, `JSON.stringify(window.__log)`); got == "[]" {
			t.Logf("shim log stayed empty for 3s (expected)")
		}
		// The frame RAN but its client never loaded — the exact silent
		// failure mode, reported by the frame's own instrumentation.
		errs := env.eval(t, `JSON.stringify(window.__harnessErrors)`)
		if !strings.Contains(errs, "widget client missing") {
			env.dump(t)
			t.Errorf("the frame must report the missing client, got harness errors %s", errs)
		}
		env.dump(t)
	})

	// Negative (b), hard rule 11: a one-character typo in the client
	// script URL — agent-host.md's #1 authoring mistake ("a widget that
	// renders and silently never receives anything"). The URL 404s, the
	// widget client never loads, and no handshake ever starts.
	t.Run("typo'd client script URL never initializes", func(t *testing.T) {
		env := startWidgetE2E(t, widgetE2EClientTypo)

		var arrived string
		err := chromedp.Run(env.ctx,
			chromedp.Poll(`window.__log.some(function (m) { return m.method === "ui/initialize"; }) ? "arrived" : ""`,
				&arrived, chromedp.WithPollingInterval(50*time.Millisecond), chromedp.WithPollingTimeout(3*time.Second)),
		)
		if err == nil || arrived == "arrived" {
			env.dump(t)
			t.Fatalf("ui/initialize arrived although the client script URL was typo'd")
		}
		errs := env.eval(t, `JSON.stringify(window.__harnessErrors)`)
		if !strings.Contains(errs, "widget client missing") {
			env.dump(t)
			t.Errorf("the frame must report the missing client, got harness errors %s", errs)
		}
		env.dump(t)
	})
}
