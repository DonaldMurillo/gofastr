package stream

import (
	"testing"
	"time"
)

// A data write that holds the write lock past PongTimeout (inside its
// WriteTimeout) delays the Ping. The pong clock starts when the Ping is
// on the wire, so a peer that answers promptly after that survives.
func TestPongClockStartsAtPingWrite(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	defer conn.Close()

	// A slow data write holds the lock across the first Ping's turn.
	conn.mu.Lock()
	time.Sleep(200 * time.Millisecond)
	conn.mu.Unlock()

	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	if op, err := readServerFrame(cli); err != nil || op != wsopcodePing {
		t.Fatalf("first server frame = %v, %v; want a Ping", op, err)
	}
	time.Sleep(20 * time.Millisecond)
	select {
	case <-conn.Closed():
		t.Fatal("connection closed 20ms after its Ping went out, inside PongTimeout")
	default:
	}
	if _, err := cli.Write([]byte{0x80 | wsopcodePong, 0x80, 0xa, 0xb, 0xc, 0xd}); err != nil {
		t.Fatalf("peer pong: %v", err)
	}
}
