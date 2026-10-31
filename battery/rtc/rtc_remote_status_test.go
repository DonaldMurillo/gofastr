package rtc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// statusesFor returns the status bodies pY's socket saw for id, in order.
func statusesFor(t *testing.T, envs []wireEnvelope, id string) []string {
	t.Helper()
	var out []string
	for _, env := range envs {
		if env.Type != "status" {
			continue
		}
		var sp statusPayload
		if err := json.Unmarshal(env.Payload, &sp); err != nil {
			t.Fatalf("status payload: %v", err)
		}
		if sp.ID == id {
			out = append(out, string(sp.Status))
		}
	}
	return out
}

// remoteStatusRoom starts one replica with pY local and a fake bus the
// test publishes other replicas' lane messages on.
func remoteStatusRoom(t *testing.T) (*wsClient, fanout.Fanout) {
	t.Helper()
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = time.Hour, time.Hour
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	y, _ := join(t, startSignaler(t, s1), "room1", "pY")
	t.Cleanup(y.close)
	return y, bus
}

// A status mirror lost on the lane is repaired by the next roster beat
// carrying the new status for the same seat. The delta announced only
// joins and leaves, so pY kept the stale status for good (2026-10-27 B12).
func TestRosterBeatRepairsStatus(t *testing.T) {
	y, bus := remoteStatusRoom(t)
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1",
		Members: []PeerInfo{{ID: "pX", Order: 5, Status: json.RawMessage(`{"muted":false}`)}}})
	collectEnvs(y, 200*time.Millisecond) // the join

	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1",
		Members: []PeerInfo{{ID: "pX", Order: 5, Status: json.RawMessage(`{"muted":true}`)}}})
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1",
		Members: []PeerInfo{{ID: "pX", Order: 5, Status: json.RawMessage(`{"muted":true}`)}}})
	got := statusesFor(t, collectEnvs(y, 300*time.Millisecond), "pX")
	if len(got) != 1 || got[0] != `{"muted":true}` {
		t.Fatalf("pY saw statuses %v for pX, want one {\"muted\":true}", got)
	}
}

// A status from the replica pX left, delivered after pX reconnected on
// another replica, is not the peer's status: the merged view keeps the
// newer seat's, and pY must not hear the old one (2026-10-27 B13).
func TestStaleReplicaStatusIsDropped(t *testing.T) {
	y, bus := remoteStatusRoom(t)
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1",
		Members: []PeerInfo{{ID: "pX", Order: 1}}})
	moved := PeerInfo{ID: "pX", Order: 2}
	fakeReplica(t, bus, "ghostC", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &moved})
	collectEnvs(y, 200*time.Millisecond) // the join and the reconnect

	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evStatus, Room: "room1", ID: "pX",
		Status: json.RawMessage(`{"old":true}`)})
	if got := statusesFor(t, collectEnvs(y, 300*time.Millisecond), "pX"); len(got) != 0 {
		t.Fatalf("pY heard %v from the replica pX left", got)
	}

	fakeReplica(t, bus, "ghostC", fanoutMsg{Kind: evStatus, Room: "room1", ID: "pX",
		Status: json.RawMessage(`{"new":true}`)})
	if got := statusesFor(t, collectEnvs(y, 300*time.Millisecond), "pX"); len(got) != 1 || got[0] != `{"new":true}` {
		t.Fatalf("pY saw %v from pX's current replica, want one {\"new\":true}", got)
	}
}
