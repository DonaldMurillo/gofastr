package stream

import (
	"bytes"
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
// can surface as the response to a different user's queued request —
// and the peer partly controls them, since ping payloads are echoed
// verbatim. Every condition is therefore checked before the 101.
//
// httptest's recorder is not a Hijacker, so a handshake that passes
// validation fails with a "hijacking" error — that is the success
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
// MESSAGE to the application as if it were whole — and browsers
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
// protocol errors — previously both were delivered to the application
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
