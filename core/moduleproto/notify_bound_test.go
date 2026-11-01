package moduleproto

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// Notifications to a counterparty that stopped reading each parked a
// writer goroutine on the codec, with no bound: a Notify whose ctx ended
// returned, its write did not. Pending notification writes now count
// against the inflight cap, and a Notify past it is refused at once.
func TestNotifyWritesAreBounded(t *testing.T) {
	connX, connY := net.Pipe()
	defer connX.Close()
	defer connY.Close()
	codec, err := NewCodec(connX, connX, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPeer(codec, RoleHost, WithMaxInflight(2))
	notify := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		return p.Notify(ctx, "note", nil)
	}
	for i := range 2 {
		if err := notify(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("notify %d to a stalled reader: %v, want DeadlineExceeded", i, err)
		}
	}
	if err := notify(); !errors.Is(err, ErrInflightCap) {
		t.Fatalf("notify past the cap: %v, want ErrInflightCap", err)
	}
	// The reader comes back: the parked writes finish and free their slots.
	go func() { _, _ = io.Copy(io.Discard, connY) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := notify()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("notify after the reader resumed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A Call whose ctx already ended allocates no id: onID would arm a cancel
// watcher (one more notification) for a frame that is never written.
func TestCallDoneCtxAllocatesNoID(t *testing.T) {
	host, _, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := host.CallWithID(ctx, "anything", nil, func(uint64) { called = true })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("onID ran for a Call whose ctx had already ended")
	}
}
