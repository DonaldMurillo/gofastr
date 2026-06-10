package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestComputeAcceptKey(t *testing.T) {
	// RFC 6455 example
	got := computeAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Fatalf("computeAcceptKey = %q, want %q", got, want)
	}
}

func TestWebSocketConnWriteRead(t *testing.T) {
	buf := &bytes.Buffer{}
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: buf},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}
	go conn.writePump()

	if err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := conn.WriteString("world"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	// Give writePump time to drain
	time.Sleep(50 * time.Millisecond)

	conn.Close()

	written := buf.String()
	if !strings.Contains(written, "hello") || !strings.Contains(written, "world") {
		t.Fatalf("expected written output to contain messages, got %q", written)
	}
}

func TestWebSocketConnClose(t *testing.T) {
	var callbackRan int32
	cb := func() { atomic.StoreInt32(&callbackRan, 1) }
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		onClose:    []func(){cb},
	}
	go conn.writePump()

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close again — must not panic
	if err := conn.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Verify the closed channel is signaled
	select {
	case <-conn.Closed():
	case <-time.After(time.Second):
		t.Fatal("closed channel not signaled")
	}

	// OnClose runs in a goroutine — eventually fires
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&callbackRan) == 0 {
		select {
		case <-deadline:
			t.Fatal("OnClose callback not called")
		default:
			runtime.Gosched()
		}
	}
}

func TestWebSocketConnWriteAfterClose(t *testing.T) {
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{},
	}
	go conn.writePump()
	conn.Close()

	if err := conn.Write([]byte("x")); err != ErrClosed {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestWebSocketClosedChannel(t *testing.T) {
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
	}
	select {
	case <-conn.Closed():
		t.Fatal("should not be closed yet")
	default:
	}
	conn.Close()
	select {
	case <-conn.Closed():
	default:
		t.Fatal("should be closed now")
	}
}

func TestWriteFrameFormatsCorrectly(t *testing.T) {
	buf := &bytes.Buffer{}
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: buf},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
	}

	if err := conn.writeFrame(wsopcodeText, []byte("hi")); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	data := buf.Bytes()
	if got := data[0] & 0x0F; got != wsopcodeText {
		t.Fatalf("opcode = %d, want %d", got, wsopcodeText)
	}
	if data[0]&0x80 == 0 {
		t.Fatal("expected FIN bit set")
	}
	if data[1] != 2 {
		t.Fatalf("payload length = %d, want 2", data[1])
	}
}

func TestWriteFrameExtendedLength16(t *testing.T) {
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = 'A'
	}
	buf := &bytes.Buffer{}
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: buf},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
	}

	if err := conn.writeFrame(wsopcodeText, payload); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	data := buf.Bytes()
	if data[1] != 126 {
		t.Fatalf("expected 126 marker for 16-bit length, got %d", data[1])
	}
	length := binary.BigEndian.Uint16(data[2:4])
	if length != 200 {
		t.Fatalf("extended length = %d, want 200", length)
	}
}

func TestReadFrameSmallMessage(t *testing.T) {
	// Build a masked text frame: "hello"
	payload := []byte("hello")
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}

	frame := []byte{
		0x81,                      // FIN + text opcode
		byte(0x80 | len(payload)), // masked + length
	}
	frame = append(frame, mask[:]...)
	frame = append(frame, masked...)

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}

	f, err := conn.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if string(f.payload) != "hello" {
		t.Fatalf("payload = %q, want %q", f.payload, "hello")
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	// Build a frame claiming 2MB payload
	frame := []byte{
		0x81,       // FIN + text opcode
		0x80 | 127, // masked + 64-bit length
	}
	lenBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBytes, 2<<20) // 2MB
	frame = append(frame, lenBytes...)
	frame = append(frame, make([]byte, 4)...) // mask key
	// No actual payload data — readFrame will fail on io.ReadFull

	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame), w: io.Discard},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20}, // 1MB limit
	}

	_, err := conn.readFrame()
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type nopConn struct {
	r io.Reader
	w io.Writer
}

func (n *nopConn) Read(p []byte) (int, error)  { return n.r.Read(p) }
func (n *nopConn) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n *nopConn) Close() error                { return nil }
