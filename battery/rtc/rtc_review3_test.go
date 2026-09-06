package rtc

// rtc_review3_test.go pins the third review round: the round-2 fixes
// re-examined and the lane ingest nobody had fed hostile beats. Each
// test was red before its fix.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// TestRemoteDisplacementSweepsRoom: a peer that migrates to another
// replica empties the local room through the lane join, not through
// peerGone (which no-ops for the displaced socket). That path must arm
// the same idle sweep, or the room and its channel goroutine live for
// the life of the process on every load-balancer move.
func TestRemoteDisplacementSweepsRoom(t *testing.T) {
	f := fanout.NewInProcess()
	cfg := func() Config { return Config{RoomIdleTTL: 50 * time.Millisecond} }
	s1, s2 := newTestSignaler(t, cfg()), newTestSignaler(t, cfg())
	for _, s := range []*Signaler{s1, s2} {
		s.heartbeatEvery = time.Hour
		s.remoteTTL = time.Hour
		if _, err := s.SetFanout(f); err != nil {
			t.Fatalf("SetFanout: %v", err)
		}
	}
	b1, b2 := startSignaler(t, s1), startSignaler(t, s2)

	a1, _ := join(t, b1, "room1", "pA")
	defer a1.close()
	waitFor(t, 2*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "s2 to see pA")

	a2, _ := join(t, b2, "room1", "pA")
	defer a2.close()
	waitFor(t, 2*time.Second, func() bool {
		s1.mu.Lock()
		defer s1.mu.Unlock()
		rm, held := s1.rooms["room1"]
		return held && len(rm.peers) == 0
	}, "s1's local pA to be displaced")

	waitFor(t, 2*time.Second, func() bool {
		s1.mu.Lock()
		defer s1.mu.Unlock()
		_, held := s1.rooms["room1"]
		return !held
	}, "s1's displaced-empty room to be swept with no accessor call")
}

// TestRefusalDropsSocketPromptly: the close code is on the wire before
// the close handshake wait begins, so a refusal must not hold the
// hijacked socket and its goroutines for the full CloseTimeout (1 s)
// per unauthenticated dial. The client reads the close frame, answers
// nothing, and must see EOF well inside that second.
func TestRefusalDropsSocketPromptly(t *testing.T) {
	s := New(Config{Authorize: func(r *http.Request) (Join, error) { return Join{}, errors.New("no") }})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	c, ok := wsTryDial(srv.URL + "/ws/room1?peer=x")
	if !ok {
		t.Fatal("refused upgrade must be accepted and closed with a code")
	}
	defer c.close()
	if _, err := c.recv(2 * time.Second); !errors.Is(err, errClosed) || c.closeCode != 4403 {
		t.Fatalf("want close 4403, got err=%v code=%d", err, c.closeCode)
	}
	_, err := c.recv(300 * time.Millisecond)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the refused socket was still held 300 ms after the close frame")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, errClosed) {
		t.Fatalf("after the close frame: %v, want EOF", err)
	}
}

// forgedBeat publishes one roster beat from a node this Signaler has
// never heard of.
func forgedBeat(t *testing.T, f fanout.Fanout, room string, members []PeerInfo) {
	t.Helper()
	msg := fanoutMsg{Node: fanout.NewNodeID(), Kind: evRoster, Room: room, Members: members}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Publish(context.Background(), rtcFanoutTopic, fanout.Wrap(msg.Node, body)); err != nil {
		t.Fatal(err)
	}
}

// TestRosterBeatMemberCap: MaxPeers caps a room local plus remote, so
// a lane beat naming more members than that is dropped whole (as a
// beat with one bad member already is). One 5000-member message used
// to put 5001 peers in a room capped at 4 and flood its local
// members' channels with joins.
func TestRosterBeatMemberCap(t *testing.T) {
	f := fanout.NewInProcess()
	s := newTestSignaler(t, Config{MaxPeers: 4})
	s.heartbeatEvery = time.Hour
	s.remoteTTL = time.Hour
	if _, err := s.SetFanout(f); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	base := startSignaler(t, s)
	a, _ := join(t, base, "room1", "pA")
	defer a.close()

	var flood []PeerInfo
	for i := range 5000 {
		flood = append(flood, PeerInfo{ID: "ghost" + strconv.Itoa(i), Order: int64(i)})
	}
	forgedBeat(t, f, "room1", flood)
	a.expectSilence(t, 300*time.Millisecond)
	if got := len(s.Peers("room1")); got != 1 {
		t.Fatalf("an over-cap beat landed: %d members in a room capped at 4", got)
	}

	// Control: a beat within the cap is applied.
	forgedBeat(t, f, "room1", []PeerInfo{{ID: "r1", Order: 1}, {ID: "r2", Order: 2}, {ID: "r3", Order: 3}})
	waitFor(t, 2*time.Second, func() bool { return len(s.Peers("room1")) == 4 }, "an in-cap beat to land")
}

