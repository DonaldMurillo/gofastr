package mcp

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// TestServeStdioReturnsOnCancelWhileIdle: a client that sends nothing
// left ServeStdio parked in the line read, so a SIGTERM that cancelled
// ctx did nothing until the supervisor's SIGKILL. The context ends the
// session even with no line pending.
func TestServeStdioReturnsOnCancelWhileIdle(t *testing.T) {
	in, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewServer().ServeStdio(ctx, in, io.Discard) }()

	time.Sleep(20 * time.Millisecond) // let it park in the read
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ServeStdio = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeStdio still blocked on silent input after its context ended")
	}
}
