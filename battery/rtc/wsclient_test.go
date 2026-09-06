package rtc

// wsclient_test.go is a minimal RFC 6455 client for driving the
// signaling server end to end without a browser: handshake, masked
// client frames, server frame reads, close detection. Modeled on
// core/stream's connid_test.go raw dialer; kept in-package because
// core/stream's own client helpers are unexported.

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// wireEnvelope is the StateChannel wire shape the tests decode.
type wireEnvelope struct {
	Type     string          `json:"type"`
	Sequence uint64          `json:"sequence"`
	Payload  json.RawMessage `json:"payload"`
}

// errClosed is returned when the server sends a close frame.
var errClosed = errors.New("ws client: server closed the connection")

type wsClient struct {
	closeCode uint16 // status code of the last close frame read, 0 if none
	t         *testing.T
	conn      net.Conn
	br        *bufio.Reader

	mu sync.Mutex // serializes frame writes
}

// wsDial performs a real WebSocket handshake against url (host/path).
func wsDial(t *testing.T, url string) *wsClient {
	t.Helper()
	var conn net.Conn
	var path string
	if host, p, ok := strings.Cut(url, "/ws/"); ok {
		var err error
		conn, err = net.Dial("tcp", strings.TrimPrefix(host, "http://"))
		if err != nil {
			t.Fatalf("ws dial %s: %v", url, err)
		}
		path = "/ws/" + p
	} else {
		t.Fatalf("ws dial: url %s must contain /ws/", url)
	}
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		t.Fatalf("ws dial: key: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + strings.TrimPrefix(strings.Split(url, "/ws/")[0], "http://") + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("ws dial: write: %v", err)
	}
	c := &wsClient{t: t, conn: conn, br: bufio.NewReader(conn)}
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := c.br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("ws dial: expected 101, got %q err %v", status, err)
	}
	for {
		line, err := c.br.ReadString('\n')
		if err != nil {
			t.Fatalf("ws dial: headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	c.conn.SetReadDeadline(time.Time{})
	return c
}

// writeFrame writes one masked client frame (clients must mask).
func (c *wsClient) writeFrame(opcode byte, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		c.t.Fatalf("ws write: mask: %v", err)
	}
	header := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(0x80|n))
	case n <= 0xffff:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(append(header, 0x80|127), ext[:]...)
	}
	header = append(header, mask[:]...)
	masked := make([]byte, n)
	for i := range n {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		c.t.Fatalf("ws write: %v", err)
	}
}

// send marshals v and writes it as one text frame.
func (c *wsClient) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		c.t.Fatalf("ws send: %v", err)
	}
	c.writeFrame(0x1, data)
}

// recv reads one message. Returns errClosed on a server close frame,
// and a net error (timeout/EOF) when the connection dies without one.
func (c *wsClient) recv(timeout time.Duration) ([]byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
			return nil, err
		}
		opcode := hdr[0] & 0x0f
		masked := hdr[1]&0x80 != 0
		length := int(hdr[1] & 0x7f)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint64(ext[:]))
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.br, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x1, 0x2:
			return payload, nil
		case 0x8:
			if len(payload) >= 2 {
				c.closeCode = binary.BigEndian.Uint16(payload[:2])
			}
			return nil, errClosed
		case 0x9: // ping: answer pong and keep reading
			c.writeFrame(0xA, payload)
		}
	}
}

// recvEnv reads and decodes one envelope.
func (c *wsClient) recvEnv(timeout time.Duration) (wireEnvelope, error) {
	var env wireEnvelope
	data, err := c.recv(timeout)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("ws recv: %w (%s)", err, data)
	}
	return env, nil
}

// expectEnv reads the next envelope and requires its type.
func (c *wsClient) expectEnv(t *testing.T, typ string) wireEnvelope {
	t.Helper()
	env, err := c.recvEnv(5 * time.Second)
	if err != nil {
		t.Fatalf("expected %s envelope: %v", typ, err)
	}
	if env.Type != typ {
		t.Fatalf("expected %s envelope, got %s (%s)", typ, env.Type, env.Payload)
	}
	return env
}

// expectSilence asserts no envelope arrives within d on a socket that
// stays open: a close frame or a dead connection is not silence, it is
// a socket that could not have spoken.
func (c *wsClient) expectSilence(t *testing.T, d time.Duration) {
	t.Helper()
	env, err := c.recvEnv(d)
	if err == nil {
		t.Fatalf("expected silence, got %s envelope (%s)", env.Type, env.Payload)
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("expected silence on an open socket, but the socket died: %v", err)
	}
}

// close tears the TCP connection down. A close frame is not needed:
// the server's read loop sees the disconnect either way, and writing
// a frame on an already-dead conn would turn a cleanup into a fatal.
func (c *wsClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.Close()
}

// serveJoin is the Serve wrapper every test uses: room from the path,
// identity from the query string (the host's stand-in). In production
// these fields come from the host's own auth; Serve trusts what it is
// handed.
func serveJoin(s *Signaler, w http.ResponseWriter, r *http.Request) {
	room, _ := strings.CutPrefix(r.URL.Path, "/ws/")
	q := r.URL.Query()
	s.Serve(w, r, Join{
		Room:        room,
		PeerID:      q.Get("peer"),
		Role:        q.Get("role"),
		DisplayName: q.Get("name"),
	})
}

// startSignaler runs one Signaler behind an httptest server routing
// /ws/{room} to Serve. Returns the base URL. Closes the Signaler on
// cleanup.
func startSignaler(t *testing.T, s *Signaler) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveJoin(s, w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)
	return srv.URL
}

// newTestSignaler builds a Signaler with an Authorize stub so the
// ServeHTTP path is exercisable.
func newTestSignaler(t *testing.T, cfg Config) *Signaler {
	t.Helper()
	if cfg.Authorize == nil {
		cfg.Authorize = func(r *http.Request) (Join, error) {
			return Join{Room: "room1"}, nil
		}
	}
	return New(cfg)
}

// join dials /ws/{room}, consumes the snapshot, and returns the
// client plus the snapshot envelope.
func join(t *testing.T, base, room, peer string) (*wsClient, wireEnvelope) {
	t.Helper()
	c := wsDial(t, base+"/ws/"+room+"?peer="+peer)
	return c, c.expectEnv(t, "snapshot")
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
