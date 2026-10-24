package queue

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// uniqueDBQueues returns the dialects the DBQueue uniqueness contract runs
// on: SQLite always, Postgres when pgtest can reach one.
func uniqueDBQueues(t *testing.T) map[string]func(t *testing.T) *DBQueue {
	return map[string]func(t *testing.T) *DBQueue{
		"sqlite": func(t *testing.T) *DBQueue {
			_, q := openDBQueue(t, 1)
			return q
		},
		"postgres": func(t *testing.T) *DBQueue {
			q, err := NewDBQueue(pgtest.DB(t))
			if err != nil {
				t.Fatalf("new db queue: %v", err)
			}
			return q
		},
	}
}

func TestDBUniqueKeyRefusesLiveDuplicate(t *testing.T) {
	for name, open := range uniqueDBQueues(t) {
		t.Run(name, func(t *testing.T) {
			q := open(t)
			ctx := context.Background()
			if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "user-1"}); err != nil {
				t.Fatalf("first enqueue: %v", err)
			}
			err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "user-1"})
			if !errors.Is(err, ErrDuplicateJob) {
				t.Fatalf("second enqueue: err = %v, want ErrDuplicateJob", err)
			}
			// The key is scoped to the type, and an empty key never dedupes.
			if err := q.Enqueue(ctx, Job{Type: "export", UniqueKey: "user-1"}); err != nil {
				t.Errorf("same key, other type: %v", err)
			}
			for range 2 {
				if err := q.Enqueue(ctx, Job{Type: "digest"}); err != nil {
					t.Errorf("no key: %v", err)
				}
			}

			// A claimed job still holds its key, and a retried one too.
			job, err := q.Dequeue(ctx, "digest")
			if err != nil || job.UniqueKey != "user-1" {
				t.Fatalf("dequeue: job = %+v, err = %v", job, err)
			}
			if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "user-1"}); !errors.Is(err, ErrDuplicateJob) {
				t.Errorf("while claimed: err = %v, want ErrDuplicateJob", err)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "user-1"}); !errors.Is(err, ErrDuplicateJob) {
				t.Errorf("after a retry: err = %v, want ErrDuplicateJob", err)
			}

			// Finishing the job frees the key.
			job, err = q.Dequeue(ctx, "digest")
			if err != nil || job.UniqueKey != "user-1" {
				t.Fatalf("second dequeue: job = %+v, err = %v", job, err)
			}
			if err := q.Ack(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "user-1"}); err != nil {
				t.Errorf("after ack: %v", err)
			}
		})
	}
}

func TestDBUniqueKeyFreedByDeadLetter(t *testing.T) {
	for name, open := range uniqueDBQueues(t) {
		t.Run(name, func(t *testing.T) {
			q := open(t)
			ctx := context.Background()
			if err := q.Enqueue(ctx, Job{ID: "dead", Type: "digest", UniqueKey: "k", MaxAttempts: 1}); err != nil {
				t.Fatal(err)
			}
			job, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err := q.Enqueue(ctx, Job{ID: "next", Type: "digest", UniqueKey: "k"}); err != nil {
				t.Fatalf("after dead-letter: %v", err)
			}
			// Replaying the dead job would make two live jobs with one key.
			if err := q.Replay(ctx, "dead"); !errors.Is(err, ErrDuplicateJob) {
				t.Fatalf("replay over a live key: err = %v, want ErrDuplicateJob", err)
			}
			jobs, err := q.ListJobs(ctx, "failed", 10, 0)
			if err != nil || len(jobs) != 1 || jobs[0].UniqueKey != "k" {
				t.Fatalf("failed jobs = %+v, err = %v; want the dead job kept with its key", jobs, err)
			}
		})
	}
}

