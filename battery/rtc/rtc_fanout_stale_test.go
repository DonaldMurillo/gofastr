package rtc

import (
	"bytes"
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
// through directly. Arrivals during the drain queue behind it, as the
// bus delivers a subscriber's messages in publish order.
func (h *heldFanout) release() {
	for {
		h.mu.Lock()
		if len(h.queue) == 0 {
			h.holding = false
			h.mu.Unlock()
			return
		}
		deliver := h.queue[0]
		h.queue = h.queue[1:]
		h.mu.Unlock()
		deliver()
	}
}

var _ fanout.Fanout = (*heldFanout)(nil)

// TestHeldFanoutReleaseKeepsOrder: a message published while release is
// still draining lands after every held one. The seam holds delivery of
// the first held message until the third has reached the wrapper.
func TestHeldFanoutReleaseKeepsOrder(t *testing.T) {
	bus := fanout.NewInProcess()
	h := &heldFanout{Fanout: bus}
	var mu sync.Mutex
	var got []string
	inFirst, arrived := make(chan struct{}), make(chan struct{})
	cancel, err := h.Subscribe("t", func(p []byte) {
		if string(p) == "1" {
			close(inFirst)
			<-arrived
		}
		mu.Lock()
		got = append(got, string(p))
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	publish := func(p string) {
		if err := bus.Publish(context.Background(), "t", []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	h.hold()
	publish("1")
	publish("2")
	waitFor(t, 3*time.Second, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.queue) == 2 }, "both held")
	go h.release()
	<-inFirst
	publish("3")
	waitFor(t, 3*time.Second, func() bool {
		h.mu.Lock()
		queued := len(h.queue)
		h.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		return queued == 2 || len(got) > 0
	}, "the third message reaches the wrapper")
	close(arrived)
	waitFor(t, 3*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 3 }, "all three delivered")
	if got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Fatalf("delivery order %v, want [1 2 3]", got)
	}
}

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

// TestJoinMirrorPublishedLateIsStillStale: R1's join goroutine stalls
// between hydrating p1 and publishing its mirror until after p1 has
// moved to R2, so the mirror leaves R1 later than the R2 seat joined.
// The seam (the announce gate, plus R1's lane held) pins that window.
func TestJoinMirrorPublishedLateIsStillStale(t *testing.T) {
	bus := fanout.NewInProcess()
	lane1, lane2 := &heldFanout{Fanout: bus}, &heldFanout{Fanout: bus}
	s1 := newTestSignaler(t, Config{})
	s2 := newTestSignaler(t, Config{})
	for _, s := range []*Signaler{s1, s2} {
		s.heartbeatEvery, s.remoteTTL = time.Hour, time.Hour
	}
	s1.testAtAnnounce = make(chan string)
	s1.testAnnounceGate = make(chan struct{})
	oldAtGate, publishOld := make(chan struct{}), make(chan struct{})
	go func() {
		for id := range s1.testAtAnnounce {
			if id == "p1" {
				close(oldAtGate)
				<-publishOld
			}
			s1.testAnnounceGate <- struct{}{}
		}
	}()
	if _, err := s1.SetFanout(lane1); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.SetFanout(lane2); err != nil {
		t.Fatal(err)
	}
	base1, base2 := startSignaler(t, s1), startSignaler(t, s2)

	lane2.hold()
	w, _ := join(t, base1, "room1", "pW")
	defer w.close()
	old, _ := join(t, base1, "room1", "p1")
	defer old.close()
	<-oldAtGate
	lane1.hold()
	nu, _ := join(t, base2, "room1", "p1")
	defer nu.close()

	close(publishOld) // R1 announces the old seat now, after R2's seat joined
	if je := w.expectEnv(t, "join"); !bytes.Contains(je.Payload, []byte(`"p1"`)) {
		t.Fatalf("join envelope = %s, want p1", je.Payload)
	}
	lane1.release() // R2's seat displaces the old one on R1
	w.expectEnv(t, "leave")
	w.expectEnv(t, "join")

	lane2.release() // the late mirror reaches R2
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pW") }, "R2 catches up with R1's roster")
	w.expectSilence(t, 500*time.Millisecond)
	if !hasPeer(s2, "room1", "p1") {
		t.Fatal("R2 dropped its own live p1 on a join mirror for an older seat")
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

// TestStaleJoinMirrorIsDropped: a join mirror whose seat predates the
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

	fakeReplica(t, bus, "ghost", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &PeerInfo{ID: "p1", Order: 1}})
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
