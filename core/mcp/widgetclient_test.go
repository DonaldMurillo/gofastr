package mcp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// The MCP Apps widget client lives in widgetclient.js and speaks ext-apps
// JSON-RPC 2.0 over postMessage to the chat host. Its invariants can't be
// Go-typed away, so the dangerous regressions — a typo'd method string is a
// widget that silently never works — are pinned at the source level
// (deterministic, no browser), in the style of pluginhost's frame client
// pins. Every pin runs against non-comment JS: the header comment names the
// same strings, so a whole-file Contains would stay green with the code
// deleted.

// nonCommentJS strips whole-line and trailing // comments, returning the
// executable lines joined — the same approach as pluginhost's
// TestBrokerJS_NeverEmitsAllowSameOrigin scan, so pins can't be satisfied by
// prose.
func nonCommentJS(js string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") ||
			strings.HasPrefix(code, "/*") {
			continue
		}
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = strings.TrimSpace(code[:idx])
		}
		b.WriteString(code)
		b.WriteString("\n")
	}
	return b.String()
}

// jsBody slices code from the start marker to the end marker (first
// occurrence after start), so a pin can be scoped to one function instead of
// being satisfied by an unrelated line elsewhere in the file.
func jsBody(code, start, end string) string {
	si := strings.Index(code, start)
	if si < 0 {
		return ""
	}
	rest := code[si:]
	ei := strings.Index(rest[len(start):], end)
	if ei < 0 {
		return ""
	}
	return rest[:len(start)+ei]
}

// Every method-name string of the ext-apps widget surface, pinned QUOTED in
// executable code: the outbound requests and notifications appear at their
// sender/registration sites, so a typo'd or deleted literal fails here
// instead of shipping a widget that silently never works.
func TestWidgetClientJS_PinMethodNames(t *testing.T) {
	code := nonCommentJS(string(widgetClientJSBytes))
	for _, m := range []string{
		// Widget → host: handshake.
		"ui/initialize",
		"ui/notifications/initialized",
		// Widget → host requests.
		"tools/call",
		"resources/read",
		"ui/open-link",
		"ui/request-display-mode",
		"ui/message",
		"ui/update-model-context",
		// Widget → host notification.
		"ui/notifications/size-changed",
		// Host → widget notifications (dispatchable, so registrable).
		"ui/notifications/tool-input",
		"ui/notifications/tool-input-partial",
		"ui/notifications/tool-result",
		"ui/notifications/tool-cancelled",
		"ui/notifications/host-context-changed",
		"ui/resource-teardown",
		"notifications/message",
	} {
		if !strings.Contains(code, `"`+m+`"`) {
			t.Errorf("widget client missing quoted method literal %q in executable code", m)
		}
	}
}

// The handshake order: ui/initialize is sent as a request and
// ui/notifications/initialized is notified only from inside its response
// handler, so the textual order inside connect() is load-bearing — flipping
// it sends initialized before the host answered initialize.
func TestWidgetClientJS_HandshakeNotifiesAfterInitializeResult(t *testing.T) {
	code := nonCommentJS(string(widgetClientJSBytes))
	body := jsBody(code, "connect: function", "request: request")
	if body == "" {
		t.Fatal("connect() body not found in widget client")
	}
	ii := strings.Index(body, `"ui/initialize"`)
	ni := strings.Index(body, `"ui/notifications/initialized"`)
	if ii < 0 || ni < 0 {
		t.Fatal("connect() must reference both handshake literals")
	}
	if ni < ii {
		t.Error("ui/notifications/initialized must be sent after the ui/initialize response, not before")
	}
	if !strings.Contains(body, "appCapabilities: appCapabilities") {
		t.Error("connect() must carry the app's appCapabilities in ui/initialize params")
	}
}

// (1) postMessage source validation, mirror of pluginhost's frame client:
// messages are accepted ONLY from the parent window, never gated on
// event.origin (the sandboxed widget reports origin "null"; an origin-string
// check is the wrong tool in both directions), the JSON-RPC version is
// checked, and host→widget requests are dropped rather than dispatched —
// except ui/resource-teardown, the one inbound request, which is answered
// (pinned by the channel-contract test).
func TestWidgetClientJS_ValidatesMessageSource(t *testing.T) {
	code := nonCommentJS(string(widgetClientJSBytes))
	if !strings.Contains(code, "event.source === window.parent") &&
		!strings.Contains(code, "window.parent === event.source") {
		t.Error("onWindowMessage must accept only messages whose source is the parent window (in code, not prose)")
	}
	// The check above pins the comparison (===, not !==); this pins the
	// BRANCH polarity — negation and return together. The bare predicate
	// stays green when the branch is inverted to `if (fromParent)
	// return;`, which would accept messages from any window.
	if !strings.Contains(code, "if (!fromParent) return;") {
		t.Error("onWindowMessage must return when the message is NOT from the parent window — the negated branch is the gate, not the bare predicate")
	}
	if strings.Contains(code, "event.origin") {
		t.Error("onWindowMessage must not gate on event.origin — the widget frame is opaque-origin; source identity is the gate")
	}
	if !strings.Contains(code, "msg.jsonrpc !== JSON_RPC_VERSION") {
		t.Error("onWindowMessage must reject a wrong JSON-RPC version")
	}
	if !strings.Contains(code, "if (msg.id !== undefined) return;") {
		t.Error("onWindowMessage must drop host→widget requests (method + id) instead of dispatching them as notifications")
	}
}

