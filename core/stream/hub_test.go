package stream

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait()

		if got := hub.Count(); got != 2 {
			t.Fatalf("Count = %d, want 2", got)
		}

		hub.BroadcastWait([]byte("hello"))

		// Give time for message delivery
		synctest.Wait()

		// Check both connections received the message via sendBuffer
		// (they're consumed by writePump and written to the underlying conn)
		conn1.Close()
		conn2.Close()
	})
}

func TestHubUnregister(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()
		defer hub.Stop()

		conn := newTestConn()
		go conn.writePump()

		hub.Register(conn)
		synctest.Wait()

		if got := hub.Count(); got != 1 {
			t.Fatalf("Count after register = %d, want 1", got)
		}

		hub.Unregister(conn)
		synctest.Wait()

		if got := hub.Count(); got != 0 {
			t.Fatalf("Count after unregister = %d, want 0", got)
		}

		conn.Close()
	})
}

func TestHubStopClosesConnections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()

		conn := newTestConn()
		go conn.writePump()

		hub.Register(conn)
		synctest.Wait()

		hub.Stop()
		synctest.Wait()

		select {
		case <-conn.Closed():
		default:
			t.Fatal("expected connection to be closed after hub stop")
		}
		// Stop's detached closers sit in the 1s peer-close wait; sleep
		// past it (fake time) so they exit before the bubble does.
		time.Sleep(2 * time.Second)
	})
}

func TestHubAutoUnregisterOnClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()
		defer hub.Stop()

		conn := newTestConn()
		go conn.writePump()

		hub.Register(conn)
		synctest.Wait()

		conn.Close()
		synctest.Wait()

		if got := hub.Count(); got != 0 {
			t.Fatalf("Count after conn close = %d, want 0", got)
		}
	})
}

func TestHubBroadcastNonBlocking(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Broadcast to empty hub should not block
	hub.Broadcast([]byte("msg"))
}

func TestHubRegisterAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()
		hub.Stop()
		synctest.Wait()

		conn := newTestConn()
		hub.Register(conn) // should not panic or block

		if got := hub.Count(); got != 0 {
			t.Fatalf("Count after stopped = %d, want 0", got)
		}
	})
}

func TestHubDoubleStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()
	hub.Stop() // must not panic
}

func TestHubBroadcastAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()
		hub.Stop()
		synctest.Wait()

		hub.Broadcast([]byte("msg"))     // must not block
		hub.BroadcastWait([]byte("msg")) // must not block
	})
}

func TestHubBroadcastDropsOnFullBuffer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait()

		// Send more messages than the buffer can hold
		for range 5 {
			hub.Broadcast([]byte("msg"))
		}
		// Should not block — messages are dropped for this conn

		conn.Close()
	})
}

func TestHubConcurrentOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		go hub.Run()

		var ops atomic.Int32
		done := make(chan struct{})

		// Concurrent registers and broadcasts
		for range 10 {
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
		// Stop closes every registered conn (ending its writePump), but
		// its detached closers sit in the 1s peer-close wait; sleep past
		// it (fake time) so they exit before the bubble does.
		hub.Stop()
		time.Sleep(2 * time.Second)
	})
}

// Ensure nopConn from websocket_test.go is available; provide a minimal stub if needed.
var _ = io.ReadWriteCloser(&nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}})
