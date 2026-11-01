package rtc

// rtc_review4_test.go pins four cross-replica and overflow findings: a
// remote join announced twice to local peers, a leave published for a
// peer another replica still holds, signaling frames lost silently when
// a room's channel queue fills, and a seat left behind by a remote
// displacement.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// collectEnvs drains c for d and returns every envelope it read. It
// stops early when the socket dies.
func collectEnvs(c *wsClient, d time.Duration) []wireEnvelope {
	var out []wireEnvelope
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		env, err := c.recvEnv(time.Until(deadline))
		if err != nil {
			return out
		}
		out = append(out, env)
	}
	return out
}

// joinLeaveCounts tallies join and leave envelopes by subject id.
func joinLeaveCounts(t *testing.T, envs []wireEnvelope) (joins, leaves map[string]int) {
	t.Helper()
	joins, leaves = map[string]int{}, map[string]int{}
	for _, env := range envs {
		switch env.Type {
		case "join":
			var p PeerInfo
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				t.Fatalf("join payload: %v", err)
			}
			joins[p.ID]++
		case "leave":
			var l leavePayload
			if err := json.Unmarshal(env.Payload, &l); err != nil {
				t.Fatalf("leave payload: %v", err)
			}
			leaves[l.ID]++
		}
	}
	return joins, leaves
}

// TestEvictedSeatJoinsAnnouncedOnce: the seat eviction on s2 beats a
// roster listing p2 before p2's own join mirror. s1 must tell pY about
// p2 once: rtc.js reads a second join for a known peer as a reconnect
// and tears down the connection it just built.
func TestEvictedSeatJoinsAnnouncedOnce(t *testing.T) {
	base1, base2, _, s2 := twoReplicas(t, func() Config { return Config{MaxSocketsPerUser: 1} })
	y, _ := join(t, base1, "room1", "pY")
	defer y.close()
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pY") }, "s2 sees pY")

	p1, _ := joinAs(t, base2, "room1", "p1", "u")
	defer p1.close()
	y.expectEnv(t, "join")
	p2, _ := joinAs(t, base2, "room1", "p2", "u")
	defer p2.close()

	joins, leaves := joinLeaveCounts(t, collectEnvs(y, 700*time.Millisecond))
	if joins["p2"] != 1 || leaves["p1"] != 1 || joins["p1"] != 0 {
		t.Fatalf("pY saw joins %v leaves %v, want one leave p1 and one join p2", joins, leaves)
	}
}

// TestRemoteJoinOncePerSeat: a join mirror for a peer the remote view
// already holds at the same Order says nothing new; a later Order is a
// reconnect and is announced; a stale beat for the older seat is not.
func TestRemoteJoinOncePerSeat(t *testing.T) {
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = time.Hour, time.Hour
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	y, _ := join(t, startSignaler(t, s1), "room1", "pY")
	defer y.close()

	pX := PeerInfo{ID: "pX", Order: 5}
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1", Members: []PeerInfo{pX}})
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &pX})
	joins, _ := joinLeaveCounts(t, collectEnvs(y, 300*time.Millisecond))
	if joins["pX"] != 1 {
		t.Fatalf("roster then join mirror for one seat: pY saw %d joins, want 1", joins["pX"])
	}

	moved := PeerInfo{ID: "pX", Order: 9}
	fakeReplica(t, bus, "ghostC", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &moved})
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evRoster, Room: "room1", Members: []PeerInfo{pX}})
	joins, _ = joinLeaveCounts(t, collectEnvs(y, 300*time.Millisecond))
	if joins["pX"] != 1 {
		t.Fatalf("reconnect on ghostC then a stale ghostB beat: pY saw %d joins, want 1", joins["pX"])
	}
}

// beatAs publishes one raw roster beat as a fake replica, without
// t.Fatal so a beating goroutine can call it.
func beatAs(bus fanout.Fanout, node string, members []PeerInfo) error {
	body, err := json.Marshal(fanoutMsg{Node: node, Kind: evRoster, Room: "room1", Members: members})
	if err != nil {
		return err
	}
	return bus.Publish(context.Background(), rtcFanoutTopic, fanout.Wrap(node, body))
}

