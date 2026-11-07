package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a settable clock for q.now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// openPolicyQueue opens a SQLite DBQueue on a fake clock with opts.
func openPolicyQueue(t *testing.T, opts ...DBQueueOption) (*DBQueue, *fakeClock) {
	t.Helper()
	db := openSQLiteDB(t)
	q, err := NewDBQueue(db, opts...)
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	clock := newFakeClock()
	q.now = clock.Now
	return q, clock
}

// stored reads one job's row back through ListJobs.
func stored(t *testing.T, q *DBQueue, id string) Job {
	t.Helper()
	jobs, err := q.ListJobs(context.Background(), "all", 1000, 0)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	for _, j := range jobs {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("job %s not stored", id)
	return Job{}
}

// claimAndFail claims the next job and settles it with handlerErr, as the
// worker loop does after a failed handler.
func claimAndFail(t *testing.T, q *DBQueue, handlerErr error) Job {
	t.Helper()
	ctx := context.Background()
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if _, err := q.settleFailure(ctx, job, handlerErr); err != nil {
		t.Fatalf("settle: %v", err)
	}
	return job
}

func TestRetryAfterSetsNextAttempt(t *testing.T) {
	q, clock := openPolicyQueue(t, WithBackoff(time.Second, time.Minute))
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, RetryAfter(90*time.Second, errors.New("503")))
	got := stored(t, q, "j1")
	if got.Status != "pending" {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if want := clock.Now().Add(90 * time.Second); !got.ScheduledAt.Equal(want) {
		t.Fatalf("scheduled_at = %v, want %v (Retry-After, not the 1s backoff)", got.ScheduledAt, want)
	}
}

func TestRetryAfterNegativeIsNow(t *testing.T) {
	q, clock := openPolicyQueue(t, WithBackoff(time.Minute, time.Hour))
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, RetryAfter(-time.Hour, errors.New("503")))
	if got := stored(t, q, "j1"); !got.ScheduledAt.Equal(clock.Now()) {
		t.Fatalf("scheduled_at = %v, want now %v", got.ScheduledAt, clock.Now())
	}
}

func TestRetryScheduleFollowsSteps(t *testing.T) {
	steps := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	q, clock := openPolicyQueue(t, WithRetrySchedule(steps...))
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 10}); err != nil {
		t.Fatal(err)
	}
	// The last step repeats once the schedule runs out.
	for i, want := range append(steps, 10*time.Minute) {
		claimAndFail(t, q, errors.New("boom"))
		got := stored(t, q, "j1")
		if d := got.ScheduledAt.Sub(clock.Now()); d != want {
			t.Fatalf("attempt %d: delay %v, want %v", i+1, d, want)
		}
		clock.Advance(want)
	}
}

func TestRetryJitterBounds(t *testing.T) {
	for _, tc := range []struct {
		r    float64
		want time.Duration
	}{
		{0, 80 * time.Second},     // 100s * (1 - 0.2)
		{0.5, 100 * time.Second},  // centre
		{0.75, 110 * time.Second}, // 100s * (1 + 0.1)
	} {
		q, clock := openPolicyQueue(t, WithRetrySchedule(100*time.Second), WithRetryJitter(0.2))
		q.jitterRand = func() float64 { return tc.r }
		if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
			t.Fatal(err)
		}
		claimAndFail(t, q, errors.New("boom"))
		if d := stored(t, q, "j1").ScheduledAt.Sub(clock.Now()); d != tc.want {
			t.Fatalf("r=%v: delay %v, want %v", tc.r, d, tc.want)
		}
	}
}

func TestRetryJitterSkipsRetryAfter(t *testing.T) {
	q, clock := openPolicyQueue(t, WithRetrySchedule(time.Minute), WithRetryJitter(0.5))
	q.jitterRand = func() float64 { return 0 }
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, RetryAfter(40*time.Second, errors.New("429")))
	if d := stored(t, q, "j1").ScheduledAt.Sub(clock.Now()); d != 40*time.Second {
		t.Fatalf("delay %v, want the receiver's 40s unjittered", d)
	}
}

func TestRetryPolicyOptionsRefused(t *testing.T) {
	for name, opts := range map[string][]DBQueueOption{
		"schedule+backoff": {WithRetrySchedule(time.Second), WithBackoff(time.Second, time.Minute)},
		"negative step":    {WithRetrySchedule(time.Second, -time.Second)},
		"empty schedule":   {WithRetrySchedule()},
		"jitter NaN":       {WithRetryJitter(math.NaN())},
		"jitter negative":  {WithRetryJitter(-0.1)},
		"jitter over one":  {WithRetryJitter(1.5)},
		"jitter infinite":  {WithRetryJitter(math.Inf(1))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDBQueue(openSQLiteDB(t), opts...); err == nil {
				t.Fatal("NewDBQueue accepted the options")
			}
		})
	}
}

