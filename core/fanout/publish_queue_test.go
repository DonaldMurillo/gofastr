package fanout

import (
	"context"
	"errors"
	"sync"
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
	f := newStalledFanout() // never released: Publish blocks until ctx timeout
	send, stop := PublishQueue(f, "t", 4)
	defer stop()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
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
