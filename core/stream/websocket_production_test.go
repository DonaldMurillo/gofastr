package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pipeConn pairs an io.ReadWriteCloser using net.Pipe so we get real
// SetReadDeadline / SetWriteDeadline semantics. Used to drive the
// close handshake and idle timeout tests.
func newPipePair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	return a, b
}

// ----- (a) Close handshake -----

// After our side sends Close, the read pump should wait briefly for the
// peer's Close before tearing down the TCP conn.
func TestWSCloseHandshakeWaitsPeer(t *testing.T) {
	srv, cli := newPipePair(t)

	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		config:     WSConfig{WriteTimeout: time.Second, CloseTimeout: 500 * time.Millisecond},
	}
	go conn.writePump()

	// Peer reads our Close frame, then responds with its own.
	gotClose := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := cli.Read(buf)
		gotClose <- buf[:n]
		// Send a masked Close frame back.
		mask := [4]byte{0x01, 0x02, 0x03, 0x04}
		frame := []byte{0x80 | wsopcodeClose, 0x80 | 0}
		frame = append(frame, mask[:]...)
		cli.Write(frame)
	}()

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case frame := <-gotClose:
		// First byte should be FIN|Close.
		if len(frame) < 2 || frame[0]&0x0F != wsopcodeClose {
			t.Fatalf("peer did not receive close frame, got %x", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("peer never received close frame")
	}
}

// If the peer never responds, Close still returns within CloseTimeout
// instead of hanging forever.
func TestWSCloseHandshakeTimesOut(t *testing.T) {
	srv, cli := newPipePair(t)
	// Peer drains writes so the Close frame goes out, but never replies.
	go io.Copy(io.Discard, cli)

	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		config:     WSConfig{WriteTimeout: time.Second, CloseTimeout: 100 * time.Millisecond},
	}
	go conn.writePump()

	start := time.Now()
	conn.Close()
	elapsed := time.Since(start)
	if elapsed > 600*time.Millisecond {
		t.Fatalf("Close took %v, expected <600ms (CloseTimeout=100ms)", elapsed)
	}
}

// Adversarial: the previous implementation scanned raw bytes from the
// peer and matched on `b & 0x0F == 0x8`. ANY byte with low nibble 0x8
// (0x08, 0x18, 0x28, 0x38, 0x48, …) — extremely common in binary
// payloads, mask bytes, length octets — falsely matched as Close. The
// fix must parse frames, not byte-grep the stream.
func TestWSCloseHandshakeIgnoresFalsePositiveBytes(t *testing.T) {
	srv, cli := newPipePair(t)
	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{WriteTimeout: time.Second, CloseTimeout: 250 * time.Millisecond},
	}
	go conn.writePump()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Drain our outbound Close frame.
		buf := make([]byte, 16)
		_, _ = cli.Read(buf)
		// Stream garbage bytes containing 0x18, 0x28, 0x38 — every one of
		// these has low nibble 0x8 but is NOT a Close frame opcode byte.
		// The old impl would short-circuit on the first one.
		_, _ = cli.Write([]byte{0x18, 0x28, 0x38, 0x48, 0x88, 0x98})
		// Then close the underlying pipe WITHOUT sending a real Close.
		_ = cli.Close()
	}()

	start := time.Now()
	conn.Close()
	elapsed := time.Since(start)
	<-done

	// We expect Close to wait roughly CloseTimeout (peer never sent a
	// real Close), NOT to return immediately on a payload byte.
	if elapsed < 200*time.Millisecond {
		t.Fatalf("Close returned in %v — byte-grep falsely treated a payload byte as Close; should wait for the real frame or CloseTimeout", elapsed)
	}
}

// ----- (b) Read idle timeout -----

// Configure short idle/pong; assert connection closes when peer never
// responds to ping.
func TestWSIdlePingPongCloses(t *testing.T) {
	srv, cli := newPipePair(t)

	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		config: WSConfig{
			ReadLimit:       1 << 20,
			ReadIdleTimeout: 100 * time.Millisecond,
			PongTimeout:     150 * time.Millisecond,
			WriteTimeout:    time.Second,
		},
	}
	go conn.writePump()
	conn.startKeepalive()

	// Drain whatever the keepalive sends so writePump doesn't stall.
	go func() {
		io.Copy(io.Discard, cli)
	}()

	select {
	case <-conn.Closed():
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("idle ping/pong did not close conn within 2s")
	}
}

// If the peer responds to ping with pong, the connection stays open.
func TestWSPongKeepsAlive(t *testing.T) {
	srv, cli := newPipePair(t)

	conn := &WebSocketConn{
		conn:       srv,
		sendBuffer: make(chan []byte, 4),
		closed:     make(chan struct{}),
		config: WSConfig{
			ReadLimit:       1 << 20,
			ReadIdleTimeout: 80 * time.Millisecond,
			PongTimeout:     200 * time.Millisecond,
			WriteTimeout:    time.Second,
		},
	}
	go conn.writePump()
	conn.startKeepalive()
	// Background reader so pong frames get consumed and reset the
	// awaiting-pong flag.
	go func() {
		for {
			_, err := conn.Read()
			if err != nil {
				return
			}
		}
	}()

	// Peer reads pings and writes back pongs continuously for ~400ms.
	stop := make(chan struct{})
	go func() {
		buf := make([]byte, 32)
		for {
			select {
			case <-stop:
				return
			default:
			}
			cli.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, err := cli.Read(buf)
			if err != nil {
				continue
			}
			// If we see a ping, send a pong.
			if n >= 2 && buf[0]&0x0F == wsopcodePing {
				mask := [4]byte{0xa, 0xb, 0xc, 0xd}
				resp := []byte{0x80 | wsopcodePong, 0x80 | 0}
				resp = append(resp, mask[:]...)
				cli.Write(resp)
			}
		}
	}()

	select {
	case <-conn.Closed():
		close(stop)
		t.Fatal("conn closed despite pongs being received")
	case <-time.After(400 * time.Millisecond):
		close(stop)
		conn.Close()
	}
}

