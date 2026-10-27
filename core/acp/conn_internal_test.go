package acp

import (
	"context"
	"io"
	"testing"
	"time"
)

// conn.close must leave c.closed non-nil and closed. Client.RequestPermission
// selects on that channel without holding c.mu, so setting it to nil both
// raced that read and killed the arm - a select on a nil channel never fires -
// leaving a permission request issued after teardown blocked until its own
// context expired instead of returning "connection closed".
func TestConnCloseKeepsTeardownArmLive(t *testing.T) {
	c := newConn(io.Discard)
	c.close()
	c.close() // idempotent: a second teardown must not panic on a closed channel

	if c.closed == nil {
		t.Fatal("closed channel was set to nil; the teardown select arm is dead")
	}
	select {
	case <-c.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("teardown arm never fired; a permission request after close would hang")
	}
}

// The teardown signal must be observable from another goroutine while close
// runs, which is the shape the race detector flags if closed is written
// without synchronisation. Run with -race.
func TestConnCloseIsRaceFreeUnderReaders(t *testing.T) {
	c := newConn(io.Discard)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			select {
			case <-c.closed:
			case <-time.After(2 * time.Second):
			}
			done <- struct{}{}
		}()
	}
	c.close()
	for i := 0; i < 8; i++ {
		<-done
	}
}

// A Client an embedder kept past Serve's return writes nothing: the
// caller owns out again, and a write to an output nobody reads any more
// blocked forever, past RequestPermission's own deadline.
func TestClientAfterCloseWritesNothing(t *testing.T) {
	r, w := io.Pipe() // nothing reads r: any write blocks
	defer r.Close()
	c := newConn(w)
	c.close()
	client := &Client{conn: c, sessionID: "s1"}
	errs := make(chan error, 2)
	go func() { errs <- client.Update(AgentMessageChunk("m1", "late")) }()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil)
		errs <- err
	}()
	for range 2 {
		select {
		case err := <-errs:
			if err == nil {
				t.Error("a write after close succeeded")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a Client call after close blocked writing to an unread output")
		}
	}
}
