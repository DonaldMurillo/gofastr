package rtc

import (
	"testing"
)

// A join for a peer the recipient's snapshot already listed is not
// delivered: the channel may dequeue it after the snapshot, and the
// browser module would read it as a reconnect. A leave clears the mark,
// so the same peer's next join (a real rejoin) is delivered.
func TestJoinForListedPeerDroppedOnce(t *testing.T) {
	s := newTestSignaler(t, Config{})
	t.Cleanup(s.Close)
	s.mu.Lock()
	rm := s.newRoomLocked("r")
	rm.peers["pA"] = &roomPeer{info: PeerInfo{ID: "pA"}}
	rm.peers["pB"] = &roomPeer{info: PeerInfo{ID: "pB"}}
	s.mu.Unlock()
	src := roomSource{s: s, name: "r", listed: rm.listed}

	snap, _ := src.SnapshotFor("pA")
	if len(snap.Peers) != 1 || snap.Peers[0].ID != "pB" {
		t.Fatalf("pA's snapshot = %+v, want pB listed", snap.Peers)
	}
	joinB := roomEvent{kind: evJoin, peer: PeerInfo{ID: "pB"}}
	if _, ok := src.FilterEvent("pA", joinB); ok {
		t.Fatal("pA was sent a join for pB, which its snapshot already listed")
	}
	if _, ok := src.FilterEvent("pA", roomEvent{kind: evLeave, id: "pB"}); !ok {
		t.Fatal("pA was not sent pB's leave")
	}
	if _, ok := src.FilterEvent("pA", joinB); !ok {
		t.Fatal("pA was not sent pB's join after pB left: a rejoin must arrive")
	}
	// A listed peer whose join already went out before the snapshot
	// (so no duplicate ever comes) leaves and rejoins: the rejoin must
	// arrive, which only holds if the leave cleared the mark.
	src.SnapshotFor("pA") // re-hydrate: pB is listed again
	if _, ok := src.FilterEvent("pA", roomEvent{kind: evLeave, id: "pB"}); !ok {
		t.Fatal("pA was not sent pB's leave")
	}
	if _, ok := src.FilterEvent("pA", joinB); !ok {
		t.Fatal("pA was not sent pB's rejoin: the leave must clear the snapshot's mark")
	}
	// A peer the snapshot did not list joins normally.
	if _, ok := src.FilterEvent("pA", roomEvent{kind: evJoin, peer: PeerInfo{ID: "pC"}}); !ok {
		t.Fatal("pA was not sent a join for pC, which its snapshot never listed")
	}
}
