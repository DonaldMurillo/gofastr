package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Finding 6: cross-origin upgrade should be rejected (CSWSH protection).
// We can't actually upgrade against httptest because hijack isn't supported,
// but we can assert that the Origin check runs *before* the hijack step.
func TestWSRejectsCrossOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	r.Header.Set("Origin", "https://evil.example")
	r.Host = "example.com"
	w := httptest.NewRecorder()

	_, err := Upgrade(w, r, WSConfig{})
	if err == nil {
		t.Fatal("cross-origin upgrade unexpectedly allowed")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "origin") {
		t.Fatalf("expected origin error, got %v", err)
	}
}

// CheckOrigin override should permit cross-origin when explicitly opted in.
func TestWSCheckOriginOverride(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	r.Header.Set("Origin", "https://allowed.example")
	r.Host = "example.com"
	w := httptest.NewRecorder()

	called := false
	_, err := Upgrade(w, r, WSConfig{
		CheckOrigin: func(_ *http.Request) bool { called = true; return true },
	})
	// Will fail on hijack (httptest doesn't support it), but origin must
	// have been consulted first.
	if !called {
		t.Fatal("CheckOrigin not invoked")
	}
	if err == nil || !strings.Contains(err.Error(), "hijack") {
		// any error after the origin check passes is fine, but we must
		// not see an "origin" error
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "origin") {
			t.Fatalf("origin still rejected after override: %v", err)
		}
	}
}

// Finding 7a: slow writer must not pin the writePump forever — a write
// deadline must apply so the goroutine returns within a bounded time.
func TestWSSlowWriterReturns(t *testing.T) {
	bw := &blockingWriter{block: make(chan struct{})}
	conn := &WebSocketConn{
		conn:       bw,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		config:     WSConfig{WriteTimeout: 100 * time.Millisecond, ReadLimit: 1 << 20},
	}

	pumpDone := make(chan struct{})
	go func() {
		conn.writePump()
		close(pumpDone)
	}()

	_ = conn.Write([]byte("blocked"))
	// The pump should error out via the deadline, close, and return.
	select {
	case <-pumpDone:
	case <-time.After(2 * time.Second):
		close(bw.block)
		t.Fatal("writePump never returned despite write deadline")
	}
	close(bw.block)
}

// Finding 7b: ping flood must NOT recurse and blow the stack.
func TestWSPingFloodNoStackOverflow(t *testing.T) {
	// Build N back-to-back masked ping frames (server is connected as
	// a "client" for our purposes here, but mask=1 is what real clients
	// must send).
	const N = 5000
	var frame bytes.Buffer
	mask := [4]byte{0x01, 0x02, 0x03, 0x04}
	for i := 0; i < N; i++ {
		frame.WriteByte(0x80 | wsopcodePing)
		frame.WriteByte(0x80 | 0) // masked, length 0
		frame.Write(mask[:])
	}
	// Final text frame to trigger return
	payload := []byte("done")
	frame.WriteByte(0x80 | wsopcodeText)
	frame.WriteByte(0x80 | byte(len(payload)))
	frame.Write(mask[:])
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame.Bytes()), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}

	got, err := conn.readFrame()
	if err != nil {
		t.Fatalf("readFrame after ping flood: %v", err)
	}
	if string(got.payload) != "done" {
		t.Fatalf("payload = %q, want %q", got.payload, "done")
	}
}

// Finding 7c: 64-bit length with top bit set must be treated as oversize,
// not silently turned negative.
func TestWSHugeLengthRejected(t *testing.T) {
	mask := [4]byte{0xaa, 0xbb, 0xcc, 0xdd}
	var frame bytes.Buffer
	frame.WriteByte(0x80 | wsopcodeText)
	frame.WriteByte(0x80 | 127)
	lenBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBuf, 0xFFFFFFFFFFFFFFFF)
	frame.Write(lenBuf)
	frame.Write(mask[:])

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame.Bytes()), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}

	_, err := conn.readFrame()
	if err == nil {
		t.Fatal("expected error for oversize 64-bit length, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Finding 13a: control frames must be <=125 bytes per RFC 6455.
func TestWSOversizeControlFrameRejected(t *testing.T) {
	mask := [4]byte{0x01, 0x02, 0x03, 0x04}
	// 126-byte ping frame (using extended length) — invalid for control frames
	var frame bytes.Buffer
	frame.WriteByte(0x80 | wsopcodePing)
	frame.WriteByte(0x80 | 126)
	frame.Write([]byte{0x00, 0x7E}) // 126 bytes
	frame.Write(mask[:])
	frame.Write(make([]byte, 126))

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame.Bytes()), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}

	_, err := conn.readFrame()
	if err == nil {
		t.Fatal("expected protocol error for oversized control frame")
	}
}

// Finding 13b: unmasked client frames must be rejected per RFC 6455.
func TestWSUnmaskedClientFrameRejected(t *testing.T) {
	// Build an unmasked text frame "hello" — client MUST mask
	payload := []byte("hello")
	var frame bytes.Buffer
	frame.WriteByte(0x80 | wsopcodeText)
	frame.WriteByte(byte(len(payload))) // mask bit not set
	frame.Write(payload)

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame.Bytes()), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20, requireMask: true},
	}

	_, err := conn.readFrame()
	if err == nil {
		t.Fatal("expected error for unmasked client frame")
	}
}

// blockingWriter blocks Write until block is closed.
type blockingWriter struct {
	mu       sync.Mutex
	block    chan struct{}
	deadline time.Time
}

func (b *blockingWriter) Read(_ []byte) (int, error) { return 0, io.EOF }
func (b *blockingWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	dl := b.deadline
	b.mu.Unlock()
	if !dl.IsZero() {
		select {
		case <-b.block:
			return len(p), nil
		case <-time.After(time.Until(dl)):
			return 0, &timeoutErr{}
		}
	}
	select {
	case <-b.block:
		return len(p), nil
	case <-time.After(5 * time.Second):
		return 0, io.ErrUnexpectedEOF
	}
}
func (b *blockingWriter) Close() error { return nil }
func (b *blockingWriter) SetWriteDeadline(t time.Time) error {
	b.mu.Lock()
	b.deadline = t
	b.mu.Unlock()
	return nil
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