func TestPermanentDeadLettersAtOnce(t *testing.T) {
	q, _ := openPolicyQueue(t)
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 25}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, fmt.Errorf("receiver answered 404: %w", ErrPermanent))
	got := stored(t, q, "j1")
	if got.Status != "failed" || got.Attempts != 1 {
		t.Fatalf("status %q attempts %d, want failed after 1", got.Status, got.Attempts)
	}
	if !strings.Contains(got.LastError, "404") {
		t.Fatalf("last_error %q lost the handler's error", got.LastError)
	}
}

func TestPermanentWrapperIsPermanent(t *testing.T) {
	err := Permanent(errors.New("bad request"))
	if !errors.Is(err, ErrPermanent) {
		t.Fatal("Permanent(err) is not ErrPermanent")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("Permanent lost the cause: %q", err)
	}
	// Permanent beats a RetryAfter in the same chain.
	q, _ := openPolicyQueue(t)
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 5}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, Permanent(RetryAfter(time.Minute, errors.New("gone"))))
	if got := stored(t, q, "j1"); got.Status != "failed" {
		t.Fatalf("status %q, want failed", got.Status)
	}
}

func TestMaxAgeGivesUp(t *testing.T) {
	q, clock := openPolicyQueue(t, WithRetrySchedule(time.Hour))
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 25, MaxAge: 90 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	claimAndFail(t, q, errors.New("boom")) // next try at +1h, inside the age
	if got := stored(t, q, "j1"); got.Status != "pending" {
		t.Fatalf("first failure: status %q, want pending", got.Status)
	}
	clock.Advance(time.Hour)
	claimAndFail(t, q, errors.New("boom")) // next try at +2h, past 90m
	got := stored(t, q, "j1")
	if got.Status != "failed" {
		t.Fatalf("second failure: status %q, want failed past MaxAge", got.Status)
	}
	if !strings.Contains(got.LastError, "boom") {
		t.Fatalf("last_error %q lost the handler's error", got.LastError)
	}
}

func TestMaxAgeRoundTrips(t *testing.T) {
	q, _ := openPolicyQueue(t)
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "deliver", MaxAge: 96 * time.Hour, SequenceKey: "order-7"}); err != nil {
		t.Fatal(err)
	}
	if got := stored(t, q, "j1"); got.MaxAge != 96*time.Hour || got.SequenceKey != "order-7" {
		t.Fatalf("listed MaxAge %v SequenceKey %q", got.MaxAge, got.SequenceKey)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.MaxAge != 96*time.Hour || job.SequenceKey != "order-7" {
		t.Fatalf("claimed MaxAge %v SequenceKey %q", job.MaxAge, job.SequenceKey)
	}
}

func TestMaxAgeNegativeRefused(t *testing.T) {
	q, _ := openPolicyQueue(t)
	if err := q.Enqueue(context.Background(), Job{Type: "deliver", MaxAge: -time.Second}); err == nil {
		t.Fatal("Enqueue accepted a negative MaxAge")
	}
}

func TestSequenceKeyRunsInOrder(t *testing.T) {
	q, clock := openPolicyQueue(t)
	ctx := context.Background()
	for _, j := range []Job{
		{ID: "a1", Type: "deliver", SequenceKey: "a"},
		{ID: "a2", Type: "deliver", SequenceKey: "a"},
		{ID: "b1", Type: "deliver", SequenceKey: "b"},
		{ID: "free", Type: "deliver"},
	} {
		if err := q.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Millisecond) // distinct created_at
	}
	var claimed []Job
	for {
		job, err := q.Dequeue(ctx)
		if errors.Is(err, ErrNoJob) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		claimed = append(claimed, job)
	}
	var ids []string
	for _, j := range claimed {
		ids = append(ids, j.ID)
	}
	if got := strings.Join(ids, ","); got != "a1,b1,free" {
		t.Fatalf("claimed %s while a1 runs, want a1,b1,free (a2 waits)", got)
	}
	if err := q.Ack(ctx, claimed[0]); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil || job.ID != "a2" {
		t.Fatalf("after a1 acked: got %q, %v; want a2", job.ID, err)
	}
}

func TestSequenceKeyWaitsOnRetry(t *testing.T) {
	q, clock := openPolicyQueue(t, WithRetrySchedule(time.Minute))
	ctx := context.Background()
	for _, id := range []string{"a1", "a2"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", SequenceKey: "a", MaxAttempts: 3}); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Millisecond)
	}
	claimAndFail(t, q, errors.New("boom")) // a1 waits a minute
	if job, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("a2 ran (%q) while a1 waits to retry", job.ID)
	}
	clock.Advance(time.Minute)
	if job, err := q.Dequeue(ctx); err != nil || job.ID != "a1" {
		t.Fatalf("got %q, %v; want a1's retry", job.ID, err)
	}
}

func TestSequenceKeyDeadLetterFreesKey(t *testing.T) {
	q, clock := openPolicyQueue(t)
	ctx := context.Background()
	for _, id := range []string{"a1", "a2"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", SequenceKey: "a", MaxAttempts: 3}); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Millisecond)
	}
	claimAndFail(t, q, Permanent(errors.New("410")))
	if job, err := q.Dequeue(ctx); err != nil || job.ID != "a2" {
		t.Fatalf("got %q, %v; want a2 once a1 dead-lettered", job.ID, err)
	}
}

