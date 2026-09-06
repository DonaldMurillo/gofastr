package rtc

// rtc_test.go covers the single-replica behaviour: join/snapshot/
// leave, replacement, the idle-room TTL, roster ordering, and the
// config/edge surfaces (New validation, Mount, ServeHTTP, Kick,
// Close, PermissionsPolicy). The wire-level security guards live in
// rtc_security_test.go; the fanout lane in rtc_fanout_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestSnapshotShape pins the hydration payload: room, self, the other
// members, and the ICE list; the connecting peer is not repeated in
// peers, and the sequence space starts clean.
func TestSnapshotShape(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, snapA := join(t, base, "room1", "pA")
	defer a.close()
	var view roomSnapshot
	if err := json.Unmarshal(snapA.Payload, &view); err != nil {
		t.Fatalf("snapshot payload: %v", err)
	}
	if view.Room != "room1" || view.Self.ID != "pA" || len(view.Peers) != 0 {
		t.Fatalf("snapshot = %+v", view)
	}
	if len(view.ICEServers) != 0 {
		t.Fatalf("static ICE list should be empty, got %v", view.ICEServers)
	}

	b, snapB := join(t, base, "room1", "pB")
	defer b.close()
	var viewB roomSnapshot
	if err := json.Unmarshal(snapB.Payload, &viewB); err != nil {
		t.Fatalf("snapshot B payload: %v", err)
	}
	if len(viewB.Peers) != 1 || viewB.Peers[0].ID != "pA" {
		t.Fatalf("B's snapshot should list pA only, got %+v", viewB.Peers)
	}
	if viewB.Self.ID != "pB" {
		t.Fatalf("B's self = %+v", viewB.Self)
	}
	// A saw the join.
	je := a.expectEnv(t, "join")
	var joined PeerInfo
	if err := json.Unmarshal(je.Payload, &joined); err != nil || joined.ID != "pB" {
		t.Fatalf("join envelope = %s (%v)", je.Payload, err)
	}
	if je.Sequence <= snapA.Sequence {
		t.Fatalf("join sequence %d must exceed snapshot %d", je.Sequence, snapA.Sequence)
	}
}

// TestLeaveEventAndRosters: the socket closes, the room sees leave,
// and Peers/Rooms reflect membership.
func TestLeaveEventAndRosters(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	b, _ := join(t, base, "room1", "pB")
	// B's snapshot already contains pA, so B's next envelope is A's
	// leave; no join event for pA is re-delivered to B.

	waitFor(t, 2*time.Second, func() bool { return len(s.Peers("room1")) == 2 }, "both peers")
	if got := s.Rooms(); len(got) != 1 || got[0] != "room1" {
		t.Fatalf("Rooms = %v", got)
	}
	a.close()
	le := b.expectEnv(t, "leave")
	var lp leavePayload
	if err := json.Unmarshal(le.Payload, &lp); err != nil || lp.ID != "pA" {
		t.Fatalf("leave envelope = %s (%v)", le.Payload, err)
	}
	waitFor(t, 2*time.Second, func() bool { return len(s.Peers("room1")) == 1 }, "peer removed")
	if got := s.Peers("room1")[0].ID; got != "pB" {
		t.Fatalf("remaining peer = %s", got)
	}
	if got := s.Peers("nope"); len(got) != 0 {
		t.Fatalf("unknown room Peers = %v", got)
	}
}

// TestReplacedPeerSeesLeaveThenJoin pins the reconnecting-tab rule: a
// join with an existing id closes the OLD socket, and the room sees
// leave then join, in that order.
func TestReplacedPeerSeesLeaveThenJoin(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	old, _ := join(t, base, "room1", "p1")
	watcher, _ := join(t, base, "room1", "pW")
	defer watcher.close()
	// The watcher's snapshot contains p1; the next envelopes it sees
	// are the replacement's leave and join.

	nu, _ := join(t, base, "room1", "p1")
	defer nu.close()

	le := watcher.expectEnv(t, "leave")
	var lp leavePayload
	if err := json.Unmarshal(le.Payload, &lp); err != nil || lp.ID != "p1" {
		t.Fatalf("leave envelope = %s (%v)", le.Payload, err)
	}
	je := watcher.expectEnv(t, "join")
	var jp PeerInfo
	if err := json.Unmarshal(je.Payload, &jp); err != nil || jp.ID != "p1" {
		t.Fatalf("join envelope = %s (%v)", je.Payload, err)
	}
	if je.Sequence <= le.Sequence {
		t.Fatalf("join (%d) must follow leave (%d)", je.Sequence, le.Sequence)
	}
	// The old socket is closed by the replacement. It may first
	// receive envelopes already in flight (e.g. the replacement's
	// join), so drain until the close arrives.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := old.recv(time.Until(deadline)); err != nil {
			if !errors.Is(err, errClosed) {
				t.Fatalf("old socket should close, got %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old socket never closed")
		}
	}
	// Two peers total: the replacement did not double-register.
	waitFor(t, 2*time.Second, func() bool { return len(s.Peers("room1")) == 2 }, "replacement merged")
}

