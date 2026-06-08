package queue

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ─── F9: RedisQueue implements Browsable ────────────────────────────────────

func TestRedisBrowsable_ListJobsAndStats(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "browse")
	ctx := context.Background()

	// Initially: no dead jobs, ListJobs and Stats should return empty results.
	jobs, err := q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("ListJobs empty: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats empty: %v", err)
	}
	if stats["failed"] != 0 {
		t.Errorf("expected failed=0, got %d", stats["failed"])
	}

	// Drive a job to DLQ via Nack exceeding MaxAttempts.
	_ = q.Enqueue(ctx, Job{ID: "dead-r1", Type: "email", MaxAttempts: 1})
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	// ListJobs "failed" must return the dead job.
	jobs, err = q.ListJobs(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("ListJobs after dead: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 failed job, got %d", len(jobs))
	}
	if jobs[0].ID != "dead-r1" {
		t.Errorf("expected job ID dead-r1, got %q", jobs[0].ID)
	}

	// Stats must reflect it.
	stats, err = q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after dead: %v", err)
	}
	if stats["failed"] != 1 {
		t.Errorf("expected failed=1, got %d", stats["failed"])
	}
}

func TestRedisBrowsable_ListJobsLimit(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "browse2")
	ctx := context.Background()

	// Push 5 jobs directly to the dead list.
	for i := 0; i < 5; i++ {
		_ = q.Enqueue(ctx, Job{ID: "dead-lim-" + string(rune('a'+i)), Type: "x", MaxAttempts: 1})
		job, _ := q.Dequeue(ctx)
		_ = q.Nack(ctx, job.ID)
	}

	// Limit to 3.
	jobs, err := q.ListJobs(ctx, "failed", 3)
	if err != nil {
		t.Fatalf("ListJobs with limit: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs with limit=3, got %d", len(jobs))
	}
}

func TestRedisBrowsable_ListJobsAllStatus(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "browse3")
	ctx := context.Background()

	// Drive a job to DLQ.
	_ = q.Enqueue(ctx, Job{ID: "dead-all", Type: "x", MaxAttempts: 1})
	job, _ := q.Dequeue(ctx)
	_ = q.Nack(ctx, job.ID)

	// Passing empty status should return same as "failed" for Redis (dead list).
	jobs, err := q.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListJobs all: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job with empty status, got %d", len(jobs))
	}
}

// ─── F17: Scheduler uses slog not fmt.Printf ────────────────────────────────

// failQueue always returns an error on Enqueue to trigger the error path.
type failQueue struct{}

func (f *failQueue) Enqueue(_ context.Context, _ Job) error {
	return ErrQueueClosed
}
func (f *failQueue) Dequeue(_ context.Context, _ ...string) (Job, error) { return Job{}, ErrNoJob }
func (f *failQueue) Ack(_ context.Context, _ string) error               { return nil }
func (f *failQueue) Nack(_ context.Context, _ string) error              { return nil }
func (f *failQueue) Close() error                                        { return nil }

// slogCapture captures slog records for assertions.
type slogCapture struct {
	records []slog.Record
}

func (c *slogCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c *slogCapture) Handle(_ context.Context, r slog.Record) error {
	c.records = append(c.records, r)
	return nil
}
func (c *slogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *slogCapture) WithGroup(_ string) slog.Handler      { return c }

func TestSchedulerEnqueueErrorLogsViaSlog(t *testing.T) {
	cap := &slogCapture{}
	logger := slog.New(cap)

	sched := NewSchedulerWithLogger(&failQueue{}, logger)
	sched.Every(50 * time.Millisecond).Job("test-job", nil).Register()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	sched.Start(ctx)

	if len(cap.records) == 0 {
		t.Fatal("expected slog to capture enqueue error, got no records")
	}
	found := false
	for _, rec := range cap.records {
		if strings.Contains(rec.Message, "enqueue") || strings.Contains(rec.Message, "scheduler") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a scheduler enqueue-error log, got: %+v", cap.records)
	}
}

// ─── F18: MemoryQueue handler timeout is configurable ───────────────────────

func TestMemoryQueue_DefaultTimeout30s(t *testing.T) {
	q := NewMemoryQueue(1)
	if q.handlerTimeout != 30*time.Second {
		t.Errorf("default handler timeout should be 30s, got %v", q.handlerTimeout)
	}
	defer q.Close()
}

func TestMemoryQueue_WithHandlerTimeout_SlowJobCompletes(t *testing.T) {
	// With a 200ms timeout, a 100ms handler should succeed.
	q := NewMemoryQueue(1, WithHandlerTimeout(200*time.Millisecond))
	defer q.Close()

	done := make(chan struct{})
	q.RegisterHandler("slow", func(ctx context.Context, job Job) error {
		select {
		case <-time.After(100 * time.Millisecond):
			close(done)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	q.Start()

	if err := q.Enqueue(context.Background(), Job{Type: "slow", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-done:
		// Handler completed successfully — timeout was long enough.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler did not complete within the configured timeout window")
	}
}

func TestMemoryQueue_WithHandlerTimeout_ShortTimeoutCancels(t *testing.T) {
	// With a 10ms timeout, a handler sleeping 100ms should be cancelled and
	// the job retried (or dead-lettered after MaxAttempts), but not silently succeed.
	cancelled := make(chan struct{}, 1)
	q := NewMemoryQueue(1, WithHandlerTimeout(10*time.Millisecond))
	defer q.Close()

	q.RegisterHandler("long", func(ctx context.Context, job Job) error {
		select {
		case <-time.After(100 * time.Millisecond):
			return nil
		case <-ctx.Done():
			select {
			case cancelled <- struct{}{}:
			default:
			}
			return ctx.Err()
		}
	})
	q.Start()

	if err := q.Enqueue(context.Background(), Job{Type: "long", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-cancelled:
		// Context was cancelled due to short timeout — expected.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler was not cancelled within the short timeout window")
	}
}