// stableIDMove sets up s1 with pY local and pX held by two fake
// replicas: ghostB announced it once (the replica that crashed) and
// ghostC keeps beating it (where it reconnected with the same id). The
// returned stop ends ghostC's beats.
func stableIDMove(t *testing.T) (*wsClient, fanout.Fanout, *Signaler, func()) {
	t.Helper()
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = 30*time.Millisecond, 150*time.Millisecond
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	y, _ := join(t, startSignaler(t, s1), "room1", "pY")
	t.Cleanup(y.close)

	if err := beatAs(bus, "ghostB", []PeerInfo{{ID: "pX", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_ = beatAs(bus, "ghostC", []PeerInfo{{ID: "pX", Order: 2}})
			select {
			case <-done:
				return
			case <-time.After(40 * time.Millisecond):
			}
		}
	}()
	var once sync.Once
	stop := func() { once.Do(func() { close(done); wg.Wait() }) }
	t.Cleanup(stop)
	waitFor(t, 3*time.Second, func() bool { return s1.remoteHasPeer("room1", "pX") }, "pX on the remote view")
	return y, bus, s1, stop
}

// TestExpiredSeatKeepsLivePeer: ghostB's entry expires while ghostC
// still beats pX. The sweep must not tell pY that pX left: nothing
// ghostC sends later would re-announce it.
func TestExpiredSeatKeepsLivePeer(t *testing.T) {
	y, _, s1, _ := stableIDMove(t)
	_, leaves := joinLeaveCounts(t, collectEnvs(y, 3*150*time.Millisecond))
	if leaves["pX"] != 0 {
		t.Fatalf("pY saw %d leaves for pX while ghostC holds it", leaves["pX"])
	}
	if !hasPeer(s1, "room1", "pX") {
		t.Fatal("merged roster lost pX while ghostC holds it")
	}
}

// TestLeaveMirrorKeepsLivePeer: ghostB's leave mirror for pX while
// ghostC still holds it is not a departure either.
func TestLeaveMirrorKeepsLivePeer(t *testing.T) {
	y, bus, _, _ := stableIDMove(t)
	collectEnvs(y, 100*time.Millisecond) // the joins for pX
	fakeReplica(t, bus, "ghostB", fanoutMsg{Kind: evLeave, Room: "room1", ID: "pX"})
	_, leaves := joinLeaveCounts(t, collectEnvs(y, 300*time.Millisecond))
	if leaves["pX"] != 0 {
		t.Fatalf("pY saw %d leaves for pX on ghostB's mirror while ghostC holds it", leaves["pX"])
	}
}

// TestLastSeatGoneStillLeaves: the guard above must not swallow a real
// departure: once ghostC stops beating, its entry expires and pY hears
// one leave.
func TestLastSeatGoneStillLeaves(t *testing.T) {
	y, _, _, stop := stableIDMove(t)
	collectEnvs(y, 100*time.Millisecond)
	stop()
	_, leaves := joinLeaveCounts(t, collectEnvs(y, 3*150*time.Millisecond))
	if leaves["pX"] != 1 {
		t.Fatalf("pY saw %d leaves for pX after every seat expired, want 1", leaves["pX"])
	}
}

// TestSignalFloodDeliveredOrClosed: seven peers trickle ICE at pB at
// once. Every frame must reach pB, or pB's socket must close so the
// client reconnects and renegotiates. A frame dropped on a socket that
// stays open leaves the far side waiting for ever.
func TestSignalFloodDeliveredOrClosed(t *testing.T) {
	s := newTestSignaler(t, Config{
		MaxFramesPerSecond: 100000,
		WS:                 stream.WSConfig{SendBuffer: 8192},
	})
	base := startSignaler(t, s)
	b, _ := join(t, base, "room1", "pB")
	defer b.close()

	const senders, frames = 7, 400
	clients := make([]*wsClient, senders)
	for i := range clients {
		clients[i], _ = join(t, base, "room1", fmt.Sprintf("s%d", i))
		defer clients[i].close()
		b.expectEnv(t, "join")
	}

	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *wsClient) {
			defer wg.Done()
			for n := range frames {
				c.send(map[string]any{"kind": "signal", "to": "pB", "type": "ice",
					"data": map[string]any{"candidate": fmt.Sprintf("c-%d", n)}})
			}
		}(c)
	}

	got := 0
	for got < senders*frames {
		env, err := b.recvEnv(3 * time.Second)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				wg.Wait()
				t.Fatalf("pB got %d of %d signals and its socket is still open", got, senders*frames)
			}
			t.Logf("pB's socket closed after %d of %d signals", got, senders*frames)
			wg.Wait()
			return
		}
		if env.Type == "signal" {
			got++
		}
	}
	wg.Wait()
}

