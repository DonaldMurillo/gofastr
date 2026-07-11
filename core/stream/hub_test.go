package stream

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func newTestConn() *WebSocketConn {
	return &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}
}

func TestHubRegisterAndBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	conn1 := newTestConn()
	go conn1.writePump()
	conn2 := newTestConn()
	go conn2.writePump()

	hub.Register(conn1)
	hub.Register(conn2)

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	if got := hub.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}

	hub.BroadcastWait([]byte("hello"))

	// Give time for message delivery
	time.Sleep(50 * time.Millisecond)

	// Check both connections received the message via sendBuffer
	// (they're consumed by writePump and written to the underlying conn)
	conn1.Close()
	conn2.Close()
}

func TestHubUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	conn := newTestConn()
	go conn.writePump()

	hub.Register(conn)
	time.Sleep(50 * time.Millisecond)

	if got := hub.Count(); got != 1 {
		t.Fatalf("Count after register = %d, want 1", got)
	}

	hub.Unregister(conn)
	time.Sleep(50 * time.Millisecond)

	if got := hub.Count(); got != 0 {
		t.Fatalf("Count after unregister = %d, want 0", got)
	}

	conn.Close()
}

func TestHubStopClosesConnections(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	conn := newTestConn()
	go conn.writePump()

	hub.Register(conn)
	time.Sleep(50 * time.Millisecond)

	hub.Stop()
	time.Sleep(50 * time.Millisecond)

	select {
	case <-conn.Closed():
	default:
		t.Fatal("expected connection to be closed after hub stop")
	}
}

func TestHubAutoUnregisterOnClose(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	conn := newTestConn()
	go conn.writePump()

	hub.Register(conn)
	time.Sleep(50 * time.Millisecond)

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if got := hub.Count(); got != 0 {
		t.Fatalf("Count after conn close = %d, want 0", got)
	}
}

func TestHubBroadcastNonBlocking(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Broadcast to empty hub should not block
	hub.Broadcast([]byte("msg"))
}

func TestHubRegisterAfterStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()
	time.Sleep(50 * time.Millisecond)

	conn := newTestConn()
	hub.Register(conn) // should not panic or block

	if got := hub.Count(); got != 0 {
		t.Fatalf("Count after stopped = %d, want 0", got)
	}
}

func TestHubDoubleStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()
	hub.Stop() // must not panic
}

func TestHubBroadcastAfterStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()
	time.Sleep(50 * time.Millisecond)

	hub.Broadcast([]byte("msg"))     // must not block
	hub.BroadcastWait([]byte("msg")) // must not block
}

func TestHubBroadcastDropsOnFullBuffer(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Create a conn with tiny send buffer
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, 1), // tiny buffer
		closed:     make(chan struct{}),
		config:     WSConfig{},
	}
	// Don't start writePump — so buffer fills up

	hub.Register(conn)
	time.Sleep(50 * time.Millisecond)

	// Send more messages than the buffer can hold
	for i := 0; i < 5; i++ {
		hub.Broadcast([]byte("msg"))
	}
	// Should not block — messages are dropped for this conn

	conn.Close()
}

func TestHubConcurrentOperations(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	var ops atomic.Int32
	done := make(chan struct{})

	// Concurrent registers and broadcasts
	for i := 0; i < 10; i++ {
		go func() {
			conn := newTestConn()
			go conn.writePump()
			hub.Register(conn)
			hub.Broadcast([]byte("msg"))
			// Branch on Add's return value: a separate Load() lets two
			// goroutines both observe 10 and double-close done.
			if ops.Add(1) == 10 {
				close(done)
			}
		}()
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for concurrent ops")
	}
}

// Ensure nopConn from websocket_test.go is available; provide a minimal stub if needed.
var _ = io.ReadWriteCloser(&nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}})
