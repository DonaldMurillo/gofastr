package stream

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// validUpgradeReq builds a complete RFC 6455 §4.2.1 handshake.
func validUpgradeReq() *http.Request {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==") // 16 bytes, RFC example
	r.Host = "example.com"
	return r
}

// Property: the origin's definition of "this is a WebSocket upgrade"
// must not be looser than an intermediary's.
//
// A proxy that doesn't recognise a request as an upgrade forwards it as
// an ordinary request on a pooled backend connection and waits for an
// ordinary response. If we hijack and emit frames anyway, those bytes
// can surface as the response to a different user's queued request,
// and the peer partly controls them, since ping payloads are echoed
// verbatim. Every condition is therefore checked before the 101.
//
// httptest's recorder is not a Hijacker, so a handshake that passes
// validation fails with a "hijacking" error; that is the success
// signal here.
func TestUpgradeRejectsNonRFCHandshake(t *testing.T) {
	t.Run("complete handshake reaches hijack", func(t *testing.T) {
		_, err := Upgrade(httptest.NewRecorder(), validUpgradeReq(), WSConfig{})
		if err == nil || !strings.Contains(err.Error(), "hijack") {
			t.Fatalf("valid handshake should reach hijacking, got %v", err)
		}
	})

	// One case per RFC condition, not a matrix.
	for name, mut := range map[string]func(*http.Request){
		"non-GET method":    func(r *http.Request) { r.Method = "POST" },
		"no Connection hdr": func(r *http.Request) { r.Header.Del("Connection") },
		"wrong version":     func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") },
		"key not 16 bytes":  func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "c2hvcnQ=") },
		"key not base64":    func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "!!!not-base64!!!") },
	} {
		t.Run(name, func(t *testing.T) {
			r := validUpgradeReq()
			mut(r)
			_, err := Upgrade(httptest.NewRecorder(), r, WSConfig{})
			if err == nil || strings.Contains(err.Error(), "hijack") {
				t.Errorf("handshake was accepted (err=%v)", err)
			}
		})
	}
}

// Property: a frame parser must implement the message state machine it
// claims to. `fin` was previously read and then used only for the
// control-frame check, so a TEXT frame with FIN=0 returned HALF A
// MESSAGE to the application as if it were whole, and browsers
// fragment large sends, so this fired on legitimate traffic too.
// ReadLimit is documented as a *message* bound, which only holds once
// fragments accumulate.
func TestFragmentedMessageReassembly(t *testing.T) {
	// TEXT "Hel" (FIN=0) + CONT "lo" (FIN=1) → one "Hello".
	frames := append(wsTextFrag(0x1, false, "Hel"), wsTextFrag(0x0, true, "lo")...)
	c := wsConnFromBytes(frames)

	f, err := c.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got := string(f.payload); got != "Hello" {
		t.Fatalf("fragments not reassembled: got %q, want %q", got, "Hello")
	}
	if f.opcode != 0x1 {
		t.Errorf("reassembled opcode = 0x%X, want 0x1 (the opener's)", f.opcode)
	}
}

// A continuation with nothing to continue, and a reserved opcode, are
// protocol errors; previously both were delivered to the application
// as ordinary data.
func TestRejectsBareContinuationAndReservedOpcode(t *testing.T) {
	t.Run("bare continuation", func(t *testing.T) {
		c := wsConnFromBytes(wsTextFrag(0x0, true, "orphan"))
		if _, err := c.readFrame(); err == nil {
			t.Fatal("continuation with no opener was accepted")
		}
	})
	t.Run("reserved opcode", func(t *testing.T) {
		c := wsConnFromBytes(wsTextFrag(0x3, true, "reserved"))
		if _, err := c.readFrame(); err == nil {
			t.Fatal("reserved opcode 0x3 was delivered as data")
		}
	})
}

// wsConnFromBytes wraps a canned frame stream in a connection whose
// reads come from that buffer.
func wsConnFromBytes(b []byte) *WebSocketConn {
	return &WebSocketConn{
		conn:       readOnlyConn{Reader: bytes.NewReader(b)},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}
}

// readOnlyConn adapts a Reader to io.ReadWriteCloser; writes are
// discarded (a Ping in these streams would try to Pong).
type readOnlyConn struct{ io.Reader }

func (readOnlyConn) Write(p []byte) (int, error) { return len(p), nil }
func (readOnlyConn) Close() error                { return nil }

// wsTextFrag builds one unmasked frame with the given opcode/FIN/payload.
// Payloads here are short, so the 7-bit length form is always enough.
func wsTextFrag(opcode byte, fin bool, payload string) []byte {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	return append([]byte{b0, byte(len(payload))}, payload...)
}

