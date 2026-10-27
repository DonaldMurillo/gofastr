package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// The in-memory and Redis backends honour Job.ScheduledAt the way DBQueue
// does: a job due later is invisible to Dequeue until the clock passes it,
// and it does not hold back a due job enqueued behind it. Both used to hand
// a delayed job out at once.
func TestDequeueHoldsFutureJobs(t *testing.T) {
	backends := []struct {
		name string
		make func(now func() time.Time) Queue
	}{
		{"memory", func(now func() time.Time) Queue {
			q := NewMemoryQueue(0)
			q.now = now
			return q
		}},
		{"redis", func(now func() time.Time) Queue {
			q := NewRedisQueue(newMockRedis(), "test")
			q.now = now
			return q
		}},
	}
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
			q := b.make(func() time.Time { return clock })

			// The delayed job outranks the due one, so a selection that
			// ignores ScheduledAt would pick it first.
			later := Job{ID: "later", Type: "t", Priority: 10, ScheduledAt: clock.Add(time.Hour)}
			if err := q.Enqueue(ctx, later); err != nil {
				t.Fatal(err)
			}
			if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
				t.Fatalf("Dequeue before due: err = %v, want ErrNoJob", err)
			}

			if err := q.Enqueue(ctx, Job{ID: "now", Type: "t"}); err != nil {
				t.Fatal(err)
			}
			got, err := q.Dequeue(ctx)
			if err != nil || got.ID != "now" {
				t.Fatalf("Dequeue = %q, %v; want the due job", got.ID, err)
			}
			if err := q.Ack(ctx, got); err != nil {
				t.Fatal(err)
			}
			if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
				t.Fatalf("Dequeue with only the delayed job: err = %v, want ErrNoJob", err)
			}

			clock = clock.Add(time.Hour)
			got, err = q.Dequeue(ctx)
			if err != nil || got.ID != "later" {
				t.Fatalf("Dequeue once due = %q, %v; want the delayed job", got.ID, err)
			}
			if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
				t.Fatalf("delayed job delivered twice: err = %v", err)
			}
		})
	}
}

// An idle memory worker wakes for a delayed job when it comes due, without
// another enqueue to nudge it, and not before. The worker first sleeps for
// a job an hour out, so the sooner job has to pull the wake timer earlier.
func TestMemoryWorkerWaitsForScheduledAt(t *testing.T) {
	q := NewMemoryQueue(1)
	ran := make(chan time.Time, 2)
	q.RegisterHandler("t", func(context.Context, Job) error {
		ran <- time.Now()
		return nil
	})
	q.Start()
	defer q.Close()

	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for armed := false; !armed; {
		if time.Now().After(deadline) {
			t.Fatal("idle worker never armed its wake timer")
		}
		q.pmu.Lock()
		armed = !q.wakeAt.IsZero()
		q.pmu.Unlock()
		time.Sleep(time.Millisecond)
	}
	due := time.Now().Add(150 * time.Millisecond)
	if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: due}); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-ran:
		if at.Before(due) {
			t.Fatalf("handler ran %s before ScheduledAt", due.Sub(at))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker never ran the delayed job")
	}
}

// Close does not wait out a job scheduled far ahead: workers exit once only
// not-yet-due jobs remain.
func TestMemoryCloseSkipsFutureJobs(t *testing.T) {
	q := NewMemoryQueue(1)
	var runs atomic.Int32
	q.RegisterHandler("t", func(context.Context, Job) error {
		runs.Add(1)
		return nil
	})
	q.Start()
	if err := q.Enqueue(context.Background(), Job{Type: "t", ScheduledAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = q.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on a job due in an hour")
	}
	if n := runs.Load(); n != 0 {
		t.Fatalf("Close ran the future job early (%d runs)", n)
	}
}

// A due job deeper in the delayed list than one LRange page is still
// promoted: the scan pages through the whole list.
func TestRedisPromotesPastFirstPage(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	q := NewRedisQueue(newMockRedis(), "test")
	q.now = func() time.Time { return clock }
	for range maxSkipDrain + 10 {
		if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: clock.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Enqueue(ctx, Job{ID: "soon", Type: "t", ScheduledAt: clock.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	got, err := q.Dequeue(ctx)
	if err != nil || got.ID != "soon" {
		t.Fatalf("Dequeue = %q, %v; want the job that came due", got.ID, err)
	}
}

// lostPromoteRedis reports every LRem on the delayed list as having removed
// nothing, the view of a worker that lost the race to promote an entry.
type lostPromoteRedis struct{ *mockRedis }

func (f lostPromoteRedis) LRem(ctx context.Context, key string, count int64, value any) (int64, error) {
	if key == "test:delayed" {
		return 0, nil
	}
	return f.mockRedis.LRem(ctx, key, count, value)
}

// A worker whose LRem removed nothing takes its pushed copy back: the
// worker that removed the entry delivers it, so the job is delivered once.
func TestRedisLostPromoteRaceSkips(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	q := NewRedisQueue(lostPromoteRedis{newMockRedis()}, "test")
	q.now = func() time.Time { return clock }
	if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: clock.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	if job, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("Dequeue = %q, %v; want ErrNoJob", job.ID, err)
	}
}

// mainPushFailRedis fails LPush onto the main list only.
type mainPushFailRedis struct{ *mockRedis }

func (f mainPushFailRedis) LPush(ctx context.Context, key string, values ...any) error {
	if key == "test" {
		return errors.New("redis down")
	}
	return f.mockRedis.LPush(ctx, key, values...)
}

// A due entry whose push onto the main list fails stays on the delayed
// list, not nowhere.
func TestRedisFailedPromoteRestores(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	r := newMockRedis()
	q := NewRedisQueue(mainPushFailRedis{r}, "test")
	q.now = func() time.Time { return clock }
	if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: clock.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	if _, err := q.Dequeue(ctx); err == nil {
		t.Fatal("Dequeue hid the failed promote")
	}
	if n := len(r.lists["test:delayed"]); n != 1 {
		t.Fatalf("delayed list holds %d entries after the failed promote, want 1", n)
	}
}

// mainPushDiesRedis panics at the push onto the main list: the process
// dies there, with no chance to restore anything.
type mainPushDiesRedis struct{ *mockRedis }

func (f mainPushDiesRedis) LPush(ctx context.Context, key string, values ...any) error {
	if key == "test" {
		panic("process died")
	}
	return f.mockRedis.LPush(ctx, key, values...)
}

// A worker that dies mid-promote leaves the job on a list. Taking it off
// the delayed list first lost it for good when the push never ran.
func TestRedisPromoteCrashKeepsJob(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	r := newMockRedis()
	q := NewRedisQueue(mainPushDiesRedis{r}, "test")
	q.now = func() time.Time { return clock }
	if err := q.Enqueue(ctx, Job{Type: "t", ScheduledAt: clock.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	func() {
		defer func() { _ = recover() }()
		_, _ = q.Dequeue(ctx)
	}()
	if n := len(r.lists["test:delayed"]) + len(r.lists["test"]); n != 1 {
		t.Fatalf("lists hold %d copies of the job after the crash, want 1", n)
	}
}
