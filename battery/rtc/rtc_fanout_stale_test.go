package rtc

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// heldFanout wraps a fanout so one subscriber's inbound lane can be held
// and released in publish order: the deterministic stand-in for a bus
// that delivers one replica's messages late.
type heldFanout struct {
	fanout.Fanout
	mu      sync.Mutex
	holding bool
	queue   []func()
}

func (h *heldFanout) Subscribe(topic string, fn func([]byte)) (func(), error) {
	return h.Fanout.Subscribe(topic, func(payload []byte) {
		h.mu.Lock()
		if h.holding {
			h.queue = append(h.queue, func() { fn(payload) })
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		fn(payload)
	})
}

func (h *heldFanout) hold() {
	h.mu.Lock()
	h.holding = true
	h.mu.Unlock()
}

// release delivers everything held, in order, then lets later messages
// through directly.
func (h *heldFanout) release() {
	h.mu.Lock()
	queued := h.queue
	h.queue, h.holding = nil, false
	h.mu.Unlock()
	for _, deliver := range queued {
		deliver()
	}
}

var _ fanout.Fanout = (*heldFanout)(nil)

// TestLateJoinMirrorDoesNotKickTheMovedPeer pins #474's duplicate leave.
// p1 joins R1, then moves to R2 before R1's join mirror has reached R2.
// When that stale mirror lands, R2 must recognise it predates the local
// socket and drop it. Before the fix R2 read it as "p1 moved to R1",
// kicked the live socket and beat an empty roster, which R1 turned into
// a second leave for the watcher: the exact CI failure, with no timer
// involved.
func TestLateJoinMirrorDoesNotKickTheMovedPeer(t *testing.T) {
	bus := fanout.NewInProcess()
	lane2 := &heldFanout{Fanout: bus}
	s1 := newTestSignaler(t, Config{})
	s2 := newTestSignaler(t, Config{})
	for _, s := range []*Signaler{s1, s2} {
		s.heartbeatEvery = 60 * time.Millisecond
		s.remoteTTL = 10 * time.Second
	}
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatalf("SetFanout s1: %v", err)
	}
	if _, err := s2.SetFanout(lane2); err != nil {
		t.Fatalf("SetFanout s2: %v", err)
	}
	base1, base2 := startSignaler(t, s1), startSignaler(t, s2)

	lane2.hold() // R2 hears nothing from R1 until released
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
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := old.recv(time.Until(deadline)); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("locally replaced socket never closed")
		}
	}

	// Now R1's stale join mirror (and its roster beats) reach R2.
	lane2.release()
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pW") }, "R2 catches up with R1's roster")
	// The watcher hears nothing more. The moved socket stays open and
	// learns about pW from the released lane, once.
	w.expectSilence(t, 500*time.Millisecond)
	pw := nu.expectEnv(t, "join")
	var pwp PeerInfo
	if json.Unmarshal(pw.Payload, &pwp) != nil || pwp.ID != "pW" {
		t.Fatalf("moved socket's first envelope = %s %s, want join pW", pw.Type, pw.Payload)
	}
	nu.expectSilence(t, 300*time.Millisecond)
	if !hasPeer(s2, "room1", "p1") {
		t.Fatal("R2 dropped its own live p1 on a stale join mirror")
	}
}

// fakeReplica publishes one raw lane message as a replica that does not
// exist, so a test can hand a receiver exactly the envelope it wants.
func fakeReplica(t *testing.T, bus fanout.Fanout, node string, msg fanoutMsg) {
	t.Helper()
	msg.Node = node
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), rtcFanoutTopic, fanout.Wrap(node, body)); err != nil {
		t.Fatal(err)
	}
}

// TestStaleJoinMirrorIsDropped: a join mirror whose clock predates the
// local socket is ignored outright, whatever replica sent it.
func TestStaleJoinMirrorIsDropped(t *testing.T) {
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = 60*time.Millisecond, 10*time.Second
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	base1 := startSignaler(t, s1)
	p1, _ := join(t, base1, "room1", "p1")
	defer p1.close()
	w, _ := join(t, base1, "room1", "pW")
	defer w.close()
	p1.expectEnv(t, "join") // pW's own arrival

	fakeReplica(t, bus, "ghost", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &PeerInfo{ID: "p1"}, At: 1})
	w.expectSilence(t, 300*time.Millisecond)
	p1.expectSilence(t, 100*time.Millisecond)
	if !hasPeer(s1, "room1", "p1") {
		t.Fatal("stale join mirror displaced the live local peer")
	}
}

// TestLocalCloseUnderRemoteSeatPublishesNoLeave: when a live remote seat
// holds the same id, the merged roster keeps the peer through the local
// socket's close, so the room hears no leave for it.
func TestLocalCloseUnderRemoteSeatPublishesNoLeave(t *testing.T) {
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = 60*time.Millisecond, 10*time.Second
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	base1 := startSignaler(t, s1)
	p1, _ := join(t, base1, "room1", "p1")
	w, _ := join(t, base1, "room1", "pW")
	defer w.close()

	// Local wins while the socket lives: the beat changes nothing on the wire.
	fakeReplica(t, bus, "ghost", fanoutMsg{Kind: evRoster, Room: "room1", Members: []PeerInfo{{ID: "p1"}}})
	waitFor(t, 3*time.Second, func() bool { return s1.remoteHasPeer("room1", "p1") }, "remote seat recorded")
	w.expectSilence(t, 200*time.Millisecond)

	p1.close()
	w.expectSilence(t, 500*time.Millisecond)
	if !hasPeer(s1, "room1", "p1") {
		t.Fatal("merged roster lost p1 while a live remote seat holds it")
	}
}

// remoteHasPeer is the locked helper under the lock, for tests.
func (s *Signaler) remoteHasPeer(room, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remoteHasPeerLocked(room, id)
}
