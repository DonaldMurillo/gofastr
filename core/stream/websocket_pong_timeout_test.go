package stream

import (
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestNegativePongTimeoutDisablesPongExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, cli := net.Pipe()
		conn := &WebSocketConn{
			conn:       srv,
			sendBuffer: make(chan []byte, 1),
			closed:     make(chan struct{}),
			peerClosed: make(chan struct{}),
			config: WSConfig{
				ReadIdleTimeout: 10 * time.Millisecond,
				PongTimeout:     -1,
				WriteTimeout:    time.Second,
			},
		}
		go io.Copy(io.Discard, cli) // consume pings without returning pongs
		go conn.writePump()
		conn.startKeepalive()

		// Advance past the old hard-coded 10-second fallback. A negative
		// timeout explicitly disables closing when a Pong does not arrive.
		time.Sleep(11 * time.Second)
		select {
		case <-conn.Closed():
			t.Fatal("negative PongTimeout still expired the connection")
		default:
		}
		conn.Close()
		cli.Close()
	})
}