// Property: the close-code echo boundary documented on wsEchoableCloseCode
// holds on the wire, not just in the predicate. Each row is one boundary
// class from that doc comment, not byte-variants of one class:
//
//   - never echoed (→1002): 1004 (reserved §7.4.1), 1015 (reserved,
//     MUST NOT appear on the wire), 1012 (IANA but not RFC-6455-defined),
//     2001 (§7.4.2 extension range), 65535 (outside the code space)
//   - echoed verbatim: 1000/1011 (RFC-defined bounds), 3000/4999
//     (library/private-use bounds)
//
// 1005/1006/999 and the 1001 positive are already pinned in
// websocket_review_test.go and not repeated.
func TestCloseEchoCodeBoundary(t *testing.T) {
	never := []uint16{1004, 1015, 1012, 2001, 65535}
	verbatim := []uint16{1000, 1011, 3000, 4999}

	t.Run("predicate surface", func(t *testing.T) {
		for _, code := range never {
			if wsEchoableCloseCode(code) {
				t.Errorf("wsEchoableCloseCode(%d) = true, want false", code)
			}
		}
		for _, code := range verbatim {
			if !wsEchoableCloseCode(code) {
				t.Errorf("wsEchoableCloseCode(%d) = false, want true", code)
			}
		}
	})

	t.Run("wire surface", func(t *testing.T) {
		for _, code := range never {
			t.Run(fmt.Sprintf("code-%d-echoes-1002", code), func(t *testing.T) {
				body := []byte{byte(code >> 8), byte(code)}
				frame := driveCloseEcho(t, body)
				if len(frame) < 4 || frame[0]&0x0F != wsopcodeClose {
					t.Fatalf("expected Close echo, got %x", frame)
				}
				if frame[2] != echo1002[0] || frame[3] != echo1002[1] {
					t.Fatalf("code %d echoed as %02x %02x, want 03 EA (1002)", code, frame[2], frame[3])
				}
			})
		}
		for _, code := range verbatim {
			t.Run(fmt.Sprintf("code-%d-echoed-verbatim", code), func(t *testing.T) {
				body := []byte{byte(code >> 8), byte(code)}
				frame := driveCloseEcho(t, body)
				if len(frame) != 4 {
					t.Fatalf("echo frame = %x, want header+status only", frame)
				}
				if frame[2] != body[0] || frame[3] != body[1] {
					t.Fatalf("code %d echoed as %02x %02x, want %02x %02x", code, frame[2], frame[3], body[0], body[1])
				}
			})
		}
	})
}

// Property: the echo Close frame carries the peer's status code and
// nothing else — the peer-controlled reason bytes are never replayed on
// our wire, even at the 125-byte control-frame payload maximum.
func TestCloseEchoDropsPeerReason(t *testing.T) {
	for name, reason := range map[string]string{
		"short reason":      "bye",
		"max-length reason": strings.Repeat("R", 123), // 2 code bytes + 123 = control max
	} {
		t.Run(name, func(t *testing.T) {
			body := append([]byte{0x03, 0xE8}, reason...) // 1000 + reason
			frame := driveCloseEcho(t, body)
			if len(frame) != 4 {
				t.Fatalf("echo frame = %x (%d bytes), want exactly 2-byte header + 2-byte status", frame, len(frame))
			}
			if frame[2] != 0x03 || frame[3] != 0xE8 {
				t.Fatalf("echo status = %02x %02x, want 03 E8 (1000)", frame[2], frame[3])
			}
		})
	}
}

// Property: a Close frame arriving mid-fragmented-message aborts the
// message; the partial fragment accumulated so far is never handed to
func TestCloseFrameAbortsFragmentedMsg(t *testing.T) {
	frames := append(wsTextFrag(0x1, false, "par"), 0x88, 0x02, 0x03, 0xE9)
	// wsConnFromBytes does not initialise the close-handshake channels
	// (its streams never carried a Close frame before), so build the
	// conn fully: readFrame closes peerClosed when it parses the Close.
	c := &WebSocketConn{
		conn:       readOnlyConn{Reader: bytes.NewReader(frames)},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}

	f, err := c.readFrame()
	if err == nil {
		t.Fatal("Close during an open fragmented message was not surfaced as an error")
	}
	if f != nil {
		t.Fatalf("partial fragment delivered as a complete message: opcode=0x%X payload=%q", f.opcode, f.payload)
	}
	select {
	case <-c.peerClosed:
	default:
		t.Error("peerClosed was not signalled after the Close frame was parsed")
	}
}

// Property: an UNMASKED client close frame is a protocol error, not a
// close handshake. A hostile client that skips masking must not get the
// courtesy of an echoed status code — the frame is rejected before the
// close handling runs, no peerClosed signal fires, and Close() would
// fall back to the bodyless echo rather than replaying peer bytes.
func TestUnmaskedCloseIsProtocolError(t *testing.T) {
	unmaskedClose := []byte{0x88, 0x02, 0x03, 0xE9} // CLOSE, code 1001, no mask
	c := &WebSocketConn{
		conn:       readOnlyConn{Reader: bytes.NewReader(unmaskedClose)},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20, requireMask: true},
	}

	f, err := c.readFrame()
	if err == nil {
		t.Fatal("unmasked client close frame was accepted (requireMask enforcement skipped it)")
	}
	if f != nil {
		t.Fatalf("frame delivered for a protocol error: %x", f.payload)
	}
	select {
	case <-c.peerClosed:
		t.Error("unmasked close still signalled peerClosed; a protocol error must not complete a handshake")
	default:
	}
	if echo := c.peerClosePayload.Load(); echo != nil {
		t.Errorf("unmasked close captured echo payload %x; peer bytes from a protocol-violating frame must not be stored for echo", *echo)
	}
}
