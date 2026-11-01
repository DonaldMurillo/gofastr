package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"
)

// sendDataFrames writes n masked text frames from the peer side.
func sendDataFrames(t *testing.T, w io.Writer, n int) {
	t.Helper()
	for i := range n {
		if _, err := w.Write(maskedClientFrame(wsopcodeText, []byte{'m', byte('a' + i%26)})); err != nil {
			t.Fatalf("peer write %d: %v", i, err)
		}
	}
}

// A push-only server (never calls Read) whose peer sent more messages
// than the read buffer holds, then vanished without a FIN or a Pong.
// The pump used to sit in the handoff forever; the keepalive skipped
// every tick while it did, so the connection was never reclaimed.
func TestStalledPushOnlyConnIsReclaimed(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	sendDataFrames(t, cli, 12) // 8 buffered, 1 in the pump, 3 queued
	// The peer drains our frames (so writes never block) but answers
	// nothing: from here on it is dead.
	go func() { _, _ = io.Copy(io.Discard, cli) }()

	select {
	case <-conn.Closed():
	case <-time.After(2 * time.Second):
		t.Fatal("dead peer behind a stalled handoff was never reclaimed")
	}
}

// The same stall where the app does read, once, and then abandons its
// read loop: past the stall bound the connection is closed.
func TestStalledReaderIsReclaimed(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	go func() { _, _ = io.Copy(io.Discard, cli) }()
	sendDataFrames(t, cli, 12)
	if _, err := conn.Read(); err != nil {
		t.Fatalf("first Read: %v", err)
	}

	select {
	case <-conn.Closed():
	case <-time.After(2 * time.Second):
		t.Fatal("abandoned reader behind a stalled handoff was never reclaimed")
	}
}

// The reason the keepalive skips a stalled handoff: a reader that is
// slow (one message per 200ms, past PongTimeout's share of the queue)
// but keeps draining must not be killed while the peer's Pong waits in
// the socket behind data frames the pump cannot read yet.
func TestSlowReaderSurvivesKeepalive(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 80 * time.Millisecond,
		PongTimeout:     300 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	sendDataFrames(t, cli, 12)
	var answered int32
	stop := make(chan struct{})
	defer close(stop)
	go pongAnsweringPeer(cli, &answered, stop)

	for i := range 12 {
		time.Sleep(200 * time.Millisecond)
		select {
		case <-conn.Closed():
			t.Fatalf("slow reader killed after %d messages", i)
		default:
		}
		if _, err := conn.Read(); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
	}
	select {
	case <-conn.Closed():
		t.Fatal("slow reader's connection closed")
	default:
	}
	conn.Close()
}

// Drain-before-error at the blocking select: when the pump's last
// message and readDone are both ready, Read returns the message. Run
// many times because select picks a ready case at random.
func TestReadDoneStillDrainsLastMessage(t *testing.T) {
	for i := range 200 {
		c := &WebSocketConn{readMsgs: make(chan []byte, 1), readDone: make(chan struct{})}
		c.readErr = io.EOF
		c.readMsgs <- []byte("last")
		close(c.readDone)
		msg, err := c.Read()
		if err != nil || string(msg) != "last" {
			t.Fatalf("iteration %d: Read = %q, %v; want the buffered message first", i, msg, err)
		}
		if _, err := c.Read(); err != io.EOF {
			t.Fatalf("iteration %d: second Read err = %v, want io.EOF", i, err)
		}
	}
}

// recordConn keeps every byte written, and stays writable after Close
// so a late frame is visible rather than swallowed by a closed socket.
type recordConn struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recordConn) Read([]byte) (int, error) { select {} }
func (r *recordConn) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *recordConn) Close() error { return nil }

func (r *recordConn) opcodes(t *testing.T) []byte {
	t.Helper()
	r.mu.Lock()
	b := bytes.Clone(r.buf.Bytes())
	r.mu.Unlock()
	var ops []byte
	for len(b) >= 2 {
		n := int(b[1] & 0x7F)
		hdr := 2
		switch n {
		case 126:
			n = int(binary.BigEndian.Uint16(b[2:4]))
			hdr = 4
		case 127:
			t.Fatal("unexpected 64-bit frame")
		}
		ops = append(ops, b[0]&0x0F)
		b = b[hdr+n:]
	}
	return ops
}

// A writer that was queued on the write mutex while Close wrote its
// Close frame must not put a data frame on the wire after it (RFC 6455
// §5.5.1: no data frames after Close).
func TestNoDataFrameAfterClose(t *testing.T) {
	rc := &recordConn{}
	c := &WebSocketConn{
		conn:       rc,
		sendBuffer: make(chan wsMessage, 1),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{CloseTimeout: 10 * time.Millisecond},
	}
	_ = c.Close()
	// The writer that lost the mutex race now runs its write.
	if err := c.writeFrame(wsopcodeText, []byte("late")); err == nil {
		t.Error("writeFrame after the Close frame returned nil")
	}
	ops := rc.opcodes(t)
	if len(ops) != 1 || ops[0] != wsopcodeClose {
		t.Fatalf("wire opcodes = %v, want only the Close frame", ops)
	}
}

// A Ping that went out just before a stall: the peer's Pong waits in the
// socket behind data frames the pump cannot read until the app drains.
// A stall past PongTimeout but inside its own bound must not kill the
// connection the moment it ends, before the pump reaches the Pong.
func TestPongClockRestartsAfterStall(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 200 * time.Millisecond,
		PongTimeout:     100 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	if op, err := readServerFrame(cli); err != nil || op != wsopcodePing {
		t.Fatalf("first server frame = %v, %v; want a Ping", op, err)
	}
	_ = cli.SetReadDeadline(time.Time{})
	go func() { _, _ = io.Copy(io.Discard, cli) }()
	sendDataFrames(t, cli, 9) // 8 buffered, the 9th stalls the pump
	// Past PongTimeout since the Ping, inside the stall bound
	// (ReadIdleTimeout + PongTimeout).
	time.Sleep(160 * time.Millisecond)
	if _, err := conn.Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The Pong reaches the pump a little after the stall ends, as it
	// would behind more queued frames.
	time.Sleep(60 * time.Millisecond)
	select {
	case <-conn.Closed():
		t.Fatal("live connection closed as its stall ended, before the pump read the Pong")
	default:
	}
	if _, err := cli.Write([]byte{0x80 | wsopcodePong, 0x80, 0xa, 0xb, 0xc, 0xd}); err != nil {
		t.Fatalf("peer pong: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	select {
	case <-conn.Closed():
		t.Fatal("connection closed after the Pong arrived")
	default:
	}
	conn.Close()
}
