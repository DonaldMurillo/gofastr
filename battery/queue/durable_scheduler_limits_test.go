package queue

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestDurableSchedulerBoundsCatchUp(t *testing.T) {
	const limit = 8
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base.Add(6 * 365 * 24 * time.Hour)
	for _, test := range []struct {
		name     string
		schedule func(*DurableScheduler) *DurableScheduleBuilder
	}{
		{name: "interval", schedule: func(s *DurableScheduler) *DurableScheduleBuilder {
			return s.Every("bounded", time.Millisecond)
		}},
		{name: "cron", schedule: func(s *DurableScheduler) *DurableScheduleBuilder {
			return s.Cron("bounded", "* * * * *")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gosqlite.Open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			db.SetMaxOpenConns(1)
			q, err := NewDBQueue(db)
			if err != nil {
				t.Fatal(err)
			}
			scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{
				OwnerID:               "bounded",
				LeaseDuration:         time.Minute,
				MaxCatchUpOccurrences: limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.schedule(scheduler).Job("bounded", nil).RegisterAt(base); err != nil {
				t.Fatal(err)
			}
			if err := scheduler.RunOnce(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			var occurrences int
			if err := db.QueryRow("SELECT COUNT(*) FROM " + q.schedulerOccurrencesTable()).Scan(&occurrences); err != nil {
				t.Fatal(err)
			}
			if occurrences != limit {
				t.Fatalf("occurrences = %d, want bounded %d", occurrences, limit)
			}
		})
	}
}

func TestOccurrenceRetentionDisableAndCadence(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "retention", OccurrenceRetention: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.sweepOccurrences(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	insertSkippedOccurrence(t, db, q, "after-sweep", now.Add(-48*time.Hour))
	if err := scheduler.sweepOccurrences(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceCount(t, db, q, "after-sweep", 1)
	if err := scheduler.sweepOccurrences(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceCount(t, db, q, "after-sweep", 0)

	disabled, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "disabled", OccurrenceRetention: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertSkippedOccurrence(t, db, q, "disabled", now.Add(-48*time.Hour))
	if err := disabled.sweepOccurrences(context.Background(), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertOccurrenceCount(t, db, q, "disabled", 1)
}

func insertSkippedOccurrence(t *testing.T, db *sql.DB, q *DBQueue, id string, created time.Time) {
	t.Helper()
	_, err := db.Exec(fmt.Sprintf(`INSERT INTO %s
		(occurrence_id, schedule_id, scheduled_tick, status, skip_reason,
		 claim_owner, claim_fence, created_at, enqueued_job_id)
		VALUES ($1,$2,$3,'skipped','missed',$4,1,$3,'')`,
		q.schedulerOccurrencesTable()), id, "bounded", created, "retention")
	if err != nil {
		t.Fatal(err)
	}
}

func assertOccurrenceCount(t *testing.T, db *sql.DB, q *DBQueue, id string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+q.schedulerOccurrencesTable()+
		" WHERE occurrence_id=$1", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("occurrence %q count = %d, want %d", id, count, want)
	}
}
