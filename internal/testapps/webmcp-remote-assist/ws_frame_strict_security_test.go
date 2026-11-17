package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pins: the assist socket's inbound frames refuse ambiguous JSON — a
// frame carrying duplicate or case-folded keys is dropped (socket stays
// usable), because stdlib json resolves such keys last-wins and the
// server would otherwise run a branch (media report, signal relay) any
// first-read intermediary parsed differently.
//
// Pinned sibling: battery/rtc's inbound signaling frame (room.go Serve
// read loop) refuses ambiguous JSON the same way — the rtc twin of this
// socket — and the app's own convention marks every decode site that is
// deliberately lenient with a gofastr:allow marker stating why
// (main.go exempts the size-capped assist tool-command body).
//
// Surfaces: session.go::handleWS read loop — handler.UnmarshalStrict of
// each frame into inboundMsg, the existing `continue` drop arm. Before
// the fix this was a plain json.Unmarshal: "Kind" case-folded onto the
// `kind` tag and the later occurrence silently re-selected the dispatch
// branch — {"kind":"signal","Kind":"media"} ran the MEDIA branch while
// the wire visibly said "signal" first.

// strictWS is a minimal RFC 6455 client for the assist sockets (modeled
// on battery/rtc's wsclient_test.go raw dialer; kept here because
// core/stream's client helpers are unexported).
type strictWS struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
	mu   sync.Mutex // serializes frame writes
}

// strictDialWS performs the handshake against srv with the role cookie.
func strictDialWS(t *testing.T, srv *httptest.Server, path string, cookie string) *strictWS {
	t.Helper()
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("setup broken: dial %s: %v", addr, err)
	}
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		conn.Close()
		t.Fatalf("setup broken: ws key: %v", err)
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Origin: http://" + addr + "\r\n" +
		"Cookie: " + cookie + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(keyBytes[:]) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("setup broken: write handshake: %v", err)
	}
	c := &strictWS{t: t, conn: conn, br: bufio.NewReader(conn)}
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := c.br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		conn.Close()
		t.Fatalf("setup broken: expected 101 on %s, got %q err %v", path, status, err)
	}
	for {
		line, err := c.br.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatalf("setup broken: handshake headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	c.conn.SetReadDeadline(time.Time{})
	return c
}

// sendText writes one masked text frame (clients must mask).
func (c *strictWS) sendText(payload string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		c.t.Fatalf("setup broken: mask: %v", err)
	}
	header := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(0x80|n))
	case n <= 0xffff:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		c.t.Fatalf("setup broken: test frame too long (%d)", n)
	}
	header = append(header, mask[:]...)
	masked := make([]byte, n)
	for i := range n {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		c.t.Fatalf("setup broken: frame write: %v", err)
	}
}

// recvText reads one message (answering pings). Returns nil on timeout —
// the "socket said nothing within d" answer.
func (c *strictWS) recvText(d time.Duration) []byte {
	c.conn.SetReadDeadline(time.Now().Add(d))
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
			return nil
		}
		opcode := hdr[0] & 0x0f
		length := int(hdr[1] & 0x7f)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil
			}
			length = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil
			}
			length = int(binary.BigEndian.Uint64(ext[:]))
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return nil
		}
		switch opcode {
		case 0x1, 0x2:
			return payload
		case 0x8:
			return nil
		case 0x9:
			c.sendText("") // pong; server discards
		}
	}
}

// strictDrainWS discards everything still in flight until the socket goes
// quiet for d, so later observations cannot misattribute connect traffic.
func strictDrainWS(c *strictWS, d time.Duration) {
	for {
		if c.recvText(d) == nil {
			return
		}
	}
}

// TestWSFrameRefusesFoldedKeys: the support socket sends a frame whose
// kind is decided by a case-folded key pair. It must be dropped — no
// media event, no sequence burn, socket usable.
func TestWSFrameRefusesFoldedKeys(t *testing.T) {
	srv, a := newTestApp(t)
	s := a.createSession()
	supportCookie := supportLogin(t, srv)
	operatorCookie := joinAs(t, srv, s.joinToken)

	supWS := strictDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", supportCookie.Name, supportCookie.Value))
	defer supWS.conn.Close()
	strictDrainWS(supWS, 400*time.Millisecond)

	opWS := strictDialWS(t, srv, "/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", operatorCookie.Name, operatorCookie.Value))
	defer opWS.conn.Close()
	strictDrainWS(supWS, 400*time.Millisecond)
	strictDrainWS(opWS, 400*time.Millisecond)

	src := sessionSource{app: a, id: s.id}
	_, seqBefore := src.SnapshotFor(roleSupport)

	// The folded frame: wire-visible kind is "signal", the case-folded
	// "Kind" silently re-selects the media branch under last-wins decode.
	supWS.sendText(`{"kind":"signal","Kind":"media","to":"peer"}`)

	gotSup := supWS.recvText(800 * time.Millisecond)
	gotOp := opWS.recvText(200 * time.Millisecond)
	_, seqAfter := src.SnapshotFor(roleSupport)
	mediaRan := seqAfter != seqBefore ||
		(gotSup != nil && strings.Contains(string(gotSup), `"kind":"media"`)) ||
		(gotOp != nil && strings.Contains(string(gotOp), `"kind":"media"`))
	if mediaRan {
		t.Errorf("SECURITY: [remoteassist-ws-lenient] the folded frame {\"kind\":\"signal\",\"Kind\":\"media\"} executed (seq %d -> %d, support envelope %s, operator envelope %s): a lenient decode case-folds \"Kind\" onto the kind tag and the last occurrence wins, so the media branch ran under a frame any first-read intermediary read as a signal — the rtc twin of this socket refuses ambiguous frames; drop them and keep the socket open",
			seqBefore, seqAfter, envOrDash(gotSup), envOrDash(gotOp))
	}

	// GREEN-guard + socket-usable proof on the SAME socket: a well-formed
	// signal must still relay to the operator peer.
	supWS.sendText(`{"kind":"signal","to":"operator","type":"ice","data":{"k":"v"}}`)
	env := opWS.recvText(2 * time.Second)
	if env == nil {
		t.Fatalf("happy path: the well-formed signal was not relayed after the folded frame — the socket or the relay died (the fix must drop the ambiguous frame, not the connection)")
	}
	var relayed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env, &relayed); err != nil || relayed.Type != "signal" {
		t.Fatalf("happy path: operator expected a signal envelope, got %s (%v)", env, err)
	}
}

func envOrDash(b []byte) string {
	if b == nil {
		return "-"
	}
	if len(b) > 120 {
		return string(b[:120]) + "…"
	}
	return string(b)
}
