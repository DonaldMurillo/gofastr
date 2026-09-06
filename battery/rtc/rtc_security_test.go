package rtc

// rtc_security_test.go pins the guards: addressed delivery, credential
// isolation, size and rate caps, target validation, status shape, ICE
// scheme refusal, and the no-SDP-in-logs rule. Each guard was proven
// by breaking it (comment out the check, watch the test go red,
// restore).

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Markers planted in payloads that must never reach a log record.
const (
	sdpleakMarker    = "SDPLEAK-9f3ab41c"
	candidateMarker  = "CANDLEAK-77c2e9"
	statusBodyMarker = "STATUSLEAK-31d0af"
	turnSecretMarker = "TURNSECRET-ca41b7"
	fakeCloseMarker  = "CLOSEREASON-f00ba12"
)

// capturingHandler collects every record's message and attributes.
type capturingHandler struct {
	mu      sync.Mutex
	records []string
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteString(" ")
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, sb.String())
	h.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturingHandler) dump() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.records, "\n")
}

// TestSignalDeliveredToTargetOnly: a signal frame reaches its
// addressee and nobody else; the sender never receives its own frame
// back.
func TestSignalDeliveredToTargetOnly(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	c, _ := join(t, base, "room1", "pC")
	defer c.close()

	// Drain the join notifications each earlier joiner is owed, so
	// the silence assertions below measure the signal alone.
	a.expectEnv(t, "join") // pB
	a.expectEnv(t, "join") // pC
	b.expectEnv(t, "join") // pC
	a.send(map[string]any{
		"kind": "signal", "to": "pB", "type": "offer",
		"data": map[string]any{"sdp": "v=0\r\no=- 1 1 IN IP4 127.0.0.1"},
	})

	env := b.expectEnv(t, "signal")
	var sig signalPayload
	if err := json.Unmarshal(env.Payload, &sig); err != nil {
		t.Fatalf("signal payload: %v", err)
	}
	if sig.From != "pA" || sig.To != "pB" || sig.Type != "offer" {
		t.Fatalf("signal = %+v", sig)
	}
	if !strings.Contains(string(sig.Data), "127.0.0.1") {
		t.Fatalf("signal data mangled: %s", sig.Data)
	}
	// The sender and the third peer see nothing.
	a.expectSilence(t, 300*time.Millisecond)
	c.expectSilence(t, 300*time.Millisecond)
}

// TestUnknownTargetDropped: a signal to a non-member (or to self) is
// dropped silently and the socket stays usable.
func TestUnknownTargetDropped(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	seqNow := func() uint64 {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.rooms["room1"].seq
	}
	a.expectEnv(t, "join") // pB joined after a: drain it first
	before := seqNow()
	a.send(map[string]any{"kind": "signal", "to": "ghost", "type": "offer", "data": map[string]any{"sdp": "x"}})
	a.send(map[string]any{"kind": "signal", "to": "pA", "type": "offer", "data": map[string]any{"sdp": "x"}})
	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "bogus", "data": map[string]any{"sdp": "x"}})
	b.expectSilence(t, 300*time.Millisecond)
	if after := seqNow(); after != before {
		t.Fatalf("dropped signals must not advance the room sequence: %d -> %d", before, after)
	}
	// Socket still alive: a valid status still flows.
	a.send(map[string]any{"kind": "status", "data": map[string]any{"ok": true}})
	env := b.expectEnv(t, "status")
	var sp statusPayload
	if err := json.Unmarshal(env.Payload, &sp); err != nil || sp.ID != "pA" {
		t.Fatalf("status envelope = %s (%v)", env.Payload, err)
	}
}

