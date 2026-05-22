package stream

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// blockingClose is an io.ReadWriteCloser whose Close blocks until release
// is signalled. Used to verify Hub.Stop does not hold its lock across
// per-conn Close calls (finding 9).
type blockingClose struct {
	closed  chan struct{}
	release chan struct{}
}

func (b *blockingClose) Read(p []byte) (int, error)  { return 0, io.EOF }
func (b *blockingClose) Write(p []byte) (int, error) { return len(p), nil }
func (b *blockingClose) Close() error {
	<-b.release
	close(b.closed)
	return nil
}

// Finding 9: Stop must not serialise per-conn Close while holding the lock.
func TestHubStopFastWithSlowClose(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	release := make(chan struct{})
	defer close(release)

	// One slow-closing conn
	slow := &WebSocketConn{
		conn:       &blockingClose{closed: make(chan struct{}), release: release},
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		config:     WSConfig{},
	}
	hub.Register(slow)

	// 99 normal conns
	for i := 0; i < 99; i++ {
		conn := newTestConn()
		go conn.writePump()
		hub.Register(conn)
	}

	// Wait for registration
	deadline := time.Now().Add(time.Second)
	for hub.Count() < 100 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/100 registered", hub.Count())
		}
		time.Sleep(5 * time.Millisecond)
	}

	stopDone := make(chan struct{})
	go func() {
		hub.Stop()
		close(stopDone)
	}()

	// Stop should not block on slow.Close — it should return promptly.
	select {
	case <-stopDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hub.Stop blocked on slow conn close")
	}
}

// Finding 10: Register racing with Stop must not deadlock the caller.
func TestHubRegisterRaceWithStop(t *testing.T) {
	for trial := 0; trial < 20; trial++ {
		hub := NewHub()
		go hub.Run()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := newTestConn()
			go conn.writePump()
			hub.Register(conn)
		}()

		// Stop concurrently
		hub.Stop()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("trial %d: Register deadlocked vs Stop", trial)
		}
	}
}

// Finding 18: simultaneous close of many conns must all be unregistered
// even while broadcast is hot.
func TestHubAllUnregisterOnMassClose(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	const N = 10
	conns := make([]*WebSocketConn, N)
	for i := 0; i < N; i++ {
		c := newTestConn()
		go c.writePump()
		hub.Register(c)
		conns[i] = c
	}

	deadline := time.Now().Add(time.Second)
	for hub.Count() < N {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d registered", hub.Count(), N)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stream broadcasts while closing all conns
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				hub.Broadcast([]byte("x"))
			}
		}
	}()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(cc *WebSocketConn) {
			defer wg.Done()
			cc.Close()
		}(c)
	}
	wg.Wait()
	close(stop)

	// All conns should be unregistered eventually
	dl := time.Now().Add(time.Second)
	for hub.Count() != 0 {
		if time.Now().After(dl) {
			t.Fatalf("Count after all-close = %d, want 0", hub.Count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ensure unused import safety
var _ = bytes.NewReader
