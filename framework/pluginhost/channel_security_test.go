package pluginhost

import (
	"strings"
	"testing"
)

// The JS channel files are the actual postMessage sinks; like the manifest
// entry checks they cannot be Go-typed away, so their dangerous regressions
// are pinned at the source level (deterministic, no browser), following the
// convention TestFrameClientJS_ValidatesMessageSource set.

// Property: the HOST→frame postMessage target origin must stay the wildcard,
// with the window-identity (event.source) check as the gate — never a
// derived origin string. The frame is opaque (origin "null"), so a concrete
// targetOrigin derived from the entry URL or document would silently drop
// every message, and an event.origin gate would either always-fail (null) or
// invite a "hardening" that breaks the channel. This is the broker-side
// mirror of TestFrameClientJS_PostsWithWildcardTargetOrigin, which pins only
// the frame side.
func TestBrokerPostsToFrameWithWildcardOrigin(t *testing.T) {
	code := nonCommentJS(string(brokerJSBytes))

	if !strings.Contains(code, `frame.contentWindow.postMessage(env, "*")`) {
		t.Fatal("broker postTo must post to the frame with targetOrigin \"*\" (the frame is opaque)")
	}
	// No postMessage to the frame may carry a derived origin as targetOrigin.
	for _, line := range strings.Split(code, "\n") {
		l := strings.TrimSpace(line)
		if !strings.Contains(l, "postMessage(") {
			continue
		}
		if strings.Contains(l, `postMessage(env, "*")`) || strings.Contains(l, `postMessage(fallback, "*")`) {
			continue
		}
		t.Errorf("broker postMessage with a non-wildcard target: %s", l)
	}
	// The gate is window identity, NOT event.origin: an origin-string check
	// against an opaque frame is a trap in both directions (it either drops
	// everything or gets "fixed" into something weaker). The broker must keep
	// accepting by source only, exactly as its header contract states.
	if strings.Contains(code, "event.origin") {
		t.Error("broker onMessage must not gate on event.origin (opaque frames report \"null\"); the source check is the gate")
	}
	// And the source gate itself must exist, in code.
	if !strings.Contains(code, "event.source") {
		t.Error("broker onMessage must check event.source against the iframe contentWindow")
	}
}

// Property: a message id controlled by the OTHER side of the channel is only
// ever looked up in a prototype-free pending map, on BOTH sides of the
// channel. The broker half is pinned by TestChannelContractPins
// ("pending: Object.create(null)"); this adds the frame-client half, where
// the HOST controls response ids, plus the reset path, so a "__proto__" or
// "constructor" id resolves undefined instead of a truthy Object.prototype
// member that passes the not-pending guard and throws on demand.
func TestChannelPendingMapsArePrototypeFree(t *testing.T) {
	client := nonCommentJS(string(frameClientJSBytes))
	broker := nonCommentJS(string(brokerJSBytes))

	// Declaration and reset on the frame side.
	if !strings.Contains(client, "pending = Object.create(null)") {
		t.Error("frame client pending map must be Object.create(null), not {}")
	}
	// Declaration and reset on the broker side (extends the existing pin to
	// cover the reset that rejectOutstanding/teardown performs).
	for _, want := range []string{
		"pending: Object.create(null)",
		"st.pending = Object.create(null)",
	} {
		if !strings.Contains(broker, want) {
			t.Errorf("broker is missing prototype-free pending map shape %s", want)
		}
	}

	// The response lookups must go through those maps by the message id, and
	// an unknown id must be dropped, not dereferenced.
	if !strings.Contains(client, "pending[msg.id]") {
		t.Error("frame client must look up responses by msg.id in its pending map")
	}
	if !strings.Contains(broker, "st.pending[msg.id]") {
		t.Error("broker must look up responses by msg.id in its per-instance pending map")
	}

	// No literal-object pending map may shadow the null-proto ones.
	if strings.Contains(client, "pending = {}") || strings.Contains(client, "pending: {}") {
		t.Error("frame client declares a literal {} pending map — prototype keys become truthy pending entries")
	}
	if strings.Contains(broker, "pending = {}") || strings.Contains(broker, "pending: {}") {
		t.Error("broker declares a literal {} pending map — prototype keys become truthy pending entries")
	}
}