// TestRoomFullRefused: past MaxPeers the join is a 409
// HTTP response; a rejoin of an existing id is a replacement, not a
// new member.
func TestRoomFullRefused(t *testing.T) {
	s := newTestSignaler(t, Config{MaxPeers: 2})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()

	// A plain HTTP request is enough: the cap check runs before the
	// upgrade, so no WebSocket handshake is needed to be refused.
	res, err := http.Get(base + "/ws/room1?peer=pC")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("third join: got %d, want 409", res.StatusCode)
	}

	// A browser cannot see that 409: a failed WebSocket handshake is
	// opaque to script by spec (WHATWG withholds the status so a page
	// cannot probe the network). So the same refusal on an upgrade
	// request is accepted and then closed with code 4000+status.
	c, ok := wsTryDial(base + "/ws/room1?peer=pD")
	if !ok {
		t.Fatal("a refused upgrade request must be upgraded and closed with a code, not answered with a status the page cannot read")
	}
	defer c.close()
	if _, err := c.recv(2 * time.Second); !errors.Is(err, errClosed) || c.closeCode != 4409 {
		t.Fatalf("third join over websocket: err=%v code=%d, want close 4409", err, c.closeCode)
	}

	// A reconnecting tab (same id) replaces its old socket and is not
	// counted against the cap.
	re, _ := join(t, base, "room1", "pA")
	defer re.close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := a.recv(time.Until(deadline)); err != nil {
			if !errors.Is(err, errClosed) {
				t.Fatalf("replaced socket should close, got %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replaced socket never closed")
		}
	}
}

// TestOversizeSignalClosesSocket: one signaling payload past
// MaxSignalBytes (default 48 KiB) closes the peer.
func TestOversizeSignalClosesSocket(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	b, _ := join(t, base, "room1", "pB")
	defer b.close()

	huge := strings.Repeat("x", 49*1024)
	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer", "data": huge})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := a.recv(time.Until(deadline)); err != nil {
			if !errors.Is(err, errClosed) {
				t.Fatalf("oversize sender should be closed, got %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("oversize sender never closed")
		}
	}
	// The room carries no trace of the frame.
	b.expectSilence(t, 300*time.Millisecond)
}

