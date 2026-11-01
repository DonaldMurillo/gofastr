package ws

import (
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

// resumeServer is setupServer that also hands back the bus.
func resumeServer(t *testing.T) (string, ids.SessionID, string, *engine.Bus) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake",
		engine.NewDispatcher(bus, tool.NewRegistry()))
	mux := multiplex.New()
	mux.RegisterEngine(eng)
	secret, _ := auth.GenerateSecret()
	enc := auth.NewEncoder(secret)
	tok, _ := enc.Encode(auth.Claims{
		Sessions:      []ids.SessionID{session},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	srv := httptest.NewServer(&Handler{Mux: mux, Encoder: enc, Revocations: auth.NewRevocationList()})
	t.Cleanup(func() { srv.Close(); bus.Close() })
	return strings.TrimPrefix(srv.URL, "http://"), session, tok, bus
}

// dialResume sends the handshake with extra query and returns the conn.
func dialResume(t *testing.T, host string, session ids.SessionID, tok, extra string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	req := fmt.Sprintf("GET /?session=%s&token=%s%s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n",
		session, tok, extra, host)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	return conn
}

// A reconnect with lastEventId gets the events it missed.
func TestWSReplaysAfterLastEventID(t *testing.T) {
	host, session, tok, bus := resumeServer(t)
	for _, txt := range []string{"evt-one", "evt-two", "evt-three"} {
		if _, err := bus.Publish(control.TextDelta{Text: txt}, ""); err != nil {
			t.Fatal(err)
		}
	}
	conn := dialResume(t, host, session, tok, "&lastEventId=1")
	got := readAllFrames(t, conn, 1500*time.Millisecond)
	if !strings.HasPrefix(got, "HTTP/1.1 101") {
		t.Fatalf("handshake failed: %q", got)
	}
	two, three := strings.Index(got, "evt-two"), strings.Index(got, "evt-three")
	if two < 0 || three < 0 || two > three {
		t.Fatalf("missed events not replayed in order: %q", got)
	}
	if strings.Contains(got, "evt-one") {
		t.Fatalf("event 1 replayed past lastEventId: %q", got)
	}
}

// A malformed lastEventId is refused before the upgrade.
func TestWSRefusesBadLastEventID(t *testing.T) {
	host, session, tok, _ := resumeServer(t)
	conn := dialResume(t, host, session, tok, "&lastEventId=-1")
	got := readAllFrames(t, conn, 500*time.Millisecond)
	if !strings.HasPrefix(got, "HTTP/1.1 400") {
		t.Fatalf("got %q, want 400", got)
	}
}
