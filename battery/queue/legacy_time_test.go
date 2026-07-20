package queue

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

const legacyTimeLayout = "2006-01-02 15:04:05.999999999-07:00"

// legacyQueueDB creates the queue schema, seeds rows through rawInsert in
// the legacy mattn space-separated text format (as an old binary would
// have written them), then constructs a fresh DBQueue over the same DB —
// the upgrade scenario a new binary sees at startup.
func legacyQueueDB(t *testing.T, rawInsert func(t *testing.T, db *sql.DB)) *DBQueue {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := NewDBQueue(db); err != nil {
		t.Fatalf("bootstrap queue: %v", err)
	}
	rawInsert(t, db)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatalf("reopen queue: %v", err)
	}
	return q
}

// A job claimed moments ago by a legacy-format writer keeps its lease: the
// space-separated claimed_at must not compare as expired against the pure
// driver's RFC3339 binds (lexicographic TEXT comparison), which would
// double-run an in-flight job.
func TestLegacyActiveLeaseNotReclaimedQueue(t *testing.T) {
	now := time.Now().UTC()
	q := legacyQueueDB(t, func(t *testing.T, db *sql.DB) {
		past := now.Add(-time.Hour).Format(legacyTimeLayout)
		if _, err := db.Exec(`INSERT INTO "queue_jobs"
			(id, occurrence_id, type, payload, priority, lane, attempts, max_attempts,
			 created_at, scheduled_at, status, claimed_at)
			VALUES ('j1','', 'work','null',0,'',1,3,?,?,'claimed',?)`,
			past, past, now.Format(legacyTimeLayout)); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := q.Dequeue(context.Background(), "work"); !errors.Is(err, ErrNoJob) {
		t.Fatalf("in-flight job with an active legacy lease was reclaimed (err=%v)", err)
	}
}

// A pending job whose legacy-format scheduled_at is an hour in the future
// (e.g. retry backoff written by an old binary) must wait, not run now.
func TestLegacyFutureScheduledWaits(t *testing.T) {
	now := time.Now().UTC()
	q := legacyQueueDB(t, func(t *testing.T, db *sql.DB) {
		past := now.Add(-time.Hour).Format(legacyTimeLayout)
		if _, err := db.Exec(`INSERT INTO "queue_jobs"
			(id, occurrence_id, type, payload, priority, lane, attempts, max_attempts,
			 created_at, scheduled_at, status)
			VALUES ('j2','', 'work','null',0,'',0,3,?,?,'pending')`,
			past, now.Add(time.Hour).Format(legacyTimeLayout)); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := q.Dequeue(context.Background(), "work"); !errors.Is(err, ErrNoJob) {
		t.Fatalf("job scheduled an hour ahead (legacy format) dequeued now (err=%v)", err)
	}
}

// An expired legacy-format lease is still reclaimed — guards against
// "fixing" the comparison by ignoring legacy rows.
func TestLegacyExpiredLeaseReclaimed(t *testing.T) {
	now := time.Now().UTC()
	q := legacyQueueDB(t, func(t *testing.T, db *sql.DB) {
		past := now.Add(-2 * time.Hour).Format(legacyTimeLayout)
		if _, err := db.Exec(`INSERT INTO "queue_jobs"
			(id, occurrence_id, type, payload, priority, lane, attempts, max_attempts,
			 created_at, scheduled_at, status, claimed_at)
			VALUES ('j3','', 'work','null',0,'',1,3,?,?,'claimed',?)`,
			past, past, now.Add(-time.Hour).Format(legacyTimeLayout)); err != nil {
			t.Fatal(err)
		}
	})
	job, err := q.Dequeue(context.Background(), "work")
	if err != nil {
		t.Fatalf("expired legacy lease not reclaimed: %v", err)
	}
	if job.ID != "j3" {
		t.Fatalf("reclaimed job = %q, want j3", job.ID)
	}
}
