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
// which is behaviorally tested — so the boundary never rests on the JS alone.

// (1) postMessage source validation: messages are accepted ONLY from a
// mounted frame's contentWindow, and event.origin is deliberately NOT trusted
// (opaque frames report origin "null").
func TestBrokerJS_ValidatesMessageSource(t *testing.T) {
	js := string(brokerJSBytes)
	if !strings.Contains(js, "contentWindow === event.source") {
		t.Error("onMessage must accept only messages whose source is a mounted frame's contentWindow")
	}
	// It must reject wrong envelope version and non-plugin source markers.
	if !strings.Contains(js, "msg.v !== ENVELOPE_VERSION") {
		t.Error("onMessage must reject a wrong envelope version")
	}
	if !strings.Contains(js, `msg.src !== "plugin"`) {
		t.Error("onMessage must reject messages not marked src:plugin")
	}
}

// (2) The iframe sandbox attribute is set from the authoritative sandboxFor
// (pinned separately in TestBrokerJS_SandboxForIsAuthoritative) and the
// same-origin token must appear NOWHERE as a literal that could be emitted.
func TestBrokerJS_NeverEmitsAllowSameOrigin(t *testing.T) {
	js := string(brokerJSBytes)
	// allow-same-origin may appear ONLY inside the SAME_ORIGIN_COLLAPSING
	// filter (as a key to strip), never in an additive/emitting position.
	for _, line := range strings.Split(js, "\n") {
		if !strings.Contains(line, "allow-same-origin") {
			continue
		}
		if strings.Contains(line, "SAME_ORIGIN_COLLAPSING") {
			continue // the strip-filter declaration — allowed
		}
		t.Errorf("allow-same-origin referenced outside the strip filter: %q", strings.TrimSpace(line))
	}
}

// (3) The host→frame post uses targetOrigin "*" (correct for an opaque frame,
// where a concrete targetOrigin would never match) — the source check, not an
// origin string, is the gate. Pin the rationale so nobody "hardens" it into a
// concrete origin that silently drops every message.
func TestBrokerJS_PostsWithWildcardTargetOrigin(t *testing.T) {
	js := string(brokerJSBytes)
	if !strings.Contains(js, `postMessage(env, "*")`) {
		t.Error("postTo must use targetOrigin \"*\" for the opaque frame (source check is the gate)")
	}
}