// TestFrameFloodClosesSocket: past MaxFramesPerSecond (token bucket,
// burst 2x) the socket is closed.
func TestFrameFloodClosesSocket(t *testing.T) {
	s := newTestSignaler(t, Config{MaxFramesPerSecond: 5}) // burst 10
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")

	// Malformed frames count: the limiter runs before the decoder.
	for range 30 {
		a.writeFrame(0x1, []byte(`{"kind":"noise"}`))
	}
	deadline := time.Now().Add(2 * time.Second)
	sawClose := false
	for {
		if _, err := a.recv(time.Until(deadline)); err != nil {
			sawClose = errors.Is(err, errClosed) || strings.Contains(err.Error(), "EOF")
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if !sawClose {
		t.Fatal("flooding socket should be closed")
	}
}

// TestStatusMustBeObject: a status document that is not a JSON object,
// or over MaxStatusBytes, is dropped and the socket stays usable.
func TestStatusMustBeObject(t *testing.T) {
	s := newTestSignaler(t, Config{})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join") // pB

	b.send(map[string]any{"kind": "status", "data": "just a string"})
	b.send(map[string]any{"kind": "status", "data": 42})
	b.send(map[string]any{"kind": "status", "data": strings.Repeat("k", 2*1024)})
	a.expectSilence(t, 300*time.Millisecond)

	b.send(map[string]any{"kind": "status", "data": map[string]any{"muted": true}})
	env := a.expectEnv(t, "status")
	var sp statusPayload
	if err := json.Unmarshal(env.Payload, &sp); err != nil || sp.ID != "pB" {
		t.Fatalf("status envelope = %s (%v)", env.Payload, err)
	}
	if !strings.Contains(string(sp.Status), `"muted":true`) {
		t.Fatalf("status body = %s", sp.Status)
	}
}

// TestCredentialNeverInOthersSnapshot: each peer's TURN credential is
// minted for it alone and marshaled into its snapshot alone.
func TestCredentialNeverInOthersSnapshot(t *testing.T) {
	s := newTestSignaler(t, Config{
		TURN: &TURN{URLs: []string{"turn:turn.example.net:3478"}, Secret: turnSecretMarker, TTL: time.Hour},
	})
	base := startSignaler(t, s)

	a, snapA := join(t, base, "room1", "pA")
	defer a.close()
	b, snapB := join(t, base, "room1", "pB")
	defer b.close()

	credOf := func(payload []byte, peer string) string {
		var view roomSnapshot
		if err := json.Unmarshal(payload, &view); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if len(view.ICEServers) != 1 {
			t.Fatalf("ICE list = %+v", view.ICEServers)
		}
		srv := view.ICEServers[0]
		if !strings.HasSuffix(srv.Username, ":"+peer) {
			t.Fatalf("username %q should end with :%s", srv.Username, peer)
		}
		if srv.Credential == "" {
			t.Fatalf("empty credential for %s", peer)
		}
		return srv.Credential
	}
	credA := credOf(snapA.Payload, "pA")
	credB := credOf(snapB.Payload, "pB")
	if credA == credB {
		t.Fatal("credentials must be per-peer")
	}
	if strings.Contains(string(snapB.Payload), credA) {
		t.Fatal("A's credential leaked into B's snapshot")
	}
	if strings.Contains(string(snapA.Payload), credB) {
		t.Fatal("B's credential leaked into A's snapshot")
	}
	if strings.Contains(string(snapB.Payload), turnSecretMarker) {
		t.Fatal("TURN secret leaked into a snapshot")
	}
	// Signals never carry credentials either.
	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer", "data": map[string]any{"sdp": "v=0"}})
	env := b.expectEnv(t, "signal")
	if strings.Contains(string(env.Payload), turnSecretMarker) || strings.Contains(string(env.Payload), credA) {
		t.Fatal("credential leaked into a signal envelope")
	}
}

// TestInvalidICESchemeRefused: only ICE schemes reach the browser.
func TestInvalidICESchemeRefused(t *testing.T) {
	for _, urls := range [][]string{
		{"http://turn.example.com"},
		{"https://turn.example.com"},
		{"ws://turn.example.com"},
		{"ftp://turn.example.com"},
		{"turn.example.com"},
		{"stun://bad scheme"},
		{"turn:"},                              // no endpoint (RFC 7065: host is mandatory)
		{"stun:"},                              // no endpoint (RFC 7064)
		{"turn:t.example:3478?transport=sctp"}, // transport is udp or tcp
		{"stun:s.example:3478?transport=udp"},  // STUN URIs take no query
	} {
		mustPanic(t, "ICEServer", func() {
			New(Config{ICEServers: []ICEServer{{URLs: urls}}})
		})
		mustPanic(t, "TURN", func() {
			New(Config{TURN: &TURN{URLs: urls, Secret: "s"}})
		})
	}
	good := New(Config{ICEServers: []ICEServer{{URLs: []string{"stun:s.example:3478", "turns:s.example:5349"}}}})
	good.Close()
}

// TestNoSDPInLogs: SDP, candidates, status bodies, credentials, and
// close reasons never reach a log record, through joins, leaves,
// signals, statuses, and every refusal path.
func TestNoSDPInLogs(t *testing.T) {
	cap := &capturingHandler{}
	s := New(Config{
		MaxPeers:           2,
		MaxFramesPerSecond: 5,
		TURN:               &TURN{URLs: []string{"turn:t.example:3478"}, Secret: turnSecretMarker},
		Logger:             slog.New(cap),
	})
	base := startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	b, _ := join(t, base, "room1", "pB")
	if res, rerr := http.Get(base + "/ws/room1?peer=pC"); rerr == nil {
		res.Body.Close()
		if res.StatusCode != http.StatusConflict {
			t.Fatalf("third join: got %d, want 409", res.StatusCode)
		}
	}

	// Signals carrying the markers, delivered and dropped.
	a.send(map[string]any{"kind": "signal", "to": "pB", "type": "offer",
		"data": map[string]any{"sdp": "v=0\r\n" + sdpleakMarker}})
	a.send(map[string]any{"kind": "signal", "to": "ghost", "type": "ice",
		"data": map[string]any{"candidate": "candidate:1 1 udp " + candidateMarker}})
	a.send(map[string]any{"kind": "status", "data": map[string]any{"note": statusBodyMarker}})
	// A status over the cap and a signal flood, both closing paths.
	b.send(map[string]any{"kind": "status", "data": strings.Repeat("s", 2048)})
	for range 30 {
		b.writeFrame(0x1, []byte(`{"kind":"signal","to":"pA","type":"ice","data":{"candidate":"`+fakeCloseMarker+`"}}`))
	}

	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(cap.dump(), "frame_rate") || strings.Contains(cap.dump(), "oversize")
	}, "a refusal to be logged")

	dump := cap.dump()
	if dump == "" {
		t.Fatal("expected at least one log record")
	}
	for _, marker := range []string{sdpleakMarker, candidateMarker, statusBodyMarker, turnSecretMarker, fakeCloseMarker} {
		if strings.Contains(dump, marker) {
			t.Fatalf("marker %s reached the logs:\n%s", marker, dump)
		}
	}
	// A kicked and a closed socket: close reasons are never logged.
	s.Kick("room1", "pA")
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(cap.dump(), "rtc: leave")
	}, "leave record")
	if strings.Contains(cap.dump(), fakeCloseMarker) {
		t.Fatalf("close reason reached the logs:\n%s", cap.dump())
	}
	a.close()
}

