package queue

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordQueue captures enqueued jobs for deterministic assertions.
type recordQueue struct {
	mu   sync.Mutex
	jobs []Job
}

func (r *recordQueue) Enqueue(_ context.Context, j Job) error {
	r.mu.Lock()
	r.jobs = append(r.jobs, j)
	r.mu.Unlock()
	return nil
}
func (r *recordQueue) Dequeue(_ context.Context, _ ...string) (Job, error) { return Job{}, ErrNoJob }
func (r *recordQueue) Ack(_ context.Context, _ Job) error                  { return nil }
func (r *recordQueue) Nack(_ context.Context, _ Job) error                 { return nil }
func (r *recordQueue) Close() error                                        { return nil }

func (r *recordQueue) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

// A Cron schedule must enqueue exactly when its next tick arrives, driven
// deterministically off a fixed base time, no wall-clock sleeps.
func TestSchedulerCronFiresAtNextTick(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)

	// Anchor "now" so NextRun is computed from a known base.
	base := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	if err := sched.Cron("0 2 * * *").Job("nightly", nil).RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}

	// Before 02:00, must not fire.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 1, 59, 0, 0, time.UTC))
	if got := rq.count(); got != 0 {
		t.Fatalf("fired early: got %d enqueues, want 0", got)
	}

	// At 02:00, must fire exactly once.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 1 {
		t.Fatalf("at next tick: got %d enqueues, want 1", got)
	}

	// Same day, after fire, must not double-fire; next run rolled to tomorrow.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 8, 23, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 1 {
		t.Fatalf("double fired: got %d enqueues, want 1", got)
	}

	// Tomorrow 02:00, fires again.
	sched.dispatchDue(context.Background(), time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC))
	if got := rq.count(); got != 2 {
		t.Fatalf("next day: got %d enqueues, want 2", got)
	}
}

// An invalid cron spec is reported at registration time, not silently dropped.
func TestSchedulerCronRejectsBadSpec(t *testing.T) {
	sched := NewScheduler(&recordQueue{})
	if err := sched.Cron("not a cron").Job("x", nil).Register(); err == nil {
		t.Fatal("expected error for bad cron spec, got nil")
	}
}

// Interval-based schedules keep working exactly as before alongside cron ones.
func TestSchedulerEveryStillWorks(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	sched.Every(time.Hour).Job("hourly", nil).Register()

	// dispatchDue with a now past NextRun must fire the interval job.
	future := time.Now().Add(2 * time.Hour)
	sched.dispatchDue(context.Background(), future)
	if got := rq.count(); got != 1 {
		t.Fatalf("interval job: got %d enqueues, want 1", got)
	}
}

// A spec no date matches (February 30) is refused at registration: Next
// returns the zero time for it, and a zero NextRun is due on every tick.
func TestCronThatNeverFiresIsRefused(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	if err := sched.Cron("0 0 30 2 *").Job("never", nil).Register(); err == nil {
		t.Fatal("Register accepted a spec that never fires")
	}
	sched.dispatchDue(context.Background(), time.Now())
	if got := rq.count(); got != 0 {
		t.Fatalf("got %d enqueues, want 0", got)
	}
}

// The durable builder refuses it too, before it writes a row.
func TestDurableCronNeverFiringRefused(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{OwnerID: "never"})
	if err != nil {
		t.Fatalf("NewDurableScheduler: %v", err)
	}
	err = scheduler.Cron("never", "0 0 30 2 *").Job("email", nil).
		RegisterAt(time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("RegisterAt accepted a spec that never fires")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + q.schedulerSchedulesTable()).Scan(&count); err != nil {
		t.Fatalf("count schedules: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted %d schedules", count)
	}
}

// A schedule whose next firing lies past Next's five-year search stops
// instead of firing on every tick. 2100 is not a leap year, so after
// 2096-02-29 the next February 29 is in 2104.
func TestCronPastSearchHorizonStops(t *testing.T) {
	rq := &recordQueue{}
	var log bytes.Buffer
	sched := NewSchedulerWithLogger(rq, slog.New(slog.NewTextHandler(&log, nil)))
	base := time.Date(2096, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := sched.Cron("0 0 29 2 *").Job("leap", nil).RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}
	fire := time.Date(2096, 2, 29, 0, 0, 0, 0, time.UTC)
	sched.dispatchDue(context.Background(), fire)
	if got := rq.count(); got != 1 {
		t.Fatalf("on 2096-02-29: got %d enqueues, want 1", got)
	}
	for i := 1; i <= 3; i++ {
		sched.dispatchDue(context.Background(), fire.Add(time.Duration(i)*time.Minute))
	}
	if got := rq.count(); got != 1 {
		t.Fatalf("after the last firing: got %d enqueues, want 1", got)
	}
	if !strings.Contains(log.String(), "cron schedule stopped") || !strings.Contains(log.String(), "job_type=leap") {
		t.Fatalf("no stop warning naming the job type: %q", log.String())
	}
}

// The stop is for cron schedules only: an interval schedule whose NextRun
// is the zero time still fires.
func TestIntervalAtZeroTimeStillFires(t *testing.T) {
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	if err := sched.Every(0).Job("tick", nil).RegisterAt(time.Time{}); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}
	sched.dispatchDue(context.Background(), time.Now())
	if got := rq.count(); got != 1 {
		t.Fatalf("got %d enqueues, want 1", got)
	}
}
