//go:build red

package rtc

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: strict JSON (duplicate + case-folded keys refused) at every request-body decode —
// the inbound signaling frame is the tree's one inbound request decode still on plain
// json.Unmarshal (house strict-decoder convention pinned everywhere else).
// Surfaces: room.go::Serve read loop :352-355 — verified: {"kind":"status","Kind":"signal",...}
// decodes Kind="signal" (case-folded overwrite); dup "kind" keys last-win.
// Finding: the read loop decodes inboundMsg with encoding/json's lenient rules, so one frame
// can name its kind twice (last wins) or spell it with folded case and overwrite the first
// binding. HONEST CAVEAT: no privilege delta today (both kinds equally available, to/type
// validated identically, data opaque); this pins the convention gap so a future
// server-meaningful envelope field cannot be shadowed.
// Fix direction: decode inboundMsg with the shared strict decoder (dup-key and case-folded-key
// refusal, as the battery private decoder / acp frames use) and drop the frame on refusal;
// the socket stays usable (drop, not close, like every other malformed frame).

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

// frameStrictAssertDropped proves one raw inbound frame is dropped: the
// victim hears nothing on a socket that stays open, and the sender stays
// usable (the TestUnknownTargetDropped dropped-and-usable shape). It is
// expectSilence plus the round-5 SECURITY tag on the violation legs.
func frameStrictAssertDropped(t *testing.T, raw, name string) {
	t.Helper()
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join") // pB joined after a: drain it first

	a.writeFrame(0x1, []byte(raw))
	env, err := b.recvEnv(300 * time.Millisecond)
	if err == nil {
		t.Errorf("SECURITY: [rtc-frame-lenient] %s frame %s was decoded and delivered as a %s envelope: the read loop's plain json.Unmarshal lets folded-case and duplicate envelope keys overwrite the first binding", name, raw, env.Type)
	} else {
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("SECURITY: [rtc-frame-lenient] %s frame %s closed the victim socket (%v): strictness must drop the frame, not the connection", name, raw, err)
		}
	}
	// Dropped, and the relay is still alive: a well-formed status from
	// the same sender still reaches the victim.
	a.send(map[string]any{"kind": "status", "data": map[string]any{"ok": true}})
	env = b.expectEnv(t, "status")
	var sp statusPayload
	if err := json.Unmarshal(env.Payload, &sp); err != nil || sp.ID != "pA" {
		t.Fatalf("setup broken: status envelope = %s (%v)", env.Payload, err)
	}
}

func TestFrameDecodeRedRefusesFoldedKeys(t *testing.T) {
	frameStrictAssertDropped(t,
		`{"kind":"status","Kind":"signal","to":"pB","type":"offer","data":{}}`,
		"case-folded")
}

func TestFrameDecodeRedRefusesDuplicateKind(t *testing.T) {
	frameStrictAssertDropped(t,
		`{"kind":"status","kind":"signal","to":"pB","type":"offer","data":{}}`,
		"duplicate-kind")
}

// TestFrameDecodeRedWellFormedDelivers is the GREEN-guard leg: a well-formed
// status/signal pair DOES deliver, proving any drop above is strictness and
// not a broken relay. Passes today and must keep passing after the fix.
func TestFrameDecodeRedWellFormedDelivers(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join") // pB

	a.send(map[string]any{"kind": "status", "data": map[string]any{"muted": false}})
	env := b.expectEnv(t, "status")
	var sp statusPayload
	if err := json.Unmarshal(env.Payload, &sp); err != nil || sp.ID != "pA" {
		t.Fatalf("setup broken: status envelope = %s (%v)", env.Payload, err)
	}
	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer", "data": map[string]any{"sdp": "v=0"}})
	sig := b.expectEnv(t, "signal")
	var sip signalPayload
	if err := json.Unmarshal(sig.Payload, &sip); err != nil || sip.From != "pA" || sip.To != "pB" || sip.Type != "offer" {
		t.Fatalf("setup broken: signal envelope = %s (%v)", sig.Payload, err)
	}
}