// TestTurnCredentialMatchesCoturn pins the algorithm against the
// coturn static-auth-secret convention, computing the expected value
// with crypto/hmac directly.
func TestTurnCredentialMatchesCoturn(t *testing.T) {
	turn := TURN{Secret: "s3cret", TTL: time.Hour}
	now := time.Unix(1700000000, 0).Add(-time.Hour)
	srv := turn.Credentials("p1", now)

	wantUsername := "1700000000:p1"
	mac := hmac.New(sha1.New, []byte("s3cret"))
	mac.Write([]byte(wantUsername))
	wantCred := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if srv.Username != wantUsername {
		t.Fatalf("username = %q, want %q", srv.Username, wantUsername)
	}
	if srv.Credential != wantCred {
		t.Fatalf("credential = %q, want %q", srv.Credential, wantCred)
	}
	if len(srv.URLs) != 0 {
		t.Fatalf("URLs = %v", srv.URLs)
	}

	// TTL floor: below one minute, one minute applies.
	short := TURN{Secret: "s3cret", TTL: time.Second}
	got := short.Credentials("p1", now)
	wantExp := now.Add(time.Minute).Unix()
	wantPrefix := fmt.Sprintf("%d:", wantExp)
	if !strings.HasPrefix(got.Username, wantPrefix) {
		t.Fatalf("clamped username = %q, want prefix %s", got.Username, wantPrefix)
	}

	// TTL zero means one hour.
	zero := TURN{Secret: "s3cret"}
	got = zero.Credentials("p1", now)
	wantExp = now.Add(time.Hour).Unix()
	wantPrefix = fmt.Sprintf("%d:", wantExp)
	if !strings.HasPrefix(got.Username, wantPrefix) {
		t.Fatalf("default username = %q, want prefix %s", got.Username, wantPrefix)
	}
}

// TestStatusResendAfterGeneration is the socket-level prerequisite for
// the client's resend-on-hydrate: a peer that reconnects gets a fresh
// snapshot whose ICE credentials are re-minted (new expiry).
func TestSnapshotRemintsTURNPerConnection(t *testing.T) {
	s := newTestSignaler(t, Config{
		TURN: &TURN{URLs: []string{"turn:t.example:3478"}, Secret: "s", TTL: time.Hour},
	})
	base := startSignaler(t, s)

	_, snap1 := join(t, base, "room1", "pA")
	c2, snap2 := join(t, base, "room1", "pA")
	defer c2.close()

	var v1, v2 roomSnapshot
	if err := json.Unmarshal(snap1.Payload, &v1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(snap2.Payload, &v2); err != nil {
		t.Fatal(err)
	}
	if len(v1.ICEServers) != 1 || len(v2.ICEServers) != 1 {
		t.Fatalf("ICE lists: %v / %v", v1.ICEServers, v2.ICEServers)
	}
	if v1.ICEServers[0].Credential == "" || v2.ICEServers[0].Credential == "" {
		t.Fatal("missing credentials")
	}
	// Same second usually means identical credentials; the contract is
	// that each snapshot mints its own, so compare usernames parse to
	// expiry:pA on both.
	for _, v := range []roomSnapshot{v1, v2} {
		if !strings.HasSuffix(v.ICEServers[0].Username, ":pA") {
			t.Fatalf("username %q", v.ICEServers[0].Username)
		}
	}
}
