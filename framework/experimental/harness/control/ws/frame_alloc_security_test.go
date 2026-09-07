package ws

// Pins: memory tracks bytes actually delivered — an inbound frame's
// Pinned sibling: core/stream/websocket.go::readFrame :853-861 states the
// property verbatim as the grown-reader contract — "Do NOT allocate
// `length` up front: the peer declares it in a handful of header bytes ...
// Grow while reading instead, so memory tracks bytes actually delivered.
// A read deadline bounds the stall." The harness control-plane ws
// transport never adopted it.
// Property: memory tracks bytes actually delivered — an inbound frame's
// payload make() is never sized by the header-claimed length before the
// payload bytes arrive, and a stalled half-sent frame is bounded by a read
// deadline.
// Surfaces: ws.go::readFrame :471 — `payload = make([]byte, length)` sizes
// the allocation from the 8-byte extended header (cap 16 MiB) BEFORE
// io.ReadFull delivers anything, with no read deadline on the stall.
// Probe-verified: 10 wire bytes claiming a 16 MiB frame pin
// 16,778,200 bytes of TotalAlloc with zero payload delivered.
// Finding: an authenticated-but-hostile control client (the socket is the
// harness's remote control surface) pins 16 MiB of heap per connection for
// the life of the stall using 10 bytes — multiply by reconnects and the
// harness process OOMs without a single payload byte sent.
// Fix direction: port the core/stream shape — chunked growth
// (make cap min(claimed, chunk), append while reading) plus a read
// deadline around the payload read so a half-sent frame can't hold its
// buffer indefinitely.

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestFrameAllocRedTracksDeliveredBytes serves a 10-byte header claiming a
// 16 MiB frame, then blocks: zero payload bytes delivered. readFrame's
// TotalAlloc delta from that point must stay under 1 MiB (memory tracks
// delivered bytes, not claimed bytes). Today the eager make() pins the full
// 16 MiB the moment the header lands.
func TestFrameAllocRedTracksDeliveredBytes(t *testing.T) {
	fake := newFrameAllocRedConn()
	conn := &Conn{netConn: fake, reader: bufio.NewReader(fake)}
	done := make(chan struct{})
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	go func() {
		defer close(done)
		_, _, _ = conn.readFrame()
	}()

	// Wait until the blocking read (the one after the header was fully
	// consumed and the payload buffer was sized) is entered.
	select {
	case <-fake.entered:
	case <-time.After(2 * time.Second):
		fake.Close()
		t.Fatal("setup broken: readFrame never reached the blocked payload read")
	}
	time.Sleep(50 * time.Millisecond) // let the eager make() land, if any
	runtime.ReadMemStats(&m1)

	delta := m1.TotalAlloc - m0.TotalAlloc
	if delta > 1<<20 {
		t.Errorf("SECURITY: [ws-eager-frame-alloc] readFrame allocated %d bytes after a 10-byte wire header claiming 16 MiB with ZERO payload delivered (bound: 1 MiB) — ws.go::readFrame :471 sizes payload = make([]byte, length) from the header before io.ReadFull and with no read deadline; core/stream readFrame :853-861 pins the grown-reader contract ('memory tracks bytes actually delivered')", delta)
	}

	// Bounded cleanup: unblock the read and join the goroutine.
	fake.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("setup broken: readFrame goroutine did not return after close")
	}
}

// frameAllocRedConn is a net.Conn whose first Read serves a 10-byte
// RFC 6455 header (FIN+text, 64-bit length) claiming exactly 16 MiB —
// the largest length readFrame admits — and whose subsequent Reads signal
// entered and block until Close.
type frameAllocRedConn struct {
	entered    chan struct{}
	enteredOne sync.Once
	closeCh    chan struct{}
	closeOne   sync.Once
	served     bool
}

func newFrameAllocRedConn() *frameAllocRedConn {
	return &frameAllocRedConn{
		entered: make(chan struct{}),
		closeCh: make(chan struct{}),
	}
}

func (c *frameAllocRedConn) Read(p []byte) (int, error) {
	if !c.served {
		c.served = true
		hdr := []byte{0x81, 0x7F, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(hdr[2:], 16<<20)
		return copy(p, hdr), nil
	}
	c.enteredOne.Do(func() { close(c.entered) })
	<-c.closeCh
	return 0, io.EOF
}

func (c *frameAllocRedConn) Write(p []byte) (int, error) { return len(p), nil }

func (c *frameAllocRedConn) Close() error {
	c.closeOne.Do(func() { close(c.closeCh) })
	return nil
}

func (c *frameAllocRedConn) LocalAddr() net.Addr  { return frameAllocRedAddr{} }
func (c *frameAllocRedConn) RemoteAddr() net.Addr { return frameAllocRedAddr{} }

func (c *frameAllocRedConn) SetDeadline(time.Time) error      { return nil }
func (c *frameAllocRedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *frameAllocRedConn) SetWriteDeadline(time.Time) error { return nil }

type frameAllocRedAddr struct{}

func (frameAllocRedAddr) Network() string { return "tcp" }
func (frameAllocRedAddr) String() string  { return "frame-alloc-red" }