// TestRemoteDisplacementFreesSeat: a peer that moves to another replica
// gives back its seat here. A seat left in the FIFO counts a dead
// socket against the cap, so the principal's next socket evicts a live
// one.
func TestRemoteDisplacementFreesSeat(t *testing.T) {
	base1, base2, s1, s2 := twoReplicas(t, func() Config { return Config{MaxSocketsPerUser: 2} })
	l, _ := joinAs(t, base1, "room1", "pL", "u")
	defer l.close()
	old, _ := joinAs(t, base1, "room1", "p1", "u")
	defer old.close()
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "p1") }, "s2 sees p1")

	moved, _ := joinAs(t, base2, "room1", "p1", "u")
	defer moved.close()
	waitFor(t, 3*time.Second, func() bool {
		s1.mu.Lock()
		defer s1.mu.Unlock()
		return s1.rooms["room1"].peers["p1"] == nil
	}, "s1 drops its local p1")

	s1.mu.Lock()
	seats := len(s1.seatOrder["u"])
	lp := s1.rooms["room1"].listed
	s1.mu.Unlock()
	lp.mu.Lock()
	_, listed := lp.by["p1"]
	lp.mu.Unlock()
	if seats != 1 {
		t.Fatalf("s1 holds %d seats after p1 moved away, want 1 (pL)", seats)
	}
	if listed {
		t.Fatal("s1 still holds the moved peer's listed set")
	}

	n, _ := join(t, base1, "room1", "pN")
	defer n.close()
	if !l.alive(300 * time.Millisecond) {
		t.Fatal("a new socket evicted live pL: the moved peer's seat was never freed")
	}
}

// TestLocalCloseUnderNewerSeatAnnouncesIt: pX reconnected on ghostC
// with the same id, and ghostC's roster beat (not its join mirror)
// reached s1 first, so local won and pY heard nothing. When the old
// local socket closes, pY must hear one join for the newer seat: the
// join mirror that lands afterwards finds that seat already in the
// remote view and says nothing, and a lost mirror says nothing either.
func TestLocalCloseUnderNewerSeatAnnouncesIt(t *testing.T) {
	bus := fanout.NewInProcess()
	s1 := newTestSignaler(t, Config{})
	s1.heartbeatEvery, s1.remoteTTL = time.Hour, time.Hour
	if _, err := s1.SetFanout(bus); err != nil {
		t.Fatal(err)
	}
	base1 := startSignaler(t, s1)
	x, _ := join(t, base1, "room1", "pX")
	y, _ := join(t, base1, "room1", "pY")
	defer y.close()
	x.expectEnv(t, "join") // pY's arrival

	moved := PeerInfo{ID: "pX", Order: time.Now().Add(time.Hour).UnixNano()}
	fakeReplica(t, bus, "ghostC", fanoutMsg{Kind: evRoster, Room: "room1", Members: []PeerInfo{moved}})
	waitFor(t, 3*time.Second, func() bool { return s1.remoteHasPeer("room1", "pX") }, "ghostC's seat recorded")
	y.expectSilence(t, 200*time.Millisecond) // local wins while the socket lives

	x.close()
	je := y.expectEnv(t, "join")
	var jp PeerInfo
	if json.Unmarshal(je.Payload, &jp) != nil || jp.ID != "pX" || jp.Order != moved.Order {
		t.Fatalf("join envelope = %s, want ghostC's pX seat", je.Payload)
	}
	fakeReplica(t, bus, "ghostC", fanoutMsg{Kind: evJoin, Room: "room1", Peer: &moved, At: time.Now().UnixNano()})
	joins, leaves := joinLeaveCounts(t, collectEnvs(y, 300*time.Millisecond))
	if joins["pX"] != 0 || leaves["pX"] != 0 {
		t.Fatalf("after the announce pY saw joins %v leaves %v, want nothing more", joins, leaves)
	}
}
