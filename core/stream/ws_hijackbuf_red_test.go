//go:build red

package stream

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: websocket data-channel integrity — every frame the client sends
// after a valid handshake reaches Read(). Bytes pipelined/coalesced with the
// handshake in the same TCP write are buffered by net/http in the request's
// bufio.Reader; Upgrade must drain bufrw.Reader before handing the pumps the
// raw conn.
// Surfaces: core/stream/websocket.go::Upgrade :217-225 (the hijacked
// *bufio.ReadWriter is only Flushed; its Reader — holding whatever net/http
// read past the handshake — is never drained) and :280-289 (wsc.conn = the raw
// conn, so the buffered bytes are orphaned and the read pump can never see
// them).
// Finding (verified with a real probe): a client that does ONE
// Write(handshake || masked text frame) never has that first frame delivered —
// Read() silently loses it while a second frame written afterwards IS
// delivered. For an authenticate-first protocol the auth frame disappears and
// the connection hangs.
// Fix direction: after Hijack, copy everything bufrw.Reader still holds
// (io.Reader) in front of the raw conn — e.g. prefix the read path with those
// bytes — before the read pump takes ownership.

// wsHijackRead is one server-side Read() result.
type wsHijackRead struct {
	payload []byte
	err     error
}

// TestWSRedHijackCoalescedFrame mirrors upgradeOnce (connid_test.go): a real
// listener so Hijack works, a raw TCP client that speaks the handshake — but
// pipelines the first data frame into the SAME Write as the handshake, exactly
// like a browser or SDK that queues the auth frame before the 101 arrives.
func TestWSRedHijackCoalescedFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup broken: listen: %v", err)
	}
	defer ln.Close()

	reads := make(chan wsHijackRead, 4)
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, WSConfig{})
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			p, err := conn.Read()
			reads <- wsHijackRead{payload: p, err: err}
			if err != nil {
				return
			}
		}
	}))

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("setup broken: dial: %v", err)
	}
	defer cli.Close()

	// Handshake for the real listener + a masked client text frame, in ONE
	// Write — the coalesced case net/http buffers inside its request reader.
	handshake := "GET /ws HTTP/1.1\r\n" +
		"Host: " + ln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	oneWrite := append([]byte(handshake),
		maskedClientFrame(wsopcodeText, []byte("HELLO-FIRST-FRAME"))...)
	if _, err := cli.Write(oneWrite); err != nil {
		t.Fatalf("setup broken: coalesced write: %v", err)
	}

	// Drain the 101 response, bounded.
	if err := cli.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setup broken: client deadline: %v", err)
	}
	resp := make([]byte, 0, 512)
	buf := make([]byte, 512)
	for !strings.Contains(string(resp), "\r\n\r\n") {
		n, err := cli.Read(buf)
		resp = append(resp, buf[:n]...)
		if err != nil {
			t.Fatalf("setup broken: reading 101 response (got %q): %v", resp, err)
		}
	}
	if !strings.Contains(string(resp), "101 Switching Protocols") {
		t.Fatalf("setup broken: expected 101, got: %s", resp)
	}

	// Leg 1: the frame coalesced with the handshake must reach Read().
	select {
	case r := <-reads:
		if r.err != nil {
			t.Fatalf("setup broken: server Read error: %v", r.err)
		}
		if string(r.payload) != "HELLO-FIRST-FRAME" {
			t.Errorf("SECURITY: [ws-hijackbuf] first frame delivered as %q, want %q — the coalesced frame was mangled in transit", r.payload, "HELLO-FIRST-FRAME")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [ws-hijackbuf] the frame coalesced with the handshake never reached Read() — Upgrade flushes the hijacked *bufio.ReadWriter but never drains its Reader, so the bytes net/http buffered past the handshake are orphaned when the read pump is handed the raw conn; an authenticate-first client loses its auth frame and hangs")
	}

	// Leg 2 (no-desync pin): a frame written normally afterwards must still
	// arrive, byte-exact, proving the stream is intact beyond the loss.
	second := maskedClientFrame(wsopcodeText, []byte("HELLO-SECOND-FRAME"))
	if _, err := cli.Write(second); err != nil {
		t.Fatalf("setup broken: second frame write: %v", err)
	}
	select {
	case r := <-reads:
		if r.err != nil {
			t.Fatalf("setup broken: server Read error on second frame: %v", r.err)
		}
		if string(r.payload) != "HELLO-SECOND-FRAME" {
			t.Errorf("SECURITY: [ws-hijackbuf] second frame delivered as %q, want %q — the hijack lost more than the coalesced bytes; the stream is desynced", r.payload, "HELLO-SECOND-FRAME")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [ws-hijackbuf] second frame (written after the handshake) also never reached Read() — the connection is dead, not just lossy")
	}
}
