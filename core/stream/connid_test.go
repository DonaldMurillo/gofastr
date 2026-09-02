package stream

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// upgradeOnce drives one real Upgrade over a raw TCP pair and returns
// the server-side connection. Modeled on the subprotocol tests in
// websocket_production_test.go: a real listener so Hijack works, a raw
// dialer that speaks the handshake.
func upgradeOnce(t *testing.T, cfg WSConfig) *WebSocketConn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type result struct {
		conn *WebSocketConn
		err  error
	}
	res := make(chan result, 1)
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, cfg)
		res <- result{conn, err}
	}))

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + ln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), "101 Switching Protocols") {
		t.Fatalf("expected 101, got: %s", buf[:n])
	}

	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("Upgrade: %v", r.err)
		}
		t.Cleanup(func() { _ = r.conn.Close() })
		return r.conn
	case <-time.After(2 * time.Second):
		t.Fatal("server never completed the upgrade")
		return nil
	}
}

// Every upgrade gets a distinct, non-empty connection id, so a client's
// reconnect (a new connection) can never be confused with the
// connection it replaced (#377).
func TestUpgradeConnectionIDDistinct(t *testing.T) {
	var mu sync.Mutex
	ids := make(map[string]int)

	for range 4 {
		conn := upgradeOnce(t, WSConfig{CheckOrigin: func(*http.Request) bool { return true }})
		id := conn.ConnectionID()
		if id == "" {
			t.Fatal("Upgrade minted an empty connection id")
		}
		mu.Lock()
		ids[id]++
		mu.Unlock()
	}
	if len(ids) != 4 {
		t.Fatalf("4 upgrades produced %d distinct ids: %v", len(ids), ids)
	}
}

// A caller-supplied id wins over the generated one: applications that
// mint their own ids (one per browser session) keep correlation.
func TestUpgradeConnectionIDFromConfig(t *testing.T) {
	conn := upgradeOnce(t, WSConfig{
		CheckOrigin:  func(*http.Request) bool { return true },
		ConnectionID: "sess-abc-gen1",
	})
	if got := conn.ConnectionID(); got != "sess-abc-gen1" {
		t.Fatalf("ConnectionID = %q, want the configured sess-abc-gen1", got)
	}
}