// TestEmptyRoomDroppedAfterTTL: an empty room's state is dropped after
// RoomIdleTTL (lazily, on access); a busy room is not.
func TestEmptyRoomDroppedAfterTTL(t *testing.T) {
	s := newTestSignaler(t, Config{RoomIdleTTL: 60 * time.Millisecond})
	base := startSignaler(t, s)

	a, _ := join(t, base, "gone", "pA")
	busy, _ := join(t, base, "busy", "pB")
	defer busy.close()

	waitFor(t, 2*time.Second, func() bool { return len(s.Rooms()) == 2 }, "both rooms")
	a.close()
	time.Sleep(150 * time.Millisecond)

	// The sweep runs on access: Rooms() itself must drop the empty
	// room and keep the busy one.
	got := s.Rooms()
	if len(got) != 1 || got[0] != "busy" {
		t.Fatalf("Rooms after TTL = %v, want [busy]", got)
	}
	if peers := s.Peers("gone"); len(peers) != 0 {
		t.Fatalf("dropped room Peers = %v", peers)
	}
}

// TestPeersSortedByOrderThenID pins the roster order the wire also
// uses for politeness.
func TestPeersSortedByOrderThenID(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "zz")
	defer a.close()
	b, _ := join(t, base, "room1", "aa")
	defer b.close()

	waitFor(t, 2*time.Second, func() bool { return len(s.Peers("room1")) == 2 }, "both peers")
	got := s.Peers("room1")
	if got[0].ID != "zz" || got[1].ID != "aa" {
		t.Fatalf("join order should sort first: %v then %v", got[0].ID, got[1].ID)
	}
	if got[0].Order > got[1].Order {
		t.Fatalf("order values out of join order")
	}

	// Same order, different ids: the id breaks the tie.
	s.mu.Lock()
	s.rooms["room1"].peers["zz"].info.Order = 123
	s.rooms["room1"].peers["aa"].info.Order = 123
	s.mu.Unlock()
	got = s.Peers("room1")
	if got[0].ID != "aa" || got[1].ID != "zz" {
		t.Fatalf("tie should break on id: %v, %v", got[0].ID, got[1].ID)
	}
}

// TestMintedPeerIDIsHex: without Join.PeerID the server mints one.
func TestMintedPeerIDIsHex(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)
	c, snap := join(t, base, "room1", "")
	defer c.close()
	var view roomSnapshot
	if err := json.Unmarshal(snap.Payload, &view); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(view.Self.ID) != 32 {
		t.Fatalf("minted id = %q, want 32 hex chars", view.Self.ID)
	}
	for _, r := range view.Self.ID {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("minted id %q is not hex", view.Self.ID)
		}
	}
}

// TestJoinValidationRefused: an invalid Join is a 400 before the
// upgrade.
func TestJoinValidationRefused(t *testing.T) {
	s := newTestSignaler(t, Config{})
	startSignaler(t, s)

	bad := []Join{
		{Room: ""},
		{Room: "has space"},
		{Room: strings.Repeat("r", 129)},
		{Room: "room", PeerID: "bad id"},
		{Room: "room", PeerID: strings.Repeat("p", 65)},
		{Room: "room", Role: strings.Repeat("x", 33)},
		{Room: "room", DisplayName: "bad\x01name"},
		{Room: "room", DisplayName: strings.Repeat("n", 65)},
		{Room: "room", User: strings.Repeat("u", 129)},
	}
	for _, j := range bad {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ws/room1", nil)
		s.Serve(w, req, j)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Join %+v: got %d, want 400", j, w.Code)
		}
	}
}

// mustPanic runs fn and requires a panic whose message carries the
// "rtc:" prefix (the battery's construction-error contract, matching
// relay.New).
func mustPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("%s: want panic, got none", what)
		}
		msg, ok := r.(string)
		if !ok || !strings.HasPrefix(msg, "rtc:") {
			t.Fatalf("%s: panic = %#v, want an \"rtc:\"-prefixed message", what, r)
		}
	}()
	fn()
}

