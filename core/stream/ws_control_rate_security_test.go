package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// controlFrames builds n masked, empty control frames of opcode, then a
// text frame "done".
func controlFrames(opcode byte, n int) []byte {
	var b bytes.Buffer
	mask := [4]byte{0x01, 0x02, 0x03, 0x04}
	for range n {
		b.WriteByte(0x80 | opcode)
		b.WriteByte(0x80)
		b.Write(mask[:])
	}
	payload := []byte("done")
	b.WriteByte(0x80 | wsopcodeText)
	b.WriteByte(0x80 | byte(len(payload)))
	b.Write(mask[:])
	for i, c := range payload {
		b.WriteByte(c ^ mask[i%4])
	}
	return b.Bytes()
}

// TestControlFramesRateLimited: the read pump answers a Ping and clears
// a Pong itself, so an app's own frame limit never sees them and a raw
// client could flood both with no bound (ledger R21 of the 2026-10-27
// bug bash, battery/rtc's MaxFramesPerSecond). Past
// MaxControlFramesPerSecond (burst twice the rate) the pump refuses the
// connection with 1008.
func TestControlFramesRateLimited(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opcode byte
		n      int
		refuse bool
	}{
		{"ping burst", wsopcodePing, 8, false},
		{"ping flood", wsopcodePing, 40, true},
		{"pong flood", wsopcodePong, 40, true},
	} {
		conn := &WebSocketConn{
			conn:       &nopConn{r: bytes.NewReader(controlFrames(tc.opcode, tc.n)), w: io.Discard},
			sendBuffer: make(chan wsMessage, 8),
			closed:     make(chan struct{}),
			config:     WSConfig{ReadLimit: 1 << 20, MaxControlFramesPerSecond: 4},
		}
		got, err := conn.readFrame()
		if !tc.refuse {
			if err != nil || string(got.payload) != "done" {
				t.Fatalf("%s: %v, %v; want the text frame", tc.name, got, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: read %q past the control-frame budget", tc.name, got.payload)
		}
		own := conn.ownClosePayload.Load()
		if own == nil || len(*own) < 2 || binary.BigEndian.Uint16((*own)[:2]) != 1008 {
			t.Fatalf("%s: close payload %v, want 1008", tc.name, own)
		}
	}
}

// TestUpgradeDefaultsControlRate: zero takes the default and a negative
// value, the opt-out, is kept.
func TestUpgradeDefaultsControlRate(t *testing.T) {
	if got := upgradeOnce(t, WSConfig{}).config.MaxControlFramesPerSecond; got != defaultControlFramesPerSecond {
		t.Fatalf("default control rate = %d, want %d", got, defaultControlFramesPerSecond)
	}
	if got := upgradeOnce(t, WSConfig{MaxControlFramesPerSecond: -1}).config.MaxControlFramesPerSecond; got != -1 {
		t.Fatalf("opt-out rewritten to %d", got)
	}
}
