package pluginhost

import (
	"strings"
	"testing"
)

// Pins: frame→host request dispatch is bounded (contract answer, round 5:
// yes — "both directions" includes the inbound dispatch). handleRequest
// consults an in-flight counter up to MAX_INFLIGHT and answers a saturated
// request with E_SATURATED through the always-answered reply channel.
// (2026-09-06 adversarial pass, round 5.)
//
// Property: frame→host request dispatch is bounded — a plugin frame
// (untrusted by construction per the package's sandbox docs) must not be
// able to flood the fully-privileged host page's CPU with unbounded
// in-flight request handling.
//
// Surface: framework/pluginhost/host/pluginhost.js::handleRequest — every
// inbound "request" envelope goes straight to Promise.resolve().then(run)
// with no in-flight/concurrency bound; only each side's OUTBOUND pending
// map is bounded (request() at MAX_INFLIGHT with E_SATURATED).
//
// Finding (verified): handleRequest consults no bound of any kind before
// invoking the handler, so a tight postMessage loop from the sandboxed
// frame drives unlimited concurrent host-side handler work.
//
// Fix direction: consult an inbound saturation bound (counter/semaphore up
// to MAX_INFLIGHT) before dispatch and reply E_SATURATED-style when full.
// The predicate below accepts any recognizable bound mechanism, mirroring
// how channel_security_test accepts its guards.
func TestBrokerRedInboundSaturation(t *testing.T) {
	code := nonCommentJS(string(brokerJSBytes))

	// Scope to handleRequest's body: the file already contains a bound
	// (MAX_INFLIGHT / E_SATURATED) for the OUTBOUND request() direction, so
	// a whole-file Contains would stay green while the inbound hole stays
	// open. Brace-matched extraction, comments stripped per the sibling
	// harness so prose in the header can't satisfy the pin.
	const marker = "function handleRequest("
	start := strings.Index(code, marker)
	if start < 0 {
		t.Fatal("setup broken: handleRequest not found in host/pluginhost.js")
	}
	open := strings.Index(code[start:], "{")
	if open < 0 {
		t.Fatal("setup broken: handleRequest has no body")
	}
	depth := 0
	end := -1
	for i := start + open; i < len(code); i++ {
		if code[i] == '{' {
			depth++
		} else if code[i] == '}' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end < 0 {
		t.Fatal("setup broken: handleRequest body is unbalanced")
	}
	body := code[start+open+1 : end]

	// The dispatch site: where the handler is actually invoked. Today that
	// is Promise.resolve().then(run); fall back to the first handler
	// reference, and if a fix restructures dispatch entirely, any bound
	// anywhere in the body counts (disp < 0).
	disp := strings.Index(body, "then(run")
	if disp < 0 {
		disp = strings.Index(body, "handler(")
	}

	// Loose acceptance: any recognizable inbound bound mechanism — the
	// existing bound constant, a saturation-style rejection code, an
	// in-flight counter, or a semaphore.
	lower := strings.ToLower(body)
	boundAt := -1
	for _, m := range []string{"max_inflight", "e_saturated", "saturat", "inflight", "in_flight", "semaphore"} {
		if i := strings.Index(lower, m); i >= 0 && (boundAt < 0 || i < boundAt) {
			boundAt = i
		}
	}
	if boundAt < 0 || (disp >= 0 && boundAt > disp) {
		t.Errorf("SECURITY: [pluginhost-frameflood] handleRequest dispatches frame→host requests with no saturation bound consulted before handler invocation — an untrusted sandboxed frame can flood the fully-privileged host page with unbounded in-flight handler work (only the outbound pending map is bounded). handleRequest body:\n%s", body)
	}

	// And saturation must ANSWER the frame, not drop it silently: an
	// E_SATURATED-style rejection reply through the always-answered channel.
	if !strings.Contains(body, "reply(") ||
		!(strings.Contains(body, "E_SATURATED") || strings.Contains(lower, "saturat") || strings.Contains(body, "E_BUSY")) {
		t.Errorf("SECURITY: [pluginhost-frameflood] saturated frame→host requests have no E_SATURATED-style rejection reply in handleRequest (the broker's contract is that every request is answered). handleRequest body:\n%s", body)
	}
}
