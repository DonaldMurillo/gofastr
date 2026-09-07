//go:build red

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

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T2, example-grade). Tests-only; no fix applied.
//
// Pinned sibling that makes this the contract: battery/rtc's inbound
// signaling frame (room.go Serve read loop) is being pinned to refuse
// ambiguous JSON this same round — the rtc twin of this socket — and the
// app's own convention marks every decode site that is deliberately
// lenient with a gofastr:allow marker stating why (main.go:474 exempts the
// size-capped assist tool-command body). The websocket read loop carries
// no marker: it is an unmarked lenient member of the strict-decode family,
// not a decided exception.
//
// Property: the example's inbound websocket frames refuse ambiguous JSON —
// a frame carrying duplicate or case-folded keys must be dropped (socket
// stays usable), because stdlib json resolves such keys last-wins and the
// server would run a branch (media report, signal relay) any first-read
// intermediary parsed differently.
//
// Surfaces: session.go::handleWS read loop :744-747 — plain json.Unmarshal
// of each frame into inboundMsg; "Kind" case-folds onto the `kind` tag and
// the later occurrence silently re-selects the dispatch branch.
//
// Finding (verified today, legs below): the support socket sending
// {"kind":"signal","Kind":"media","to":"peer"} runs the MEDIA branch —
// setMedia publishes a "media" event and burns a sequence number — while
// the wire visibly says "signal" first. The frame is executed under a
// decode no two readers agree on.
//
// Fix direction: decode each frame with the strict-key walk (the rtc twin
// / handler.UnmarshalStrict posture) and drop the frame on ambiguity (the
// existing `continue` arm), keeping the socket open for well-formed frames.

// redWS is a minimal RFC 6455 client for the assist sockets (modeled on
// battery/rtc's wsclient_test.go raw dialer; kept here with red- prefixes
// because core/stream's client helpers are unexported).
type redWS struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
	mu   sync.Mutex // serializes frame writes
}

// redDialWS performs the handshake against srv with the role cookie.
func redDialWS(t *testing.T, srv *httptest.Server, path string, cookie string) *redWS {
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
	c := &redWS{t: t, conn: conn, br: bufio.NewReader(conn)}
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
func (c *redWS) sendText(payload string) {
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
func (c *redWS) recvText(d time.Duration) []byte {
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

// redDrainWS discards everything still in flight until the socket goes
// quiet for d, so later observations cannot misattribute connect traffic.
func redDrainWS(c *redWS, d time.Duration) {
	for {
		if c.recvText(d) == nil {
			return
		}
	}
}

// TestWSFrameRedRefusesFoldedKeys: the support socket sends a frame whose
// kind is decided by a case-folded key pair. It must be dropped — no media
// event, no sequence burn, socket usable. Today the media branch runs.
func TestWSFrameRedRefusesFoldedKeys(t *testing.T) {
	srv, a := newTestApp(t)
	s := a.createSession()
	supportCookie := supportLogin(t, srv)
	operatorCookie := joinAs(t, srv, s.joinToken)

	supWS := redDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", supportCookie.Name, supportCookie.Value))
	defer supWS.conn.Close()
	redDrainWS(supWS, 400*time.Millisecond)

	opWS := redDialWS(t, srv, "/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", operatorCookie.Name, operatorCookie.Value))
	defer opWS.conn.Close()
	redDrainWS(supWS, 400*time.Millisecond)
	redDrainWS(opWS, 400*time.Millisecond)

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
		t.Errorf("SECURITY: [remoteassist-ws-lenient] the folded frame {\"kind\":\"signal\",\"Kind\":\"media\"} executed (seq %d -> %d, support envelope %s, operator envelope %s): json.Unmarshal case-folds \"Kind\" onto the kind tag and the last occurrence wins, so the media branch ran under a frame any first-read intermediary read as a signal — the rtc twin of this socket refuses ambiguous frames; drop them and keep the socket open",
			seqBefore, seqAfter, redEnvOrDash(gotSup), redEnvOrDash(gotOp))
	}

	// GREEN-guard + socket-usable proof on the SAME socket: a well-formed
	// signal must still relay to the operator peer.
	supWS.sendText(`{"kind":"signal","to":"operator","type":"ice","data":{"k":"v"}}`)
	env := opWS.recvText(2 * time.Second)
	if env == nil {
		t.Fatalf("happy path: the well-formed signal was not relayed after the folded frame — the socket or the relay died (fix must drop the ambiguous frame, not the connection)")
	}
	var relayed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env, &relayed); err != nil || relayed.Type != "signal" {
		t.Fatalf("happy path: operator expected a signal envelope, got %s (%v)", env, err)
	}
}

func redEnvOrDash(b []byte) string {
	if b == nil {
		return "-"
	}
	if len(b) > 120 {
		return string(b[:120]) + "…"
	}
	return string(b)
}