// (2) The widget→host post uses targetOrigin "*" (the frame is opaque, a
// concrete targetOrigin is the wrong tool); the source check, not an origin
// string, is the gate. Mirror of the pluginhost pin so nobody "hardens" it
// into an origin that silently drops every message.
func TestWidgetClientJS_PostsWithWildcardTargetOrigin(t *testing.T) {
	js := string(widgetClientJSBytes)
	if !strings.Contains(js, `postMessage(msg, "*")`) {
		t.Error(`postToHost must use targetOrigin "*" (source check is the gate)`)
	}
}

// (3) No external URLs: the client runs inside the host's sandboxed widget
// iframe, and a script that phones home would punch out of the isolation the
// host put it in. Mirror of the pluginhost scan: whole comment lines are
// skipped but trailing comments are NOT stripped (a URL's own "//" would be
// read as a comment start and destroy the scheme before the check), so a URL
// in a trailing comment flags — the safe direction.
func TestWidgetClientJS_NoExternalURLs(t *testing.T) {
	js := string(widgetClientJSBytes)
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") ||
			strings.HasPrefix(code, "/*") {
			continue // whole-line comment
		}
		if strings.Contains(code, "http://") || strings.Contains(code, "https://") {
			t.Errorf("external URL in executable code: %q", strings.TrimSpace(line))
		}
	}
}

// The envelope is JSON-RPC 2.0 with no extra framing: the constant is
// declared once and used on BOTH the outbound construction and the inbound
// check, so a drifted version silently drops every message on one side.
func TestWidgetClientJS_EnvelopeVersion(t *testing.T) {
	re := regexp.MustCompile(`JSON_RPC_VERSION = "(\d+\.\d+)"`)
	m := re.FindStringSubmatch(string(widgetClientJSBytes))
	if m == nil {
		t.Fatal("widget client: JSON_RPC_VERSION declaration not found")
	}
	if m[1] != "2.0" {
		t.Errorf("JSON_RPC_VERSION = %q, want 2.0 (JSON-RPC 2.0 is the ext-apps transport)", m[1])
	}
	code := nonCommentJS(string(widgetClientJSBytes))
	if !strings.Contains(code, "jsonrpc: JSON_RPC_VERSION") {
		t.Error("outbound messages must be built from JSON_RPC_VERSION")
	}
	if !strings.Contains(code, "msg.jsonrpc !== JSON_RPC_VERSION") {
		t.Error("inbound messages must be checked against JSON_RPC_VERSION")
	}
}

