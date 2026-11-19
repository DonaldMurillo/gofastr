package fanout

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSweepFanoutSendAfterStopDeliversNothing(t *testing.T) {
	for range 50 {
		var hits atomic.Int32
		send, stop := SubscriberQueue(func([]byte) { hits.Add(1) }, 8)
		stop()
		for range 20 {
			send([]byte("late"))
		}
		time.Sleep(time.Millisecond)
		if n := hits.Load(); n != 0 {
			t.Fatalf("a send after stop was delivered %d times", n)
		}
	}
}

func TestSweepFanoutUnwrapForeignDropsOwnAndMalformed(t *testing.T) {
	if _, _, ok := UnwrapForeign("me", Wrap("me", []byte("x"))); ok {
		t.Fatal("UnwrapForeign accepted an envelope this node sent")
	}
	if _, _, ok := UnwrapForeign("me", []byte(`{"b":"x"}`)); ok {
		t.Fatal("UnwrapForeign accepted an envelope with no node id")
	}
	origin, body, ok := UnwrapForeign("me", Wrap("peer", []byte("x")))
	if !ok || origin != "peer" || string(body) != "x" {
		t.Fatalf("UnwrapForeign(peer) = %q, %q, %v", origin, body, ok)
	}
}

type deadlineFanout struct {
	got chan time.Duration
	buf chan []byte
}

func (d *deadlineFanout) Publish(ctx context.Context, _ string, p []byte) error {
	dl, ok := ctx.Deadline()
	if !ok {
		d.got <- -1
	} else {
		d.got <- time.Until(dl)
	}
	d.buf <- p
	return nil
}

func (d *deadlineFanout) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

func TestSweepFanoutPublishQueueDeadlineAndCopy(t *testing.T) {
	f := &deadlineFanout{got: make(chan time.Duration, 1), buf: make(chan []byte, 1)}
	send, stop := PublishQueue(f, "t", 4)
	defer stop()
	payload := []byte("hello")
	send(payload)
	copy(payload, "XXXXX")
	if left := <-f.got; left <= 4*time.Second || left > 5*time.Second {
		t.Fatalf("publish deadline = %v from now, want 5s", left)
	}
	if got := string(<-f.buf); got != "hello" {
		t.Fatalf("published %q, want the bytes as of send", got)
	}
}
