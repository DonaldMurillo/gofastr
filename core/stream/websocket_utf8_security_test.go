package stream

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"
)

// A WebSocket text frame must carry valid UTF-8 (RFC 6455 §5.6, §8.1):
// a browser that receives one that does not fails the whole connection
// with 1007. Write used to queue any bytes as text and return nil, so
// one bad message killed the socket from the far side with no error
// here. These tests pin the refusal on every path into the send queue
// and the binary path that carries arbitrary bytes instead.

// newPumpedConn returns a conn over a recordConn with its write pump
// running, so frames land on the "wire" the test then parses.
func newPumpedConn(buf int) (*WebSocketConn, *recordConn) {
	rc := &recordConn{}
	c := &WebSocketConn{
		conn:       rc,
		sendBuffer: make(chan wsMessage, buf),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{CloseTimeout: time.Millisecond},
	}
	go c.writePump()
	return c, rc
}

// wantFrame is one expected frame on the wire.
type wantFrame struct {
	opcode  byte
	payload []byte
}

// waitFrames waits until the peer holds at least n frames.
func waitFrames(t *testing.T, rc *recordConn, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(recordedFrames(t, rc)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("peer got %d frames, want %d", len(recordedFrames(t, rc)), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// assertFrames checks the recorded frames are exactly want, then Close.
func assertFrames(t *testing.T, rc *recordConn, want []wantFrame) {
	t.Helper()
	got := recordedFrames(t, rc)
	if len(got) != len(want)+1 {
		t.Fatalf("peer got %d frames %v, want %d messages and a Close", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].opcode != w.opcode || !bytes.Equal(got[i].payload, w.payload) {
			t.Fatalf("frame %d = 0x%X %q, want 0x%X %q", i, got[i].opcode, got[i].payload, w.opcode, w.payload)
		}
	}
	if last := got[len(want)]; last.opcode != wsopcodeClose {
		t.Fatalf("last frame opcode 0x%X, want Close", last.opcode)
	}
}

// Property: Write and WriteString refuse bytes that are not valid
// UTF-8 with ErrInvalidUTF8 and queue nothing, so the peer never sees
// them; valid multi-byte UTF-8 goes out as text, byte for byte.
func TestWriteRefusesInvalidUTF8(t *testing.T) {
	c, rc := newPumpedConn(8)
	for name, err := range map[string]error{
		"Write lone 0xFF":              c.Write([]byte("bad\xff")),
		"Write truncated sequence":     c.Write([]byte("caf\xc3")),
		"Write surrogate half":         c.Write([]byte("\xed\xa0\x80")),
		"WriteString overlong":         c.WriteString("\xc0\xaf"),
		"WriteString continuation":     c.WriteString("\x80abc"),
		"Write invalid after valid 4B": c.Write([]byte("🎉\xfe")),
	} {
		if !errors.Is(err, ErrInvalidUTF8) {
			t.Errorf("%s = %v, want ErrInvalidUTF8", name, err)
		}
	}
	valid := "héllo, 世界 🎉"
	if err := c.WriteString(valid); err != nil {
		t.Fatalf("WriteString(valid multi-byte UTF-8) = %v, want nil", err)
	}
	c.Close()
	assertFrames(t, rc, []wantFrame{{wsopcodeText, []byte(valid)}})
}

// Property: WriteBinary sends opcode 0x2 with the exact bytes given,
// none of which need be UTF-8.
func TestWriteBinaryArrivesAsOpcode2(t *testing.T) {
	c, rc := newPumpedConn(8)
	payload := []byte{0xff, 0x00, 0xfe, 0x80, 0xc3, 'x'}
	if err := c.WriteBinary(payload); err != nil {
		t.Fatalf("WriteBinary = %v", err)
	}
	waitFrames(t, rc, 1) // the write pump sent it, not Close's drain
	c.Close()
	if err := c.WriteBinary([]byte{1}); err != ErrClosed {
		t.Fatalf("WriteBinary after Close = %v, want ErrClosed", err)
	}
	assertFrames(t, rc, []wantFrame{{wsopcodeBinary, payload}})
}

// mixedMessages queues alternating text and binary messages on c.
func mixedMessages(t *testing.T, c *WebSocketConn, n int) []wantFrame {
	t.Helper()
	var want []wantFrame
	for i := range n {
		if i%2 == 0 {
			text := []byte{'t', byte('a' + i)}
			if err := c.Write(text); err != nil {
				t.Fatalf("Write %d: %v", i, err)
			}
			want = append(want, wantFrame{wsopcodeText, text})
		} else {
			bin := []byte{0xff, byte(i)}
			if err := c.WriteBinary(bin); err != nil {
				t.Fatalf("WriteBinary %d: %v", i, err)
			}
			want = append(want, wantFrame{wsopcodeBinary, bin})
		}
	}
	return want
}

// Property: text and binary share one queue, so they reach the peer in
// Write order, each with its own opcode, whether the write pump sends
// them or Close's drain does.
func TestTextAndBinaryKeepWriteOrder(t *testing.T) {
	t.Run("write pump", func(t *testing.T) {
		c, rc := newPumpedConn(16)
		want := mixedMessages(t, c, 12)
		// Every frame on the wire before Close: the pump wrote them.
		waitFrames(t, rc, len(want))
		c.Close()
		assertFrames(t, rc, want)
	})
	t.Run("close drain", func(t *testing.T) {
		// No write pump: every message is still queued when Close
		// starts, so drainQueued alone writes them.
		rc := &recordConn{}
		c := &WebSocketConn{
			conn:       rc,
			sendBuffer: make(chan wsMessage, 16),
			closed:     make(chan struct{}),
			peerClosed: make(chan struct{}),
			config:     WSConfig{CloseTimeout: time.Millisecond},
		}
		want := mixedMessages(t, c, 12)
		c.Close()
		assertFrames(t, rc, want)
	})
}

// Property: Hub.Broadcast and BroadcastWait refuse text that is not
// valid UTF-8 up front with ErrInvalidUTF8 and send it to no one;
// BroadcastBinary carries arbitrary bytes as opcode 0x2, in order with
// the text around it.
func TestHubBroadcastRefusesInvalidUTF8(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()
	c, rc := newPumpedConn(8)
	hub.Register(c)

	if err := hub.BroadcastWait([]byte("x\xff")); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("BroadcastWait(invalid) = %v, want ErrInvalidUTF8", err)
	}
	if err := hub.Broadcast([]byte("\xc3")); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("Broadcast(invalid) = %v, want ErrInvalidUTF8", err)
	}
	bin := []byte{0x00, 0xff, 0xfe}
	if err := hub.BroadcastWait([]byte("t1 ✓")); err != nil {
		t.Fatalf("BroadcastWait(valid) = %v", err)
	}
	hub.BroadcastBinaryWait(bin)
	if err := hub.BroadcastWait([]byte("t2")); err != nil {
		t.Fatalf("BroadcastWait(valid) = %v", err)
	}
	hub.BroadcastBinary([]byte{0x80})

	waitFrames(t, rc, 4)
	c.Close()
	assertFrames(t, rc, []wantFrame{
		{wsopcodeText, []byte("t1 ✓")},
		{wsopcodeBinary, bin},
		{wsopcodeText, []byte("t2")},
		{wsopcodeBinary, []byte{0x80}},
	})
}

// rawSource is a SnapshotSource whose snapshot and events are
// json.RawMessage: json.Marshal passes their bytes through without a
// UTF-8 check, which is how a StateChannel envelope could carry bytes a
// text frame must not.
type rawSource struct {
	snap json.RawMessage
}

func (s *rawSource) SnapshotFor(string) (json.RawMessage, uint64) { return s.snap, 1 }
func (s *rawSource) FilterEvent(_ string, ev json.RawMessage) (any, bool) {
	return ev, true
}

func startRawChannel(t *testing.T, src *rawSource) *StateChannel[string, json.RawMessage, json.RawMessage] {
	t.Helper()
	c := NewStateChannel[string, json.RawMessage, json.RawMessage](src)
	go c.Run()
	t.Cleanup(func() {
		c.Stop()
		time.Sleep(2 * time.Second)
	})
	return c
}

// Property: a StateChannel never queues an envelope that is not valid
// UTF-8. A snapshot that is not closes the connection (it can never
// hydrate), as an unserializable one does; an event that is not is
// dropped and the next valid event still arrives.
func TestStateChannelRefusesInvalidUTF8(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := startRawChannel(t, &rawSource{snap: json.RawMessage("\"\xff\"")})
			conn := newChannelConn(4)
			c.Connect("r", conn)
			select {
			case <-conn.Closed():
			case <-time.After(5 * time.Second):
				t.Fatal("connection with a non-UTF-8 snapshot was not closed")
			}
			if n := len(conn.sendBuffer); n != 0 {
				t.Fatalf("%d messages queued; a non-UTF-8 snapshot must not be", n)
			}
			// Let that Close finish its CloseTimeout wait before cleanup's
			// Stop closes the conn again: the second Close parks on
			// closeOnce's mutex, which synctest does not count as durably
			// blocked, so fake time would never reach the first one's
			// timer and the bubble would hang.
			time.Sleep(2 * time.Second)
		})
	})
	t.Run("event", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := startRawChannel(t, &rawSource{snap: json.RawMessage(`"ok"`)})
			conn := newChannelConn(4)
			c.Connect("r", conn)
			if env := recvEnvelope(t, conn); env.Type != SnapshotEnvelopeType {
				t.Fatalf("first envelope %q, want the snapshot", env.Type)
			}
			c.Publish("bad", json.RawMessage("\"\xfe\""))
			c.Publish("good", json.RawMessage(`"fine"`))
			raw := recvRaw(t, conn)
			if !utf8.Valid(raw) {
				t.Fatalf("queued a text envelope that is not UTF-8: %q", raw)
			}
			var env SequencedEnvelope[json.RawMessage]
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if env.Type != "good" {
				t.Fatalf("next envelope %q, want the valid event after the dropped one", env.Type)
			}
		})
	})
}
