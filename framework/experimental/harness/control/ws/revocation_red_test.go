//go:build red

package ws

import (
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

// ---------------------------------------------------------------------------
// Property: a connection authenticated by a credential that is later revoked
// stops executing commands — revocation must reach established sockets, not
// just new handshakes.
// Surfaces: ws.go Handler.ServeHTTP / Conn.handleText — the harness control
// plane's operator WebSocket (loopback bearer-token transport).
// Finding: auth.Verify(h.Encoder, h.Revocations, tok) runs ONLY in
// ServeHTTP before the 101 (ws.go:89); the verified claims are then frozen
// into Conn (ws.go:127-136) and every later frame rides that snapshot:
// handleText re-checks AllowsCommand against the frozen claims (ws.go:286)
// but never re-checks the RevocationList. RevocationList.Revoke(jti)
// therefore terminates nothing: an operator socket upgraded before the
// revoke keeps dispatching SendInput/CancelTurn/... frames for the life of
// the TCP connection. There is also no server-initiated ping/keepalive or
// max lifetime that would ever cycle the socket back through Verify (the
// hand-rolled RFC6455 layer has no ReadIdleTimeout equivalent).
// Severity: loopback operator tool (dev harness surface; token TTL bounds
// exposure and the REST twin re-verifies every request — the held WS socket
// is the asymmetric surface; revocation's whole point on this plane is to
// cut off a compromised/stale operator session immediately).
// Fix direction: make revocation observable to live sockets — re-Verify
// (or check Revocations.IsRevoked on the captured jti) per inbound frame or
// on a periodic ping, closing the socket on failure; alternatively have
// Revoke terminate sockets carrying that jti.
// ---------------------------------------------------------------------------

// setupRevocableServer is setupServer with the RevocationList and the
// token's JTI exposed so the test can revoke after the 101.
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

func TestHarnessWsRedRevokedMidSocket(t *testing.T) {
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

	// Attack: socket upgraded (101 answered), THEN the jti is revoked, THEN
	// a command frame arrives on the established socket.
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
			"token jti was revoked after the 101 (received the full turn %.200q). auth.Verify runs only "+
			"in the handshake (ws.go:89); the claims frozen into Conn drive per-frame AllowsCommand "+
			"(ws.go:286) but the RevocationList is never re-checked, so Revoke(jti) terminates nothing "+
			"and the socket has no keepalive or lifetime bound that would ever re-Verify. Re-verify per "+
			"frame or on ping, or have Revoke close sockets carrying the jti.", got)
	}
}
