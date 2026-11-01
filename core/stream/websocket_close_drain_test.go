package stream

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// recordedFrames parses the unmasked server frames a recordConn holds.
func recordedFrames(t *testing.T, rc *recordConn) []wsFrame {
	t.Helper()
	rc.mu.Lock()
	b := bytes.Clone(rc.buf.Bytes())
	rc.mu.Unlock()
	var frames []wsFrame
	for len(b) >= 2 {
		n, hdr := int(b[1]&0x7F), 2
		if n == 126 {
			n, hdr = int(binary.BigEndian.Uint16(b[2:4])), 4
		} else if n == 127 {
			t.Fatal("unexpected 64-bit frame")
		}
		frames = append(frames, wsFrame{opcode: b[0] & 0x0F, payload: b[hdr : hdr+n]})
		b = b[hdr+n:]
	}
	return frames
}

// Property: a message Write accepted before Close reaches the peer, in
// order, ahead of the Close frame. Close used to write its Close frame
// as soon as it won the write lock; every message still queued in
// sendBuffer then got ErrClosed in the pump and was dropped, though
// Write had returned nil for it. The drop raced the pump, so the
// property is checked over many trials and must hold in each.
func TestCloseWritesQueuedMessagesBeforeCloseFrame(t *testing.T) {
	const trials, n = 50, 32
	for trial := range trials {
		rc := &recordConn{}
		c := &WebSocketConn{
			conn:       rc,
			sendBuffer: make(chan wsMessage, n),
			closed:     make(chan struct{}),
			peerClosed: make(chan struct{}),
			config:     WSConfig{CloseTimeout: time.Millisecond},
		}
		go c.writePump()
		for i := range n {
			if err := c.WriteString(fmt.Sprintf("m%d", i)); err != nil {
				t.Fatalf("Write %d: %v", i, err)
			}
		}
		c.Close()
		if err := c.WriteString("late"); err != ErrClosed {
			t.Fatalf("Write after Close = %v, want ErrClosed", err)
		}
		frames := recordedFrames(t, rc)
		if len(frames) != n+1 {
			t.Fatalf("trial %d: peer got %d frames, want %d messages and a Close", trial, len(frames), n)
		}
		for i, f := range frames[:n] {
			if want := fmt.Sprintf("m%d", i); f.opcode != wsopcodeText || string(f.payload) != want {
				t.Fatalf("trial %d: frame %d = 0x%X %q, want text %q", trial, i, f.opcode, f.payload, want)
			}
		}
		if last := frames[n]; last.opcode != wsopcodeClose {
			t.Fatalf("trial %d: last frame opcode 0x%X, want Close", trial, last.opcode)
		}
	}
}

// slowConn takes slow to write each frame, like a peer whose receive
// window opens a little at a time. It records the write deadline each
// write ran under, and honours it unless ignoreDeadline is set.
type slowConn struct {
	slow           time.Duration
	ignoreDeadline bool
	mu             sync.Mutex
	deadline       time.Time
	deadlines      []time.Time
}

func (s *slowConn) Read([]byte) (int, error) { select {} }
func (s *slowConn) Close() error             { return nil }
func (s *slowConn) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	s.deadline = t
	s.mu.Unlock()
	return nil
}
func (s *slowConn) Write(p []byte) (int, error) {
	s.mu.Lock()
	dl := s.deadline
	s.mu.Unlock()
	if !s.ignoreDeadline && !dl.IsZero() && time.Until(dl) < s.slow {
		time.Sleep(max(time.Until(dl), 0))
		return 0, timeoutErr{}
	}
	time.Sleep(s.slow)
	s.mu.Lock()
	s.deadlines = append(s.deadlines, dl)
	s.mu.Unlock()
	return len(p), nil
}

// written returns the deadline of every completed write, in order.
func (s *slowConn) written() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.deadlines...)
}

// closeQueued queues n messages on a pump-less conn over sc, closes it,
// and returns when Close started and how long it took.
func closeQueued(t *testing.T, sc *slowConn, n int, writeTimeout time.Duration) (time.Time, time.Duration) {
	t.Helper()
	c := &WebSocketConn{
		conn:       sc,
		sendBuffer: make(chan wsMessage, n),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     WSConfig{WriteTimeout: writeTimeout, CloseTimeout: time.Millisecond},
	}
	for i := range n {
		if err := c.WriteString(fmt.Sprintf("m%d", i)); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	start := time.Now()
	done := make(chan struct{})
	go func() { c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close never returned")
	}
	return start, time.Since(start)
}

// Property: the drain in Close is bounded by WriteTimeout, so a peer
// that reads slowly cannot hold Close open for as long as the queue is
// deep, and every drained frame carries a write deadline, even when
// WriteTimeout is disabled. Here the queue would take a second to
// write and WriteTimeout is 100ms.
func TestCloseDrainBoundedByWriteTimeout(t *testing.T) {
	const n = 50
	for name, sc := range map[string]*slowConn{
		"peer honours deadlines": {slow: 20 * time.Millisecond},
		// The loop's own bound, for a conn whose deadline never fires.
		"peer ignores deadlines": {slow: 20 * time.Millisecond, ignoreDeadline: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, took := closeQueued(t, sc, n, 100*time.Millisecond)
			// Drain (100ms) + the Close frame's write + CloseTimeout,
			// with slack for a loaded machine.
			if took > 600*time.Millisecond {
				t.Fatalf("Close took %v draining a slow peer; WriteTimeout is 100ms", took)
			}
			if got := len(sc.written()); got == 0 || got > n {
				t.Fatalf("Close wrote %d frames; want some of the %d queued, not all", got, n)
			}
		})
	}
	t.Run("deadline with WriteTimeout disabled", func(t *testing.T) {
		sc := &slowConn{}
		start, _ := closeQueued(t, sc, 3, -1)
		written := sc.written()
		if len(written) != 4 {
			t.Fatalf("wrote %d frames, want 3 messages and a Close", len(written))
		}
		for i, dl := range written[:3] {
			if dl.IsZero() || dl.After(start.Add(defaultWriteTimeout+time.Second)) {
				t.Fatalf("drained frame %d ran under deadline %v; want one within %v of Close", i, dl, defaultWriteTimeout)
			}
		}
	})
}

// Property: a negative SendBuffer means the default, as zero does, and
// is settled before the hijack. It used to reach make(chan) after the
// hijack and panic there, killing the handler and leaking the socket.
func TestUpgradeNegativeSendBufferUsesDefault(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		panicked any
		capacity int
		err      error
	}
	got := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var res result
		defer func() {
			res.panicked = recover()
			got <- res
		}()
		conn, err := Upgrade(w, r, WSConfig{SendBuffer: -1, CloseTimeout: 10 * time.Millisecond})
		res.err = err
		if err == nil {
			res.capacity = cap(conn.sendBuffer)
			conn.Close()
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := "GET /ws HTTP/1.1\r\nHost: " + ln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-got:
		if res.panicked != nil {
			t.Fatalf("Upgrade panicked after hijacking the connection: %v", res.panicked)
		}
		if res.err != nil {
			t.Fatalf("Upgrade: %v", res.err)
		}
		if res.capacity != 32 {
			t.Fatalf("send buffer capacity %d, want the default 32", res.capacity)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler never returned")
	}
}
