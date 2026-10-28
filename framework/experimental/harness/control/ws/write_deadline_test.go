package ws

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

// A client that stops reading wedged the event pump: the frame write had
// no deadline, and net/http no longer manages a hijacked connection
// (2026-10-27 bug bash, nowritedeadline). The write times out and the
// pump leaves.
func TestEventPumpLeavesStalledClient(t *testing.T) {
	prev := wsFrameWriteTimeout
	wsFrameWriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { wsFrameWriteTimeout = prev })

	server, client := net.Pipe() // client never reads
	defer server.Close()
	defer client.Close()
	c := &Conn{netConn: server, writer: bufio.NewWriter(server)}
	ch := make(chan control.EventEnvelope, 1)
	ch <- control.EventEnvelope{ID: 1, Kind: "turn"}
	done := make(chan struct{})
	go func() { c.eventPump(context.Background(), ch); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event pump still blocked on a client that stopped reading")
	}
}
