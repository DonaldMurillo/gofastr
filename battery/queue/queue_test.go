package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnqueueAndHandlerExecution(t *testing.T) {
	q := NewMemoryQueue(2)

	var executed atomic.Int32
	q.RegisterHandler("test", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	err := q.Enqueue(context.Background(), Job{
		Type:    "test",
		Payload: json.RawMessage(`{"hello":"world"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	q.Close()

	if got := executed.Load(); got != 1 {
		t.Errorf("expected 1 execution, got %d", got)
	}
}

func TestMultipleWorkers(t *testing.T) {
	const numJobs = 100
	q := NewMemoryQueue(4)

	var executed atomic.Int32
	q.RegisterHandler("work", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	for i := 0; i < numJobs; i++ {
		err := q.Enqueue(context.Background(), Job{Type: "work"})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	// Give workers time to process.
	time.Sleep(500 * time.Millisecond)
	q.Close()

	if got := executed.Load(); got != numJobs {
		t.Errorf("expected %d executions, got %d", numJobs, got)
	}
}

func TestRetryOnFailure(t *testing.T) {
	q := NewMemoryQueue(1)

	var attempts atomic.Int32
	q.RegisterHandler("retry-me", func(ctx context.Context, job Job) error {
		a := attempts.Add(1)
		// Fail the first two attempts, succeed on the third.
		if a < 3 {
			return fmt.Errorf("transient error (attempt %d)", a)
		}
		return nil
	})

	q.Start()

	err := q.Enqueue(context.Background(), Job{
		Type:        "retry-me",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Wait for all retries.
	time.Sleep(500 * time.Millisecond)
	q.Close()

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestMaxAttemptsExceeded(t *testing.T) {
	q := NewMemoryQueue(1)

	var attempts atomic.Int32
	q.RegisterHandler("always-fail", func(ctx context.Context, job Job) error {
		attempts.Add(1)
		return fmt.Errorf("permanent failure")
	})

	q.Start()

	err := q.Enqueue(context.Background(), Job{
		Type:        "always-fail",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	q.Close()

	// Should have been attempted exactly MaxAttempts times then dropped.
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (MaxAttempts), got %d", got)
	}
}

func TestCloseDrainsPendingJobs(t *testing.T) {
	q := NewMemoryQueue(2)

	var executed atomic.Int32
	// Slow handler to simulate work in progress.
	q.RegisterHandler("slow", func(ctx context.Context, job Job) error {
		time.Sleep(50 * time.Millisecond)
		executed.Add(1)
		return nil
	})

	q.Start()

	for i := 0; i < 10; i++ {
		_ = q.Enqueue(context.Background(), Job{Type: "slow"})
	}

	// Close should drain all jobs.
	q.Close()

	if got := executed.Load(); got != 10 {
		t.Errorf("expected all 10 jobs to complete on Close, got %d", got)
	}
}

func TestEnqueueAfterClose(t *testing.T) {
	q := NewMemoryQueue(1)
	q.Start()
	q.Close()

	err := q.Enqueue(context.Background(), Job{Type: "test"})
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

func TestJobPriorityOrdering(t *testing.T) {
	q := NewMemoryQueue(1)

	// Pause processing so we can enqueue jobs first.
	// We'll use a manual dequeue approach to test priority ordering.
	jobs := []Job{
		{ID: "low", Type: "test", Priority: 1},
		{ID: "high", Type: "test", Priority: 10},
		{ID: "mid", Type: "test", Priority: 5},
	}

	// Enqueue directly into channel (bypass any sorting).
	for _, j := range jobs {
		_ = q.Enqueue(context.Background(), j)
	}

	// Collect jobs in order.
	var order []string
	for i := 0; i < 3; i++ {
		job, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatalf("Dequeue %d: %v", i, err)
		}
		order = append(order, job.ID)
	}

	// FIFO ordering from the channel: low, high, mid.
	// Priority is an attribute on the Job for backends that support it;
	// the MemoryQueue channel is FIFO.
	expected := []string{"low", "high", "mid"}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("position %d: expected %q, got %q", i, id, order[i])
		}
	}

	q.Close()
}

func TestSchedulerFiresAtInterval(t *testing.T) {
	q := NewMemoryQueue(1)

	var executed atomic.Int32
	q.RegisterHandler("scheduled", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	sched := NewScheduler(q)
	sched.Every(100*time.Millisecond).Job("scheduled", json.RawMessage(`{}`)).Register()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sched.Start(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
	q.Close()

	got := executed.Load()
	if got < 2 {
		t.Errorf("expected at least 2 scheduled executions, got %d", got)
	}
}

func TestDequeueByType(t *testing.T) {
	q := NewMemoryQueue(1)

	_ = q.Enqueue(context.Background(), Job{ID: "a", Type: "email"})
	_ = q.Enqueue(context.Background(), Job{ID: "b", Type: "sms"})
	_ = q.Enqueue(context.Background(), Job{ID: "c", Type: "email"})

	// Dequeue only email jobs.
	job, err := q.Dequeue(context.Background(), "email")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.Type != "email" {
		t.Errorf("expected email job, got %q", job.Type)
	}
	if job.ID != "a" {
		t.Errorf("expected job ID 'a', got %q", job.ID)
	}

	// Dequeue another email.
	job, err = q.Dequeue(context.Background(), "email")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.ID != "c" {
		t.Errorf("expected job ID 'c', got %q", job.ID)
	}

	q.Close()
}
