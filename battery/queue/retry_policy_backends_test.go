package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryPermanentDeadLettersAtOnce(t *testing.T) {
	q := NewMemoryQueue(1)
	t.Cleanup(func() { q.Close() })
	var runs int32
	var mu sync.Mutex
	q.RegisterHandler("deliver", func(context.Context, Job) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return Permanent(errors.New("404"))
	})
	q.Start()
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		dead, _ := q.ListJobs(context.Background(), "failed", 10, 0)
		return len(dead) == 1
	}, 5*time.Second, "j1 dead-lettered")
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("handler ran %d times, want 1", runs)
	}
}

func TestMemoryRetryAfterWaits(t *testing.T) {
	q := NewMemoryQueue(1)
	t.Cleanup(func() { q.Close() })
	var mu sync.Mutex
	var at []time.Time
	q.RegisterHandler("deliver", func(context.Context, Job) error {
		mu.Lock()
		defer mu.Unlock()
		at = append(at, time.Now())
		if len(at) == 1 {
			return RetryAfter(300*time.Millisecond, errors.New("429"))
		}
		return nil
	})
	q.Start()
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(at) == 2
	}, 5*time.Second, "j1 retried")
	mu.Lock()
	defer mu.Unlock()
	if gap := at[1].Sub(at[0]); gap < 300*time.Millisecond {
		t.Fatalf("retried after %v, want at least the 300ms Retry-After", gap)
	}
}

func TestMemoryMaxAgeGivesUp(t *testing.T) {
	q := NewMemoryQueue(1)
	t.Cleanup(func() { q.Close() })
	q.RegisterHandler("deliver", func(context.Context, Job) error { return errors.New("boom") })
	q.Start()
	if err := q.Enqueue(context.Background(), Job{
		ID: "j1", Type: "deliver", MaxAttempts: 25,
		CreatedAt: time.Now().Add(-2 * time.Hour), MaxAge: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		dead, _ := q.ListJobs(context.Background(), "failed", 10, 0)
		return len(dead) == 1 && dead[0].Attempts == 1
	}, 5*time.Second, "j1 dead-lettered on its first failure past MaxAge")
}

func TestMemoryNackMaxAgeGivesUp(t *testing.T) {
	q := NewMemoryQueue(0)
	t.Cleanup(func() { q.Close() })
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "deliver", MaxAttempts: 25,
		CreatedAt: time.Now().Add(-2 * time.Hour), MaxAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if dead, _ := q.ListJobs(ctx, "failed", 10, 0); len(dead) != 1 {
		t.Fatalf("dead-letter set %v, want j1", dead)
	}
}

func TestSequenceKeyRefusedUnsupported(t *testing.T) {
	ctx := context.Background()
	mq := NewMemoryQueue(0)
	t.Cleanup(func() { mq.Close() })
	if err := mq.Enqueue(ctx, Job{Type: "deliver", SequenceKey: "a"}); !errors.Is(err, ErrSequenceUnsupported) {
		t.Fatalf("MemoryQueue: %v, want ErrSequenceUnsupported", err)
	}
	rq := NewRedisQueue(newMockRedis(), "seq-q")
	t.Cleanup(func() { rq.Close() })
	if err := rq.Enqueue(ctx, Job{Type: "deliver", SequenceKey: "a"}); !errors.Is(err, ErrSequenceUnsupported) {
		t.Fatalf("RedisQueue: %v, want ErrSequenceUnsupported", err)
	}
}

func TestMaxAgeNegativeRefusedEverywhere(t *testing.T) {
	ctx := context.Background()
	mq := NewMemoryQueue(0)
	t.Cleanup(func() { mq.Close() })
	if err := mq.Enqueue(ctx, Job{Type: "deliver", MaxAge: -time.Second}); err == nil {
		t.Fatal("MemoryQueue accepted a negative MaxAge")
	}
	rq := NewRedisQueue(newMockRedis(), "age-q")
	t.Cleanup(func() { rq.Close() })
	if err := rq.Enqueue(ctx, Job{Type: "deliver", MaxAge: -time.Second}); err == nil {
		t.Fatal("RedisQueue accepted a negative MaxAge")
	}
}

func TestRedisNackMaxAgeGivesUp(t *testing.T) {
	ctx := context.Background()
	q := NewRedisQueue(newMockRedis(), "age-q")
	t.Cleanup(func() { q.Close() })
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "deliver", MaxAttempts: 25,
		CreatedAt: time.Now().Add(-2 * time.Hour), MaxAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatal(err)
	}
	dead, err := q.ListJobs(ctx, "failed", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0].ID != "j1" {
		t.Fatalf("dead-letter list %v, want j1", dead)
	}
}
