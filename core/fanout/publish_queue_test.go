package fanout

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stalledFanout blocks Publish until released; Subscribe is a no-op.
type stalledFanout struct {
	release chan struct{}
	mu      sync.Mutex
	got     [][]byte
}

func newStalledFanout() *stalledFanout {
	return &stalledFanout{release: make(chan struct{})}
}

func (s *stalledFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.got = append(s.got, append([]byte(nil), payload...))
	s.mu.Unlock()
	return nil
}

func (s *stalledFanout) Subscribe(string, func([]byte)) (func(), error) {
	return func() {}, nil
}

func TestPublishQueueSendNeverBlocks(t *testing.T) {
	f := newStalledFanout() // released only at the end: Publish blocks
	send, stop := PublishQueue(f, "t", 4)
	defer func() {
		close(f.release) // stop waits for the publish in flight
		stop()
	}()

	done := make(chan struct{})
	go func() {
		for range 100 {
			send([]byte("x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send blocked on a stalled backend")
	}
}

func TestPublishQueueDelivers(t *testing.T) {
	f := newStalledFanout()
	close(f.release) // healthy backend
	send, stop := PublishQueue(f, "t", 8)
	defer stop()

	send([]byte("a"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.got)
		f.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("queued payload never published")
}

// errFanout fails every Publish; PublishQueue must swallow (log) the error
// and keep going rather than wedging or panicking.
type errFanout struct{ calls sync.WaitGroup }

func (e *errFanout) Publish(context.Context, string, []byte) error {
	e.calls.Done()
	return errors.New("boom")
}
func (e *errFanout) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

func TestPublishQueueSurvivesErrors(t *testing.T) {
	f := &errFanout{}
	f.calls.Add(3)
	send, stop := PublishQueue(f, "t", 8)
	defer stop()
	send([]byte("1"))
	send([]byte("2"))
	send([]byte("3"))
	done := make(chan struct{})
	go func() { f.calls.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish attempts stopped after an error")
	}
}

// stop returns only after the publish in flight has finished, so a
// goodbye the caller publishes next is the last message on the lane: a
// roster still in flight landed after it and restored the departed
// members on every peer until their TTL.
func TestStopWaitsForInflightPublish(t *testing.T) {
	f := newStalledFanout()
	send, stop := PublishQueue(f, "t", 4)
	send([]byte("roster"))
	// Let the drainer take the payload into Publish.
	time.Sleep(50 * time.Millisecond)
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop returned while a publish was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(f.release)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not return after the publish finished")
	}
	f.mu.Lock()
	n := len(f.got)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("published %d messages, want the one in flight", n)
	}
	send([]byte("late"))
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	n = len(f.got)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("a send after stop published (%d messages)", n)
	}
}

// firstHeld holds the first Publish until release closes and counts
// every Publish that starts.
type firstHeld struct {
	entered chan struct{}
	release chan struct{}
	starts  atomic.Int32
}

func (f *firstHeld) Publish(context.Context, string, []byte) error {
	if f.starts.Add(1) == 1 {
		close(f.entered)
		<-f.release
	}
	return nil
}

func (f *firstHeld) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

// The drainer can pick a queued payload in the same instant stop closes
// the queue. No publish may start once stop has returned: one that did
// landed after the caller's goodbye. The race is the scheduler's, so the
// test runs it many times.
func TestNoPublishStartsAfterStop(t *testing.T) {
	for range 200 {
		f := &firstHeld{entered: make(chan struct{}), release: make(chan struct{})}
		send, stop := PublishQueue(f, "t", 8)
		for range 6 {
			send([]byte("x"))
		}
		<-f.entered
		stopped := make(chan struct{})
		go func() { stop(); close(stopped) }()
		close(f.release)
		<-stopped
		n := f.starts.Load()
		time.Sleep(time.Millisecond)
		if got := f.starts.Load(); got != n {
			t.Fatalf("%d publishes started after stop returned", got-n)
		}
	}
}