// TestNewPanicsOnBadConfig: bad paths, ICE entries, and negative
// limits are refused at New with an "rtc:"-prefixed panic, not at
// first use.
func TestNewPanicsOnBadConfig(t *testing.T) {
	badPaths := []string{"rel", "/", "/tail/", "/a/b/..", "/p%20th", "/a//b", "/a?b"}
	for _, p := range badPaths {
		mustPanic(t, "path "+p, func() { New(Config{Path: p}) })
	}
	mustPanic(t, "http ICE URL", func() {
		New(Config{ICEServers: []ICEServer{{URLs: []string{"http://turn.example.com"}}}})
	})
	mustPanic(t, "mixed ICE URLs", func() {
		New(Config{ICEServers: []ICEServer{{URLs: []string{"turn:example.com", "ftp://x"}}}})
	})
	mustPanic(t, "TURN without URLs", func() {
		New(Config{TURN: &TURN{Secret: "s"}})
	})
	mustPanic(t, "TURN URL without a scheme", func() {
		New(Config{TURN: &TURN{URLs: []string{"h:1"}, Secret: "s"}})
	})
	mustPanic(t, "negative MaxPeers", func() {
		New(Config{TURN: &TURN{URLs: []string{"turn:h:1"}, Secret: "s"}, MaxPeers: -1})
	})
	// A payload cap the socket read limit cannot carry is dead config:
	// the frame is cut off before the cap is ever consulted.
	mustPanic(t, "MaxSignalBytes above the read limit", func() {
		New(Config{MaxSignalBytes: 100 * 1024})
	})
	mustPanic(t, "MaxStatusBytes above the read limit", func() {
		New(Config{MaxStatusBytes: 70 * 1024})
	})
	mustPanic(t, "MaxSignalBytes above a tightened read limit", func() {
		New(Config{MaxSignalBytes: 20 * 1024, WS: stream.WSConfig{ReadLimit: 16 * 1024}})
	})
	good := New(Config{
		ICEServers: []ICEServer{{URLs: []string{"stun:stun.example.net:3478", "stuns:h:5349"}}},
		TURN:       &TURN{URLs: []string{"turn:h:3478?transport=udp", "turns:h:5349"}, Secret: "s"},
		Path:       "/rtc",
	})
	good.Close()
}

func TestMountPanicsWithoutAuthorize(t *testing.T) {
	s := New(Config{})
	defer s.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Mount without Authorize should panic")
			}
		}()
		s.Mount(router.New())
	}()

	// With Authorize, Mount registers the endpoint on the router and
	// ServeHTTP consults it.
	authed := New(Config{Authorize: func(r *http.Request) (Join, error) {
		return Join{}, errors.New("no")
	}})
	defer authed.Close()
	rt := router.New()
	authed.Mount(rt)
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest(http.MethodGet, DefaultPath, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("mounted route: got %d, want 403 from Authorize", w.Code)
	}
	notFound := httptest.NewRecorder()
	rt.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("unmounted path should 404, got %d", notFound.Code)
	}
}

// TestServeHTTPAuthorizeRefuses: a plain error is 403; an *HTTPError
// carries its own status and message; success flows into Serve.
func TestServeHTTPAuthorizeRefuses(t *testing.T) {
	gone := newTestSignaler(t, Config{Authorize: func(r *http.Request) (Join, error) {
		return Join{}, &HTTPError{Status: http.StatusGone, Message: "session ended"}
	}})
	w := httptest.NewRecorder()
	gone.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusGone || w.Body.String() != "session ended\n" {
		t.Fatalf("HTTPError refusal: got %d %q", w.Code, w.Body.String())
	}

	plain := newTestSignaler(t, Config{Authorize: func(r *http.Request) (Join, error) {
		return Join{}, errors.New("no")
	}})
	w = httptest.NewRecorder()
	plain.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain refusal: got %d, want 403", w.Code)
	}

	// Authorized path reaches Serve; an invalid Join surfaces as 400.
	joiner := newTestSignaler(t, Config{Authorize: func(r *http.Request) (Join, error) {
		return Join{Room: r.URL.Query().Get("room")}, nil
	}})
	w = httptest.NewRecorder()
	joiner.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x?room=", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("authorized but invalid join: got %d, want 400", w.Code)
	}
}

// TestServeHTTPPanicsWithoutAuthorize is the direct-call spelling of
// the Mount rule.
func TestServeHTTPPanicsWithoutAuthorize(t *testing.T) {
	s := New(Config{})
	defer s.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("ServeHTTP without Authorize should panic")
			}
		}()
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}()
}

