//go:build red

package ws

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
)

// Property: inbound JSON on operator-reachable control-plane surfaces
// is decoded under strict top-level key rules (duplicate keys rejected,
// keys must exact-match the envelope's json tags) — the repo standard
// core/handler/bind.go validateBodyKeys enforces on production Bind
// surfaces.
// Surfaces: ws.go handleText text-frame envelope {"frame","body"} —
// loopback bearer-token operator transport, dev harness surface.
// Finding: ws.go:267 json.Unmarshal(payload, &f) is fully non-strict:
// a duplicate top-level key is resolved last-wins and a case-folded
// key ("FRAME" vs "frame") matches its tag case-insensitively. A frame
// like {"frame":"event","frame":"command",...} is dispatched as a
// command even though any first-read intermediary (proxy, logger,
// audit trail) parsed it as an event frame, and the body is re-decoded
// non-strictly by control.UnmarshalCommand afterwards.
// Fix direction: strict-decode the envelope (DisallowUnknownFields +
// validateBodyKeys-equivalent duplicate/case-fold rejection over the
// raw payload) and answer with the existing controlError("bad frame:")
// path instead of dispatching.
// Round-6 mechanism split: exact duplicates and case-folded keys are
// separate top-level tests below (independently fixable mechanisms).

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

// TestHarnessWsRedRejectsDuplicateKeys: exact duplicate top-level "frame"
// keys — wire-level last-wins.
func TestHarnessWsRedRejectsDuplicateKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed envelope
	// dispatches and streams the turn, so the refusal demanded
	// below can only come from key strictness, not plumbing.
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
		t.Errorf("SECURITY: [ws-strict-keys] a text frame with a smuggled key shape (duplicate top-level frame key) was "+
			"decoded and dispatched (received %.200q). ws.go:267 json.Unmarshal is non-strict: "+
			"duplicate top-level keys resolve last-wins, so "+
			"{\"frame\":\"event\",\"frame\":\"command\",...} runs as a command while any "+
			"first-read intermediary saw an event frame. Strict-decode the envelope and answer "+
			"via controlError(\"bad frame:\") instead of dispatching.", got)
	}
}

// TestHarnessWsRedRejectsCaseFoldedKeys: "FRAME"/"BODY" case-fold onto
// the tagged fields via stdlib json's tag-insensitive match — the frame
// still dispatches as a command; survives a dedup-only fix.
func TestHarnessWsRedRejectsCaseFoldedKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed envelope
	// dispatches and streams the turn, so the refusal demanded
	// below can only come from key strictness, not plumbing.
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
		t.Errorf("SECURITY: [ws-strict-keys] a text frame with a smuggled key shape (case-folded top-level keys) was "+
			"decoded and dispatched (received %.200q). ws.go:267 json.Unmarshal is non-strict: "+
			"case-folded keys still match their tags, so "+
			"{\"FRAME\":\"command\",...} runs as a command while any "+
			"first-read intermediary parsed a different frame. Strict-decode the envelope and answer "+
			"via controlError(\"bad frame:\") instead of dispatching.", got)
	}
}
