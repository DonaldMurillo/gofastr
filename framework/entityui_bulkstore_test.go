package framework

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

func bulkStoreJob(id, key string, count int) entityui.BulkJob {
	return entityui.BulkJob{ID: id, Entity: "notes", Action: "delete", Count: count, Creator: "u1", FilterHash: "h", Key: key}
}

// claimed creates a store, writes job with ids, and takes its lease for
// runner "r1".
func claimed(t *testing.T, db *sql.DB, job entityui.BulkJob, ids []string) (*sqlBulkStore, time.Time) {
	t.Helper()
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, job, ids); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if ok, err := s.Claim(ctx, job.ID, "r1", now, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	return s, now
}

// The snapshot store keeps a job and its ids, hands back the unsettled
// ids in confirm order, keeps an id's first outcome, and on finish keeps
// the tally on the job and drops the ids.
func TestSQLBulkStoreRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		ids := make([]string, 0, bulkInsertBatch+3)
		for i := range bulkInsertBatch + 3 {
			ids = append(ids, "r"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
		}
		s, _ := claimed(t, db, bulkStoreJob("j1", "k1", len(ids)), ids)
		if _, err := newSQLBulkStore(ctx, db); err != nil {
			t.Fatalf("second ensure: %v", err)
		}
		got, err := s.Job(ctx, "j1")
		if err != nil || got.Status != entityui.BulkQueued || got.Creator != "u1" || got.Count != len(ids) || got.Key != "k1" {
			t.Fatalf("job = %+v, %v", got, err)
		}
		first, err := s.Pending(ctx, "j1", 2)
		if err != nil || !slices.Equal(first, ids[:2]) {
			t.Fatalf("pending = %v, %v; want %v", first, err, ids[:2])
		}
		if err := s.Settle(ctx, "j1", "r1", map[string]string{ids[0]: entityui.BulkRowDone, ids[1]: entityui.BulkRowSkipped}); err != nil {
			t.Fatal(err)
		}
		if err := s.Settle(ctx, "j1", "r1", map[string]string{ids[0]: entityui.BulkRowFailed}); err != nil {
			t.Fatal(err)
		}
		if tally, err := s.Tally(ctx, "j1"); err != nil || len(tally) != 2 || tally[entityui.BulkRowDone] != 1 || tally[entityui.BulkRowSkipped] != 1 {
			t.Fatalf("tally = %v, %v; want 1 done 1 skipped (the first outcome kept)", tally, err)
		}
		rest, err := s.Pending(ctx, "j1", len(ids))
		if err != nil || len(rest) != len(ids)-2 || rest[0] != ids[2] {
			t.Fatalf("pending after settle = %d ids, %v", len(rest), err)
		}
		if err := s.Settle(ctx, "j1", "r1", map[string]string{ids[2]: "maybe"}); err == nil {
			t.Fatal("an unknown outcome settled")
		}
		if err := s.Finish(ctx, "j1", "r1", "paused", time.Now()); err == nil {
			t.Fatal("an unknown status was written")
		}
		if err := s.Finish(ctx, "j1", "r1", entityui.BulkQueued, time.Now()); err == nil {
			t.Fatal("Finish wrote queued, which is not a final status")
		}
		if err := s.Finish(ctx, "j1", "r1", entityui.BulkDone, time.Now()); err != nil {
			t.Fatal(err)
		}
		got, err = s.Job(ctx, "j1")
		if err != nil || got.Status != entityui.BulkDone || got.Done != 1 || got.Skipped != 1 || got.Failed != 0 {
			t.Fatalf("finished job = %+v, %v; want done with 1 done 1 skipped", got, err)
		}
		var left int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + bulkItemsTable + ` WHERE job_id = 'j1'`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("a finished job kept %d ids (%v), want 0", left, err)
		}
		if _, err := s.Job(ctx, "nope"); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("unknown job = %v", err)
		}
		if err := s.Finish(ctx, "nope", "r1", entityui.BulkDone, time.Now()); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("finish unknown job = %v", err)
		}
		if err := s.Enqueued(ctx, "nope"); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("enqueued unknown job = %v", err)
		}
	})
}