// TestInitMountsAndStopsWithApp pins the plugin lifecycle relay-style:
// Init (via InitPlugins, no Start) registers the route on the app's
// router where a refusing Authorize answers, and the app's stop hooks
// close the Signaler: sockets drop, Rooms empties, later joins 503.
func TestInitMountsAndStopsWithApp(t *testing.T) {
	s := New(Config{
		Authorize: func(r *http.Request) (Join, error) {
			return Join{}, &HTTPError{Status: http.StatusUnauthorized, Message: "no session"}
		},
	})
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(s)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	// The mounted route answers with the Authorize refusal.
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, DefaultPath, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("route after Init: got %d, want 401 from refusing Authorize", w.Code)
	}

	// A live local peer gives the shutdown half something to observe.
	// Serve trusts the join it is handed; the app's Authorize gates
	// the mounted route, not direct Serve calls.
	base := startSignaler(t, s)
	c, _ := join(t, base, "room1", "pA")
	defer c.close()
	waitFor(t, 2*time.Second, func() bool { return len(s.Rooms()) == 1 }, "room registered")

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if rooms := s.Rooms(); len(rooms) != 0 {
		t.Fatalf("Rooms after app Shutdown = %v, want empty", rooms)
	}
	if _, err := c.recv(2 * time.Second); !errors.Is(err, errClosed) {
		t.Fatalf("socket should close on app Shutdown, got %v", err)
	}
	w2 := httptest.NewRecorder()
	s.Serve(w2, httptest.NewRequest(http.MethodGet, "/ws/room1", nil), Join{Room: "room1"})
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("Serve after app Shutdown: got %d, want 503", w2.Code)
	}
}

// TestInitRefusesNilAuthorize: Init reports a missing gate as an
// error (the runtime spelling of Mount's panic) instead of panicking
// through plugin wiring.
func TestInitRefusesNilAuthorize(t *testing.T) {
	s := New(Config{})
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(s)
	if err := app.InitPlugins(); err == nil {
		t.Fatal("Init without Authorize should return an error")
	}
}

// TestKickClosesPeer: a revocation closes the local socket and the
// room sees the leave; an unknown id reports false.
func TestKickClosesPeer(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	b, _ := join(t, base, "room1", "pB")
	defer b.close()

	if s.Kick("room1", "ghost") {
		t.Fatal("kick of unknown peer should report false")
	}
	if !s.Kick("room1", "pA") {
		t.Fatal("kick of live peer should report true")
	}
	// The kicked peer may have envelopes in flight (e.g. pB's join);
	// drain until the close arrives.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := a.recv(time.Until(deadline)); err != nil {
			if !errors.Is(err, errClosed) {
				t.Fatalf("kicked socket should close, got %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kicked socket never closed")
		}
	}
	le := b.expectEnv(t, "leave")
	var lp leavePayload
	if err := json.Unmarshal(le.Payload, &lp); err != nil || lp.ID != "pA" {
		t.Fatalf("leave envelope = %s (%v)", le.Payload, err)
	}
}

// TestCloseClosesSocketsAndRefusesJoins; Close is idempotent.
func TestCloseClosesSocketsAndRefusesJoins(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	s.Close()
	s.Close() // second call must not panic
	if _, err := a.recv(2 * time.Second); !errors.Is(err, errClosed) {
		t.Fatalf("socket should close on Close, got %v", err)
	}
	w := httptest.NewRecorder()
	s.Serve(w, httptest.NewRequest(http.MethodGet, "/ws/room1", nil), Join{Room: "room1"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("join after Close: got %d, want 503", w.Code)
	}
}

// TestPermissionsPolicyOpensSelf pins the exact header values.
func TestPermissionsPolicyOpensSelf(t *testing.T) {
	cases := []struct {
		features []Feature
		want     string
	}{
		// Every feature the helper knows is closed unless named: an
		// omitted directive is ALLOWED on the app's own origin (the
		// default allowlist for camera, microphone, and
		// display-capture is self), so leaving one out would open it.
		{nil, "geolocation=(), microphone=(), camera=(), display-capture=()"},
		{[]Feature{Camera}, "geolocation=(), microphone=(), camera=(self), display-capture=()"},
		{[]Feature{Camera, Microphone}, "geolocation=(), microphone=(self), camera=(self), display-capture=()"},
		{[]Feature{Microphone}, "geolocation=(), microphone=(self), camera=(), display-capture=()"},
		{[]Feature{Camera, Microphone, DisplayCapture}, "geolocation=(), microphone=(self), camera=(self), display-capture=(self)"},
		{[]Feature{DisplayCapture}, "geolocation=(), microphone=(), camera=(), display-capture=(self)"},
		{[]Feature{Camera, Camera}, "geolocation=(), microphone=(), camera=(self), display-capture=()"},
	}
	for _, tc := range cases {
		if got := PermissionsPolicy(tc.features...); got != tc.want {
			t.Fatalf("PermissionsPolicy(%v) = %q, want %q", tc.features, got, tc.want)
		}
	}
}
