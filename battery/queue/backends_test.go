package queue

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The DB-backed scenarios run against both queue databases: an in-memory
// SQLite, and a real Postgres when one is configured (internal/pgtest
// skips the suite when it is not).

// forBackendDBs runs assert once per backend, as one subtest per backend.
func forBackendDBs(t *testing.T, assert func(t *testing.T, db *sql.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		assert(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		assert(t, pgtest.DB(t))
	})
}

// forBackendQueues is forBackendDBs with a DBQueue built from opts on each
// backend's database.
func forBackendQueues(t *testing.T, opts []DBQueueOption, assert func(t *testing.T, db *sql.DB, q *DBQueue)) {
	t.Helper()
	forBackendDBs(t, func(t *testing.T, db *sql.DB) {
		q, err := NewDBQueue(db, opts...)
		if err != nil {
			t.Fatal(err)
		}
		assert(t, db, q)
	})
}

func TestWideQueueNumbersParity(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("wide queue numbers require 64-bit Go integers")
	}
	wide, err := strconv.Atoi("2147483648")
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "upgraded"
		}
		t.Run(name, func(t *testing.T) {
			forBackendDBs(t, func(t *testing.T, db *sql.DB) {
				q := newDurableTestQueue(t, db)
				cfg := DurableSchedulerConfig{OwnerID: "wide-numbers"}
				sched, err := NewDurableScheduler(q, cfg)
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
				if legacy {
					if err := q.Enqueue(ctx, Job{ID: "old", Type: "old", Priority: 17, ScheduledAt: base}); err != nil {
						t.Fatal(err)
					}
					if q.dialect == dialectPostgres {
						for _, ddl := range []string{
							"ALTER TABLE " + q.qt() + " ALTER COLUMN priority TYPE INTEGER, ALTER COLUMN attempts TYPE INTEGER, ALTER COLUMN max_attempts TYPE INTEGER",
							"ALTER TABLE " + q.schedulerSchedulesTable() + " ALTER COLUMN priority TYPE INTEGER, ALTER COLUMN max_attempts TYPE INTEGER",
						} {
							if _, err := db.Exec(ddl); err != nil {
								t.Fatal(err)
							}
						}
					}
					q = newDurableTestQueue(t, db)
					sched, err = NewDurableScheduler(q, cfg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := q.Dequeue(ctx, "old")
					if err != nil || got.ID != "old" || got.Priority != 17 || got.Attempts != 1 || got.MaxAttempts != 3 {
						t.Errorf("legacy job = %+v, %v", got, err)
					}
				}
				t.Run("job", func(t *testing.T) {
					job := Job{ID: "wide", Type: "direct", Priority: wide, Attempts: wide, MaxAttempts: wide + 2, ScheduledAt: base}
					if err := q.Enqueue(ctx, job); err != nil {
						t.Fatal(err)
					}
					got, err := q.Dequeue(ctx, job.Type)
					if err != nil || got.ID != job.ID || got.Priority != wide || got.Attempts != wide+1 || got.MaxAttempts != wide+2 {
						t.Errorf("claimed job = %+v, %v", got, err)
					}
				})
				t.Run("schedule", func(t *testing.T) {
					if err := sched.Every("wide", time.Minute).Job("scheduled", nil).
						Priority(-wide - 1).MaxAttempts(wide).RegisterAt(base); err != nil {
						t.Fatal(err)
					}
					if err := sched.RunOnce(ctx, base.Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
					jobs := pendingJobs(t, q)
					if len(jobs) != 1 || jobs[0].Type != "scheduled" || jobs[0].Priority != -wide-1 || jobs[0].MaxAttempts != wide {
						t.Errorf("scheduled jobs = %+v", jobs)
					}
				})
			})
		})
	}
}