// One runner holds a job at a time. A second runner is refused while the
// lease is live, takes it once it runs out, and the first runner can then
// neither settle nor finish. A finished job cannot be claimed.
func TestSQLBulkStoreLeaseFencesWriters(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		s, now := claimed(t, db, bulkStoreJob("j1", "k1", 2), []string{"a", "b"})
		if ok, err := s.Claim(ctx, "j1", "r2", now.Add(30*time.Second), now.Add(time.Hour)); err != nil || ok {
			t.Fatalf("SECURITY: a second runner took a live lease: %v, %v", ok, err)
		}
		if ok, err := s.Claim(ctx, "j1", "r1", now.Add(30*time.Second), now.Add(2*time.Minute)); err != nil || !ok {
			t.Fatalf("the holder could not renew: %v, %v", ok, err)
		}
		if err := s.Settle(ctx, "j1", "r2", map[string]string{"a": entityui.BulkRowDone}); !errors.Is(err, entityui.ErrBulkLeaseLost) {
			t.Fatalf("SECURITY: a runner without the lease settled: %v", err)
		}
		later := now.Add(3 * time.Minute)
		if ok, err := s.Claim(ctx, "j1", "r2", later, later.Add(time.Minute)); err != nil || !ok {
			t.Fatalf("an expired lease was not taken over: %v, %v", ok, err)
		}
		if err := s.Settle(ctx, "j1", "r1", map[string]string{"a": entityui.BulkRowDone}); !errors.Is(err, entityui.ErrBulkLeaseLost) {
			t.Fatalf("SECURITY: the runner that lost the lease settled: %v", err)
		}
		if err := s.Finish(ctx, "j1", "r1", entityui.BulkDone, later); !errors.Is(err, entityui.ErrBulkLeaseLost) {
			t.Fatalf("SECURITY: the runner that lost the lease finished the job: %v", err)
		}
		if tally, _ := s.Tally(ctx, "j1"); len(tally) != 0 {
			t.Fatalf("SECURITY: refused settles wrote %v", tally)
		}
		if err := s.Finish(ctx, "j1", "r2", entityui.BulkDone, later); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.Claim(ctx, "j1", "r3", later.Add(time.Hour), later.Add(2*time.Hour)); err != nil || ok {
			t.Fatalf("a finished job was claimed: %v, %v", ok, err)
		}
		if ok, err := s.Claim(ctx, "j1", "", later, later); err == nil || ok {
			t.Fatal("a claim with no runner token was accepted")
		}
	})
}

// A run key holds one queued job: creating it again answers the queued
// job and writes nothing; once that job finishes, the key is free.
func TestSQLBulkStoreRunKeyOneQueued(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		s, now := claimed(t, db, bulkStoreJob("j1", "same", 1), []string{"a"})
		got, err := s.Create(ctx, bulkStoreJob("j2", "same", 1), []string{"a"})
		if err != nil || got.ID != "j1" {
			t.Fatalf("second create = %+v, %v; want job j1", got, err)
		}
		if _, err := s.Job(ctx, "j2"); !errors.Is(err, errBulkJobUnknown) {
			t.Fatalf("SECURITY: the duplicate confirm wrote job j2: %v", err)
		}
		if _, err := s.Create(ctx, bulkStoreJob("j3", "", 1), []string{"a"}); err == nil {
			t.Fatal("a job with no run key was created")
		}
		if err := s.Finish(ctx, "j1", "r1", entityui.BulkDone, now); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Create(ctx, bulkStoreJob("j4", "same", 1), []string{"a"}); err != nil || got.ID != "j4" {
			t.Fatalf("create after the first finished = %+v, %v; want j4", got, err)
		}
	})
}

