package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// clientFrame builds one masked client frame with the given FIN bit and
// opcode, so a test can fragment a message the way a client library may
// (RFC 6455 §5.4).
func clientFrame(fin bool, op byte, payload []byte) []byte {
	f := maskedFrame(payload)
	f[0] = op
	if fin {
		f[0] |= 0x80
	}
	return f
}

// helloCommand is the command frame the fake provider answers with
// ws-hello.
func helloCommand(t *testing.T, session ids.SessionID) []byte {
	t.Helper()
	body, err := control.MarshalCommand(control.SendInput{SessionID: session, Content: engine.SimpleInput("hello")})
	if err != nil {
		t.Fatal(err)
	}
	f, err := json.Marshal(struct {
		Frame string          `json:"frame"`
		Body  json.RawMessage `json:"body"`
	}{Frame: "command", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// A client may split a message into a TEXT frame without FIN and
// continuation frames, with control frames between them. The harness
// read each fragment as a whole command, so the first half was decoded
// alone and the continuation closed the socket (2026-10-27 H13).
func TestWSFragmentedCommandRuns(t *testing.T) {
	urlStr, session, tok, cleanup := setupServer(t)
	defer cleanup()
	conn := dialWS(t, strings.TrimPrefix(urlStr, "http://"), session, tok)
	defer conn.Close()
	cmd := helloCommand(t, session)
	half := len(cmd) / 2
	// A whole message first, so the fragmented one must start from an
	// empty buffer.
	wire := maskedFrame(cmd)
	wire = append(wire, clientFrame(false, opcodeText, cmd[:half])...)
	wire = append(wire, clientFrame(true, opcodePing, []byte("p"))...)
	wire = append(wire, clientFrame(true, opcodeContinuation, cmd[half:])...)
	if _, err := conn.Write(wire); err != nil {
		t.Fatal(err)
	}
	got := readAllFrames(t, conn, 3*time.Second)
	// The second command lands while the first turn runs, so it may run
	// its own turn or be refused as busy; either way it decoded whole.
	second := strings.Count(got, "ws-hello") == 2 || strings.Contains(got, "another client is sending input")
	if !strings.Contains(got, "ws-hello") || !second || strings.Contains(got, "bad frame") {
		t.Fatalf("fragmented command did not run as one message:\n%q", got)
	}
}

// A continuation with no message open, a TEXT frame while one is open,
// a fragmented control frame, a control frame over 125 bytes, and a
// message past the 16 MiB cap are protocol errors: the socket closes and
// nothing is dispatched.
func TestWSFragmentErrorsClose(t *testing.T) {
	urlStr, session, tok, cleanup := setupServer(t)
	defer cleanup()
	cmd := helloCommand(t, session)
	half := len(cmd) / 2
	big := bytes.Repeat([]byte("a"), 9<<20)
	for _, tc := range []struct {
		name   string
		frames [][]byte
	}{
		{"continuation opens nothing", [][]byte{clientFrame(true, opcodeContinuation, cmd)}},
		{"text inside a message", [][]byte{
			clientFrame(false, opcodeText, cmd[:half]),
			clientFrame(true, opcodeText, cmd),
		}},
		{"fragmented ping", [][]byte{clientFrame(false, opcodePing, []byte("p"))}},
		{"ping over 125 bytes", [][]byte{clientFrame(true, opcodePing, bytes.Repeat([]byte("p"), 126))}},
		// Only the second frame's header (2 bytes, an 8-byte length, the
		// mask) is sent: the refusal comes before its payload is read, so
		// an open message and the frame after it never pass the cap.
		{"message over the cap", [][]byte{
			clientFrame(false, opcodeText, big),
			clientFrame(true, opcodeContinuation, big[:8<<20])[:14],
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := dialWS(t, strings.TrimPrefix(urlStr, "http://"), session, tok)
			defer conn.Close()
			for _, f := range tc.frames {
				if _, err := conn.Write(f); err != nil {
					t.Fatal(err)
				}
			}
			got := readAllFrames(t, conn, 2*time.Second)
			if !strings.Contains(got, "\x88\x00") || strings.Contains(got, "ws-hello") || strings.Contains(got, "bad frame") {
				t.Fatalf("want a close and no dispatch, got %q", got)
			}
		})
	}
}

// The upgrade answered 101 to any request with an Upgrade header and a
// non-empty key (2026-10-27 H14). It now runs core/stream's opening
// handshake check before the hijack, so a malformed one answers 400.
func TestWSRefusesMalformedHandshake(t *testing.T) {
	urlStr, session, tok, cleanup := setupServer(t)
	defer cleanup()
	host := strings.TrimPrefix(urlStr, "http://")
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	for _, tc := range []struct{ name, method, conn, version, key string }{
		{"post", "POST", "Upgrade", "13", key},
		{"no connection token", "GET", "keep-alive", "13", key},
		{"old version", "GET", "Upgrade", "8", key},
		{"short key", "GET", "Upgrade", "13", "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", host)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			fmt.Fprintf(conn, "%s /?session=%s&token=%s HTTP/1.1\r\nHost: %s\r\n"+
				"Upgrade: websocket\r\nConnection: %s\r\nContent-Length: 0\r\n"+
				"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: %s\r\n\r\n",
				tc.method, session, tok, host, tc.conn, tc.key, tc.version)
			buf := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _ := conn.Read(buf)
			if !strings.HasPrefix(string(buf[:n]), "HTTP/1.1 400") {
				t.Fatalf("want 400, got %q", buf[:n])
			}
		})
	}
}
