package ws

import (
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
