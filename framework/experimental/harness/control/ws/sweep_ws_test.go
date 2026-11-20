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

func TestWSUpgradeRefusesUnboundSession(t *testing.T) {
	urlStr, _, tok, cleanup := setupServer(t)
	defer cleanup()
	host := strings.TrimPrefix(urlStr, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := fmt.Sprintf("GET /?session=%s&token=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", ids.NewSessionID(), tok, host)
	_, _ = conn.Write([]byte(req))
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	if !strings.HasPrefix(string(buf[:n]), "HTTP/1.1 403") {
		t.Fatalf("upgrade to a session the token is not bound to: got %q, want 403", string(buf[:n]))
	}
}

func TestWSIdleSocketClosesOnRevocation(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	mux := multiplex.New()
	mux.RegisterEngine(engine.NewEngine(session, bus, fakeProvider{}, "fake", engine.NewDispatcher(bus, tool.NewRegistry())))
	secret, _ := auth.GenerateSecret()
	enc := auth.NewEncoder(secret)
	rl := auth.NewRevocationList()
	jti := ids.NewJTI()
	tok, _ := enc.Encode(auth.Claims{JTI: jti, Sessions: []ids.SessionID{session}, IdentityClass: control.IdentityHuman})
	srv := httptest.NewServer(&Handler{Mux: mux, Encoder: enc, Revocations: rl, recheck: 20 * time.Millisecond})
	defer srv.Close()
	conn := dialWS(t, strings.TrimPrefix(srv.URL, "http://"), session, tok)
	defer conn.Close()
	rl.Revoke(jti)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	for {
		if _, err := conn.Read(buf); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("an idle socket outlived its revoked token")
			}
			return
		}
	}
}
