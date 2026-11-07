package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// TestPGSequenceKeyOrderUnderWorkers runs three keys' jobs through eight
// racing Postgres workers: each key's jobs must run one at a time and in
// enqueue order, while other keys run beside them.
func TestPGSequenceKeyOrderUnderWorkers(t *testing.T) {
	db := pgtest.DB(t)
	db.SetMaxOpenConns(10)
	q, err := NewDBQueue(db, WithTable("seq_order_jobs"), WithWorkers(8))
	if err != nil {
		t.Fatal(err)
	}
	const keys, perKey = 3, 15
	var mu sync.Mutex
	order := map[string][]int{}
	running := map[string]int{}
	overlap := ""
	q.RegisterHandler("deliver", func(_ context.Context, job Job) error {
		mu.Lock()
		running[job.SequenceKey]++
		if running[job.SequenceKey] > 1 && overlap == "" {
			overlap = job.SequenceKey
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(job.ID, job.SequenceKey+"-"))
		order[job.SequenceKey] = append(order[job.SequenceKey], n)
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		running[job.SequenceKey]--
		mu.Unlock()
		return nil
	})
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for i := range perKey {
		for k := range keys {
			key := fmt.Sprintf("k%d", k)
			if err := q.Enqueue(ctx, Job{
				ID: fmt.Sprintf("%s-%d", key, i), Type: "deliver", SequenceKey: key,
				CreatedAt: base.Add(time.Duration(i) * time.Millisecond),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	q.Start(ctx)
	t.Cleanup(func() { q.Close() })
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		total := 0
		for _, ns := range order {
			total += len(ns)
		}
		return total == keys*perKey
	}, 30*time.Second, "every job ran")
	mu.Lock()
	defer mu.Unlock()
	if overlap != "" {
		t.Fatalf("two jobs of key %s ran at once", overlap)
	}
	for key, ns := range order {
		for i, n := range ns {
			if n != i {
				t.Fatalf("key %s ran in order %v", key, ns)
			}
		}
	}
}

// TestPGSequenceReplayRaceOneClaim pauses a claim of the newer job after
// it read the key as free, replays and claims the older job meanwhile,
// then lets the paused claim finish: one key, one claim, whichever wins.
// A trigger holds the newer job's UPDATE on an advisory lock the test owns.
func TestPGSequenceReplayRaceOneClaim(t *testing.T) {
	db := pgtest.DB(t)
	db.SetMaxOpenConns(8)
	q, err := NewDBQueue(db, WithTable("seq_race_jobs"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"older", "newer"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: "deliver", SequenceKey: "record", MaxAttempts: 3,
			CreatedAt: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	claimAndFail(t, q, Permanent(errors.New("410")))
	const barrier = 6802026
	if _, err := db.Exec(fmt.Sprintf(`CREATE FUNCTION seq_race_pause() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN
		IF NEW.id = 'newer' AND NEW.status = 'claimed' THEN PERFORM pg_advisory_xact_lock(%d); END IF;
		RETURN NEW;
	END $$;
	CREATE TRIGGER seq_race_pause BEFORE UPDATE ON seq_race_jobs
	FOR EACH ROW EXECUTE FUNCTION seq_race_pause()`, barrier)); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_lock($1)", barrier); err != nil {
		t.Fatal(err)
	}
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", barrier); err != nil {
				t.Error(err)
			}
		}
	}
	defer unlock()
	paused := make(chan error, 1)
	go func() {
		_, err := q.Dequeue(ctx)
		paused <- err
	}()
	waitFor(t, func() bool {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_locks
			WHERE locktype = 'advisory' AND objid = $1 AND NOT granted)`, barrier).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		return waiting
	}, 5*time.Second, "newer's claim paused inside its UPDATE")
	if err := q.Replay(ctx, "older"); err != nil {
		t.Fatal(err)
	}
	if job, err := q.Dequeue(ctx); err != nil || job.ID != "older" {
		t.Fatalf("got %q, %v; want older", job.ID, err)
	}
	unlock()
	if err := <-paused; err != nil && !errors.Is(err, ErrNoJob) {
		t.Fatalf("paused claim: %v", err)
	}
	var claimed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seq_race_jobs WHERE sequence_key = 'record' AND status = 'claimed'`).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if claimed != 1 {
		t.Fatalf("key record has %d live claims, want 1", claimed)
	}
	if lost := stored(t, q, "newer"); lost.Status != "pending" || lost.Attempts != 0 {
		t.Fatalf("losing claim left newer %s at attempt %d, want pending at 0", lost.Status, lost.Attempts)
	}
}
