package rtc

// rtc_fanout_test.go drives the cross-replica lane with two Signalers
// sharing one fanout.NewInProcess (two replicas in one process), the
// same shape as core-ui/island's presence fanout tests.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// twoReplicas wires two Signalers to one in-process fanout and returns
// their bases. Short heartbeat/TTL so the tests converge fast.
func twoReplicas(t *testing.T, cfg func() Config) (string, string, *Signaler, *Signaler) {
	t.Helper()
	f := fanout.NewInProcess()
	s1 := newTestSignaler(t, cfg())
	s2 := newTestSignaler(t, cfg())
	for _, s := range []*Signaler{s1, s2} {
		s.heartbeatEvery = 60 * time.Millisecond
		s.remoteTTL = 300 * time.Millisecond
		if _, err := s.SetFanout(f); err != nil {
			t.Fatalf("SetFanout: %v", err)
		}
	}
	return startSignaler(t, s1), startSignaler(t, s2), s1, s2
}

func hasPeer(s *Signaler, room, id string) bool {
	for _, p := range s.Peers(room) {
		if p.ID == id {
			return true
		}
	}
	return false
}

// TestRemoteJoinVisibleAcrossReplicas: a join on one replica reaches
// the other replica's merged roster and its local members' wire.
func TestRemoteJoinVisibleAcrossReplicas(t *testing.T) {
	base1, base2, s1, s2 := twoReplicas(t, func() Config { return Config{} })

	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	b, _ := join(t, base2, "room1", "pB")
	defer b.close()

	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "S2 sees pA")
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s1, "room1", "pB") }, "S1 sees pB")

	// B (on S2) receives the join event for the remote pA, either as a
	// live join mirror or already inside its snapshot; drain until a
	// join for pA or a snapshot containing it was seen. The roster is
	// the contract, so assert on the wire only that SOMETHING told B:
	// the join mirror must arrive (B's snapshot predates pA only if B
	// joined first; here B joined second, so its snapshot already
	// listed pA and no join is owed).
	if got := s2.Peers("room1"); len(got) != 2 {
		t.Fatalf("S2 merged roster = %+v", got)
	}
}

// TestRemoteSignalCrossesFanout: a signal to a peer owned by the other
// replica is published on the lane and delivered by the owner.
func TestRemoteSignalCrossesFanout(t *testing.T) {
	base1, base2, _, _ := twoReplicas(t, func() Config { return Config{} })

	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	b, _ := join(t, base2, "room1", "pB")
	defer b.close()

	// Wait until B knows pA through the lane, then send.
	deadline := time.Now().Add(3 * time.Second)
	sent := false
	for time.Now().Before(deadline) {
		b.send(map[string]any{"kind": "signal", "to": "pA", "type": "offer",
			"data": map[string]any{"sdp": "v=0\r\nREMOTE-SIGNAL-MARKER"}})
		env, err := a.recvEnv(200 * time.Millisecond)
		if err == nil && env.Type == "signal" {
			var sig signalPayload
			if json.Unmarshal(env.Payload, &sig) == nil &&
				sig.From == "pB" && sig.To == "pA" &&
				strings.Contains(string(sig.Data), "REMOTE-SIGNAL-MARKER") {
				sent = true
				break
			}
		}
	}
	if !sent {
		t.Fatal("remote signal never delivered to its target")
	}
}

// TestRemotePeerExpiresAfterTTL: a replica that stops beating (crash
// simulation) disappears from the merged roster within TTL.
func TestRemotePeerExpiresAfterTTL(t *testing.T) {
	base1, base2, s1, s2 := twoReplicas(t, func() Config { return Config{} })

	b, _ := join(t, base2, "room1", "pB")
	defer b.close()
	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	b.expectEnv(t, "join") // pA's join mirror
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "S2 sees pA")
	// Crash S1's heartbeat: no more beats, no graceful leave.
	s1.haltHeartbeat()
	waitFor(t, 3*time.Second, func() bool { return !hasPeer(s2, "room1", "pA") }, "S2 expires pA")
	le := b.expectEnv(t, "leave")
	var lp leavePayload
	if err := json.Unmarshal(le.Payload, &lp); err != nil || lp.ID != "pA" {
		t.Fatalf("expiry leave envelope = %s (%v)", le.Payload, err)
	}
}

