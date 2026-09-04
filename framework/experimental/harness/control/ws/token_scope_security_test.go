package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Property: a capability token's Commands claim gates every command frame
// after the WS upgrade, the same claim rest.go enforces per route. ws.go
// binds the session at connect (AllowsSession) but Conn carries no claims
// into handleText, which dispatches any decoded command via mux.Dispatch.
// A SendInput-only token must therefore be refused when it sends a
// CancelTurn frame.

// dialWS performs the handshake with the given token and returns the raw
// TCP conn once the server answered 101.
func dialWS(t *testing.T, host string, session ids.SessionID, tok string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	req := fmt.Sprintf("GET /?session=%s&token=%s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n",
		session, tok, host,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("handshake read: %v", err)
	}
	if !strings.HasPrefix(string(buf[:n]), "HTTP/1.1 101") {
		t.Fatalf("handshake refused: %q", string(buf[:n]))
	}
	return conn
}

// sendCommandFrame wraps cmd in the wire frame envelope and sends it masked.
func sendCommandFrame(t *testing.T, conn net.Conn, cmd control.Command) {
	t.Helper()
	body, err := control.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	f, err := json.Marshal(struct {
		Frame string          `json:"frame"`
		Body  json.RawMessage `json:"body"`
	}{Frame: "command", Body: body})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if _, err := conn.Write(maskedFrame(f)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// readRefusalFrame reads inbound bytes until a control.Error-shaped frame
// appears or a bounded window elapses. Returns whatever arrived.
func readRefusalFrame(t *testing.T, conn net.Conn) string {
	t.Helper()
	var all []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			all = append(all, buf[:n]...)
			s := string(all)
			if strings.Contains(s, `"reason"`) {
				return s
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			break
		}
	}
	return string(all)
}

// wsRefusal reports whether the payload looks like a dispatch refusal for
// the forbidden command kind.
func wsRefusal(payload string) bool {
	if payload == "" {
		return false
	}
	if !strings.Contains(payload, `"frame"`) {
		return false
	}
	for _, marker := range []string{"InvalidCommand", "not permitted", "forbidden", "scope", "token", "CancelTurn"} {
		if strings.Contains(payload, marker) {
			return true
		}
	}
	return false
}

func TestWSTokenCommandScopeEnforced(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)
	mux := multiplex.New()
	mux.RegisterEngine(eng)
	t.Cleanup(func() { bus.Close() })

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	enc := auth.NewEncoder(secret)
	claimsBase := auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		Sessions:      []ids.SessionID{session},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	}
	inputOnly := claimsBase
	inputOnly.JTI = ids.NewJTI()
	inputOnly.Commands = []string{"SendInput"}
	narrowTok, err := enc.Encode(inputOnly)
	if err != nil {
		t.Fatalf("encode narrow token: %v", err)
	}

	h := &Handler{Mux: mux, Encoder: enc, Revocations: auth.NewRevocationList()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	// Happy guard: the SAME narrow token's allowed command (SendInput)
	// still runs end-to-end on this connection — the refusal below is
	// command-kind-specific, not a blanket rejection.
	conn1 := dialWS(t, host, session, narrowTok)
	defer conn1.Close()
	sendCommandFrame(t, conn1, control.SendInput{
		SessionID: session,
		Content:   engine.SimpleInput("hello"),
	})
	if got := readUntilTextDelta(t, conn1); !strings.Contains(got, "ws-hello") {
		t.Fatalf("happy path: SendInput under a SendInput-only token must still stream the turn (got %q)", got)
	}

	// Attack: the same token sends CancelTurn, a kind its Commands omit.
	conn2 := dialWS(t, host, session, narrowTok)
	defer conn2.Close()
	sendCommandFrame(t, conn2, control.CancelTurn{SessionID: session})
	got := readRefusalFrame(t, conn2)
	if !wsRefusal(got) {
		t.Fatalf("SECURITY: [ws-cmd-scope] a Commands=[SendInput] token dispatched CancelTurn over the "+
			"WebSocket transport and no refusal frame came back (received %q). The upgrade path checks "+
			"AllowsSession but Conn carries no claims into handleText, which dispatches any decoded "+
			"command; rest.go enforces AllowsCommand for the same verbs on every command route.",
			got)
	}
}

