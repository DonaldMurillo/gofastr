package ws

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
)

// Inbound text frames are decoded under strict top-level key rules:
// duplicate and case-folded keys resolve last-wins under stdlib json, so
// {"frame":"event","frame":"command",...} would dispatch as a command
// while any first-read intermediary (proxy, logger, audit trail) parsed
// an event frame. The envelope is strict-decoded (handler.UnmarshalStrict)
// and a smuggled shape is answered through the controlError("bad frame:")
// path instead of dispatching (handler.DecodeStrict parity with
// core/handler Bind surfaces).

// sendRawFrame writes one masked client text frame with raw JSON bytes.
func sendRawFrame(t *testing.T, conn net.Conn, raw []byte) {
	t.Helper()
	if _, err := conn.Write(maskedFrame(raw)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// readAllFrames collects every inbound byte for a bounded window
// (no early return) so the assertion sees the full response set.
func readAllFrames(t *testing.T, conn net.Conn, window time.Duration) string {
	t.Helper()
	var all []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			all = append(all, buf[:n]...)
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			break
		}
	}
	return string(all)
}

// wsStrictRefusal reports whether the payload shows an explicit
// rejection (controlError-style frame) rather than a dispatched turn.
func wsStrictRefusal(payload string) bool {
	if payload == "" || strings.Contains(payload, "ws-hello") {
		return false
	}
	for _, marker := range []string{"bad frame", "InvalidCommand", "duplicate", "fold", "reject"} {
		if strings.Contains(payload, marker) {
			return true
		}
	}
	return false
}

// TestWsFrameRejectsDuplicateKeys: exact duplicate top-level "frame"
// keys — wire-level last-wins.
func TestWsFrameRejectsDuplicateKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed envelope dispatches
	// and streams the turn, so the refusal demanded below can only come
	// from key strictness, not plumbing.
	u, sess, tok, cleanup := setupServer(t)
	host := strings.TrimPrefix(u, "http://")
	defer cleanup()
	conn1 := dialWS(t, host, sess, tok)
	defer conn1.Close()
	body1, err := control.MarshalCommand(control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput("hello"),
	})
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	sendRawFrame(t, conn1, []byte(`{"frame":"command","body":`+string(body1)+`}`))
	if got := readUntilTextDelta(t, conn1); !strings.Contains(got, "ws-hello") {
		t.Fatalf("happy path: well-formed frame must stream the turn (got %.200q)", got)
	}

	// Attack: same command body, smuggled key shape.
	host2, sess2, tok2, cleanup2 := setupServer(t)
	host2 = strings.TrimPrefix(host2, "http://")
	defer cleanup2()
	conn2 := dialWS(t, host2, sess2, tok2)
	defer conn2.Close()
	body2, err := control.MarshalCommand(control.SendInput{
		SessionID: sess2,
		Content:   engine.SimpleInput("attack"),
	})
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	sendRawFrame(t, conn2, []byte(`{"frame":"event","frame":"command","body":`+string(body2)+`}`))
	got := readAllFrames(t, conn2, 1500*time.Millisecond)
	if !wsStrictRefusal(got) {
		t.Errorf("SECURITY: [ws-strict-keys] a text frame with a duplicate top-level frame key was "+
			"decoded and dispatched (received %.200q) — duplicate top-level keys resolve last-wins, "+
			"so the frame runs as a command while any first-read intermediary saw an event frame; "+
			"strict-decode the envelope via handler.UnmarshalStrict and answer controlError(\"bad frame:\")",
			got)
	}
}

// TestWsFrameRejectsCaseFoldedKeys: "FRAME"/"BODY" case-fold onto the
// tagged fields via stdlib json's tag-insensitive match — the frame
// still dispatches as a command; survives a dedup-only fix.
func TestWsFrameRejectsCaseFoldedKeys(t *testing.T) {
	u, sess, tok, cleanup := setupServer(t)
	host := strings.TrimPrefix(u, "http://")
	defer cleanup()
	conn1 := dialWS(t, host, sess, tok)
	defer conn1.Close()
	body1, err := control.MarshalCommand(control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput("hello"),
	})
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	sendRawFrame(t, conn1, []byte(`{"frame":"command","body":`+string(body1)+`}`))
	if got := readUntilTextDelta(t, conn1); !strings.Contains(got, "ws-hello") {
		t.Fatalf("happy path: well-formed frame must stream the turn (got %.200q)", got)
	}

	// Attack: same command body, smuggled key shape.
	host2, sess2, tok2, cleanup2 := setupServer(t)
	host2 = strings.TrimPrefix(host2, "http://")
	defer cleanup2()
	conn2 := dialWS(t, host2, sess2, tok2)
	defer conn2.Close()
	body2, err := control.MarshalCommand(control.SendInput{
		SessionID: sess2,
		Content:   engine.SimpleInput("attack"),
	})
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	sendRawFrame(t, conn2, []byte(`{"FRAME":"command","BODY":`+string(body2)+`}`))
	got := readAllFrames(t, conn2, 1500*time.Millisecond)
	if !wsStrictRefusal(got) {
		t.Errorf("SECURITY: [ws-strict-keys] a text frame with case-folded top-level keys was "+
			"decoded and dispatched (received %.200q) — case-folded keys still match their tags, "+
			"so the frame runs as a command while any first-read intermediary parsed a different "+
			"frame; strict-decode the envelope via handler.UnmarshalStrict and answer "+
			"controlError(\"bad frame:\")", got)
	}
}