// Unenqueued lists the queued jobs no Enqueued call marked, created
// before the cutoff; Prune drops finished jobs past it and keeps the rest.
func TestSQLBulkStoreResumeAndPrune(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		s, now := claimed(t, db, bulkStoreJob("done", "k1", 1), []string{"a"})
		if err := s.Finish(ctx, "done", "r1", entityui.BulkDone, now); err != nil {
			t.Fatal(err)
		}
		for _, j := range []entityui.BulkJob{bulkStoreJob("orphan", "k2", 1), bulkStoreJob("handed", "k3", 1)} {
			if _, err := s.Create(ctx, j, []string{"a"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Enqueued(ctx, "handed"); err != nil {
			t.Fatal(err)
		}
		cut := time.Now().Add(time.Minute)
		jobs, err := s.Unenqueued(ctx, cut)
		if err != nil || len(jobs) != 1 || jobs[0].ID != "orphan" {
			t.Fatalf("Unenqueued = %+v, %v; want orphan alone", jobs, err)
		}
		if jobs, _ := s.Unenqueued(ctx, now.Add(-time.Hour)); len(jobs) != 0 {
			t.Fatalf("Unenqueued before the jobs existed = %+v", jobs)
		}
		if n, err := s.Prune(ctx, now.Add(-time.Second)); err != nil || n != 0 {
			t.Fatalf("Prune before the finish = %d, %v; want 0", n, err)
		}
		if n, err := s.Prune(ctx, cut); err != nil || n != 1 {
			t.Fatalf("Prune = %d, %v; want the finished job", n, err)
		}
		for _, id := range []string{"orphan", "handed"} {
			if _, err := s.Job(ctx, id); err != nil {
				t.Fatalf("Prune dropped queued job %s: %v", id, err)
			}
		}
	})
}

// Create is all or nothing: a reused job id or a repeated record id
// leaves no partial job behind. Pending with no room returns nothing.
func TestSQLBulkStoreCreateRollsBack(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, bulkStoreJob("j1", "k1", 1), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, bulkStoreJob("j1", "k9", 1), []string{"b"}); err == nil {
		t.Fatal("a reused job id was created")
	}
	if _, err := s.Create(ctx, bulkStoreJob("j2", "k2", 2), []string{"a", "a"}); err == nil {
		t.Fatal("a selection naming one record twice was created")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + bulkJobsTable + ` WHERE id = 'j2'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed create left %d job rows (%v), want 0", n, err)
	}
	if got, err := s.Pending(ctx, "j1", 0); err != nil || got != nil {
		t.Fatalf("Pending limit 0 = %v, %v; want nil", got, err)
	}
}

// Every store call reports a database it cannot reach rather than
// answering as if the job were empty, settled or free.
func TestSQLBulkStoreReportsDBErrors(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	now := time.Now()
	if _, err := s.Create(ctx, bulkStoreJob("j1", "k1", 1), []string{"a"}); err == nil {
		t.Error("Create on a closed database succeeded")
	}
	if _, err := s.Claim(ctx, "j1", "r1", now, now); err == nil {
		t.Error("Claim on a closed database succeeded")
	}
	if _, err := s.Pending(ctx, "j1", 10); err == nil {
		t.Error("Pending on a closed database returned no error")
	}
	if err := s.Settle(ctx, "j1", "r1", map[string]string{"a": entityui.BulkRowDone}); err == nil {
		t.Error("Settle on a closed database succeeded")
	}
	if err := s.Finish(ctx, "j1", "r1", entityui.BulkDone, now); err == nil {
		t.Error("Finish on a closed database succeeded")
	}
	if err := s.Enqueued(ctx, "j1"); err == nil {
		t.Error("Enqueued on a closed database succeeded")
	}
	if _, err := s.Unenqueued(ctx, now); err == nil {
		t.Error("Unenqueued on a closed database returned no error")
	}
	if _, err := s.Prune(ctx, now); err == nil {
		t.Error("Prune on a closed database succeeded")
	}
	if _, err := newSQLBulkStore(ctx, db); err == nil {
		t.Error("ensuring the schema on a closed database succeeded")
	}
}
