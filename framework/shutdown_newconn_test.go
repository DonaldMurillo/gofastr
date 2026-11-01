package framework

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// Shutdown closed the connections sitting in StateNew, then waited on
// the WebSocket close handshakes, then called Server.Shutdown. The
// listener was still accepting during that wait, so a connection that
// arrived then sat in StateNew and Server.Shutdown spared it for five
// seconds (go.dev/issue/22682), the stall the first close removed.
func TestShutdownClosesNewConnAcceptedDuringStreamEnd(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	returned := make(chan struct{})
	a.Router().Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		// The peer below never answers the close frame, so the server
		// waits out this timeout inside endAll.
		c, err := stream.Upgrade(w, r, stream.WSConfig{
			CheckOrigin:  func(*http.Request) bool { return true },
			CloseTimeout: 1500 * time.Millisecond,
		})
		if err != nil {
			return
		}
		go func() {
			for {
				if _, err := c.Read(); err != nil {
					return
				}
			}
		}()
	}))
	addr, done := startStreamApp(t, a)
	conn, br := wsHandshake(t, addr, "/ws")
	<-returned
	settleAfterHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	shut := make(chan error, 1)
	go func() { shut <- a.Shutdown(ctx) }()

	// The close frame means endAll is running and waiting on this peer.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0]&0x0f != 0x8 {
		t.Fatalf("no close frame from the drain: % x, %v", hdr, err)
	}
	late, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("the listener was already closed during endAll (%v); the test cannot reach the window", err)
	}
	defer late.Close()

	if err := <-shut; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if took := time.Since(started); took > 3500*time.Millisecond {
		t.Fatalf("Shutdown took %v; the drain waited on a connection accepted during the stream end", took)
	}
	_ = late.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := late.Read(make([]byte, 1)); err == nil {
		t.Fatal("the late connection is still open after Shutdown")
	}
	<-done
}
