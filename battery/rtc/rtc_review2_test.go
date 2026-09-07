package rtc

// rtc_review2_test.go pins the outward review round: behaviours the
// field (coturn docs, the TURN REST draft, PeerJS server) settled that
// the first cut got wrong. Each test was red before its fix.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// TestIdleRoomSweptWithoutAccess: the idle sweep runs on its own clock.
// A room whose last peer left is gone RoomIdleTTL later even when
// nothing calls Rooms or Peers. (The sweep used to run only from the
// Rooms accessor, so a host that only served joins kept every room and
// its channel goroutine for the life of the process, and the example's
// ?room= let one client mint them without bound.)
func TestIdleRoomSweptWithoutAccess(t *testing.T) {
	s := newTestSignaler(t, Config{RoomIdleTTL: 50 * time.Millisecond})
	base := startSignaler(t, s)

	a, _ := join(t, base, "gone", "pA")
	a.close()

	waitFor(t, 2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.rooms) == 0
	}, "the empty room to be swept with no accessor call")
}

// TestIdleSweepKeepsRemoteRoster: dropping the LOCAL room must not
// discard what other replicas hold for that name. Heartbeat and remote
// TTL are pinned to an hour so nothing can rebuild the table behind
// the assertion: if pA survives, it survived the sweep itself.
func TestIdleSweepKeepsRemoteRoster(t *testing.T) {
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

	a, _ := join(t, b1, "room1", "pA")
	defer a.close()
	b, _ := join(t, b2, "room1", "pB")
	waitFor(t, 2*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "s2 to see pA")

	b.close()
	waitFor(t, 2*time.Second, func() bool {
		s2.mu.Lock()
		defer s2.mu.Unlock()
		_, held := s2.rooms["room1"]
		return !held
	}, "s2's empty local room to be swept")

	if !hasPeer(s2, "room1", "pA") {
		t.Fatalf("live remote pA vanished from s2 after the local idle sweep: Peers = %v", s2.Peers("room1"))
	}
}

// countingFanout counts the lane publishes one replica makes.
type countingFanout struct {
	fanout.Fanout
	publishes atomic.Int64
}

func (c *countingFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	c.publishes.Add(1)
	return c.Fanout.Publish(ctx, topic, payload)
}

// TestStatusPublishesOnceOnLane: one inbound status frame is one lane
// message. The status mirror already carries the document and the
// heartbeat is the convergence backstop; a full-roster beat on top
// turned a client's permitted 128 frames/s into 2× the messages and
// ~9× the bytes on the cluster-wide lane.
func TestStatusPublishesOnceOnLane(t *testing.T) {
	inner := fanout.NewInProcess()
	counted := &countingFanout{Fanout: inner}
	s1, s2 := newTestSignaler(t, Config{}), newTestSignaler(t, Config{})
	s1.heartbeatEvery, s2.heartbeatEvery = time.Hour, time.Hour
	if _, err := s1.SetFanout(counted); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	if _, err := s2.SetFanout(inner); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	b1, b2 := startSignaler(t, s1), startSignaler(t, s2)

	a, _ := join(t, b1, "room1", "pA")
	defer a.close()
	b, _ := join(t, b2, "room1", "pB")
	defer b.close()
	waitFor(t, 2*time.Second, func() bool { return hasPeer(s2, "room1", "pA") && hasPeer(s1, "room1", "pB") }, "rosters to converge")
	a.expectEnv(t, "join")
	time.Sleep(100 * time.Millisecond)
	before := counted.publishes.Load()

	const frames = 10
	for i := range frames {
		a.send(map[string]any{"kind": "status", "data": map[string]int{"n": i}})
	}
	for range frames {
		b.expectEnv(t, "status")
	}
	time.Sleep(100 * time.Millisecond)

	if got := counted.publishes.Load() - before; got != frames {
		t.Fatalf("%d status frames became %d lane publishes, want %d", frames, got, frames)
	}
}