// TestWSTokenSessionScopeOnCommandBody pins the session side of the
// token-scope property the test above pins on the command side: the
// Sessions claim must gate every dispatched command's TARGET session,
// not just the URL the socket upgraded on.
//
// The upgrade path checks claims.AllowsSession(sessParam) once, but
// Conn.handleText then dispatches whatever SessionID the command BODY
// carries: handleSendInput/handleCancel/handleAnswer all look up
// m.engines[cmd.SessionID] without comparing it to c.session. rest.go
// does not have this hole — it derives c.SessionID = sessID from the
// URL path (overriding any body value) after a per-route
// AllowsSession check, and its comments claim ws.go "mirrors" that.
// It doesn't: a token minted for session A (a web sidecar's token, a
// spawned agent's token) can drive a whole other session's engine —
// inject input, cancel turns, answer that session's permission
// prompts — by typing B into the body of the frame.
func TestWSTokenSessionScopeOnCommandBody(t *testing.T) {
	sessionA := ids.NewSessionID()
	sessionB := ids.NewSessionID()
	busA := engine.NewBus(sessionA)
	busB := engine.NewBus(sessionB)
	t.Cleanup(func() { busA.Close(); busB.Close() })

	mux := multiplex.New()
	for _, setup := range []struct {
		sess ids.SessionID
		bus  *engine.Bus
	}{
		{sessionA, busA},
		{sessionB, busB},
	} {
		reg := tool.NewRegistry()
		d := engine.NewDispatcher(setup.bus, reg)
		mux.RegisterEngine(engine.NewEngine(setup.sess, setup.bus, fakeProvider{}, "fake", d))
	}

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	enc := auth.NewEncoder(secret)
	tok, err := enc.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		Sessions:      []ids.SessionID{sessionA}, // bound to A only
		Commands:      []string{"SendInput", "CancelTurn", "AnswerPermission"},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	h := &Handler{Mux: mux, Encoder: enc, Revocations: auth.NewRevocationList()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	// In-process observer of session B's bus: the harm is a turn
	// starting on B, and the attacker socket is subscribed to A's bus
	// so the attack itself is silent on the wire.
	obsCtx, obsCancel := context.WithCancel(context.Background())
	t.Cleanup(obsCancel)
	eventsB := busB.Subscribe(obsCtx)

	// Attach legally through session A.
	conn := dialWS(t, host, sessionA, tok)
	defer conn.Close()

	// Attack: command body names session B.
	sendCommandFrame(t, conn, control.SendInput{
		SessionID: sessionB,
		Content:   engine.SimpleInput("pwned-via-body-session"),
	})

	// The refusal must come back on the socket…
	if got := readRefusalFrame(t, conn); !wsRefusal(got) {
		t.Errorf("SECURITY: [ws-session-scope] no refusal for a cross-session SendInput body (got %q)", got)
	}
	// …and session B's engine must never have started a turn.
	select {
	case env, ok := <-eventsB:
		if ok {
			t.Errorf("SECURITY: [ws-session-scope] a token bound to session A drove session B: bus B saw %q", env.Payload)
		}
	case <-time.After(300 * time.Millisecond):
		// Clean: nothing arrived on B.
	}
}

// setupRevocableServer is setupServer with the RevocationList and the
// token's JTI exposed so a test can revoke after the 101.
func setupRevocableServer(t *testing.T) (url string, sess ids.SessionID, tok string, jti ids.JTI, rl *auth.RevocationList, cleanup func()) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)
	mux := multiplex.New()
	mux.RegisterEngine(eng)

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	enc := auth.NewEncoder(secret)
	rl = auth.NewRevocationList()
	jti = ids.NewJTI()
	tok, err = enc.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           jti,
		Sessions:      []ids.SessionID{session},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	h := &Handler{Mux: mux, Encoder: enc, Revocations: rl}
	srv := httptest.NewServer(h)
	return srv.URL, session, tok, jti, rl, func() { srv.Close(); bus.Close() }
}

// TestWsSocketStopsOnRevocation: a connection authenticated by a
// credential that is later revoked stops executing commands —
// revocation must reach established sockets, not just new handshakes
// (handleText re-verifies every command frame; revocationWatch closes
// the socket on its ticker).
func TestWsSocketStopsOnRevocation(t *testing.T) {
	// Happy guard on its own stack: the same server + token dispatches a
	// command frame when the jti is NOT revoked, so the refusal demanded
	// below can only come from the revocation, not plumbing.
	u1, sess1, tok1, _, _, cleanup1 := setupRevocableServer(t)
	defer cleanup1()
	conn1 := dialWS(t, strings.TrimPrefix(u1, "http://"), sess1, tok1)
	defer conn1.Close()
	sendCommandFrame(t, conn1, control.SendInput{
		SessionID: sess1,
		Content:   engine.SimpleInput("hello"),
	})
	if got := readUntilTextDelta(t, conn1); !strings.Contains(got, "ws-hello") {
		t.Fatalf("happy path: well-formed frame must stream the turn (got %.200q)", got)
	}

	// Attack: socket upgraded (101 answered), THEN the jti is revoked,
	// THEN a command frame arrives on the established socket.
	u2, sess2, tok2, jti2, rl2, cleanup2 := setupRevocableServer(t)
	defer cleanup2()
	conn2 := dialWS(t, strings.TrimPrefix(u2, "http://"), sess2, tok2)
	defer conn2.Close()
	rl2.Revoke(jti2)
	sendCommandFrame(t, conn2, control.SendInput{
		SessionID: sess2,
		Content:   engine.SimpleInput("after-revoke"),
	})
	got := readAllFrames(t, conn2, 1500*time.Millisecond)
	if strings.Contains(got, "ws-hello") {
		t.Errorf("SECURITY: [ws-stale-revocation] a command frame dispatched on a socket whose "+
			"token jti was revoked after the 101 (received the full turn %.200q) — auth.Verify must "+
			"re-run per inbound frame and on a periodic watch, closing the socket on refusal", got)
	}
}
