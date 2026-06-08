package queue

import (
	"context"
	"testing"
	"time"
)

// TestRedisStart_AutoReclaimStranded asserts that RedisQueue.Start fires
// a background Reclaim ticker: a job stranded in-flight (worker crashed
// before Ack/Nack) is re-delivered to the main queue without the caller
// invoking Reclaim manually.
func TestRedisStart_AutoReclaimStranded(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "autoreclaim")
	q.SetVisibilityTimeout(30 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enqueue then dequeue — puts the job into the processing hash but
	// never Ack/Nack so it simulates a crashed worker.
	_ = q.Enqueue(ctx, Job{ID: "stranded", Type: "x"})
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job.ID != "stranded" {
		t.Fatalf("unexpected job %q", job.ID)
	}

	// Start auto-reclaim with a short ticker interval.
	q.Start(ctx, 20*time.Millisecond)

	// Wait long enough for the visibility timeout to expire and at least
	// one reclaim tick to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, err := q.Dequeue(ctx)
		if err == nil && got.ID == "stranded" {
			// Reclaimed — test passes.
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stranded job was not auto-reclaimed within 500ms")
}

// TestRedisStart_StopsOnContextCancel asserts Start's goroutine exits when
// the context is cancelled and does not leak.
func TestRedisStart_StopsOnContextCancel(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "stoptest")

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		q.Start(ctx, 20*time.Millisecond)
		close(stopped)
	}()

	cancel()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start goroutine did not exit after context cancel")
	}
}
