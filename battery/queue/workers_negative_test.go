package queue

import (
	"context"
	"testing"
)

// WithWorkers(-1) stored the negative. Start then sized its done channel
// with make(chan struct{}, q.workers+laneCount), and a negative total
// panicked inside Start's goroutine, taking the process down. A negative
// count now keeps the default of one shared worker; zero keeps meaning
// "no shared workers" (lane workers only), which the tests here rely on.
func TestNegativeWorkersKeepsDefault(t *testing.T) {
	_, q := openDBQueueOpts(t, WithWorkers(-1))
	if q.workers != 1 {
		t.Fatalf("WithWorkers(-1): workers = %d, want the default 1", q.workers)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, q = openDBQueueOpts(t, WithWorkers(0))
	if q.workers != 0 {
		t.Fatalf("WithWorkers(0): workers = %d, want 0 (no shared pool)", q.workers)
	}
}
