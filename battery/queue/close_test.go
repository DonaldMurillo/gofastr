package queue

import (
	"context"
	"testing"
	"time"
)

// Close waits on q.stopped, and only Start's goroutine ever closes it. A queue
// that was constructed and abandoned — a startup sequence that failed after
// NewDBQueue, a test that only exercised Enqueue — therefore hung forever on
// shutdown instead of exiting.
func TestCloseWithoutStartReturns(t *testing.T) {
	_, q := openDBQueue(t, 1)
	done := make(chan error, 1)
	go func() { done <- q.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked forever on a queue that was never started")
	}
}

// Close is documented as safe to call twice; the never-started path must keep
// that promise rather than closing an already-closed channel.
func TestCloseWithoutStartIsIdempotent(t *testing.T) {
	_, q := openDBQueue(t, 1)
	for i := range 2 {
		done := make(chan error, 1)
		go func() { done <- q.Close() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Close #%d: %v", i+1, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Close #%d blocked", i+1)
		}
	}
}

// Close must still join the workers when Start did run — otherwise fixing the
// never-started hang would turn shutdown into a goroutine leak.
func TestCloseAfterStartStillJoinsWorkers(t *testing.T) {
	_, q := openDBQueue(t, 1)
	q.Start(context.Background())
	// Let the pool actually come up so Close has something to join.
	time.Sleep(50 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- q.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not join a started worker pool")
	}
	select {
	case <-q.stopped:
	default:
		t.Fatal("Close returned before the worker pool finished")
	}
}