// TestSequenceKeyReplayWaitsForRunning: a replayed job is older than the
// one running, and must still wait for it.
func TestSequenceKeyReplayWaitsForRunning(t *testing.T) {
	q, clock := openPolicyQueue(t)
	ctx := context.Background()
	for _, id := range []string{"a1", "a2"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", SequenceKey: "a", MaxAttempts: 3}); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Millisecond)
	}
	claimAndFail(t, q, Permanent(errors.New("410")))
	a2, err := q.Dequeue(ctx)
	if err != nil || a2.ID != "a2" {
		t.Fatalf("got %q, %v; want a2", a2.ID, err)
	}
	if err := q.Replay(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if job, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("replayed %q ran beside a2", job.ID)
	}
}

// TestShutdownMidHandlerRefunds: a shutdown that cancels a running handler
// hands the job back with its attempt refunded.
func TestShutdownMidHandlerRefunds(t *testing.T) {
	db := openSQLiteDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	q.RegisterHandler("deliver", func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx)
	<-started
	cancel()
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	got := stored(t, q, "j1")
	if got.Status != "pending" || got.Attempts != 0 {
		t.Fatalf("status %q attempts %d, want pending with the attempt refunded", got.Status, got.Attempts)
	}
}

// TestShutdownKeepsPermanentFailure: a handler that answers a shutdown
// with a permanent error is dead-lettered, not handed back.
func TestShutdownKeepsPermanentFailure(t *testing.T) {
	db := openSQLiteDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	q.RegisterHandler("deliver", func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		return Permanent(errors.New("410 gone"))
	})
	if err := q.Enqueue(context.Background(), Job{ID: "j1", Type: "deliver", MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx)
	<-started
	cancel()
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if got := stored(t, q, "j1"); got.Status != "failed" {
		t.Fatalf("status %q attempts %d, want failed", got.Status, got.Attempts)
	}
}

// TestWorkerLoopHonorsPolicyErrors runs the worker loop, not the settle
// helper, so the loop's wiring is what is checked.
func TestWorkerLoopHonorsPolicyErrors(t *testing.T) {
	db := openSQLiteDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	ran := map[string]int{}
	q.RegisterHandler("deliver", func(_ context.Context, job Job) error {
		mu.Lock()
		ran[job.ID]++
		mu.Unlock()
		if job.ID == "perm" {
			return Permanent(errors.New("404"))
		}
		return RetryAfter(time.Hour, errors.New("429"))
	})
	ctx := context.Background()
	for _, id := range []string{"perm", "later"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", MaxAttempts: 5}); err != nil {
			t.Fatal(err)
		}
	}
	q.Start(ctx)
	t.Cleanup(func() { q.Close() })
	waitFor(t, func() bool {
		return stored(t, q, "perm").Status == "failed" && stored(t, q, "later").Status == "pending" &&
			stored(t, q, "later").Attempts == 1
	}, 5*time.Second, "perm dead-lettered and later rescheduled")
	if d := time.Until(stored(t, q, "later").ScheduledAt); d < 50*time.Minute {
		t.Fatalf("later rescheduled %v out, want about an hour", d)
	}
	mu.Lock()
	defer mu.Unlock()
	if ran["perm"] != 1 || ran["later"] != 1 {
		t.Fatalf("runs %v, want one each", ran)
	}
}

// openSQLiteDB opens a private in-memory SQLite database on one
// connection, as openDBQueue does.
func openSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSequenceReplayCrashRecovers: replaying an older job while a newer
// one is claimed, then losing the newer one's worker, must not stall the
// key. The expired claim is reclaimed; the replayed job runs after it.
func TestSequenceReplayCrashRecovers(t *testing.T) {
	q, clock := openPolicyQueue(t, WithLeaseTimeout(time.Second))
	ctx := context.Background()
	for _, id := range []string{"older", "newer"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", SequenceKey: "record", MaxAttempts: 3}); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Millisecond)
	}
	claimAndFail(t, q, Permanent(errors.New("410")))
	if job, err := q.Dequeue(ctx); err != nil || job.ID != "newer" {
		t.Fatalf("got %q, %v; want newer", job.ID, err)
	}
	if err := q.Replay(ctx, "older"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second) // newer's worker is gone; its lease ran out
	re, err := q.Dequeue(ctx)
	if err != nil || re.ID != "newer" {
		t.Fatalf("got %q, %v; want newer reclaimed", re.ID, err)
	}
	if job, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("older %q ran beside the reclaimed newer", job.ID)
	}
	if err := q.Ack(ctx, re); err != nil {
		t.Fatal(err)
	}
	if job, err := q.Dequeue(ctx); err != nil || job.ID != "older" {
		t.Fatalf("got %q, %v; want older after newer", job.ID, err)
	}
}
