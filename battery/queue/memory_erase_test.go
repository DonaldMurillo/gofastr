package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// memHolds reports where q holds a job whose payload mentions s: "pending",
// "inflight" or "dead", once per job.
func memHolds(q *MemoryQueue, s string) []string {
	var out []string
	q.pmu.Lock()
	for _, pj := range q.pending.items {
		if strings.Contains(string(pj.job.Payload), s) {
			out = append(out, "pending")
		}
	}
	q.pmu.Unlock()
	q.inflightMu.Lock()
	for _, j := range q.inflight {
		if strings.Contains(string(j.Payload), s) {
			out = append(out, "inflight")
		}
	}
	q.inflightMu.Unlock()
	q.deadMu.Lock()
	for _, d := range q.dead {
		if strings.Contains(string(d.job.Payload), s) {
			out = append(out, "dead")
		}
	}
	q.deadMu.Unlock()
	return out
}

func userJob(id, user string) Job {
	return Job{ID: id, Type: "mail", UserID: user, Payload: json.RawMessage(`{"to":"` + user + `-secret"}`)}
}

// MemoryQueue held the erased user's jobs in its pending heap, its
// dequeued set and its dead list, where no eraser reached (2026-10-27
// S25 follow-up). It now registers a store eraser that removes them all,
// frees the unique keys they held, and leaves other users' jobs.
func TestEraseRemovesMemoryJobs(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue(0)
	t.Cleanup(func() { _ = q.Close() })
	dead := userJob("d1", "u1")
	dead.MaxAttempts = 1
	if err := q.Enqueue(ctx, dead); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, userJob("i1", "u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatal(err)
	}
	later := userJob("l1", "u1")
	later.ScheduledAt = time.Now().Add(time.Hour)
	keyed := userJob("k1", "u1")
	keyed.UniqueKey = "digest-u1"
	for _, j := range []Job{userJob("p1", "u1"), userJob("p2", "u2"), later, keyed} {
		if err := q.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	if got := memHolds(q, "u1-secret"); len(got) != 5 {
		t.Fatalf("setup: u1 held in %v, want 5 jobs", got)
	}
	app := eraseQueueApp(t)

	dry, err := app.EraseUserData(ctx, "u1", framework.WithEraseDryRun())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got := memHolds(q, "u1-secret"); len(got) != 5 {
		t.Fatalf("the dry run removed jobs: u1 is left in %v, want 5", got)
	}
	rep, err := app.EraseUserData(ctx, "u1")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	if got := memHolds(q, "u1-secret"); len(got) != 0 {
		t.Errorf("u1's jobs survive the erasure in %v", got)
	}
	if got := memHolds(q, "u2-secret"); len(got) != 1 || got[0] != "pending" {
		t.Errorf("u2's job is in %v, want it still pending", got)
	}
	again := userJob("k2", "u1")
	again.UniqueKey = "digest-u1"
	if err := q.Enqueue(ctx, again); errors.Is(err, ErrDuplicateJob) {
		t.Error("the erased job still holds its unique key")
	}
	name := q.eraserName()
	got, ok := storeResult(rep, name)
	want, _ := storeResult(dry, name)
	if !ok || got.RowsAffected != 5 || want.RowsAffected != 5 {
		t.Errorf("report stores = %+v, dry run %+v; want %s with 5 jobs in both", rep.Stores, dry.Stores, name)
	}
}

// The in-memory Scheduler kept a schedule's payload and fired it again on
// every tick after its user was erased. Its store eraser drops the
// schedules that name the user.
func TestEraseDropsInMemorySchedules(t *testing.T) {
	ctx := context.Background()
	rq := &recordQueue{}
	sched := NewScheduler(rq)
	t.Cleanup(func() { _ = sched.Close() })
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, u := range []string{"u1", "u2"} {
		if err := sched.Every(time.Minute).Job("digest", map[string]string{"to": u}).UserID(u).RegisterAt(base); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eraseQueueApp(t).EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	sched.dispatchDue(ctx, base.Add(time.Minute))
	jobs := rq.capturedJobs()
	if len(jobs) != 1 || jobs[0].UserID != "u2" {
		t.Fatalf("fired after erasing u1: %+v, want only u2's job", jobs)
	}
}

// Two RedisQueues with one queue name on different servers shared one
// eraser name, and the later registration replaced the earlier, so the
// first server kept the user's jobs. Both are erased now.
func TestSameNameRedisQueuesBothErased(t *testing.T) {
	ctx := context.Background()
	r1, r2 := newMockRedis(), newMockRedis()
	q1, q2 := NewRedisQueue(r1, "twin-q"), NewRedisQueue(r2, "twin-q")
	t.Cleanup(func() { _ = q1.Close(); _ = q2.Close() })
	for _, q := range []*RedisQueue{q1, q2} {
		if err := q.Enqueue(ctx, userJob("j", "u1")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eraseQueueApp(t).EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for i, r := range []*mockRedis{r1, r2} {
		if got := redisHolds(r, "u1-secret"); len(got) != 0 {
			t.Errorf("server %d keeps u1's job in %v", i+1, got)
		}
	}
}

// Close takes each in-memory store's eraser with it.
func TestCloseUnregistersMemoryStores(t *testing.T) {
	q := NewMemoryQueue(0)
	s := NewScheduler(q)
	names := []string{q.eraserName(), s.eraserName()}
	for _, n := range names {
		if !storeRegistered(n) {
			t.Fatalf("%s not registered after construction", n)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if storeRegistered(n) {
			t.Errorf("%s still registered after Close", n)
		}
	}
}
