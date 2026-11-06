package stream

import (
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestDisabledPongUsesIdleCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, client := net.Pipe()
		conn := &WebSocketConn{
			conn: server, sendBuffer: make(chan wsMessage, 1),
			closed: make(chan struct{}), peerClosed: make(chan struct{}),
			config: WSConfig{ReadIdleTimeout: time.Second, PongTimeout: -1, WriteTimeout: time.Second},
		}
		go io.Copy(io.Discard, client)
		go conn.writePump()
		conn.startKeepalive()
		time.Sleep(100 * time.Millisecond)
		conn.lastReadActivity.Store(time.Now().UnixNano())
		time.Sleep(1010 * time.Millisecond)
		if conn.pingSentAt.Load() != 0 {
			t.Error("keepalive pinged between idle cadence boundaries")
		}
		conn.Close()
		client.Close()
	})
}