// ----- (c) Protocol fuzz -----

// Adversarial frames must each result in a clean error rather than panic.
func TestWSProtocolFuzz(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
	}{
		{
			name:  "incomplete-header",
			frame: []byte{0x81}, // one byte only
		},
		{
			name: "oversized-64bit-length",
			frame: func() []byte {
				out := []byte{0x81, 0x80 | 127}
				ln := make([]byte, 8)
				binary.BigEndian.PutUint64(ln, 0xFFFFFFFFFFFFFFFF)
				out = append(out, ln...)
				out = append(out, 0, 0, 0, 0)
				return out
			}(),
		},
		{
			name: "fin0-control-close",
			frame: func() []byte {
				mask := []byte{1, 2, 3, 4}
				// FIN=0, opcode=Close — control frames MUST be FIN=1
				return append([]byte{0x00 | wsopcodeClose, 0x80 | 0}, mask...)
			}(),
		},
		{
			name: "rsv-bits-set",
			frame: func() []byte {
				mask := []byte{1, 2, 3, 4}
				// RSV1/2/3 = 1, opcode = text — must be rejected
				return append([]byte{0x80 | 0x70 | wsopcodeText, 0x80 | 0}, mask...)
			}(),
		},
		{
			name: "truncated-payload",
			frame: func() []byte {
				mask := []byte{1, 2, 3, 4}
				// claims 5 bytes but provides 2
				out := []byte{0x81, 0x80 | 5}
				out = append(out, mask...)
				out = append(out, 'h', 'i')
				return out
			}(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			conn := &WebSocketConn{
				conn:       &nopConn{r: bytes.NewReader(tc.frame), w: io.Discard},
				sendBuffer: make(chan []byte, 4),
				closed:     make(chan struct{}),
				config:     WSConfig{ReadLimit: 1 << 20, requireMask: true},
			}
			_, err := conn.readFrame()
			if err == nil {
				t.Fatalf("expected error for %s frame", tc.name)
			}
		})
	}
}

// ----- (d) Subprotocol negotiation -----

// Client offers "foo,bar"; server is configured to support "bar".
// Upgrade must echo "bar".
func TestWSSubprotocolEcho(t *testing.T) {
	// Use a real net.Listener so we can hijack.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var serverErr atomic.Value
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := Upgrade(w, r, WSConfig{
				Subprotocols: []string{"bar"},
				CheckOrigin:  func(*http.Request) bool { return true },
			})
			if err != nil {
				serverErr.Store(err.Error())
			}
		}))
	}()

	// Dial raw and craft the upgrade request.
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
		"Sec-WebSocket-Protocol: foo, bar\r\n" +
		"\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	resp := string(buf[:n])
	if !strings.Contains(resp, "101 Switching Protocols") {
		t.Fatalf("expected 101, got: %s", resp)
	}
	if !strings.Contains(strings.ToLower(resp), "sec-websocket-protocol: bar") {
		t.Fatalf("expected echoed subprotocol bar, got headers:\n%s", resp)
	}
}

// When the client offers no subprotocols, no Sec-WebSocket-Protocol header
// is echoed.
func TestWSNoSubprotocolNoHeader(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Upgrade(w, r, WSConfig{
			Subprotocols: []string{"bar"},
			CheckOrigin:  func(*http.Request) bool { return true },
		})
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
	c.Write([]byte(req))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	resp := strings.ToLower(string(buf[:n]))
	if strings.Contains(resp, "sec-websocket-protocol") {
		t.Fatalf("did not expect subprotocol header, got:\n%s", string(buf[:n]))
	}
}

// When the client offers subprotocols but none match, no header is echoed.
func TestWSSubprotocolNoMatch(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()

	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Upgrade(w, r, WSConfig{
			Subprotocols: []string{"only-supported"},
			CheckOrigin:  func(*http.Request) bool { return true },
		})
	}))

	c, _ := net.Dial("tcp", ln.Addr().String())
	defer c.Close()
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + ln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: nope1, nope2\r\n" +
		"\r\n"
	c.Write([]byte(req))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := c.Read(buf)
	resp := strings.ToLower(string(buf[:n]))
	if strings.Contains(resp, "sec-websocket-protocol") {
		t.Fatalf("expected no subprotocol echo on no-match, got:\n%s", string(buf[:n]))
	}
}

// httptest with hijack-incapable recorder is still useful for asserting
// negotiation logic that runs *before* hijack.
func TestWSSubprotocolPickFirst(t *testing.T) {
	// Use httptest just to verify pickSubprotocol selects deterministically.
	r := httptest.NewRequest("GET", "http://x/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "alpha, beta, gamma")
	got := pickSubprotocol(r, []string{"gamma", "beta"})
	if got != "gamma" {
		t.Fatalf("pickSubprotocol = %q, want gamma (server preference order)", got)
	}
}

var _ = sync.Mutex{}