// TestSlowPeerClosedNotStarved: signaling frames are not recoverable
// from a snapshot (a dropped answer leaves the far side waiting for
// ever), so a peer whose send buffer overflows is closed, which makes
// it reconnect and re-hydrate, rather than silently losing frames
// while both sockets look healthy. The sender is unaffected.
func TestSlowPeerClosedNotStarved(t *testing.T) {
	s := newTestSignaler(t, Config{MaxFramesPerSecond: 100000})
	s.cfg.WS.SendBuffer = 4
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join")

	// pB reads nothing while pA floods it past the kernel's socket
	// buffers; every frame is under MaxSignalBytes.
	blob := strings.Repeat("c", 40<<10)
	for range 120 {
		a.send(map[string]any{"kind": "signal", "to": "pB", "type": "ice", "data": map[string]string{"candidate": blob}})
	}

	// Barrier: the flood has to be PROCESSED, not merely written, before
	// pB drains. pA's frames are read in order and published through the
	// room's one FIFO, so a signal sent after them to a third, reading
	// peer arrives only once every flood frame was queued for pB or pB
	// was closed for it. Without this the client's writes return as soon
	// as the kernel takes them (16 ms for 4.8 MB on Linux loopback), the
	// server is still decoding them (a strict decode walks the whole
	// 40 KiB document, ~0.5 ms a frame), and pB, draining at once, keeps
	// step with the server and never overflows: the flood did not happen,
	// which is not the property holding. Seen as a 7-in-10 CI failure on
	// 2026-09-07 when the inbound decode became strict.
	c, _ := join(t, base, "room1", "pC")
	defer c.close()
	a.send(map[string]any{"kind": "signal", "to": "pC", "type": "ice", "data": map[string]string{"candidate": "barrier"}})
	c.expectEnv(t, "signal")

	// Now pB drains. Its socket must end in a close frame or EOF, not
	// in a read timeout on a healthy socket that quietly lost frames.
	var err error
	for err == nil {
		_, err = b.recv(2 * time.Second)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("starved peer was left open after frames were dropped for it (drain ended in %v)", err)
	}
	if !errors.Is(err, errClosed) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("drain ended in %v, want a close frame or EOF", err)
	}
	if !a.alive(200 * time.Millisecond) {
		t.Fatal("the sender's socket was closed too")
	}
}

// TestTURNRefreshPushedPerPeer: with TURN configured the server pushes
// a fresh per-peer credential on the iceServers event every half TTL,
// so a connection that outlives the credential can still restart ICE
// (TURN allocations outlive expiry, new ones do not: the TURN REST
// draft, section 2). Each peer receives only its own username.
func TestTURNRefreshPushedPerPeer(t *testing.T) {
	const secret = "not-a-secret: test hmac key" // not-a-secret: test fixture
	s := newTestSignaler(t, Config{
		TURN: &TURN{URLs: []string{"turn:t.example:3478"}, Secret: secret, TTL: time.Hour},
	})
	s.turnRefreshEvery = 40 * time.Millisecond
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join")

	for _, tc := range []struct {
		c  *wsClient
		id string
	}{{a, "pA"}, {b, "pB"}} {
		env := tc.c.expectEnv(t, "iceServers")
		var p struct {
			ICEServers []ICEServer `json:"iceServers"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			t.Fatalf("%s: decode iceServers payload: %v", tc.id, err)
		}
		var turn *ICEServer
		for i := range p.ICEServers {
			if len(p.ICEServers[i].URLs) == 1 && p.ICEServers[i].URLs[0] == "turn:t.example:3478" {
				turn = &p.ICEServers[i]
			}
		}
		if turn == nil {
			t.Fatalf("%s: no TURN entry in refresh payload %s", tc.id, env.Payload)
		}
		if !strings.HasSuffix(turn.Username, ":"+tc.id) {
			t.Fatalf("%s: refreshed username %q is not this peer's", tc.id, turn.Username)
		}
		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write([]byte(turn.Username))
		if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); turn.Credential != want {
			t.Fatalf("%s: refreshed credential does not verify against the shared secret", tc.id)
		}
	}
}

// TestNoTURNNoRefresh: without TURN there is nothing to refresh and
// the event never fires.
func TestNoTURNNoRefresh(t *testing.T) {
	s := newTestSignaler(t, Config{ICEServers: []ICEServer{{URLs: []string{"stun:stun.example:3478"}}}})
	s.turnRefreshEvery = 20 * time.Millisecond
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	a.expectSilence(t, 150*time.Millisecond)
}

// TestAuthorizeRefusalReachesSocket: a refusal from Authorize keeps
// its HTTP status for a plain request, and on an upgrade request is
// delivered as close code 4000+status after the handshake, the only
// channel a browser can read it on. An *HTTPError names the status; a
// plain error is 403.
func TestAuthorizeRefusalReachesSocket(t *testing.T) {
	s := New(Config{Authorize: func(r *http.Request) (Join, error) {
		if r.URL.Query().Get("peer") == "gone" {
			return Join{}, &HTTPError{Status: http.StatusGone, Message: "session ended"}
		}
		return Join{}, errors.New("no")
	}})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	res, err := http.Get(srv.URL + "/ws/room1?peer=gone")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusGone {
		t.Fatalf("plain request: got %d, want 410", res.StatusCode)
	}

	for _, tc := range []struct {
		peer string
		code uint16
	}{{"gone", 4410}, {"other", 4403}} {
		c, ok := wsTryDial(srv.URL + "/ws/room1?peer=" + tc.peer)
		if !ok {
			t.Fatalf("%s: refused upgrade must be accepted and closed with a code", tc.peer)
		}
		_, rerr := c.recv(2 * time.Second)
		c.close()
		if !errors.Is(rerr, errClosed) || c.closeCode != tc.code {
			t.Fatalf("%s: err=%v code=%d, want close %d", tc.peer, rerr, c.closeCode, tc.code)
		}
	}
}