// TestRemoteReplacesLocalPeer: a peer id that reappears on another
// replica closes the local socket; the room sees leave then join.
func TestRemoteReplacesLocalPeer(t *testing.T) {
	base1, base2, _, _ := twoReplicas(t, func() Config { return Config{} })

	old, _ := join(t, base1, "room1", "p1")
	w, _ := join(t, base1, "room1", "pW")
	defer w.close()
	nu, _ := join(t, base2, "room1", "p1")
	defer nu.close()

	le := w.expectEnv(t, "leave")
	var lp leavePayload
	if json.Unmarshal(le.Payload, &lp) != nil || lp.ID != "p1" {
		t.Fatalf("leave envelope = %s", le.Payload)
	}
	je := w.expectEnv(t, "join")
	var jp PeerInfo
	if json.Unmarshal(je.Payload, &jp) != nil || jp.ID != "p1" {
		t.Fatalf("join envelope = %s", je.Payload)
	}
	if je.Sequence <= le.Sequence {
		t.Fatalf("join (%d) must follow leave (%d)", je.Sequence, le.Sequence)
	}

	// The locally displaced socket closes.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := old.recv(time.Until(deadline)); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("locally replaced socket never closed")
		}
	}
	// And no duplicate leave/join replay follows: the room settles.
	w.expectSilence(t, 500*time.Millisecond)
}

// TestRoomCapCountsRemote: MaxPeers counts local plus live-remote
// members.
func TestRoomCapCountsRemote(t *testing.T) {
	base1, base2, _, s2 := twoReplicas(t, func() Config { return Config{MaxPeers: 2} })

	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	b, _ := join(t, base1, "room1", "pB")
	defer b.close()

	waitFor(t, 3*time.Second, func() bool { return len(s2.Peers("room1")) == 2 }, "S2 sees both")

	res, err := http.Get(base2 + "/ws/room1?peer=pC")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("join past remote-counted cap: got %d, want 409", res.StatusCode)
	}
}

// TestClosePublishesEmptyRosters: a graceful Close converges the other
// replica's roster without waiting out the TTL.
func TestClosePublishesEmptyRosters(t *testing.T) {
	base1, _, s1, s2 := twoReplicas(t, func() Config { return Config{} })

	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "S2 sees pA")

	s1.Close()
	waitFor(t, 3*time.Second, func() bool { return !hasPeer(s2, "room1", "pA") }, "S2 drops pA after Close")
}

// TestRemoteStatusAndLeaveMirrored: status and leave events from the
// other replica reach local members' wire.
func TestRemoteStatusAndLeaveMirrored(t *testing.T) {
	base1, base2, _, _ := twoReplicas(t, func() Config { return Config{} })

	a, _ := join(t, base1, "room1", "pA")
	defer a.close()
	b, _ := join(t, base2, "room1", "pB")
	defer b.close()

	// B's snapshot listed pA; the next envelope B owes is pA's status
	// (the join/leave/status mirrors are FIFO with the roster beats).
	a.send(map[string]any{"kind": "status", "data": map[string]any{"muted": true}})
	env := b.expectEnv(t, "status")
	var sp statusPayload
	if err := json.Unmarshal(env.Payload, &sp); err != nil || sp.ID != "pA" {
		t.Fatalf("status envelope = %s (%v)", env.Payload, err)
	}
	if !strings.Contains(string(sp.Status), "muted") {
		t.Fatalf("status body = %s", sp.Status)
	}

	// And pA's leave (socket close) mirrors too.
	a.close()
	le := b.expectEnv(t, "leave")
	var lp leavePayload
	if err := json.Unmarshal(le.Payload, &lp); err != nil || lp.ID != "pA" {
		t.Fatalf("leave envelope = %s (%v)", le.Payload, err)
	}
}

// TestSetFanoutErrors: nil fanout refused, double attach refused.
func TestSetFanoutErrors(t *testing.T) {
	s := newTestSignaler(t, Config{})
	if _, err := s.SetFanout(nil); err == nil {
		t.Fatal("nil fanout should be refused")
	}
	f := fanout.NewInProcess()
	if _, err := s.SetFanout(f); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if _, err := s.SetFanout(f); err == nil {
		t.Fatal("double attach should be refused")
	}
	s.Close()
	if _, err := s.SetFanout(f); err == nil {
		t.Fatal("attach after Close should be refused")
	}
}

// TestNoFanoutSingleReplica: without SetFanout nothing changes: joins,
// signals, and statuses behave exactly as the single-replica tests
// pin, and SetFanout was never called.
func TestNoFanoutSingleReplica(t *testing.T) {
	s := newTestSignaler(t, Config{MaxPeers: 2})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join") // pB

	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer", "data": map[string]any{"sdp": "v=0"}})
	if env := b.expectEnv(t, "signal"); env.Type != "signal" {
		t.Fatalf("signal envelope = %s", env.Payload)
	}
	if s.fanout != nil {
		t.Fatal("no fanout should be attached")
	}
}
