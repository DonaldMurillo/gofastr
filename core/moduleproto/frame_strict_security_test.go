package moduleproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// Pins: every inbound protocol frame is decoded with the no-ambiguity
// rule — a frame whose envelope repeats a key, or carries two spellings
// of one key that differ only by case, is refused as ErrInvalidFrame
// (2026-09-06/07 adversarial round 5, phase 2).
// Property: every inbound protocol frame is decoded with the no-ambiguity
// rule — a frame whose envelope repeats a key, or carries two spellings of
// one key that differ only by case, is refused, never silently normalized to
// the last spelling. The moduleproto peer transports host<->module traffic
// including module.permission and module.log requests, so a folded envelope
// key is a dispatch-confusion vector: a monitor or auditor reading the first
// occurrence of "method" sees one dispatch target while the codec hands the
// read loop another.
// Surfaces: core/moduleproto/codec.go::ReadFrame :140-146 — plain
// json.Unmarshal into Frame. Frame.UnmarshalJSON (frame.go:120-170) checks
// wire invariants but cannot see key ambiguity, because stdlib has already
// folded "method"/"method" and "Params"/"params" onto one field, last wins.
// The strict twins are the acp/a2a/mcp envelope decodes (core/acp
// params_strict_red_test.go pins the params arm; core/mcp transport is
// pinned) and handler.DecodeStrict's documented rule: "no key may repeat,
// and no two keys may fold to the same name under ASCII case folding."
// Finding (probed): ReadFrame over
// {"jsonrpc":"2.0","id":1,"method":"module.log","method":"module.permission","params":{}}
// returns a nil error and a Frame whose Method is the SECOND value —
// last-wins dispatch. The folded Params/params leg behaves the same.
// Fix direction: decode each frame through a strict pass that refuses
// repeated and case-folded envelope keys (handler.CheckObjectKeys grammar)
// and surface the rejection as ErrInvalidFrame, the sentinel the Peer
// already treats as a terminal frame-level fault.

func TestReadFrameRejectsAmbiguousKeysRed(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{
			name: "duplicate method keys pick the second dispatch target",
			line: `{"jsonrpc":"2.0","id":1,"method":"module.log","method":"module.permission","params":{}}`,
		},
		{
			name: "case-folded Params/params keys pick the last spelling",
			line: `{"jsonrpc":"2.0","id":1,"method":"module.log","Params":{"level":"info"},"params":{"level":"debug"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCodec(strings.NewReader(tc.line+"\n"), io.Discard, 0)
			if err != nil {
				t.Fatalf("setup broken: NewCodec: %v", err)
			}
			f, err := c.ReadFrame()
			if err == nil || !errors.Is(err, ErrInvalidFrame) {
				t.Errorf("SECURITY: [moduleproto-frame-lenient] ReadFrame accepted a frame with ambiguous envelope keys (%s): err=%v, decoded frame method=%q — repeated/case-folded keys must be rejected as ErrInvalidFrame, not folded last-wins onto the field", tc.name, err, frameMethod(f))
			}
		})
	}

	// GREEN-guard: the codec's clean round trip (TestCodecWriteReadRoundTrip
	// contract) must keep working, so the strict decode cannot refuse
	// well-formed frames.
	t.Run("clean frame round-trips", func(t *testing.T) {
		var buf bytes.Buffer
		c, err := NewCodec(&buf, &buf, 0)
		if err != nil {
			t.Fatalf("setup broken: NewCodec: %v", err)
		}
		if err := c.WriteFrame(NewRequest(uint64(7), "module.log", json.RawMessage(`{"verbose":true}`))); err != nil {
			t.Fatalf("setup broken: WriteFrame: %v", err)
		}
		out, err := c.ReadFrame()
		if err != nil {
			t.Fatalf("setup broken: clean frame rejected: %v", err)
		}
		if !out.IsRequest() || out.IDValue() != 7 || out.Method != "module.log" {
			t.Fatalf("setup broken: clean round-trip mangled the frame: %+v", out)
		}
	})
}

// frameMethod returns the decoded Method for the violation message, or ""
// for a nil frame.
func frameMethod(f *Frame) string {
	if f == nil {
		return ""
	}
	return f.Method
}
