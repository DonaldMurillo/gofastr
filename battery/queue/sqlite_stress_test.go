package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The reported reproduction (#656): one SQLite-file run, 12 workers over
// two lanes, left a job `claimed` past its lease window once in 15 runs
// — a nack whose UPDATE met SQLITE_BUSY and gave up. This soak drives
// that shape hard (two queues on one file, a 5ms busy timeout, half the
// handlers nacking) and asserts the drain invariant: once every job has
// settled, none sits claimed. Bounded to ~30s.
func TestSQLiteFileStressDrainsClean(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "stress.db")) +
		"?_busy_timeout=5&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const jobs = 600
	var nacked atomic.Int64
	mk := func() *DBQueue {
		q, err := NewDBQueue(db, WithWorkers(6), WithDoneRetention(time.Hour),
			WithLeaseTimeout(20*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		q.RegisterHandler("ok", func(context.Context, Job) error { return nil })
		q.RegisterHandler("boom", func(context.Context, Job) error {
			nacked.Add(1)
			return errors.New("flake")
		})
		return q
	}
	queues := []*DBQueue{mk(), mk()}
	for _, q := range queues {
		for i := range jobs / 2 {
			typ := "ok"
			if i%2 == 0 {
				typ = "boom"
			}
			if err := q.Enqueue(ctx, Job{Type: typ, Payload: []byte("{}"), MaxAttempts: 3}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
		}
	}
	for _, q := range queues {
		q.Start(ctx)
	}
	defer func() {
		for _, q := range queues {
			q.Close()
		}
	}()

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		var claimed, pending int
		for _, table := range []string{"queue_jobs"} {
			for _, status := range []string{"claimed", "pending"} {
				q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE status = $1", table)
				var n int
				if err := db.QueryRowContext(ctx, q, status).Scan(&n); err != nil {
					t.Fatalf("count %s/%s: %v", table, status, err)
				}
				if status == "claimed" {
					claimed += n
				} else {
					pending += n
				}
			}
		}
		if pending == 0 && claimed == 0 {
			// Every job settled and nothing is stuck mid-flight.
			if nacked.Load() == 0 {
				t.Fatal("the flaky handler never ran; the soak proved nothing")
			}
			for _, status := range []string{"done", "failed"} {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM queue_jobs WHERE status = $1", status).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != jobs/2 {
					t.Fatalf("%s rows=%d, want %d", status, count, jobs/2)
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	var stuck int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM queue_jobs WHERE status='claimed'").Scan(&stuck); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("drain did not finish in 25s (stuck claimed rows: %d)", stuck)
}
