package rtc

// rtc_review_test.go pins two invariants the 2026-09-04 review found
// unguarded: a peer that migrates replicas is never announced as gone
// while the local replica holds it, and a reconnect snapshot always
// outruns every event the same client applied before reconnecting.

import (
	"encoding/json"
	"testing"
	"time"
)

// TestMigratingPeerGetsNoSpuriousLeave: pX is connected on S2, then
// reconnects with the same PeerID onto S1. S2 displaces its socket and
// beats a roster without pX; S1 must NOT mirror that as a leave, since
// S1 now holds pX locally. A watcher on S1 sees joins for pX and then
// silence while pX stays.
func TestMigratingPeerGetsNoSpuriousLeave(t *testing.T) {
	base1, base2, s1, _ := twoReplicas(t, func() Config { return Config{} })

	w, _ := join(t, base1, "room1", "pW")
	defer w.close()

	oldX, _ := join(t, base2, "room1", "pX")
	defer oldX.close()
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s1, "room1", "pX") }, "S1 sees remote pX")

	newX, _ := join(t, base1, "room1", "pX")
	defer newX.close()

	// The remote join mirror and the local join, in either order.
	for i := range 2 {
		env := w.expectEnv(t, "join")
		var jp PeerInfo
		if err := json.Unmarshal(env.Payload, &jp); err != nil || jp.ID != "pX" {
			t.Fatalf("join #%d = %s (%v)", i+1, env.Payload, err)
		}
	}
	// Long enough for S2's displacement beat and several heartbeats
	// (60 ms) plus a remote TTL (300 ms) to land.
	w.expectSilence(t, 700*time.Millisecond)
	if !hasPeer(s1, "room1", "pX") {
		t.Fatal("pX vanished from S1's roster while its socket is live")
	}
	if !newX.alive(100 * time.Millisecond) {
		t.Fatal("pX's socket on S1 was closed")
	}
}

// TestReconnectSnapshotOutrunsAppliedEvents: a client that applied
// events up to sequence N and reconnects gets a snapshot with a
// sequence above N, or its reducer (strictly greater than applied)
// rejects the snapshot and it never rehydrates. This is the room.seq
// pairing with every Publish, guarded end to end.
func TestReconnectSnapshotOutrunsAppliedEvents(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	b, _ := join(t, base, "room1", "pB")
	defer b.close()

	a.expectEnv(t, "join") // pB
	b.send(map[string]any{"kind": "status", "data": map[string]any{"muted": true}})
	last := a.expectEnv(t, "status")

	a.close()
	re, snap2 := join(t, base, "room1", "pA")
	defer re.close()
	if snap2.Sequence <= last.Sequence {
		t.Fatalf("reconnect snapshot sequence %d must exceed the last applied event %d", snap2.Sequence, last.Sequence)
	}
}