// The scheduler and any caller enqueueing inside a transaction share
// enqueueWith; a duplicate must not abort the transaction on Postgres.
func TestDBUniqueKeyKeepsTxUsable(t *testing.T) {
	for name, open := range uniqueDBQueues(t) {
		t.Run(name, func(t *testing.T) {
			q := open(t)
			ctx := context.Background()
			tx, err := q.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := q.enqueueWith(ctx, tx, Job{Type: "digest", UniqueKey: "k"}); err != nil {
				t.Fatal(err)
			}
			if err := q.enqueueWith(ctx, tx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
				t.Fatalf("duplicate in tx: err = %v, want ErrDuplicateJob", err)
			}
			if err := q.enqueueWith(ctx, tx, Job{Type: "other"}); err != nil {
				t.Fatalf("tx after a duplicate: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q.qt()).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 2 {
				t.Errorf("rows = %d, want 2", n)
			}
		})
	}
}

// A table name near Postgres's 63-byte cap still gets three distinct
// indexes, so the unique index is not skipped as an existing name.
func TestDBUniqueKeyLongTableName(t *testing.T) {
	db := pgtest.DB(t)
	table := strings.Repeat("q", 62)
	q, err := NewDBQueue(db, WithTable(table))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("second enqueue: err = %v, want ErrDuplicateJob", err)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes
		 WHERE schemaname = current_schema() AND tablename = $1 AND indexname LIKE '%\_idx'`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("indexes = %d, want 3", n)
	}
}

// A table created before unique_key existed gains the column and the index.
func TestDBUniqueKeyMigratesOldTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE queue_jobs (
		id TEXT PRIMARY KEY, type TEXT NOT NULL, payload TEXT,
		priority INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3, created_at DATETIME NOT NULL,
		scheduled_at DATETIME NOT NULL, status TEXT NOT NULL DEFAULT 'pending')`); err != nil {
		t.Fatal(err)
	}
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("new db queue on an old table: %v", err)
	}
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("err = %v, want ErrDuplicateJob", err)
	}
}

func TestMemoryUniqueKeyLifecycle(t *testing.T) {
	q := NewMemoryQueue(1)
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "a", Type: "digest", UniqueKey: "k", MaxAttempts: 2}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("while pending: err = %v, want ErrDuplicateJob", err)
	}
	if err := q.Enqueue(ctx, Job{ID: "a", Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("holder's own ID: err = %v, want ErrDuplicateJob", err)
	}
	if err := q.Enqueue(ctx, Job{Type: "other", UniqueKey: "k"}); err != nil {
		t.Errorf("same key, other type: %v", err)
	}

	// A retry re-enqueues the same job, which must not trip its own key.
	job, err := q.Dequeue(ctx, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Errorf("while claimed: err = %v, want ErrDuplicateJob", err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatalf("nack requeue: %v", err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); !errors.Is(err, ErrDuplicateJob) {
		t.Errorf("after a retry: err = %v, want ErrDuplicateJob", err)
	}

	// The second failure dead-letters it and frees the key.
	job, err = q.Dequeue(ctx, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{ID: "b", Type: "digest", UniqueKey: "k"}); err != nil {
		t.Fatalf("after dead-letter: %v", err)
	}
	if err := q.Replay(ctx, "a"); !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("replay over a live key: err = %v, want ErrDuplicateJob", err)
	}

	// Ack frees it.
	job, err = q.Dequeue(ctx, "digest")
	if err != nil || job.ID != "b" {
		t.Fatalf("dequeue: job = %+v, err = %v", job, err)
	}
	if err := q.Ack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); err != nil {
		t.Errorf("after ack: %v", err)
	}
}

// The worker pool frees the key when the handler succeeds.
func TestMemoryUniqueKeyFreedByWorker(t *testing.T) {
	q := NewMemoryQueue(1)
	done := make(chan struct{}, 1)
	q.RegisterHandler("digest", func(context.Context, Job) error {
		done <- struct{}{}
		return nil
	})
	q.Start()
	t.Cleanup(func() { q.Close() })
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"}); err != nil {
		t.Fatal(err)
	}
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := q.Enqueue(ctx, Job{Type: "digest", UniqueKey: "k"})
		if err == nil {
			return
		}
		if !errors.Is(err, ErrDuplicateJob) || time.Now().After(deadline) {
			t.Fatalf("after the handler finished: err = %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// RedisClient has no atomic set-if-absent, so RedisQueue refuses a key it
// cannot hold rather than enqueueing a job it would not deduplicate.
func TestRedisRefusesUniqueKey(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "jobs")
	err := q.Enqueue(context.Background(), Job{Type: "digest", UniqueKey: "k"})
	if !errors.Is(err, ErrUniqueUnsupported) {
		t.Fatalf("err = %v, want ErrUniqueUnsupported", err)
	}
	if _, err := q.Dequeue(context.Background()); !errors.Is(err, ErrNoJob) {
		t.Errorf("the refused job was enqueued anyway: %v", err)
	}
}