// TestRosterBeatCannotRestateLocalIdentity: local wins on the join
// mirror as it already does on the leave mirror. A beat naming a
// locally held id must not publish a join carrying the lane's name,
// user, and role for that id to the room.
func TestRosterBeatCannotRestateLocalIdentity(t *testing.T) {
	f := fanout.NewInProcess()
	s := newTestSignaler(t, Config{})
	s.heartbeatEvery = time.Hour
	s.remoteTTL = time.Hour
	if _, err := s.SetFanout(f); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	base := startSignaler(t, s)
	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b := wsDial(t, base+"/ws/room1?peer=pB&name=Real%20Bob")
	defer b.close()
	a.expectEnv(t, "join")

	forgedBeat(t, f, "room1", []PeerInfo{{ID: "pB", DisplayName: "Admin", User: "admin@example.com", Role: "moderator", Order: 1}})
	a.expectSilence(t, 300*time.Millisecond)
	for _, p := range s.Peers("room1") {
		if p.ID == "pB" && p.DisplayName != "Real Bob" {
			t.Fatalf("the roster restated pB as %q", p.DisplayName)
		}
	}
}

// TestPlainRefusalStatusClamped: an HTTPError with a status outside
// 4xx/5xx (42, or the zero value of a forgotten field) is 403 on a
// plain request, not a WriteHeader panic that kills the response.
func TestPlainRefusalStatusClamped(t *testing.T) {
	s := New(Config{Authorize: func(r *http.Request) (Join, error) {
		return Join{}, &HTTPError{Status: 42, Message: "nope"}
	}})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	res, err := http.Get(srv.URL + "/ws/room1")
	if err != nil {
		t.Fatalf("plain refusal with an out-of-range status killed the response: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d, want 403", res.StatusCode)
	}
}

// TestFailedUpgradeHasStatus: stream.Upgrade writes nothing on a
// handshake it refuses, so the handler must. An authorized plain GET
// of the endpoint is 400, never an empty 200.
func TestFailedUpgradeHasStatus(t *testing.T) {
	s := New(Config{Authorize: func(r *http.Request) (Join, error) { return Join{Room: "room1"}, nil }})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	res, err := http.Get(srv.URL + "/ws/room1")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET of the signaling endpoint: %d, want 400", res.StatusCode)
	}
}

// TestRemoteJoinRespectsCap: the single-join lane mirror grows a room
// one member at a time and must stop at MaxPeers like the roster beat
// does; a known id (an update, or a peer migrating from this replica)
// is never refused.
func TestRemoteJoinRespectsCap(t *testing.T) {
	f := fanout.NewInProcess()
	s := newTestSignaler(t, Config{MaxPeers: 2})
	s.heartbeatEvery = time.Hour
	s.remoteTTL = time.Hour
	if _, err := s.SetFanout(f); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	base := startSignaler(t, s)
	a, _ := join(t, base, "room1", "pA")
	defer a.close()

	node := fanout.NewNodeID()
	remoteJoin := func(id string, order int64) {
		t.Helper()
		body, err := json.Marshal(fanoutMsg{Node: node, Kind: evJoin, Room: "room1", Peer: &PeerInfo{ID: id, Order: order}})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Publish(context.Background(), rtcFanoutTopic, fanout.Wrap(node, body)); err != nil {
			t.Fatal(err)
		}
	}
	remoteJoin("r1", 1)
	a.expectEnv(t, "join")
	remoteJoin("r2", 2) // the room is full: pA + r1
	a.expectSilence(t, 300*time.Millisecond)
	if got := len(s.Peers("room1")); got != 2 {
		t.Fatalf("room capped at 2 holds %d after an over-cap lane join", got)
	}
	remoteJoin("r1", 3) // a known id updates freely (and re-announces)
	a.expectEnv(t, "join")
	for _, p := range s.Peers("room1") {
		if p.ID == "r1" && p.Order != 3 {
			t.Fatalf("known remote id was not updated: order %d", p.Order)
		}
	}
}
