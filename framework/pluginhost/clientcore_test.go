package pluginhost

import (
	"strings"
	"testing"
)

// The security-critical client core lives in host/pluginhost.js. Its invariants
// can't be HTML-escaped or Go-typed away, so we pin the exact dangerous
// regressions at the source level (deterministic, no browser). Defense in
// depth: the opaque-origin guarantee ALSO rides the server-emitted CSP
// `sandbox allow-scripts` directive (TestAssetServerFramedIsolationDirectives),
// which is behaviorally tested, so the boundary never rests on the JS alone.

// (1) postMessage source validation: messages are accepted ONLY from a
// mounted frame's contentWindow, and event.origin is deliberately NOT trusted
// (opaque frames report origin "null").
func TestBrokerJS_ValidatesMessageSource(t *testing.T) {
	// Code lines only (nonCommentJS): the header comment states the same
	// invariants in prose, and a whole-file Contains could go vacuous the
	// day the comment happens to use the code's operand order.
	code := nonCommentJS(string(brokerJSBytes))
	if !strings.Contains(code, "contentWindow === event.source") {
		t.Error("onMessage must accept only messages whose source is a mounted frame's contentWindow")
	}
	// It must reject wrong envelope version and non-plugin source markers.
	if !strings.Contains(code, "msg.v !== ENVELOPE_VERSION") {
		t.Error("onMessage must reject a wrong envelope version")
	}
	if !strings.Contains(code, `msg.src !== "plugin"`) {
		t.Error("onMessage must reject messages not marked src:plugin")
	}
}

// (2) The iframe sandbox attribute is set from the authoritative sandboxFor
// (pinned separately in TestBrokerJS_SandboxForIsAuthoritative), which filters
// through an allow-list, so allow-same-origin must appear NOWHERE in a
// grantable position. Under an allow-list "grantable" means a key in
// ALLOWED_SANDBOX; prose mentioning the token is inert.
func TestBrokerJS_NeverEmitsAllowSameOrigin(t *testing.T) {
	js := string(brokerJSBytes)
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = strings.TrimSpace(code[:idx]) // strip trailing comment
		}
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue // whole-line comment
		}
		if strings.Contains(code, "allow-same-origin") {
			t.Errorf("allow-same-origin referenced in executable code: %q", strings.TrimSpace(line))
		}
	}
}

// (3) The host→frame post uses targetOrigin "*" (correct for an opaque frame,
// where a concrete targetOrigin would never match), the source check, not an
// origin string, is the gate. Pin the rationale so nobody "hardens" it into a
// concrete origin that silently drops every message.
func TestBrokerJS_PostsWithWildcardTargetOrigin(t *testing.T) {
	js := string(brokerJSBytes)
	if !strings.Contains(js, `postMessage(env, "*")`) {
		t.Error("postTo must use targetOrigin \"*\" for the opaque frame (source check is the gate)")
	}
}

// (4) The fallback lifecycle (#253): the broker finds the server-rendered
// [data-fui-plugin-fallback] node, hides the frame behind it while
// loading, swaps to the frame on ready, and — the load-bearing half —
// swaps BACK on bootError so a dead frame degrades to the static node
// instead of an empty box. Behavior lives in the DOM (no browser here),
// so pin the wiring at the source, in code not comments.
func TestBrokerJS_FallbackLifecycle(t *testing.T) {
	code := nonCommentJS(string(brokerJSBytes))
	if !strings.Contains(code, `querySelector("[data-fui-plugin-fallback]")`) {
		t.Error("broker must locate the fallback node in the marker")
	}
	bootIdx := strings.Index(code, `case "bootError":`)
	readyIdx := strings.Index(code, `case "ready":`)
	if bootIdx < 0 || readyIdx < 0 {
		t.Fatal("ready/bootError cases not found in broker code")
	}
	bootBody := code[bootIdx:]
	if end := strings.Index(bootBody, "break;"); end >= 0 {
		bootBody = bootBody[:end]
	}
	if !strings.Contains(bootBody, "showFallback(st)") {
		t.Error("bootError must showFallback (degrade to the static node)")
	}
	readyBody := code[readyIdx:]
	if end := strings.Index(readyBody, "break;"); end >= 0 {
		readyBody = readyBody[:end]
	}
	if !strings.Contains(readyBody, "showFrame(st)") {
		t.Error("ready must showFrame (live view takes over)")
	}
	// The other two transitions are just as load-bearing: without the
	// loading-state hide, the user sees the fallback AND an empty frame
	// stacked; without the teardown restore, an SPA-nav gap. Pin both
	// bodies (function slices), not just ready/bootError.
	if body := funcBody(code, "function mountMarker("); !strings.Contains(body, "showFallback(st)") {
		t.Error("mountMarker must showFallback (loading: frame hidden behind the static node)")
	}
	if body := funcBody(code, "function cleanup("); !strings.Contains(body, "showFallback(st)") {
		t.Error("cleanup must showFallback (restore the static node before removing the frame)")
	}
}

// funcBody returns the source of the function starting at marker, from
// its opening brace to the matching close, so a pin can assert against
// one function's body rather than the whole file.
func funcBody(code, marker string) string {
	start := strings.Index(code, marker)
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return code[start : i+1]
			}
		}
	}
	return code[start:]
}
