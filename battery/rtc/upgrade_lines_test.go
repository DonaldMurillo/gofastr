package rtc

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRefusalReadsEveryUpgradeLine: refuse told an upgrade request from
// a plain one by Header.Get("Upgrade"), the first field line only, so a
// client listing "h2c" then "websocket" on separate lines (RFC 9110
// §5.3) got the HTTP 403 a browser cannot read instead of the
// upgrade-then-close-4403 its runtime classes as refused.
func TestRefusalReadsEveryUpgradeLine(t *testing.T) {
	s := New(Config{Authorize: func(*http.Request) (Join, error) { return Join{}, errors.New("no") }})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET /ws/room1?peer=p HTTP/1.1\r\n" +
		"Host: " + strings.TrimPrefix(srv.URL, "http://") + "\r\n" +
		"Upgrade: h2c\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, " 101 ") {
		t.Fatalf("status line %q: a refused upgrade must be accepted and closed with a code", strings.TrimSpace(status))
	}
}