// The channel contract, mirroring pluginhost's frame client: a bounded
// in-flight map that rejects (not hangs, not posts) when full, a timeout
// that rejects and frees the entry, send-throw cleanup, a teardown REQUEST
// that is answered before it fails everything outstanding, and notification
// methods instead of throwing.
func TestWidgetClientJS_ChannelContract(t *testing.T) {
	code := nonCommentJS(string(widgetClientJSBytes))
	for _, s := range []string{
		"MAX_INFLIGHT = 64;", // anchored: "= 64" alone would also match 640
		`"E_TIMEOUT"`,
		`"E_SATURATED"`,
		`"E_SEND"`,
		`"E_TEARDOWN"`,
	} {
		if !strings.Contains(code, s) {
			t.Errorf("widget client missing channel contract literal %s", s)
		}
	}
	// The bound must be ENFORCED, not just declared: pin the saturation
	// check that rejects before posting.
	if !strings.Contains(code, "Object.keys(pending).length >= MAX_INFLIGHT") {
		t.Error("request() must reject when the pending map is full")
	}
	// The pending map must be null-proto wherever it is (re)created — the
	// declaration AND the reset — so a "__proto__" id looks up as
	// undefined instead of a truthy Object.prototype member. Two
	// occurrences, not one, or the reset regressed to {}.
	if n := strings.Count(code, "pending = Object.create(null)"); n < 2 {
		t.Errorf("pending map must be Object.create(null) at declaration and reset, found %d", n)
	}
	// Timeout path: the entry is freed BEFORE the reject, so the timer
	// slot does not leak and a late response is dropped by id.
	to := jsBody(code, "var timer = setTimeout(function () {", "}, timeoutMs);")
	if to == "" {
		t.Fatal("request() timeout callback not found")
	}
	if !strings.Contains(to, "delete pending[id]") || !strings.Contains(to, `"E_TIMEOUT"`) {
		t.Error("timeout must delete the pending entry and reject with E_TIMEOUT")
	}
	// A non-cloneable-params postMessage throw on the request path must
	// clean up the pending entry instead of leaking it to timeout /
	// spurious saturation.
	catch := jsBody(code, "} catch (e) {", "reject({ code: \"E_SEND\"")
	if catch == "" {
		t.Fatal("request() send-throw catch not found")
	}
	if !strings.Contains(catch, "clearTimeout(timer)") || !strings.Contains(catch, "delete pending[id]") {
		t.Error("send-throw catch must clear the timer and delete the pending entry before rejecting")
	}
	// Unknown notification methods are ignored, never thrown.
	if !strings.Contains(code, `if (typeof h !== "function") return;`) {
		t.Error("notification dispatch must return early when no handler is registered (ignore, don't throw)")
	}
	// Host teardown (spec 2026-01-26: a REQUEST, not a notification — the
	// host SHOULD wait for the response before tearing the resource
	// down): the app's handler runs awaited, the response is posted, and
	// only THEN are outstanding requests failed — the response must be
	// on the wire before the host removes the frame.
	tn := jsBody(code, "function handleTeardown(", "function onWindowMessage(")
	if tn == "" {
		t.Fatal("handleTeardown() not found")
	}
	if !strings.Contains(tn, `"ui/resource-teardown"`) {
		t.Error("handleTeardown must run the registered ui/resource-teardown handler")
	}
	if !strings.Contains(tn, "Promise.resolve().then") {
		t.Error("handleTeardown must await the handler — a promise return must delay the response")
	}
	if !strings.Contains(tn, "reply(msg.id, {}, null);") {
		t.Error("handleTeardown must answer the request with result {} on success")
	}
	if !strings.Contains(tn, `reply(msg.id, null, { code: "E_HANDLER"`) {
		t.Error("handleTeardown must answer with a JSON-RPC error when the handler throws — the host is waiting on the response")
	}
	// Every rejectOutstanding sits immediately after a reply: the
	// response is posted FIRST, on both the success and error paths.
	tnLines := strings.Split(tn, "\n")
	for i, line := range tnLines {
		if strings.Contains(line, "rejectOutstanding(") &&
			(i == 0 || !strings.Contains(tnLines[i-1], "reply(msg.id")) {
			t.Error("handleTeardown must post the response BEFORE rejectOutstanding fails outstanding requests")
		}
	}
	// And the routing: teardown is dispatched on the REQUEST path, ahead
	// of the unknown-request drop.
	om := jsBody(code, "function onWindowMessage(", "var p = pending[msg.id];")
	if om == "" {
		t.Fatal("onWindowMessage() dispatch not found")
	}
	ti := strings.Index(om, `if (msg.method === "ui/resource-teardown" && msg.id !== undefined) {`)
	di := strings.Index(om, "if (msg.id !== undefined) return;")
	if ti < 0 || di < 0 || di < ti {
		t.Error("onWindowMessage must route the ui/resource-teardown request to handleTeardown before dropping unknown requests")
	}
	if ti >= 0 && !strings.Contains(om[ti:], "handleTeardown(msg);") {
		t.Error("the teardown route must call handleTeardown")
	}
	if !strings.Contains(code, `"pagehide"`) {
		t.Error("pagehide must fail outstanding requests instead of leaking until timeout")
	}
}

// The handler serves the script with the headers an opaque-origin widget
// iframe needs: the CORP cross-origin relaxation (a same-origin default
// would block the "null"-origin frame's script fetch), nosniff, and the
// no-store dev cache. The body is exactly the embedded client.
func TestWidgetClientHandlerServedHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle(WidgetClientScriptURL, WidgetClientHandler())

	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + WidgetClientScriptURL)
	if err != nil {
		t.Fatalf("GET widget client: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("widget client status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("widget client Content-Type=%q", ct)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
		t.Errorf("widget client CORP=%q, want cross-origin (opaque-origin fetcher)", got)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("widget client must carry nosniff")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store, max-age=0" {
		t.Errorf("widget client Cache-Control=%q", cc)
	}
	if !bytes.Equal(body, WidgetClientJS()) {
		t.Error("widget client route must serve exactly the embedded bytes")
	}
}

// WidgetClientJS returns a copy: a caller scribbling on the buffer must not
// corrupt the embedded script for every other caller.
func TestWidgetClientJS_ReturnsACopy(t *testing.T) {
	b := WidgetClientJS()
	if len(b) == 0 {
		t.Fatal("WidgetClientJS() empty")
	}
	orig := string(WidgetClientJS())
	b[0] = 'X'
	if string(WidgetClientJS()) != orig {
		t.Error("WidgetClientJS() must return a copy, not the embedded slice")
	}
}
