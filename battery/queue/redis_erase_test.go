package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
)

// redisHolds reports every list entry and hash value of r that mentions s.
func redisHolds(r *mockRedis, s string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for key, l := range r.lists {
		for _, v := range l {
			if strings.Contains(v, s) {
				out = append(out, key)
			}
		}
	}
	for key, h := range r.hashes {
		for _, v := range h {
			if strings.Contains(v, s) {
				out = append(out, key)
			}
		}
	}
	return out
}

func eraseQueueApp(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return framework.NewApp(framework.WithDB(db))
}

// storeResult is the report line of the store eraser named name.
func storeResult(rep framework.EraseReport, name string) (framework.EraseTableResult, bool) {
	for _, r := range rep.Stores {
		if r.Name == name {
			return r, true
		}
	}
	return framework.EraseTableResult{}, false
}

// storeRegistered reports whether a store eraser named name is registered.
func storeRegistered(name string) bool {
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// RedisQueue stores json.Marshal(job) in its main, delayed and dead lists
// and its pop and processing hashes, none of which the queue_jobs eraser
// reaches, so a job carrying the erased user's data outlived the erasure
// (2026-10-27 S25). The queue now registers a store eraser that removes
// every job naming the user from all five, and leaves other users' jobs
// and entries it cannot decode.
func TestEraseRemovesRedisJobs(t *testing.T) {
	ctx := context.Background()
	r := newMockRedis()
	q := NewRedisQueue(r, "erase-q")
	t.Cleanup(func() { _ = q.Close() })
	job := func(id, user string) Job {
		return Job{ID: id, Type: "mail", UserID: user, Payload: json.RawMessage(`{"to":"` + user + `-secret"}`)}
	}
	// Dead: one attempt, claimed and failed.
	dead := job("d1", "u1")
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
	// Processing: claimed and still running.
	if err := q.Enqueue(ctx, job("p1", "u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatal(err)
	}
	// Main and delayed.
	later := job("l1", "u1")
	later.ScheduledAt = time.Now().Add(time.Hour)
	for _, j := range []Job{job("m1", "u1"), job("m2", "u2"), later} {
		if err := q.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	// A pop record a worker died holding, and an entry that does not decode.
	popped, _ := json.Marshal(job("o1", "u1"))
	if err := r.HSet(ctx, "erase-q:popped", "1:tok", string(popped)); err != nil {
		t.Fatal(err)
	}
	if err := r.LPush(ctx, "erase-q", "not json"); err != nil {
		t.Fatal(err)
	}
	app := eraseQueueApp(t)

	dry, err := app.EraseUserData(ctx, "u1", framework.WithEraseDryRun())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got := redisHolds(r, "u1-secret"); len(got) != 5 {
		t.Fatalf("the dry run removed jobs: u1 is left in %v, want 5 entries", got)
	}
	rep, err := app.EraseUserData(ctx, "u1")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	if got := redisHolds(r, "u1-secret"); len(got) != 0 {
		t.Errorf("u1's jobs survive the erasure in %v", got)
	}
	if got := redisHolds(r, "u2-secret"); len(got) != 1 || got[0] != "erase-q" {
		t.Errorf("u2's job is in %v, want only the main list", got)
	}
	if got := redisHolds(r, "not json"); len(got) != 1 {
		t.Errorf("the entry that does not decode is in %v, want it left on the main list", got)
	}
	got, ok := storeResult(rep, "queue_redis:erase-q")
	want, _ := storeResult(dry, "queue_redis:erase-q")
	if !ok || got.RowsAffected != 5 || want.RowsAffected != 5 {
		t.Errorf("report stores = %+v, dry run %+v; want queue_redis:erase-q with 5 jobs in both", rep.Stores, dry.Stores)
	}
	// A worker that finishes the erased claim finds nothing to settle.
	if err := q.Ack(ctx, Job{ID: "p1"}); err != nil {
		t.Errorf("Ack of an erased claim: %v", err)
	}
}

func TestClosedRedisQueueLeavesErasure(t *testing.T) {
	q := NewRedisQueue(newMockRedis(), "closed-q")
	if !storeRegistered("queue_redis:closed-q") {
		t.Fatalf("store erasers after NewRedisQueue = %+v, want the queue's", datexport.AllStoreErasers())
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if storeRegistered("queue_redis:closed-q") {
		t.Errorf("store erasers after Close = %+v, want the queue's gone", datexport.AllStoreErasers())
	}
}
